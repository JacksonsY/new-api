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
import { type ColumnDef, type Table } from '@tanstack/react-table'
import { Pencil, Search, UserPlus, UserX } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  BadgeCell,
  DataTableBulkActions as BulkActionsToolbar,
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePage,
  useDataTable,
} from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { SectionPageLayout } from '@/components/layout'
import { LongText } from '@/components/long-text'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { formatQuota } from '@/lib/format'

import {
  adminListAgents,
  adminRevokeAgent,
  adminSetAgent,
  searchUsers,
} from './api'
import type { AgentUser } from './types'

const route = getRouteApi('/_authenticated/agents/')
const COLUMN_VISIBILITY_STORAGE_KEY = 'agent-admin:column-visibility'

const USER_STATUS = { ENABLED: 1, DISABLED: 2 } as const

export function AdminAgents() {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const queryClient = useQueryClient()
  const [setAgentOpen, setSetAgentOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<AgentUser | null>(null)
  const [revokeTarget, setRevokeTarget] = useState<AgentUser | null>(null)
  const [revoking, setRevoking] = useState(false)

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

  const keyword = (globalFilter || '').trim()
  const statusFilter =
    (columnFilters.find((f) => f.id === 'status')?.value as
      | string[]
      | undefined) ?? []

  // eslint-disable-next-line @tanstack/query/exhaustive-deps
  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'agents',
      pagination.pageIndex + 1,
      pagination.pageSize,
      keyword,
      statusFilter,
    ],
    queryFn: async () => {
      const res = await adminListAgents(
        pagination.pageIndex + 1,
        pagination.pageSize,
        keyword,
        statusFilter[0] ?? ''
      )
      if (!res.success || !res.data) {
        toast.error(res.message || t('Failed to load'))
        return { items: [], total: 0 }
      }
      return { items: res.data.items || [], total: res.data.total || 0 }
    },
    placeholderData: (previousData) => previousData,
  })

  const refresh = () => queryClient.invalidateQueries({ queryKey: ['agents'] })

  async function onConfirmRevoke() {
    if (!revokeTarget) return
    setRevoking(true)
    try {
      const res = await adminRevokeAgent(revokeTarget.id)
      if (res.success) {
        toast.success(t('Revoked'))
        setRevokeTarget(null)
        refresh()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setRevoking(false)
    }
  }

  const columns = useMemo<ColumnDef<AgentUser>[]>(
    () => buildColumns(t, setEditTarget),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [t]
  )

  const { table } = useDataTable({
    data: data?.items || [],
    columns,
    columnFilters,
    columnVisibilityStorageKey: COLUMN_VISIBILITY_STORAGE_KEY,
    globalFilter,
    pagination,
    globalFilterFn: () => true,
    onPaginationChange,
    onGlobalFilterChange,
    onColumnFiltersChange,
    manualPagination: true,
    manualFiltering: true,
    totalCount: data?.total || 0,
    enableRowSelection: true,
    ensurePageInRange,
  })

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Agent Management')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button onClick={() => setSetAgentOpen(true)}>
          <UserPlus className='size-4' />
          {t('Set as Agent')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <DataTablePage
          table={table}
          columns={columns}
          isLoading={isLoading}
          isFetching={isFetching}
          emptyTitle={t('No agents yet')}
          emptyDescription={t(
            'Set a user as an agent to let them earn consumption commission.'
          )}
          skeletonKeyPrefix='agents-skeleton'
          applyHeaderSize
          toolbarProps={{
            searchPlaceholder: t('Filter by username...'),
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
            row.original.status !== USER_STATUS.ENABLED
              ? ctx?.isMobile
                ? DISABLED_ROW_MOBILE
                : DISABLED_ROW_DESKTOP
              : undefined
          }
          bulkActions={<AgentsBulkActions table={table} onDone={refresh} />}
        />

        <SetAgentDialog
          open={setAgentOpen}
          onOpenChange={setSetAgentOpen}
          onDone={refresh}
        />

        <AgentEditDrawer
          target={editTarget}
          onOpenChange={(open) => {
            if (!open) setEditTarget(null)
          }}
          onDone={refresh}
          onRevoke={(agent) => {
            setEditTarget(null)
            setRevokeTarget(agent)
          }}
        />

        <ConfirmDialog
          open={!!revokeTarget}
          onOpenChange={(open) => {
            if (!open) setRevokeTarget(null)
          }}
          title={t('Are you sure?')}
          desc={`${revokeTarget?.username ?? ''} — ${t('The user will lose agent access. Accumulated commission balance is kept.')}`}
          destructive
          isLoading={revoking}
          handleConfirm={onConfirmRevoke}
          confirmText={t('Revoke')}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function buildColumns(
  t: (k: string) => string,
  onEditRate: (a: AgentUser) => void
): ColumnDef<AgentUser>[] {
  return [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          indeterminate={table.getIsSomePageRowsSelected()}
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label='Select all'
          className='translate-y-[2px]'
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label='Select row'
          className='translate-y-[2px]'
        />
      ),
      enableSorting: false,
      enableHiding: false,
      size: 40,
    },
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
        const a = row.original
        return (
          <div className='flex min-w-[160px] flex-col gap-1'>
            <LongText className='max-w-[160px] font-medium'>
              {a.username}
            </LongText>
            {a.display_name && a.display_name !== a.username && (
              <LongText className='text-muted-foreground max-w-[180px] text-xs'>
                {a.display_name}
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
      size: 100,
      enableSorting: false,
      meta: { mobileBadge: true },
    },
    {
      id: 'rate',
      header: t('Commission Rate'),
      cell: ({ row }) => (
        <span className='tabular-nums'>
          {((row.original.usage_profit_rate || 0) * 100).toFixed(1)}%
        </span>
      ),
      size: 120,
      meta: { mobileBadge: true },
    },
    {
      id: 'downstream',
      header: t('Downstream Users'),
      cell: ({ row }) => (
        <span className='tabular-nums'>
          {row.original.downstream_count || 0}
        </span>
      ),
      size: 110,
    },
    {
      id: 'balance',
      header: t('Withdrawable Commission'),
      cell: ({ row }) => (
        <span className='tabular-nums'>
          {formatQuota(row.original.commission_quota || 0)}
        </span>
      ),
      size: 170,
    },
    {
      id: 'history',
      header: t('Total Commission'),
      cell: ({ row }) => (
        <span className='text-muted-foreground tabular-nums'>
          {formatQuota(row.original.commission_history_quota || 0)}
        </span>
      ),
      size: 150,
      meta: { mobileHidden: true },
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => (
        <div className='-ml-1.5 flex items-center gap-1'>
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  variant='ghost'
                  size='icon-sm'
                  onClick={() => onEditRate(row.original)}
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

// 「设为代理」弹窗:搜用户 → 设分润比例 → 设为代理。
function SetAgentDialog({
  open,
  onOpenChange,
  onDone,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const [rate, setRate] = useState('0.1')
  const [keyword, setKeyword] = useState('')
  const [results, setResults] = useState<AgentUser[]>([])
  const [searching, setSearching] = useState(false)
  const [busyId, setBusyId] = useState<number | null>(null)

  async function onSearch() {
    if (!keyword.trim()) {
      setResults([])
      return
    }
    setSearching(true)
    try {
      const res = await searchUsers(keyword.trim())
      if (res.success && res.data) setResults(res.data.items || [])
    } catch {
      toast.error(t('Failed to load'))
    } finally {
      setSearching(false)
    }
  }

  async function onSet(userId: number) {
    const r = parseFloat(rate)
    if (isNaN(r) || r < 0 || r > 1) {
      toast.error(t('Commission rate must be between 0 and 1'))
      return
    }
    setBusyId(userId)
    try {
      const res = await adminSetAgent({
        user_id: userId,
        agent_type: 'normal',
        usage_profit_rate: r,
      })
      if (res.success) {
        toast.success(t('Saved'))
        onDone()
        onOpenChange(false)
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setBusyId(null)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Set as Agent')}
      description={t(
        'Search a user, set the commission rate, and grant agent access.'
      )}
      contentHeight='auto'
    >
      <div className='space-y-4 py-2'>
        <div className='grid max-w-40 gap-1.5'>
          <Label htmlFor='sa-rate'>{t('Commission Rate')} (0-1)</Label>
          <Input
            id='sa-rate'
            value={rate}
            onChange={(e) => setRate(e.target.value)}
            placeholder='0.1'
            inputMode='decimal'
          />
        </div>

        <div className='grid gap-1.5'>
          <Label htmlFor='sa-search'>{t('Search username or email')}</Label>
          <div className='flex gap-2'>
            <div className='relative flex-1'>
              <Search className='text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2' />
              <Input
                id='sa-search'
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') onSearch()
                }}
                className='pl-8'
              />
            </div>
            <Button variant='outline' onClick={onSearch} disabled={searching}>
              {t('Search')}
            </Button>
          </div>
        </div>

        {results.length > 0 && (
          <div className='max-h-64 divide-y overflow-y-auto rounded-lg border'>
            {results.map((u) => (
              <div
                key={u.id}
                className='flex items-center justify-between gap-3 px-3 py-2'
              >
                <div className='min-w-0'>
                  <div className='truncate text-sm font-medium'>
                    {u.username}
                    <span className='text-muted-foreground ml-1.5 text-xs font-normal'>
                      #{u.id}
                    </span>
                  </div>
                  {u.email && (
                    <div className='text-muted-foreground truncate text-xs'>
                      {u.email}
                    </div>
                  )}
                </div>
                <Button
                  size='sm'
                  onClick={() => onSet(u.id)}
                  disabled={busyId !== null}
                >
                  {t('Set as Agent')}
                </Button>
              </div>
            ))}
          </div>
        )}
      </div>
    </Dialog>
  )
}

// 「编辑代理」抽屉(对齐超管用户列表的编辑抽屉):改分润比例, 也可在此撤销代理。
function AgentEditDrawer({
  target,
  onOpenChange,
  onDone,
  onRevoke,
}: {
  target: AgentUser | null
  onOpenChange: (open: boolean) => void
  onDone: () => void
  onRevoke: (a: AgentUser) => void
}) {
  const { t } = useTranslation()
  const [rate, setRate] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (target) setRate(String(target.usage_profit_rate ?? 0))
  }, [target])

  async function onSave() {
    if (!target) return
    const r = parseFloat(rate)
    if (isNaN(r) || r < 0 || r > 1) {
      toast.error(t('Commission rate must be between 0 and 1'))
      return
    }
    setBusy(true)
    try {
      const res = await adminSetAgent({
        user_id: target.id,
        agent_type: 'normal',
        usage_profit_rate: r,
      })
      if (res.success) {
        toast.success(t('Saved'))
        onDone()
        onOpenChange(false)
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Sheet open={!!target} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[480px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {t('Update')} {t('Agent')}
          </SheetTitle>
          <SheetDescription>
            {target ? `${target.username} #${target.id}` : ''}
          </SheetDescription>
        </SheetHeader>

        <div className={sideDrawerFormClassName()}>
          <SideDrawerSection>
            <div className='grid gap-4'>
              <div className='grid grid-cols-2 gap-4 text-sm'>
                <div>
                  <div className='text-muted-foreground text-xs'>
                    {t('Downstream Users')}
                  </div>
                  <div className='tabular-nums'>
                    {target?.downstream_count || 0}
                  </div>
                </div>
                <div>
                  <div className='text-muted-foreground text-xs'>
                    {t('Withdrawable Commission')}
                  </div>
                  <div className='tabular-nums'>
                    {formatQuota(target?.commission_quota || 0)}
                  </div>
                </div>
              </div>

              <div className='grid max-w-40 gap-1.5'>
                <Label htmlFor='er-rate'>{t('Commission Rate')} (0-1)</Label>
                <Input
                  id='er-rate'
                  value={rate}
                  onChange={(e) => setRate(e.target.value)}
                  inputMode='decimal'
                />
              </div>

              <div>
                <Button
                  variant='outline'
                  className='text-destructive'
                  onClick={() => target && onRevoke(target)}
                >
                  <UserX className='size-4' />
                  {t('Revoke')}
                </Button>
              </div>
            </div>
          </SideDrawerSection>
        </div>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button onClick={onSave} disabled={busy}>
            {busy ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}

// 批量撤销代理(勾选后浮层, 二次确认)。
function AgentsBulkActions({
  table,
  onDone,
}: {
  table: Table<AgentUser>
  onDone: () => void
}) {
  const { t } = useTranslation()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const rows = table.getFilteredSelectedRowModel().rows

  async function bulkRevoke() {
    setBusy(true)
    try {
      await Promise.all(rows.map((r) => adminRevokeAgent(r.original.id)))
      toast.success(t('{{count}} agents revoked', { count: rows.length }))
      table.resetRowSelection()
      setConfirmOpen(false)
      onDone()
    } catch {
      toast.error(t('Failed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <BulkActionsToolbar table={table} entityName='agent'>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                variant='outline'
                size='icon'
                disabled={busy}
                onClick={() => setConfirmOpen(true)}
                aria-label={t('Revoke')}
              />
            }
          >
            <UserX className='size-4' />
          </TooltipTrigger>
          <TooltipContent>{t('Revoke')}</TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={t('Are you sure?')}
        desc={t(
          '{{count}} selected users will lose agent access. Accumulated commission balance is kept.',
          { count: rows.length }
        )}
        destructive
        isLoading={busy}
        handleConfirm={bulkRevoke}
        confirmText={t('Revoke')}
      />
    </>
  )
}
