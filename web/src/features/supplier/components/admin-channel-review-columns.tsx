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

import { Button } from '@/components/design-system/button'
import { LongText } from '@/components/long-text'
import { CHANNEL_TYPES } from '@/features/channels/constants'

import type { SupplierChannel } from '../types'

export function useAdminChannelReviewColumns(handlers: {
  onApprove: (channel: SupplierChannel) => void
  onReject: (channel: SupplierChannel) => void
}): ColumnDef<SupplierChannel>[] {
  const { t } = useTranslation()
  return [
    {
      accessorKey: 'name',
      header: t('Name'),
      cell: ({ row }) => (
        <LongText className='max-w-[200px] font-medium'>
          {row.original.name}
        </LongText>
      ),
      enableSorting: false,
      enableHiding: false,
      size: 200,
      meta: { cardRole: 'title', cardSpan: 2, contentMode: 'wrap' },
    },
    {
      accessorKey: 'type',
      header: t('Type'),
      cell: ({ row }) =>
        CHANNEL_TYPES[row.original.type as keyof typeof CHANNEL_TYPES] ||
        `#${row.original.type}`,
      enableSorting: false,
      size: 140,
      meta: { cardRole: 'primary', cardOrder: 10, contentMode: 'full' },
    },
    {
      accessorKey: 'models',
      header: t('Models'),
      cell: ({ row }) => (
        <LongText className='text-muted-foreground max-w-[260px]'>
          {row.original.models || '-'}
        </LongText>
      ),
      enableSorting: false,
      size: 280,
      meta: { cardRole: 'secondary', cardOrder: 30, contentMode: 'wrap' },
    },
    {
      id: 'ratio',
      accessorKey: 'channel_ratio',
      header: t('Quote Rate'),
      cell: ({ row }) => (
        <span className='tabular-nums'>
          {row.original.channel_ratio == null
            ? '-'
            : row.original.channel_ratio.toFixed(2)}
        </span>
      ),
      enableSorting: false,
      size: 120,
      meta: { cardRole: 'primary', cardOrder: 20, contentMode: 'full' },
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => (
        <div className='-ml-1.5 flex items-center justify-start gap-1'>
          <Button size='sm' onClick={() => handlers.onApprove(row.original)}>
            {t('Approve')}
          </Button>
          <Button
            variant='ghost'
            size='sm'
            className='text-destructive'
            onClick={() => handlers.onReject(row.original)}
          >
            {t('Reject')}
          </Button>
        </div>
      ),
      meta: { pinned: 'right' as const },
    },
  ]
}
