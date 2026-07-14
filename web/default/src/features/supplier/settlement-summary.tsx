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
import { Clock, HandCoins, TrendingUp, Wallet, type LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { SupplierSettlement } from './types'

const SKELETON_KEYS = ['pending', 'matured', 'paid', 'payable']

// 供应商结算汇总卡片：待成熟 / 已成熟 / 已打款 / 应付。
// 待成熟按 gross - matured 推算；应付直接取后端下发的 payable_quota。
export function SettlementSummary({
  settlement,
  loading,
}: {
  settlement?: SupplierSettlement | null
  loading?: boolean
}) {
  const { t } = useTranslation()

  if (loading) {
    return (
      <div className='overflow-hidden rounded-lg border'>
        <div className='divide-border/60 grid grid-cols-2 divide-x divide-y sm:grid-cols-4 sm:divide-y-0'>
          {SKELETON_KEYS.map((key) => (
            <div key={key} className='px-2.5 py-3 sm:px-5 sm:py-4'>
              <Skeleton className='h-3.5 w-16' />
              <Skeleton className='mt-2 h-6 w-20' />
            </div>
          ))}
        </div>
      </div>
    )
  }

  const gross = settlement?.gross_quota || 0
  const matured = settlement?.matured_quota || 0
  const paid = settlement?.paid_quota || 0
  const payable = settlement?.payable_quota || 0
  const pending = Math.max(0, gross - matured)

  return (
    <div className='overflow-hidden rounded-lg border'>
      <div className='divide-border/60 grid grid-cols-2 divide-x divide-y sm:grid-cols-4 sm:divide-y-0'>
        <SettlementCell
          icon={Clock}
          label={t('Pending')}
          value={formatQuota(pending)}
        />
        <SettlementCell
          icon={TrendingUp}
          label={t('Matured')}
          value={formatQuota(matured)}
        />
        <SettlementCell
          icon={HandCoins}
          label={t('Paid')}
          value={formatQuota(paid)}
        />
        <SettlementCell
          icon={Wallet}
          label={t('Payable')}
          value={formatQuota(payable)}
          emphasize
        />
      </div>
    </div>
  )
}

function SettlementCell({
  icon: Icon,
  label,
  value,
  emphasize,
}: {
  icon: LucideIcon
  label: string
  value: string
  emphasize?: boolean
}) {
  return (
    <div className='px-2.5 py-3 sm:px-5 sm:py-4'>
      <div className='flex items-start gap-1.5'>
        <Icon className='text-muted-foreground/60 mt-0.5 size-3.5 shrink-0' />
        <div className='text-muted-foreground line-clamp-2 text-[11px] leading-snug font-medium tracking-wider uppercase sm:text-xs'>
          {label}
        </div>
      </div>
      <div
        className={cn(
          'mt-1.5 truncate text-sm font-semibold tabular-nums sm:mt-2 sm:text-2xl',
          emphasize ? 'text-primary' : 'text-foreground'
        )}
      >
        {value}
      </div>
    </div>
  )
}
