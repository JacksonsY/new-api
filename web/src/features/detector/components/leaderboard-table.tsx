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
import { getRouteApi } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import { getLeaderboard } from '../api'
import { useLeaderboardColumns } from './leaderboard-columns'

const route = getRouteApi('/_authenticated/detection/leaderboard/')

export function LeaderboardTable() {
  const { t } = useTranslation()
  const columns = useLeaderboardColumns()

  const { pagination, onPaginationChange, ensurePageInRange } = useTableUrlState(
    {
      search: route.useSearch(),
      navigate: route.useNavigate(),
      pagination: {
        defaultPage: 1,
        defaultPageSize: 20,
        pageSizeStorageKey: 'detector-leaderboard:page-size:v1',
      },
      globalFilter: { enabled: false },
    }
  )

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'detector-leaderboard',
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const res = await getLeaderboard(
        pagination.pageIndex + 1,
        pagination.pageSize
      )
      if (!res.success) {
        toast.error(res.message || t('Failed to load'))
        return { items: [] }
      }
      return { items: res.data?.items || [] }
    },
    placeholderData: (previousData) => previousData,
  })

  const items = data?.items || []
  // The leaderboard endpoint does not return a total count; derive a running
  // lower bound so native pagination can enable "next" only while a full page
  // came back (more rows likely exist upstream).
  const hasMore = items.length >= pagination.pageSize
  const totalCount =
    pagination.pageIndex * pagination.pageSize +
    items.length +
    (hasMore ? 1 : 0)

  const { table } = useDataTable({
    data: items,
    columns,
    pagination,
    onPaginationChange,
    manualPagination: true,
    manualSorting: true,
    totalCount,
    ensurePageInRange,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      tableLabel={t('Relay Leaderboard')}
      isLoading={isLoading}
      isFetching={isFetching}
      emptyTitle={t('No records yet')}
      emptyDescription={t(
        'Upstream domains ranked by detection score, worst first. Lower scores suggest a higher chance of relayed or substituted models.'
      )}
      skeletonKeyPrefix='detector-leaderboard-skeleton'
      applyHeaderSize
      toolbar={
        <p className='text-muted-foreground text-sm'>
          {t(
            'Upstream domains ranked by detection score, worst first. Lower scores suggest a higher chance of relayed or substituted models.'
          )}
        </p>
      }
    />
  )
}
