/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useQueryClient } from '@tanstack/react-query'
import { Loader2, PencilLine, RefreshCw, DollarSign } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { Dialog } from '@/components/dialog'
import { Label } from '@/components/ui/label'
import { formatCurrencyFromUSD } from '@/lib/currency'
import { formatTimestampToDate } from '@/lib/format'

import {
  getCodexUsage,
  setChannelBalance,
  updateChannelBalance,
} from '../../api'
import { channelsQueryKeys } from '../../lib'
import { useChannels } from '../channels-provider'
import {
  CodexUsageDialog,
  type CodexUsageDialogData,
} from './codex-usage-dialog'

type BalanceQueryDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function BalanceQueryDialog({
  open,
  onOpenChange,
}: BalanceQueryDialogProps) {
  const { t } = useTranslation()
  const { currentRow, setCurrentRow } = useChannels()
  const queryClient = useQueryClient()
  const [isQuerying, setIsQuerying] = useState(false)
  const [balance, setBalance] = useState<number | null>(null)
  const [balanceUpdatedTime, setBalanceUpdatedTime] = useState<number | null>(
    null
  )
  // 蓝图A 手动设余额：上游无余额查询接口时（多数中转站）由管理员充值后录入
  const [manualBalance, setManualBalance] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [codexUsageResponse, setCodexUsageResponse] =
    useState<CodexUsageDialogData | null>(null)

  const isCodex = currentRow?.type === 57

  const handleQueryCodexUsage = async () => {
    const row = currentRow
    if (!row) return
    setIsQuerying(true)
    try {
      const res = await getCodexUsage(row.id)
      if (!res.success) {
        throw new Error(res.message || t('Failed to fetch usage'))
      }
      setCodexUsageResponse(res)
    } catch (error: unknown) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to fetch usage')
      )
    } finally {
      setIsQuerying(false)
    }
  }

  useEffect(() => {
    if (!isCodex) return
    if (!open) return
    handleQueryCodexUsage()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, isCodex])

  if (!currentRow) return null

  const handleQueryBalance = async () => {
    setIsQuerying(true)
    try {
      const response = await updateChannelBalance(currentRow.id)
      if (response.success && response.balance !== undefined) {
        const newBalance = response.balance
        const now = Math.floor(Date.now() / 1000)

        setBalance(newBalance)
        setBalanceUpdatedTime(now)
        toast.success(t('Balance updated successfully'))

        // Update currentRow immediately with new balance and timestamp
        setCurrentRow({
          ...currentRow,
          balance: newBalance,
          balance_updated_time: now,
        })

        // Invalidate queries to refresh the table
        await queryClient.invalidateQueries({
          queryKey: channelsQueryKeys.lists(),
        })
      } else {
        toast.error(response.message || t('Failed to query balance'))
      }
    } catch (error: unknown) {
      toast.error(
        error instanceof Error ? error.message : t('Failed to query balance')
      )
    } finally {
      setIsQuerying(false)
    }
  }

  const handleSetBalance = async () => {
    const value = Number.parseFloat(manualBalance)
    if (Number.isNaN(value) || value < 0) {
      toast.error(t('Please enter a valid amount'))
      return
    }
    setIsSaving(true)
    try {
      const res = await setChannelBalance(currentRow.id, value)
      if (!res.success) {
        throw new Error(res.message || t('Failed'))
      }
      const now = Math.floor(Date.now() / 1000)
      setBalance(value)
      setBalanceUpdatedTime(now)
      setManualBalance('')
      toast.success(t('Balance updated successfully'))
      setCurrentRow({
        ...currentRow,
        balance: value,
        balance_updated_time: now,
        balance_snapshot: currentRow.used_quota,
      })
      await queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.lists(),
      })
    } catch (error: unknown) {
      toast.error(error instanceof Error ? error.message : t('Failed'))
    } finally {
      setIsSaving(false)
    }
  }

  const handleClose = () => {
    setBalance(null)
    setBalanceUpdatedTime(null)
    setCodexUsageResponse(null)
    setManualBalance('')
    onOpenChange(false)
  }

  const formatBalance = (bal: number) =>
    formatCurrencyFromUSD(bal, {
      digitsLarge: 2,
      digitsSmall: 4,
      abbreviate: false,
    })

  const formatDate = (timestamp: number) => {
    if (!timestamp) return 'Never'
    return formatTimestampToDate(timestamp)
  }

  if (isCodex) {
    return (
      <CodexUsageDialog
        open={open}
        onOpenChange={(v) => {
          if (!v) handleClose()
        }}
        channelName={currentRow.name}
        channelId={currentRow.id}
        response={codexUsageResponse}
        onRefresh={handleQueryCodexUsage}
        isRefreshing={isQuerying}
      />
    )
  }

  return (
    <Dialog
      open={open}
      onOpenChange={handleClose}
      title={t('Query Balance')}
      description={
        <>
          {t('Update balance for:')}
          <strong>{currentRow.name}</strong>
        </>
      }
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={handleClose} disabled={isQuerying}>
            {t('Close')}
          </Button>
        </>
      }
    >
      <div className='space-y-4 py-4'>
        {/* Current Balance Display */}
        <div className='bg-muted/50 rounded-lg border p-4'>
          <div className='text-muted-foreground mb-2 flex items-center gap-2 text-sm'>
            <DollarSign className='h-4 w-4' />
            <span>{t('Current Balance')}</span>
          </div>
          <div className='text-2xl font-bold'>
            {balance !== null
              ? formatBalance(balance)
              : formatBalance(currentRow.balance)}
          </div>
          <div className='text-muted-foreground mt-2 text-xs'>
            {t('Last updated:')}{' '}
            {formatDate(balanceUpdatedTime ?? currentRow.balance_updated_time)}
          </div>
        </div>

        {/* Balance Update Button */}
        <Button
          className='w-full'
          onClick={handleQueryBalance}
          disabled={isQuerying || isSaving}
        >
          {isQuerying && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
          {!isQuerying && <RefreshCw className='mr-2 h-4 w-4' />}
          {isQuerying ? t('Querying...') : t('Update Balance')}
        </Button>

        {/* 蓝图A 手动设余额：上游没有余额查询接口时的兜底，保存后照常本地推算剩余 */}
        <div className='space-y-2 rounded-lg border p-4'>
          <Label htmlFor='manual-balance'>
            {t('Set balance manually (USD)')}
          </Label>
          <p className='text-muted-foreground text-xs'>
            {t(
              'For upstreams without a balance API: enter the balance after topping up, consumption keeps deducting locally.'
            )}
          </p>
          <div className='flex gap-2'>
            <Input
              id='manual-balance'
              value={manualBalance}
              onChange={(e) => setManualBalance(e.target.value)}
              inputMode='decimal'
              placeholder='0.00'
              disabled={isSaving}
            />
            <Button
              variant='outline'
              onClick={handleSetBalance}
              disabled={isSaving || !manualBalance.trim()}
            >
              {isSaving ? (
                <Loader2 className='h-4 w-4 animate-spin' />
              ) : (
                <PencilLine className='h-4 w-4' />
              )}
              {t('Save')}
            </Button>
          </div>
        </div>
      </div>
    </Dialog>
  )
}
