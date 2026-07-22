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
import { Pencil } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { BadgeCell } from '@/components/data-table'
import { Button } from '@/components/design-system/button'
import { LongText } from '@/components/long-text'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { CHANNEL_TYPES } from '@/features/channels/constants'

import { ChannelAuditBadge } from '../channel-audit-badge'
import type { SupplierChannel } from '../types'

export function useMyChannelsColumns(
  onEdit: (channel: SupplierChannel) => void
): ColumnDef<SupplierChannel>[] {
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
        <LongText className='text-muted-foreground max-w-[240px]'>
          {row.original.models || '-'}
        </LongText>
      ),
      enableSorting: false,
      size: 260,
      meta: { cardRole: 'secondary', cardOrder: 40, contentMode: 'wrap' },
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
      id: 'audit',
      accessorKey: 'audit_status',
      header: t('Review Status'),
      cell: ({ row }) => (
        <BadgeCell>
          <ChannelAuditBadge status={row.original.audit_status} />
        </BadgeCell>
      ),
      enableSorting: false,
      size: 130,
      meta: { cardRole: 'badge', contentMode: 'wrap' },
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => (
        <div className='-ml-1.5 flex items-center justify-start gap-1'>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='ghost'
                  size='icon-sm'
                  onClick={() => onEdit(row.original)}
                  aria-label={t('Edit')}
                />
              }
            >
              <Pencil />
            </TooltipTrigger>
            <TooltipContent>{t('Edit')}</TooltipContent>
          </Tooltip>
        </div>
      ),
      meta: { pinned: 'right' as const },
    },
  ]
}
