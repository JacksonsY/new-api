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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { DataTablePage, useDataTable } from '@/components/data-table'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import { adminListSuppliers, adminReviewSupplier } from '../api'
import { SUPPLIER_STATUS } from '../types'
import {
  useAdminSuppliersColumns,
  type SupplierReviewAction,
} from './admin-suppliers-columns'

const route = getRouteApi('/_authenticated/suppliers/')

export function AdminSuppliersTable() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [action, setAction] = useState<SupplierReviewAction | null>(null)
  const [busy, setBusy] = useState(false)

  const columns = useAdminSuppliersColumns(setAction)

  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: {
      defaultPage: 1,
      defaultPageSize: 20,
      pageSizeStorageKey: 'suppliers:page-size:v1',
    },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [{ columnId: 'status', searchKey: 'status', type: 'array' }],
  })

  const keyword = (globalFilter || '').trim()
  const statusFilter =
    (columnFilters.find((f) => f.id === 'status')?.value as
      | string[]
      | undefined) ?? []

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'suppliers',
      pagination.pageIndex + 1,
      pagination.pageSize,
      keyword,
      statusFilter,
    ],
    queryFn: async () => {
      const res = await adminListSuppliers(
        statusFilter[0] ?? '',
        keyword,
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

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['suppliers'] })

  async function onConfirm() {
    if (!action) return
    setBusy(true)
    try {
      const res = await adminReviewSupplier(action.user.id, action.status)
      if (res.success) {
        toast.success(t('Saved'))
        setAction(null)
        refresh()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setBusy(false)
    }
  }

  const { table } = useDataTable({
    data: data?.items || [],
    columns,
    columnFilters,
    globalFilter,
    pagination,
    globalFilterFn: () => true,
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        tableLabel={t('Supplier Management')}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No suppliers yet')}
        emptyDescription={t(
          'No suppliers available. Try adjusting your search or filters.'
        )}
        skeletonKeyPrefix='suppliers-skeleton'
        applyHeaderSize
        toolbarProps={{
          searchPlaceholder: t('Filter by username...'),
          filters: [
            {
              columnId: 'status',
              title: t('Status'),
              options: [
                { label: t('Pending'), value: String(SUPPLIER_STATUS.PENDING) },
                {
                  label: t('Approved'),
                  value: String(SUPPLIER_STATUS.APPROVED),
                },
                {
                  label: t('Suspended'),
                  value: String(SUPPLIER_STATUS.SUSPENDED),
                },
              ],
              singleSelect: true,
            },
          ],
        }}
      />

      <ConfirmDialog
        open={!!action}
        onOpenChange={(open) => {
          if (!open) setAction(null)
        }}
        title={action ? t(action.labelKey) : ''}
        desc={action ? `${action.user.username} #${action.user.id}` : ''}
        destructive={action?.status === SUPPLIER_STATUS.SUSPENDED}
        isLoading={busy}
        handleConfirm={onConfirm}
        confirmText={action ? t(action.labelKey) : t('Confirm')}
      />
    </>
  )
}
