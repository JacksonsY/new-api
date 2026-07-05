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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars } from '@/lib/format'
import { cn } from '@/lib/utils'

import { batchAdjustUserQuota, batchUpdateUserGroup, getGroups } from '../api'
import type { BatchQuotaMode, BatchUserOpResult } from '../types'

const SKIP_REASON_KEYS: Record<string, string> = {
  not_found: 'User not found',
  no_permission: 'No permission for this user',
  insufficient_quota: 'Insufficient quota',
  conflict: 'Concurrent update conflict, try again',
  no_change: 'No change needed',
}

function useBatchResultToast() {
  const { t } = useTranslation()
  return (result: BatchUserOpResult) => {
    toast.success(
      t('{{updated}} users updated, {{skipped}} skipped', {
        updated: result.updated_count,
        skipped: result.skipped.length,
      })
    )
    if (result.skipped.length > 0) {
      const reasonCounts = new Map<string, number>()
      for (const skip of result.skipped) {
        reasonCounts.set(skip.reason, (reasonCounts.get(skip.reason) ?? 0) + 1)
      }
      const detail = [...reasonCounts.entries()]
        .map(
          ([reason, count]) =>
            `${t(SKIP_REASON_KEYS[reason] ?? reason)} × ${count}`
        )
        .join('，')
      toast.info(detail)
    }
  }
}

interface BatchDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  userIds: number[]
  onSuccess: () => void
}

export function UsersBatchGroupDialog(props: BatchDialogProps) {
  const { t } = useTranslation()
  const [group, setGroup] = useState('')
  const [loading, setLoading] = useState(false)
  const showResult = useBatchResultToast()

  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
    staleTime: 5 * 60 * 1000,
    enabled: props.open,
  })
  const groups = groupsData?.data || []

  const handleConfirm = async () => {
    if (loading || !group) return
    setLoading(true)
    try {
      const result = await batchUpdateUserGroup(props.userIds, group)
      if (result.success && result.data) {
        showResult(result.data)
        setGroup('')
        props.onOpenChange(false)
        props.onSuccess()
      } else {
        toast.error(result.message || t('Operation failed'))
      }
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : t('Operation failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Batch Change Group')}
      description={t('Move {{count}} selected users to a target group', {
        count: props.userIds.length,
      })}
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleConfirm} disabled={loading || !group}>
            {loading ? t('Processing...') : t('Confirm')}
          </Button>
        </>
      }
    >
      <div className='space-y-2'>
        <Label>{t('Target group')}</Label>
        <Select value={group} onValueChange={(v) => v !== null && setGroup(v)}>
          <SelectTrigger className='w-full'>
            <SelectValue placeholder={t('Select a group')} />
          </SelectTrigger>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              {groups.map((name) => (
                <SelectItem key={name} value={name}>
                  {name}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>
    </Dialog>
  )
}

export function UsersBatchQuotaDialog(props: BatchDialogProps) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<BatchQuotaMode>('add')
  const [amount, setAmount] = useState('')
  const [loading, setLoading] = useState(false)
  const showResult = useBatchResultToast()
  const currencyLabel = getCurrencyLabel()

  const amountValue = Number.parseFloat(amount) || 0

  const handleConfirm = async () => {
    if (loading || !amount.trim()) return
    if (mode === 'multiply') {
      if (amountValue <= 0 || amountValue > 100) return
    } else if (amountValue <= 0 && mode !== 'override') {
      return
    }

    setLoading(true)
    try {
      const payload =
        mode === 'multiply'
          ? { user_ids: props.userIds, mode, factor: amountValue }
          : {
              user_ids: props.userIds,
              mode,
              amount: parseQuotaFromDollars(Math.abs(amountValue)),
            }
      const result = await batchAdjustUserQuota(payload)
      if (result.success && result.data) {
        showResult(result.data)
        setAmount('')
        setMode('add')
        props.onOpenChange(false)
        props.onSuccess()
      } else {
        toast.error(result.message || t('Operation failed'))
      }
    } catch (e: unknown) {
      toast.error(e instanceof Error ? e.message : t('Operation failed'))
    } finally {
      setLoading(false)
    }
  }

  const modeLabels: Record<BatchQuotaMode, string> = {
    add: t('Add'),
    subtract: t('Subtract'),
    override: t('Override'),
    multiply: t('Multiply'),
  }

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Batch Adjust Quota')}
      description={t('Adjust quota for {{count}} selected users', {
        count: props.userIds.length,
      })}
      contentHeight='auto'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => props.onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={handleConfirm} disabled={loading}>
            {loading ? t('Processing...') : t('Confirm')}
          </Button>
        </>
      }
    >
      <div className='space-y-4'>
        <div className='space-y-2'>
          <Label>{t('Mode')}</Label>
          <div className='flex gap-1'>
            {(['add', 'subtract', 'override', 'multiply'] as const).map(
              (m) => (
                <Button
                  key={m}
                  type='button'
                  variant='outline'
                  size='sm'
                  className={cn(
                    mode === m &&
                      'bg-primary text-primary-foreground hover:bg-primary/90 hover:text-primary-foreground'
                  )}
                  onClick={() => {
                    setMode(m)
                    setAmount('')
                  }}
                >
                  {modeLabels[m]}
                </Button>
              )
            )}
          </div>
        </div>

        <div className='space-y-2'>
          <Label>
            {mode === 'multiply'
              ? t('Factor')
              : `${t('Amount')} (${currencyLabel})`}
          </Label>
          <Input
            type='number'
            step={mode === 'multiply' ? 0.1 : 0.000001}
            min={0}
            placeholder={
              mode === 'multiply'
                ? t('Enter multiply factor (e.g. 1.5)')
                : t('Enter amount in {{currency}}', {
                    currency: currencyLabel,
                  })
            }
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleConfirm()
            }}
          />
          {mode === 'subtract' && (
            <p className='text-muted-foreground text-xs'>
              {t('Users with insufficient quota are skipped, not overdrawn.')}
            </p>
          )}
        </div>
      </div>
    </Dialog>
  )
}
