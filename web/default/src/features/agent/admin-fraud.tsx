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
// jzlh-agent 蓝图F 反欺诈管理页：IP 重合告警的扫描/处置 + 风控管制（冻结/封码）。
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import type { ColumnDef } from '@tanstack/react-table'
import { Radar, ShieldAlert, ShieldOff } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import { SectionPageLayout } from '@/components/layout'
import { LongText } from '@/components/long-text'
import { TableId } from '@/components/table-id'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { formatQuota, formatTimestamp } from '@/lib/format'

import {
  adminApplyRiskControls,
  adminListFraudAlerts,
  adminListRiskUsers,
  adminRemoveRiskControls,
  adminReviewFraudAlert,
  adminScanFraud,
} from './api'
import {
  FRAUD_ALERT_STATUS,
  type FraudAlert,
  type FraudReviewAction,
  type RiskUser,
} from './types'

const route = getRouteApi('/_authenticated/risk/')
const COLUMN_VISIBILITY_STORAGE_KEY = 'agent-fraud:column-visibility'

export function AdminFraud() {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const queryClient = useQueryClient()
  const [reviewTarget, setReviewTarget] = useState<{
    alert: FraudAlert
    action: FraudReviewAction
  } | null>(null)
  const [remark, setRemark] = useState('')
  const [reviewing, setReviewing] = useState(false)
  const [scanning, setScanning] = useState(false)
  const [deepConfirmOpen, setDeepConfirmOpen] = useState(false)
  const [riskSheetOpen, setRiskSheetOpen] = useState(false)

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
      'agent-fraud-alerts',
      pagination.pageIndex + 1,
      pagination.pageSize,
      keyword,
      statusFilter,
    ],
    queryFn: async () => {
      const res = await adminListFraudAlerts(
        pagination.pageIndex + 1,
        pagination.pageSize,
        statusFilter[0] || '',
        keyword
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
    queryClient.invalidateQueries({ queryKey: ['agent-fraud-alerts'] })

  async function runScan(deep: boolean) {
    setScanning(true)
    try {
      const res = await adminScanFraud(30, deep)
      if (res.success) {
        toast.success(
          t('Scan finished: {{count}} new alerts', {
            count: res.data?.new_alerts ?? 0,
          })
        )
        refresh()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setScanning(false)
      setDeepConfirmOpen(false)
    }
  }

  function openReview(alert: FraudAlert, action: FraudReviewAction) {
    setRemark('')
    setReviewTarget({ alert, action })
  }

  async function onConfirmReview() {
    if (!reviewTarget) return
    setReviewing(true)
    try {
      const res = await adminReviewFraudAlert(
        reviewTarget.alert.id,
        reviewTarget.action,
        remark.trim()
      )
      if (res.success) {
        toast.success(t('Done'))
        setReviewTarget(null)
        refresh()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setReviewing(false)
    }
  }

  const columns = useMemo<ColumnDef<FraudAlert>[]>(
    () => buildColumns(t, openReview),
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
    ensurePageInRange,
  })

  const reviewCopy: Record<FraudReviewAction, { title: string; desc: string }> =
    {
      unbind: {
        title: t('Unbind relationship?'),
        desc: t(
          'The invitee will be detached from this agent and no longer generate commission. Existing balance is kept.'
        ),
      },
      clawback: {
        title: t('Unbind and claw back?'),
        desc: t(
          'All commission earned from this invitee will be confiscated (balance may go negative as debt), and the relationship will be unbound.'
        ),
      },
      dismiss: {
        title: t('Dismiss as false positive?'),
        desc: t(
          'The alert will be dismissed. If the IP overlap persists, a new alert will be created on the next scan.'
        ),
      },
      delete: {
        title: t('Delete this alert?'),
        desc: t(
          'Deletes the alert record only. A detected overlap will reappear on the next scan.'
        ),
      },
    }

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>{t('Risk Control')}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          onClick={() => setRiskSheetOpen(true)}
          disabled={scanning}
        >
          <ShieldAlert className='size-4' />
          {t('Risk controls')}
        </Button>
        <Button
          variant='outline'
          onClick={() => setDeepConfirmOpen(true)}
          disabled={scanning}
        >
          <Radar className='size-4' />
          {t('Deep scan')}
        </Button>
        <Button onClick={() => runScan(false)} disabled={scanning}>
          <Radar className='size-4' />
          {scanning ? t('Scanning...') : t('Scan now')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <DataTablePage
          table={table}
          columns={columns}
          isLoading={isLoading}
          isFetching={isFetching}
          emptyTitle={t('No fraud alerts')}
          emptyDescription={t(
            'Run a scan to detect IP overlaps between agents and their invitees.'
          )}
          skeletonKeyPrefix='agent-fraud-skeleton'
          applyHeaderSize
          toolbarProps={{
            searchPlaceholder: t('Filter by username or user ID...'),
            filters: [
              {
                columnId: 'status',
                title: t('Status'),
                options: [
                  {
                    label: t('Detected'),
                    value: FRAUD_ALERT_STATUS.DETECTED,
                  },
                  {
                    label: t('Resolved'),
                    value: FRAUD_ALERT_STATUS.RESOLVED,
                  },
                  {
                    label: t('Dismissed'),
                    value: FRAUD_ALERT_STATUS.DISMISSED,
                  },
                ],
                singleSelect: true,
              },
            ],
          }}
        />

        <AlertDialog
          open={!!reviewTarget}
          onOpenChange={(open) => {
            if (!open) setReviewTarget(null)
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>
                {reviewTarget && reviewCopy[reviewTarget.action].title}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {reviewTarget && (
                  <>
                    {t('Agent')} {reviewTarget.alert.agent_username || '-'} #
                    {reviewTarget.alert.agent_id} · {t('Invitee')}{' '}
                    {reviewTarget.alert.invitee_username || '-'} #
                    {reviewTarget.alert.invitee_id}
                    {' — '}
                    {reviewCopy[reviewTarget.action].desc}
                  </>
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            {reviewTarget && reviewTarget.action !== 'delete' && (
              <div className='grid gap-1.5'>
                <Label htmlFor='fraud-remark'>{t('Remark (optional)')}</Label>
                <Input
                  id='fraud-remark'
                  value={remark}
                  onChange={(e) => setRemark(e.target.value)}
                />
              </div>
            )}
            <AlertDialogFooter>
              <AlertDialogCancel disabled={reviewing}>
                {t('Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                variant={
                  reviewTarget?.action === 'clawback' ||
                  reviewTarget?.action === 'delete'
                    ? 'destructive'
                    : 'default'
                }
                onClick={onConfirmReview}
                disabled={reviewing}
              >
                {t('Confirm')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        <AlertDialog open={deepConfirmOpen} onOpenChange={setDeepConfirmOpen}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t('Run deep scan?')}</AlertDialogTitle>
              <AlertDialogDescription>
                {t(
                  'Deep scan additionally aggregates the logs table (login audit and opted-in request IPs). It is heavier on large deployments — prefer off-peak hours.'
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={scanning}>
                {t('Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                onClick={() => runScan(true)}
                disabled={scanning}
              >
                {scanning ? t('Scanning...') : t('Deep scan')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        <RiskControlsSheet
          open={riskSheetOpen}
          onOpenChange={setRiskSheetOpen}
        />
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function FraudStatusBadge({ status }: { status: string }) {
  const { t } = useTranslation()
  switch (status) {
    case FRAUD_ALERT_STATUS.DETECTED:
      return <Badge variant='destructive'>{t('Detected')}</Badge>
    case FRAUD_ALERT_STATUS.RESOLVED:
      return <Badge variant='secondary'>{t('Resolved')}</Badge>
    case FRAUD_ALERT_STATUS.DISMISSED:
      return <Badge variant='outline'>{t('Dismissed')}</Badge>
    default:
      return <Badge variant='outline'>{status}</Badge>
  }
}

function parseSharedIps(raw: string): string[] {
  try {
    const parsed = JSON.parse(raw || '[]')
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

function buildColumns(
  t: (k: string, o?: Record<string, unknown>) => string,
  openReview: (alert: FraudAlert, action: FraudReviewAction) => void
): ColumnDef<FraudAlert>[] {
  return [
    {
      accessorKey: 'id',
      header: t('ID'),
      cell: ({ row }) => (
        <TableId value={row.getValue('id') as number} className='w-[60px]' />
      ),
      size: 70,
      meta: { mobileHidden: true },
    },
    {
      id: 'detected_at',
      accessorKey: 'detected_at',
      header: t('Detected at'),
      cell: ({ row }) => (
        <span className='text-muted-foreground text-sm whitespace-nowrap'>
          {formatTimestamp(row.original.detected_at)}
        </span>
      ),
      size: 150,
      meta: { mobileTitle: true },
    },
    {
      id: 'agent',
      header: t('Agent'),
      cell: ({ row }) => (
        <div className='flex items-center gap-1.5'>
          <LongText className='max-w-[120px] font-medium'>
            {row.original.agent_username || '-'}
          </LongText>
          <span className='text-muted-foreground text-xs'>
            #{row.original.agent_id}
          </span>
        </div>
      ),
      size: 150,
    },
    {
      id: 'invitee',
      header: t('Invitee'),
      cell: ({ row }) => (
        <div className='flex items-center gap-1.5'>
          <LongText className='max-w-[120px] font-medium'>
            {row.original.invitee_username || '-'}
          </LongText>
          <span className='text-muted-foreground text-xs'>
            #{row.original.invitee_id}
          </span>
        </div>
      ),
      size: 150,
    },
    {
      id: 'shared_ips',
      header: t('Shared IPs'),
      cell: ({ row }) => {
        const ips = parseSharedIps(row.original.shared_ips)
        return (
          <div className='flex items-center gap-1.5'>
            <Badge variant='outline' className='tabular-nums'>
              {row.original.shared_ip_count}
            </Badge>
            <LongText className='text-muted-foreground max-w-[200px] text-xs'>
              {ips.join(', ')}
            </LongText>
          </div>
        )
      },
      size: 230,
    },
    {
      id: 'status',
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => (
        <div className='flex items-center gap-1.5'>
          <FraudStatusBadge status={row.original.status} />
          {row.original.status === FRAUD_ALERT_STATUS.RESOLVED &&
            row.original.resolved_action === 'clawback' && (
              <span className='text-muted-foreground text-xs tabular-nums'>
                -{formatQuota(row.original.clawback_quota || 0)}
              </span>
            )}
        </div>
      ),
      size: 140,
      filterFn: () => true,
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => {
        const alert = row.original
        const detected = alert.status === FRAUD_ALERT_STATUS.DETECTED
        return (
          <DataTableRowActionMenu ariaLabel={t('Open actions')}>
            {detected && (
              <>
                <DropdownMenuItem onClick={() => openReview(alert, 'unbind')}>
                  {t('Unbind')}
                </DropdownMenuItem>
                <DropdownMenuItem
                  variant='destructive'
                  onClick={() => openReview(alert, 'clawback')}
                >
                  {t('Unbind + claw back')}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => openReview(alert, 'dismiss')}>
                  {t('Dismiss')}
                </DropdownMenuItem>
              </>
            )}
            <DropdownMenuItem
              variant='destructive'
              onClick={() => openReview(alert, 'delete')}
            >
              {t('Delete')}
            </DropdownMenuItem>
          </DataTableRowActionMenu>
        )
      },
      size: 60,
    },
  ]
}

// 「风控管制」抽屉：active 风控用户列表 + 施加/解除管制。
// 低频低量数据，用轻量列表而非 DataTable，避免与主表的 URL 状态互相踩。
function RiskControlsSheet({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [userId, setUserId] = useState('')
  const [freezeAssets, setFreezeAssets] = useState(true)
  const [blockInviteCode, setBlockInviteCode] = useState(false)
  const [reason, setReason] = useState('')
  const [busy, setBusy] = useState(false)
  const [removeTarget, setRemoveTarget] = useState<RiskUser | null>(null)

  const { data } = useQuery({
    queryKey: ['agent-risk-users'],
    queryFn: async () => {
      const res = await adminListRiskUsers(1, 100)
      if (!res.success || !res.data) {
        toast.error(res.message || t('Failed to load'))
        return []
      }
      return res.data.items || []
    },
    enabled: open,
  })

  const refresh = () =>
    queryClient.invalidateQueries({ queryKey: ['agent-risk-users'] })

  async function onApply() {
    const uid = Number.parseInt(userId, 10)
    if (Number.isNaN(uid) || uid <= 0) {
      toast.error(t('Please enter a valid user ID'))
      return
    }
    if (!freezeAssets && !blockInviteCode) {
      toast.error(t('Select at least one control'))
      return
    }
    setBusy(true)
    try {
      const res = await adminApplyRiskControls({
        user_id: uid,
        freeze_assets: freezeAssets,
        block_invite_code: blockInviteCode,
        reason: reason.trim(),
      })
      if (res.success) {
        const rejected = res.data?.rejected_withdrawals ?? 0
        toast.success(
          rejected > 0
            ? t('Controls applied. {{count}} pending withdrawals rejected.', {
                count: rejected,
              })
            : t('Controls applied')
        )
        setUserId('')
        setReason('')
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

  async function onRemove() {
    if (!removeTarget) return
    setBusy(true)
    try {
      const res = await adminRemoveRiskControls(removeTarget.user_id)
      if (res.success) {
        toast.success(t('Controls removed'))
        setRemoveTarget(null)
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

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className='flex w-full flex-col gap-0 overflow-y-auto sm:max-w-[520px]'>
        <SheetHeader>
          <SheetTitle>{t('Risk controls')}</SheetTitle>
          <SheetDescription>
            {t(
              'Freeze commission assets or block the invite code of a suspicious agent while investigating. Lighter than banning the account.'
            )}
          </SheetDescription>
        </SheetHeader>

        <div className='grid gap-4 px-4 pb-4'>
          <div className='grid gap-3 rounded-lg border p-3'>
            <div className='grid max-w-40 gap-1.5'>
              <Label htmlFor='risk-uid'>{t('User ID')}</Label>
              <Input
                id='risk-uid'
                value={userId}
                onChange={(e) => setUserId(e.target.value)}
                inputMode='numeric'
              />
            </div>
            <div className='flex items-center gap-2'>
              <Checkbox
                id='risk-freeze'
                checked={freezeAssets}
                onCheckedChange={(v) => setFreezeAssets(v === true)}
              />
              <Label htmlFor='risk-freeze'>{t('Freeze assets')}</Label>
              <span className='text-muted-foreground text-xs'>
                {t('Blocks withdraw/convert, pauses maturing')}
              </span>
            </div>
            <div className='flex items-center gap-2'>
              <Checkbox
                id='risk-block'
                checked={blockInviteCode}
                onCheckedChange={(v) => setBlockInviteCode(v === true)}
              />
              <Label htmlFor='risk-block'>{t('Block invite code')}</Label>
              <span className='text-muted-foreground text-xs'>
                {t('New registrations no longer bind')}
              </span>
            </div>
            <div className='grid gap-1.5'>
              <Label htmlFor='risk-reason'>{t('Reason')}</Label>
              <Input
                id='risk-reason'
                value={reason}
                onChange={(e) => setReason(e.target.value)}
              />
            </div>
            <div>
              <Button onClick={onApply} disabled={busy}>
                <ShieldAlert className='size-4' />
                {t('Apply controls')}
              </Button>
            </div>
          </div>

          <div className='grid gap-2'>
            <div className='text-sm font-medium'>
              {t('Active controls')} ({data?.length ?? 0})
            </div>
            {(data || []).map((r) => (
              <div
                key={r.id}
                className='flex items-center justify-between gap-2 rounded-lg border p-2.5'
              >
                <div className='grid gap-1'>
                  <div className='flex items-center gap-1.5'>
                    <span className='text-sm font-medium'>
                      {r.username || '-'}
                    </span>
                    <span className='text-muted-foreground text-xs'>
                      #{r.user_id}
                    </span>
                  </div>
                  <div className='flex flex-wrap items-center gap-1'>
                    {r.freeze_assets && (
                      <Badge variant='destructive'>{t('Freeze assets')}</Badge>
                    )}
                    {r.block_invite_code && (
                      <Badge variant='secondary'>
                        {t('Block invite code')}
                      </Badge>
                    )}
                    {r.reason && (
                      <LongText className='text-muted-foreground max-w-[220px] text-xs'>
                        {r.reason}
                      </LongText>
                    )}
                  </div>
                </div>
                <Button
                  variant='outline'
                  size='sm'
                  onClick={() => setRemoveTarget(r)}
                  disabled={busy}
                >
                  <ShieldOff className='size-4' />
                  {t('Remove')}
                </Button>
              </div>
            ))}
            {(data || []).length === 0 && (
              <div className='text-muted-foreground rounded-lg border border-dashed p-4 text-center text-sm'>
                {t('No active risk controls')}
              </div>
            )}
          </div>
        </div>

        <AlertDialog
          open={!!removeTarget}
          onOpenChange={(o) => {
            if (!o) setRemoveTarget(null)
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t('Remove controls?')}</AlertDialogTitle>
              <AlertDialogDescription>
                {removeTarget &&
                  `${removeTarget.username || '-'} #${removeTarget.user_id} — `}
                {t(
                  'Frozen assets resume maturing and exits reopen. The invite code becomes valid again.'
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={busy}>
                {t('Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction onClick={onRemove} disabled={busy}>
                {t('Remove')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SheetContent>
    </Sheet>
  )
}
