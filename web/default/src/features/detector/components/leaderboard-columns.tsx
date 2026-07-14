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
import { StatusBadge } from '@/components/status-badge'
import { formatTimestamp } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { LeaderboardEntry } from '../types'

function scoreToneClass(score: number): string {
  if (score >= 80) return 'text-success'
  if (score >= 50) return 'text-warning'
  return 'text-destructive'
}

export function useLeaderboardColumns(): ColumnDef<LeaderboardEntry>[] {
  const { t } = useTranslation()
  return [
    {
      accessorKey: 'domain',
      header: t('Domain'),
      cell: ({ row }) => (
        <span className='font-mono text-xs sm:text-sm'>
          {row.original.domain}
        </span>
      ),
      enableSorting: false,
      size: 240,
      meta: { cardRole: 'title', cardSpan: 2, contentMode: 'wrap' },
    },
    {
      accessorKey: 'avg_score',
      header: t('Average Score'),
      cell: ({ row }) => (
        <span
          className={cn(
            'font-semibold tabular-nums',
            scoreToneClass(row.original.avg_score)
          )}
        >
          {Math.round(row.original.avg_score)}
        </span>
      ),
      enableSorting: false,
      size: 130,
      meta: { cardRole: 'primary', cardOrder: 10, contentMode: 'full' },
    },
    {
      accessorKey: 'min_score',
      header: t('Min Score'),
      cell: ({ row }) => (
        <span className='text-muted-foreground tabular-nums'>
          {Math.round(row.original.min_score)}
        </span>
      ),
      enableSorting: false,
      size: 110,
      meta: { cardRole: 'primary', cardOrder: 20, contentMode: 'full' },
    },
    {
      accessorKey: 'samples',
      header: t('Samples'),
      cell: ({ row }) => (
        <span className='tabular-nums'>{row.original.samples}</span>
      ),
      enableSorting: false,
      size: 100,
      meta: { cardRole: 'primary', cardOrder: 30, contentMode: 'full' },
    },
    {
      id: 'critical',
      accessorKey: 'critical_count',
      header: t('Critical Issue'),
      cell: ({ row }) =>
        row.original.critical_count > 0 ? (
          <BadgeCell>
            <StatusBadge variant='destructive'>
              {row.original.critical_count}
            </StatusBadge>
          </BadgeCell>
        ) : (
          <span className='text-muted-foreground'>0</span>
        ),
      enableSorting: false,
      size: 130,
      meta: { cardRole: 'badge', contentMode: 'wrap' },
    },
    {
      accessorKey: 'last_checked_at',
      header: t('Last Checked'),
      cell: ({ row }) => (
        <span className='text-muted-foreground text-xs'>
          {row.original.last_checked_at
            ? formatTimestamp(row.original.last_checked_at)
            : '-'}
        </span>
      ),
      enableSorting: false,
      size: 180,
      meta: { cardRole: 'secondary', cardOrder: 40, contentMode: 'full' },
    },
  ]
}
