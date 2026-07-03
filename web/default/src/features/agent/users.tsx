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
import { type ColumnDef } from '@tanstack/react-table'
import { Coins, TrendingUp, Users } from 'lucide-react'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  BadgeCell,
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePage,
  useDataTable,
} from '@/components/data-table'
import { GroupBadge } from '@/components/group-badge'
import { SectionPageLayout } from '@/components/layout'
import { LongText } from '@/components/long-text'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Progress } from '@/components/ui/progress'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import {
  formatQuota,
  formatTimestamp,
  formatTimestampToDate,
} from '@/lib/format'
import { cn } from '@/lib/utils'

import { agentListUsers } from './api'
import { StatCard } from './stat-card'
import type { AgentUser } from './types'

const route = getRouteApi('/_authenticated/agent/users/')
const COLUMN_VISIBILITY_STORAGE_KEY = 'agent-users:column-visibility'
const USER_STATUS = { ENABLED: 1, DISABLED: 2 } as const

// 额度进度条配色(与超管用户列表一致):剩余占比低→红,中→黄,高→绿
function getQuotaProgressColor(percentage: number): string {
  if (percentage <= 10) return '[&_[data-slot=progress-indicator]]:bg-rose-500'
  if (percentage <= 30) return '[&_[data-slot=progress-indicator]]:bg-amber-500'
  return '[&_[data-slot=progress-indicator]]:bg-emerald-500'
}

function isDisabledRow(u: AgentUser) {
  return u.status !== USER_STATUS.ENABLED
}

