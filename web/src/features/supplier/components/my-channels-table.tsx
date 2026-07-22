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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import { listSupplierChannels } from '../api'
import { CHANNEL_AUDIT_STATUS, type SupplierChannel } from '../types'
import { useMyChannelsColumns } from './my-channels-columns'
import { ChannelFormDrawer } from './my-channels-form-drawer'

const route = getRouteApi('/_authenticated/supplier/channels/')

export const SUPPLIER_CHANNELS_QUERY_KEY = 'supplier-channels'

export function MyChannelsTable() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [editTarget, setEditTarget] = useState<SupplierChannel | null>(null)

  const columns = useMyChannelsColumns(setEditTarget)

  const { pagination, onPaginationChange, ensurePageInRange } = useTableUrlState(
    {
      search: route.useSearch(),
      navigate: route.useNavigate(),
      pagination: {
        defaultPage: 1,
        defaultPageSize: 20,
        pageSizeStorageKey: 'supplier-my-channels:page-size:v1',
      },
      globalFilter: { enabled: false },
    }
  )

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      SUPPLIER_CHANNELS_QUERY_KEY,
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const res = await listSupplierChannels(
        pagination.pageIndex + 1,
        pagination.pageSize
      )
      if (!res.success || !res.data) {
        toast.error(res.message || t('Failed to load'))
        return { items: [], total: 0 }
      }
      return { items: res.data.items || [], total: res.data.total || 0 }
    },
    placeholderData: (previousData) => previousData,
  })

  const { table } = useDataTable({
    data: data?.items || [],
    columns,
    pagination,
    onPaginationChange,
    manualPagination: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  const offlineCount = (data?.items || []).filter(
    (ch: { audit_status: number }) =>
      ch.audit_status !== CHANNEL_AUDIT_STATUS.APPROVED
  ).length

  return (
    <>
      {/* 渠道回 pending/rejected 期间不参与调度、收益暂停——不能让供应商靠猜 */}
      {offlineCount > 0 && (
        <Alert className='mb-4'>
          <TriangleAlert className='size-4' />
          <AlertDescription>
            {t(
              '{{count}} channel(s) are pending review or rejected. They are out of the routing pool and earn nothing until approved.',
              { count: offlineCount }
            )}
          </AlertDescription>
        </Alert>
      )}
      <DataTablePage
        table={table}
        columns={columns}
        tableLabel={t('My Channels')}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No channels yet')}
        emptyDescription={t(
          'Add a channel to contribute upstream capacity for review.'
        )}
        skeletonKeyPrefix='supplier-my-channels-skeleton'
        applyHeaderSize
        toolbarProps={null}
      />

      <ChannelFormDrawer
        key={editTarget?.id ?? 'edit'}
        open={!!editTarget}
        target={editTarget}
        onOpenChange={(open) => {
          if (!open) setEditTarget(null)
        }}
        onDone={() => {
          setEditTarget(null)
          queryClient.invalidateQueries({
            queryKey: [SUPPLIER_CHANNELS_QUERY_KEY],
          })
        }}
      />
    </>
  )
}
