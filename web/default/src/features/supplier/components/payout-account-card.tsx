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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'

import { getSupplierProfile, updateSupplierPayoutInfo } from '../api'
import {
  EMPTY_PAYOUT_INFO,
  payoutMethodLabelKey,
  type SupplierPayoutInfo,
  type SupplierProfile,
  validatePayoutInfo,
} from '../types'
import { PayoutInfoFields } from './payout-info-fields'

function toPayoutInfo(p: SupplierProfile | null): SupplierPayoutInfo {
  if (!p) return EMPTY_PAYOUT_INFO
  return {
    method: p.supplier_payout_method || 'alipay',
    account: p.supplier_payout_account || '',
    name: p.supplier_payout_name || '',
    contact: p.supplier_contact || '',
  }
}

// 「收款账户」卡片：展示当前收款/联系方式，可编辑（人工打款依据）。
export function PayoutAccountCard() {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [payout, setPayout] = useState<SupplierPayoutInfo>(EMPTY_PAYOUT_INFO)

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['supplier-profile'],
    queryFn: async () => {
      const res = await getSupplierProfile()
      if (!res.success) {
        toast.error(res.message || t('Failed to load'))
        return null
      }
      return res.data ?? null
    },
  })

  useEffect(() => {
    if (data) setPayout(toPayoutInfo(data))
  }, [data])

  const hasInfo = !!data?.supplier_payout_account

  async function onSave() {
    const err = validatePayoutInfo(payout)
    if (err) {
      toast.error(t(err))
      return
    }
    setSaving(true)
    try {
      const res = await updateSupplierPayoutInfo(payout)
      if (res.success) {
        toast.success(t('Saved'))
        setEditing(false)
        refetch()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setSaving(false)
    }
  }

  const rows: { label: string; value: string }[] = [
    {
      label: t('Payout Method'),
      value: t(payoutMethodLabelKey(data?.supplier_payout_method)),
    },
    { label: t('Payout Account'), value: data?.supplier_payout_account || '-' },
    {
      label: t('Account Holder Name'),
      value: data?.supplier_payout_name || '-',
    },
    { label: t('Contact'), value: data?.supplier_contact || '-' },
  ]

  let body: React.ReactNode
  if (isLoading) {
    body = (
      <div className='space-y-2'>
        <Skeleton className='h-4 w-48' />
        <Skeleton className='h-4 w-64' />
      </div>
    )
  } else if (editing) {
    body = (
      <div className='space-y-4'>
        <PayoutInfoFields
          value={payout}
          onChange={setPayout}
          idPrefix='earn-payout'
        />
        <div className='flex items-center gap-2'>
          <Button onClick={onSave} disabled={saving}>
            {saving ? t('Saving...') : t('Save changes')}
          </Button>
          <Button
            variant='outline'
            onClick={() => {
              setPayout(toPayoutInfo(data ?? null))
              setEditing(false)
            }}
            disabled={saving}
          >
            {t('Cancel')}
          </Button>
        </div>
      </div>
    )
  } else if (hasInfo) {
    body = (
      <dl className='grid gap-x-6 gap-y-2.5 text-sm sm:grid-cols-2'>
        {rows.map((r) => (
          <div key={r.label} className='flex justify-between gap-3'>
            <dt className='text-muted-foreground'>{r.label}</dt>
            <dd className='text-right font-medium break-all'>{r.value}</dd>
          </div>
        ))}
      </dl>
    )
  } else {
    body = (
      <p className='text-muted-foreground text-sm leading-relaxed'>
        {t(
          'No payout account set yet. Add one so the administrator can pay your earnings.'
        )}
      </p>
    )
  }

  return (
    <Card data-card-hover='false'>
      <CardHeader className='flex flex-row items-center justify-between space-y-0'>
        <CardTitle className='text-base'>{t('Payout Account')}</CardTitle>
        {!editing && !isLoading && (
          <Button
            variant='outline'
            size='sm'
            onClick={() => {
              setPayout(toPayoutInfo(data ?? null))
              setEditing(true)
            }}
          >
            {hasInfo ? t('Edit') : t('Set up')}
          </Button>
        )}
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  )
}
