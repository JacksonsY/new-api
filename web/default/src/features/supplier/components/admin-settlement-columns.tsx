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
import { ReceiptText } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/design-system/button'
import { LongText } from '@/components/long-text'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { formatQuota } from '@/lib/format'

import type { SupplierUser } from '../types'

export type SettlementPayoutMode = 'pay' | 'confiscate'

export function useAdminSettlementColumns(handlers: {
  onPayout: (target: { user: SupplierUser; mode: SettlementPayoutMode }) => void
  onViewLedger: (user: SupplierUser) => void
}): ColumnDef<SupplierUser>[] {
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
      id: 'payable',
      accessorKey: 'supplier_payable_quota',
      header: t('Payable'),
      cell: ({ row }) => (
        <span className='text-primary font-semibold tabular-nums'>
          {formatQuota(row.original.supplier_payable_quota || 0)}
        </span>
      ),
      enableSorting: false,
      size: 160,
      meta: { cardRole: 'primary', cardOrder: 10, contentMode: 'full' },
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => {
        const u = row.original
        return (
          <div className='-ml-1.5 flex items-center justify-start gap-1'>
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant='ghost'
                    size='icon-sm'
                    onClick={() => handlers.onViewLedger(u)}
                    aria-label={t('Settlement Ledger')}
                  />
                }
              >
                <ReceiptText />
              </TooltipTrigger>
              <TooltipContent>{t('Settlement Ledger')}</TooltipContent>
            </Tooltip>
            <Button
              size='sm'
              disabled={!u.supplier_payable_quota}
              onClick={() => handlers.onPayout({ user: u, mode: 'pay' })}
            >
              {t('Mark as Paid')}
            </Button>
            <Button
              variant='ghost'
              size='sm'
              className='text-destructive'
              onClick={() => handlers.onPayout({ user: u, mode: 'confiscate' })}
            >
              {t('Confiscate')}
            </Button>
          </div>
        )
      },
      meta: { pinned: 'right' as const },
    },
  ]
}