// 「我的用户」— 名下用户总览。表格架构完全对齐超管用户列表:
// 勾选列 + kebab 行操作 + 状态筛选 + 服务端搜索/分页 + 禁用行置灰 + 批量启停。
export function MyUsers() {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')

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
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [{ columnId: 'status', searchKey: 'status', type: 'array' }],
  })

  const statusFilter =
    (columnFilters.find((f) => f.id === 'status')?.value as
      | string[]
      | undefined) ?? []
  const keyword = (globalFilter || '').trim()

  // eslint-disable-next-line @tanstack/query/exhaustive-deps
  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'agent-users',
      pagination.pageIndex + 1,
      pagination.pageSize,
      keyword,
      statusFilter,
    ],
    queryFn: async () => {
      const res = await agentListUsers(
        pagination.pageIndex + 1,
        pagination.pageSize,
        keyword,
        statusFilter[0] ?? ''
      )
      if (!res.success || !res.data) {
        toast.error(res.message || t('Failed to load'))
        return { items: [], total: 0, total_quota: 0, total_used_quota: 0 }
      }
      return {
        items: res.data.items || [],
        total: res.data.total || 0,
        total_quota: res.data.total_quota || 0,
        total_used_quota: res.data.total_used_quota || 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const columns = useMemo<ColumnDef<AgentUser>[]>(() => buildColumns(t), [t])

  const { table } = useDataTable({
    data: data?.items || [],
    columns,
    columnFilters,
    columnVisibilityStorageKey: COLUMN_VISIBILITY_STORAGE_KEY,
    globalFilter,
    pagination,
    globalFilterFn: () => true, // 搜索/筛选走服务端
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('My Users')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex h-full min-h-0 flex-col gap-4'>
          {/* 汇总统计 */}
          <div className='grid shrink-0 gap-4 sm:grid-cols-3'>
            <StatCard
              icon={<Users className='size-5' />}
              label={t('Invited Users')}
              value={String(data?.total || 0)}
              emphasize
            />
            <StatCard
              icon={<Coins className='size-5' />}
              label={t('Total User Balance')}
              value={formatQuota(data?.total_quota || 0)}
            />
            <StatCard
              icon={<TrendingUp className='size-5' />}
              label={t('Total User Consumption')}
              value={formatQuota(data?.total_used_quota || 0)}
            />
          </div>

          {/* 表格(占满剩余高度,内部滚动) */}
          <div className='min-h-0 flex-1'>
            <DataTablePage
              table={table}
              columns={columns}
              isLoading={isLoading}
              isFetching={isFetching}
              emptyTitle={t('No users yet')}
              emptyDescription={t(
                'Users who registered under your invitation will appear here.'
              )}
              skeletonKeyPrefix='agent-users-skeleton'
              applyHeaderSize
              toolbarProps={{
                searchPlaceholder: t('Filter by username or name...'),
                filters: [
                  {
                    columnId: 'status',
                    title: t('Status'),
                    options: [
                      {
                        label: t('Enabled'),
                        value: String(USER_STATUS.ENABLED),
                      },
                      {
                        label: t('Disabled'),
                        value: String(USER_STATUS.DISABLED),
                      },
                    ],
                    singleSelect: true,
                  },
                ],
              }}
              getRowClassName={(row, ctx) =>
                isDisabledRow(row.original)
                  ? ctx?.isMobile
                    ? DISABLED_ROW_MOBILE
                    : DISABLED_ROW_DESKTOP
                  : undefined
              }
            />
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

// 列定义(与超管用户列表同构,只读:用户/分组/额度进度条/请求/状态/最后登录/注册)
function buildColumns(t: (k: string) => string): ColumnDef<AgentUser>[] {
  return [
    {
      accessorKey: 'id',
      header: t('ID'),
      cell: ({ row }) => (
        <TableId value={row.getValue('id') as number} className='w-[60px]' />
      ),
      size: 80,
      meta: { mobileHidden: true },
    },
    {
      accessorKey: 'username',
      header: t('Username'),
      cell: ({ row }) => {
        const u = row.original
        return (
          <div className='flex min-w-[160px] flex-col gap-1'>
            <LongText className='max-w-[160px] font-medium'>
              {u.username}
            </LongText>
            {u.display_name && u.display_name !== u.username && (
              <LongText className='text-muted-foreground max-w-[180px] text-xs'>
                {u.display_name}
              </LongText>
            )}
          </div>
        )
      },
      enableHiding: false,
      size: 220,
      meta: { mobileTitle: true },
    },
    {
      id: 'group',
      accessorKey: 'group',
      header: t('Group'),
      cell: ({ row }) => (
        <BadgeCell>
          <GroupBadge group={row.original.group || 'default'} />
        </BadgeCell>
      ),
      size: 120,
      meta: { mobileBadge: true },
    },
    {
      id: 'quota',
      header: t('Quota'),
      cell: ({ row }) => {
        const u = row.original
        const used = u.used_quota || 0
        const remaining = u.quota || 0
        const total = used + remaining
        const percentage = total > 0 ? (remaining / total) * 100 : 0
        if (total === 0) {
          return (
            <StatusBadge
              label={t('No Quota')}
              variant='neutral'
              copyable={false}
              className='-ml-1.5'
            />
          )
        }
        return (
          <Tooltip>
            <TooltipTrigger
              render={<div className='w-[150px] cursor-help space-y-1' />}
            >
              <div className='flex justify-between text-xs'>
                <span className='font-medium tabular-nums'>
                  {formatQuota(remaining)}
                </span>
                <span className='text-muted-foreground tabular-nums'>
                  {formatQuota(total)}
                </span>
              </div>
              <Progress
                value={percentage}
                className={cn('h-1.5', getQuotaProgressColor(percentage))}
              />
            </TooltipTrigger>
            <TooltipContent>
              <div className='space-y-1 text-xs'>
                <div>
                  {t('Used:')} {formatQuota(used)}
                </div>
                <div>
                  {t('Remaining:')} {formatQuota(remaining)}
                </div>
                <div>
                  {t('Percentage:')} {percentage.toFixed(1)}%
                </div>
              </div>
            </TooltipContent>
          </Tooltip>
        )
      },
      size: 170,
    },
    {
      id: 'requests',
      accessorKey: 'request_count',
      header: t('Requests'),
      cell: ({ row }) => (
        <span className='tabular-nums'>{row.original.request_count ?? 0}</span>
      ),
      size: 100,
      meta: { mobileHidden: true },
    },
    {
      id: 'status',
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => (
        <BadgeCell>
          {row.original.status === USER_STATUS.ENABLED ? (
            <StatusBadge
              label={t('Enabled')}
              variant='success'
              copyable={false}
            />
          ) : (
            <StatusBadge
              label={t('Disabled')}
              variant='neutral'
              copyable={false}
            />
          )}
        </BadgeCell>
      ),
      size: 110,
      enableSorting: false,
      meta: { mobileBadge: true },
    },
    {
      id: 'last_login',
      accessorKey: 'last_login_at',
      header: t('Last Login'),
      cell: ({ row }) => (
        <span className='text-muted-foreground text-sm'>
          {row.original.last_login_at
            ? formatTimestamp(row.original.last_login_at)
            : '-'}
        </span>
      ),
      size: 170,
      meta: { mobileHidden: true },
    },
    {
      id: 'created',
      accessorKey: 'created_at',
      header: t('Created At'),
      cell: ({ row }) => (
        <span className='text-muted-foreground text-sm'>
          {row.original.created_at
            ? formatTimestampToDate(row.original.created_at)
            : '-'}
        </span>
      ),
      size: 150,
      meta: { mobileHidden: true },
    },
  ]
}
