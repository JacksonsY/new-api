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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'

import { BadgeCell } from '@/components/data-table'
import { Button } from '@/components/design-system/button'
import { LongText } from '@/components/long-text'
import { StatusBadge } from '@/components/status-badge'
import { formatQuota } from '@/lib/format'

import {
  payoutMethodLabelKey,
  SUPPLIER_STATUS,
  type SupplierUser,
} from '../types'

export type SupplierReviewAction = {
  user: SupplierUser
  status: number
  labelKey: string
}

const SUPPLIER_STATUS_BADGE: Record<
  number,
  { variant: 'success' | 'destructive' | 'warning' | 'neutral'; labelKey: string }
> = {
  [SUPPLIER_STATUS.APPROVED]: { variant: 'success', labelKey: 'Approved' },
  [SUPPLIER_STATUS.SUSPENDED]: { variant: 'destructive', labelKey: 'Suspended' },
  [SUPPLIER_STATUS.PENDING]: { variant: 'warning', labelKey: 'Pending' },
}

export function useAdminSuppliersColumns(
  onReview: (action: SupplierReviewAction) => void
): ColumnDef<SupplierUser>[] {
  const { t } = useTranslation()
  return [
    {
      accessorKey: 'username',
      header: t('Username'),
      cell: ({ row }) => {
        const u = row.original
        return (
          <div className='flex min-w-0 flex-col gap-0.5 sm:min-w-[160px]'>
            <div className='flex items-center gap-1.5'>
              <LongText className='max-w-full font-medium sm:max-w-[160px]'>
                {u.username}
              </LongText>
              <span className='text-muted-foreground text-xs'>#{u.id}</span>
            </div>
            {u.email && (
              <LongText className='text-muted-foreground max-w-full text-xs sm:max-w-[200px]'>
                {u.email}
              </LongText>
            )}
          </div>
        )
      },
      enableSorting: false,
      enableHiding: false,
      size: 240,
      meta: { cardRole: 'title', cardSpan: 2, contentMode: 'wrap' },
    },
    {
      id: 'status',
      accessorKey: 'supplier_status',
      header: t('Status'),
      cell: ({ row }) => {
        const badge = SUPPLIER_STATUS_BADGE[row.original.supplier_status] ?? {
          variant: 'neutral' as const,
          labelKey: 'None',
        }
        return (
          <BadgeCell>
            <StatusBadge variant={badge.variant}>{t(badge.labelKey)}</StatusBadge>
          </BadgeCell>
        )
      },
      enableSorting: false,
      size: 130,
      meta: { cardRole: 'badge', contentMode: 'wrap' },
    },
    {
      id: 'payable',
      accessorKey: 'supplier_payable_quota',
      header: t('Payable'),
      cell: ({ row }) => (
        <span className='tabular-nums'>
          {formatQuota(row.original.supplier_payable_quota || 0)}
        </span>
      ),
      enableSorting: false,
      size: 140,
      meta: { cardRole: 'primary', cardOrder: 10, contentMode: 'full' },
    },
    {
      id: 'payout',
      header: t('Payout / Contact'),
      cell: ({ row }) => {
        const u = row.original
        if (!u.supplier_payout_account && !u.supplier_contact) {
          return <span className='text-muted-foreground text-xs'>-</span>
        }
        const holderContact = [u.supplier_payout_name, u.supplier_contact]
          .filter(Boolean)
          .join(' · ')
        return (
          <div className='flex min-w-0 flex-col gap-0.5 sm:min-w-[180px]'>
            <div className='flex items-center gap-1.5'>
              <span className='text-muted-foreground shrink-0 text-xs'>
                {t(payoutMethodLabelKey(u.supplier_payout_method))}
              </span>
              <LongText className='max-w-full font-medium sm:max-w-[180px]'>
                {u.supplier_payout_account || '-'}
              </LongText>
            </div>
            {holderContact && (
              <LongText className='text-muted-foreground max-w-full text-xs sm:max-w-[200px]'>
                {holderContact}
              </LongText>
            )}
          </div>
        )
      },
      enableSorting: false,
      size: 220,
      meta: { cardRole: 'primary', cardOrder: 20, contentMode: 'wrap' },
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => {
        const u = row.original
        return (
          <div className='-ml-1.5 flex items-center justify-start gap-1'>
            {u.supplier_status === SUPPLIER_STATUS.PENDING && (
              <>
                <Button
                  size='sm'
                  onClick={() =>
                    onReview({
                      user: u,
                      status: SUPPLIER_STATUS.APPROVED,
                      labelKey: 'Approve',
                    })
                  }
                >
                  {t('Approve')}
                </Button>
                <Button
                  variant='ghost'
                  size='sm'
                  className='text-destructive'
                  onClick={() =>
                    onReview({
                      user: u,
                      status: SUPPLIER_STATUS.SUSPENDED,
                      labelKey: 'Reject',
                    })
                  }
                >
                  {t('Reject')}
                </Button>
              </>
            )}
            {u.supplier_status === SUPPLIER_STATUS.APPROVED && (
              <Button
                variant='ghost'
                size='sm'
                className='text-destructive'
                onClick={() =>
                  onReview({
                    user: u,
                    status: SUPPLIER_STATUS.SUSPENDED,
                    labelKey: 'Suspend',
                  })
                }
              >
                {t('Suspend')}
              </Button>
            )}
            {u.supplier_status === SUPPLIER_STATUS.SUSPENDED && (
              <Button
                variant='outline'
                size='sm'
                onClick={() =>
                  onReview({
                    user: u,
                    status: SUPPLIER_STATUS.APPROVED,
                    labelKey: 'Restore',
                  })
                }
              >
                {t('Restore')}
              </Button>
            )}
          </div>
        )
      },
      meta: { pinned: 'right' as const },
    },
  ]
}
