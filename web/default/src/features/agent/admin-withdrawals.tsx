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
import { type ColumnDef, type Row } from '@tanstack/react-table'
import { Check, Copy as CopyIcon, Settings, Undo2, X } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { BadgeCell, DataTablePage, useDataTable } from '@/components/data-table'
import { DataTableRowActionMenu } from '@/components/data-table/core/row-action-menu'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/design-system/alert-dialog'
import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { Dialog } from '@/components/dialog'
import { SectionPageLayout } from '@/components/layout'
import { LongText } from '@/components/long-text'
import { TableId } from '@/components/table-id'
import { DropdownMenuItem } from '@/components/ui/dropdown-menu'
import { Label } from '@/components/ui/label'
import { useMediaQuery } from '@/hooks'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { getCurrencyDisplay } from '@/lib/currency'
import {
  formatQuota,
  formatTimestamp,
  parseQuotaFromDollars,
  quotaUnitsToDollars,
} from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

import {
  adminListWithdrawals,
  getWithdrawSettings,
  reviewWithdrawal,
  updateWithdrawSettings,
} from './api'
import { WITHDRAWAL_METHOD_LABELS, WITHDRAWAL_STATUS } from './types'
import type { Withdrawal } from './types'
import { WithdrawalStatusBadge } from './withdrawal-status-badge'

const route = getRouteApi('/_authenticated/withdrawals/')
const COLUMN_VISIBILITY_STORAGE_KEY = 'agent-withdrawals:column-visibility'

export function AdminWithdrawals() {
  const { t } = useTranslation()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const queryClient = useQueryClient()
  const [reviewTarget, setReviewTarget] = useState<{
    withdrawal: Withdrawal
    action: 'approve' | 'reject'
  } | null>(null)
  const [adminRemark, setAdminRemark] = useState('')
  const [reviewing, setReviewing] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  // 提现策略配置走 RootAuth 的 option 端点，仅超管可见/可改。
  const isRoot = useAuthStore(
    (s) => (s.auth.user?.role ?? 0) >= ROLE.SUPER_ADMIN
  )

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
      'agent-withdrawals',
      pagination.pageIndex + 1,
      pagination.pageSize,
      keyword,
      statusFilter,
    ],
    queryFn: async () => {
      const res = await adminListWithdrawals(
        Number(statusFilter[0] || 0),
        pagination.pageIndex + 1,
        pagination.pageSize,
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
    queryClient.invalidateQueries({ queryKey: ['agent-withdrawals'] })

  function openReview(withdrawal: Withdrawal, action: 'approve' | 'reject') {
    setAdminRemark('')
    setReviewTarget({ withdrawal, action })
  }

  // claim/release 是可逆的轻操作，直接执行不弹确认框。
  async function quickAction(w: Withdrawal, action: 'claim' | 'release') {
    try {
      const res = await reviewWithdrawal(w.id, action)
      if (res.success) {
        toast.success(action === 'claim' ? t('Claimed') : t('Claim released'))
        refresh()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    }
  }

  async function onConfirmReview() {
    if (!reviewTarget) return
    if (reviewTarget.action === 'approve' && !adminRemark.trim()) {
      toast.error(t('Payout reference is required'))
      return
    }
    setReviewing(true)
    try {
      const res = await reviewWithdrawal(
        reviewTarget.withdrawal.id,
        reviewTarget.action,
        adminRemark.trim()
      )
      if (res.success) {
        toast.success(
          reviewTarget.action === 'approve'
            ? t('Marked as paid')
            : t('Rejected')
        )
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

  const columns = useMemo<ColumnDef<Withdrawal>[]>(
    () => buildColumns(t, openReview, quickAction),
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

  return (
    <SectionPageLayout fixedContent>
      <SectionPageLayout.Title>
        {t('Withdrawal Review')}
      </SectionPageLayout.Title>
      {isRoot && (
        <SectionPageLayout.Actions>
          <Button variant='outline' onClick={() => setSettingsOpen(true)}>
            <Settings className='size-4' />
            {t('Withdrawal settings')}
          </Button>
        </SectionPageLayout.Actions>
      )}
      <SectionPageLayout.Content>
        <DataTablePage
          table={table}
          columns={columns}
          isLoading={isLoading}
          isFetching={isFetching}
          emptyTitle={t('No withdrawals yet')}
          emptyDescription={t(
            'Withdrawal requests from agents will appear here.'
          )}
          skeletonKeyPrefix='agent-withdrawals-skeleton'
          applyHeaderSize
          toolbarProps={{
            searchPlaceholder: t(
              'Filter by username, payee name or account...'
            ),
            filters: [
              {
                columnId: 'status',
                title: t('Status'),
                options: [
                  {
                    label: t('Pending'),
                    value: String(WITHDRAWAL_STATUS.PENDING),
                  },
                  {
                    label: t('Paying out'),
                    value: String(WITHDRAWAL_STATUS.PROCESSING),
                  },
                  {
                    label: t('Paid'),
                    value: String(WITHDRAWAL_STATUS.APPROVED),
                  },
                  {
                    label: t('Rejected'),
                    value: String(WITHDRAWAL_STATUS.REJECTED),
                  },
                  {
                    label: t('Cancelled'),
                    value: String(WITHDRAWAL_STATUS.CANCELLED),
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
                {reviewTarget?.action === 'approve'
                  ? t('Mark as paid?')
                  : t('Reject withdrawal?')}
              </AlertDialogTitle>
              <AlertDialogDescription>
                {reviewTarget && (
                  <>
                    {formatQuota(reviewTarget.withdrawal.amount)} ·{' '}
                    {reviewTarget.withdrawal.payee_name} (
                    {reviewTarget.withdrawal.payee_account}
                    {') — '}
                    {reviewTarget.action === 'approve'
                      ? t(
                          'Confirm the payout has been completed. This cannot be undone.'
                        )
                      : t(
                          'The amount will be returned to the agent commission balance.'
                        )}
                    {reviewTarget.action === 'approve' &&
                      reviewTarget.withdrawal.fee > 0 && (
                        <>
                          {' '}
                          {t('Payout amount')}:{' '}
                          {formatQuota(
                            reviewTarget.withdrawal.amount -
                              reviewTarget.withdrawal.fee
                          )}
                        </>
                      )}
                  </>
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <div className='grid gap-1.5'>
              <Label htmlFor='review-remark'>
                {reviewTarget?.action === 'approve'
                  ? t('Payout reference')
                  : t('Remark (optional)')}
              </Label>
              <Input
                id='review-remark'
                value={adminRemark}
                onChange={(e) => setAdminRemark(e.target.value)}
                placeholder={
                  reviewTarget?.action === 'approve'
                    ? t('e.g. Alipay transfer no.')
                    : undefined
                }
              />
            </div>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={reviewing}>
                {t('Cancel')}
              </AlertDialogCancel>
              <AlertDialogAction
                variant={
                  reviewTarget?.action === 'reject' ? 'destructive' : 'default'
                }
                onClick={onConfirmReview}
                disabled={reviewing}
              >
                {reviewTarget?.action === 'approve'
                  ? t('Mark as paid')
                  : t('Reject')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>

        {isRoot && (
          <WithdrawSettingsDialog
            open={settingsOpen}
            onOpenChange={setSettingsOpen}
          />
        )}
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

// 「提现策略」配置弹窗(超管):最低提现额、手续费率、单人未决单上限。
// 复用 RootAuth 的通用 option 端点，与充值金额一致按显示货币输入、内部转 quota 存储。
function WithdrawSettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [minAmount, setMinAmount] = useState('')
  const [feePercent, setFeePercent] = useState('')
  const [maxPending, setMaxPending] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const loadSettings = useCallback(() => {
    setLoading(true)
    getWithdrawSettings()
      .then((res) => {
        if (res.success && res.data) {
          setMinAmount(String(quotaUnitsToDollars(res.data.minQuota)))
          // 与 wallet.tsx 的费率展示一致，避免浮点噪声(如 1.4999999999998)。
          setFeePercent(String(Number((res.data.feeRate * 100).toFixed(1))))
          setMaxPending(String(res.data.maxPending))
        } else {
          toast.error(res.message || t('Failed to load'))
        }
      })
      .catch(() => toast.error(t('Failed to load')))
      .finally(() => setLoading(false))
  }, [t])

  useEffect(() => {
    if (!open) return
    loadSettings()
  }, [open, loadSettings])

  async function onSave() {
    const minVal = parseFloat(minAmount)
    const feeVal = parseFloat(feePercent)
    const maxVal = parseInt(maxPending, 10)
    if (isNaN(minVal) || minVal < 0) {
      toast.error(t('Please enter a valid amount'))
      return
    }
    if (isNaN(feeVal) || feeVal < 0 || feeVal > 100) {
      toast.error(t('Fee rate must be between 0 and 100'))
      return
    }
    if (isNaN(maxVal) || maxVal < 0) {
      toast.error(t('Please enter a valid number'))
      return
    }
    setSaving(true)
    try {
      const res = await updateWithdrawSettings({
        minQuota: parseQuotaFromDollars(minVal),
        feeRate: feeVal / 100,
        maxPending: maxVal,
      })
      if (res.success) {
        toast.success(t('Saved'))
        onOpenChange(false)
      } else if (res.data && res.data.appliedKeys.length > 0) {
        // 逐项写入部分成功：指明生效/失败项,并回读实际配置刷新表单。
        const labelOf: Record<string, string> = {
          minQuota: t('Minimum withdrawal amount'),
          feeRate: t('Withdrawal fee rate'),
          maxPending: t('Max pending requests per agent'),
        }
        toast.error(
          t('Partially saved. Applied: {{applied}}; failed: {{failed}}', {
            applied: res.data.appliedKeys.map((k) => labelOf[k]).join(', '),
            failed: res.data.failedKeys.map((k) => labelOf[k]).join(', '),
          })
        )
        loadSettings()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Withdrawal settings')}
      description={t('Applies to all agents. Takes effect immediately.')}
      contentHeight='auto'
      footer={
        <div className='flex justify-end gap-2'>
          <Button
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={saving}
          >
            {t('Cancel')}
          </Button>
          <Button onClick={onSave} disabled={saving || loading}>
            {t('Save')}
          </Button>
        </div>
      }
    >
      <div className='grid gap-4 py-2'>
        <div className='grid gap-1.5'>
          <Label htmlFor='ws-min'>{t('Minimum withdrawal amount')}</Label>
          <Input
            id='ws-min'
            value={minAmount}
            onChange={(e) => setMinAmount(e.target.value)}
            inputMode='decimal'
            disabled={loading}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Agents cannot request less than this per withdrawal.')}
          </p>
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor='ws-fee'>{t('Withdrawal fee rate')} (%)</Label>
          <Input
            id='ws-fee'
            value={feePercent}
            onChange={(e) => setFeePercent(e.target.value)}
            inputMode='decimal'
            disabled={loading}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Deducted from the payout as a handling fee. 0 = no fee.')}
          </p>
        </div>
        <div className='grid gap-1.5'>
          <Label htmlFor='ws-max'>{t('Max pending requests per agent')}</Label>
          <Input
            id='ws-max'
            value={maxPending}
            onChange={(e) => setMaxPending(e.target.value)}
            inputMode='numeric'
            disabled={loading}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Caps unreviewed + in-progress requests. 0 = unlimited.')}
          </p>
        </div>
      </div>
    </Dialog>
  )
}

function buildColumns(
  t: (k: string) => string,
  openReview: (w: Withdrawal, a: 'approve' | 'reject') => void,
  quickAction: (w: Withdrawal, a: 'claim' | 'release') => void
): ColumnDef<Withdrawal>[] {
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
      id: 'time',
      accessorKey: 'created_at',
      header: t('Time'),
      cell: ({ row }) => (
        <div className='whitespace-nowrap'>
          <span className='text-muted-foreground text-sm'>
            {formatTimestamp(row.original.created_at)}
          </span>
          <WaitingHint w={row.original} />
        </div>
      ),
      size: 160,
      meta: { mobileTitle: true },
    },
    {
      id: 'user',
      header: t('User'),
      cell: ({ row }) => (
        <div className='flex items-center gap-1.5'>
          <LongText className='max-w-[120px] font-medium'>
            {row.original.username || '-'}
          </LongText>
          <span className='text-muted-foreground text-xs'>
            #{row.original.user_id}
          </span>
        </div>
      ),
      size: 150,
    },
    {
      id: 'amount',
      accessorKey: 'amount',
      header: t('Amount'),
      cell: ({ row }) => (
        <span className='tabular-nums'>{formatQuota(row.original.amount)}</span>
      ),
      size: 110,
      meta: { mobileHidden: true },
    },
    {
      id: 'fee',
      header: t('Fee'),
      cell: ({ row }) => (
        <span className='text-muted-foreground tabular-nums'>
          {formatQuota(row.original.fee)}
        </span>
      ),
      size: 90,
      meta: { mobileHidden: true },
    },
    {
      id: 'payout',
      header: t('Payout amount'),
      cell: ({ row }) => <PayoutCell w={row.original} />,
      size: 140,
      meta: { mobileBadge: true },
    },
    {
      id: 'method',
      header: t('Method'),
      cell: ({ row }) =>
        t(WITHDRAWAL_METHOD_LABELS[row.original.method] ?? row.original.method),
      size: 110,
    },
    {
      id: 'payee',
      accessorKey: 'payee_name',
      header: t('Payee Name'),
      cell: ({ row }) => row.original.payee_name,
      size: 130,
    },
    {
      id: 'account',
      header: t('Payee Account'),
      cell: ({ row }) => (
        <PayeeAccountCell account={row.original.payee_account} />
      ),
      size: 200,
      meta: { mobileHidden: true },
    },
    {
      id: 'remark',
      header: t('Remark'),
      cell: ({ row }) => (
        <span className='text-muted-foreground'>
          {[row.original.remark, row.original.admin_remark]
            .filter(Boolean)
            .join(' / ') || '-'}
        </span>
      ),
      size: 160,
      meta: { mobileHidden: true },
    },
    {
      id: 'reviewer',
      header: t('Reviewer'),
      cell: ({ row }) =>
        row.original.reviewer_name ? (
          <span className='text-sm'>{row.original.reviewer_name}</span>
        ) : (
          <span className='text-muted-foreground text-xs'>-</span>
        ),
      size: 110,
      meta: { mobileHidden: true },
    },
    {
      id: 'status',
      accessorKey: 'status',
      header: t('Status'),
      cell: ({ row }) => (
        <BadgeCell>
          <WithdrawalStatusBadge status={row.original.status} />
        </BadgeCell>
      ),
      size: 100,
      enableSorting: false,
      meta: { mobileBadge: true },
    },
    {
      id: 'actions',
      header: () => t('Actions'),
      cell: ({ row }) => (
        <RowActions
          row={row}
          openReview={openReview}
          quickAction={quickAction}
        />
      ),
      meta: { pinned: 'right' as const },
    },
  ]
}

// 人工打款两阶段操作：待审核单先「认领打款」（锁定经办人，防两个管理员各自线下
// 转账造成重复打款），认领后线下转账，回来「标记已打款」（必填打款流水号）。
function RowActions({
  row,
  openReview,
  quickAction,
}: {
  row: Row<Withdrawal>
  openReview: (w: Withdrawal, a: 'approve' | 'reject') => void
  quickAction: (w: Withdrawal, a: 'claim' | 'release') => void
}) {
  const { t } = useTranslation()
  const w = row.original
  if (w.status === WITHDRAWAL_STATUS.PENDING) {
    return (
      <div className='-ml-1.5 flex items-center'>
        <DataTableRowActionMenu ariaLabel={t('Open menu')}>
          <DropdownMenuItem onClick={() => quickAction(w, 'claim')}>
            <Check className='size-4' />
            {t('Claim payout')}
          </DropdownMenuItem>
          <DropdownMenuItem
            variant='destructive'
            onClick={() => openReview(w, 'reject')}
          >
            <X className='size-4' />
            {t('Reject')}
          </DropdownMenuItem>
        </DataTableRowActionMenu>
      </div>
    )
  }
  if (w.status === WITHDRAWAL_STATUS.PROCESSING) {
    return (
      <div className='-ml-1.5 flex items-center'>
        <DataTableRowActionMenu ariaLabel={t('Open menu')}>
          <DropdownMenuItem onClick={() => openReview(w, 'approve')}>
            <Check className='size-4' />
            {t('Mark as paid')}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => quickAction(w, 'release')}>
            <Undo2 className='size-4' />
            {t('Release claim')}
          </DropdownMenuItem>
          <DropdownMenuItem
            variant='destructive'
            onClick={() => openReview(w, 'reject')}
          >
            <X className='size-4' />
            {t('Reject')}
          </DropdownMenuItem>
        </DataTableRowActionMenu>
      </div>
    )
  }
  // 终态单不可再操作;备注在「备注」列展示,这里保持窄占位避免撑宽自适应列。
  return <span className='text-muted-foreground text-xs'>-</span>
}

// PayoutCell 实付金额 = 申请额 − 手续费；有汇率快照时按申请时汇率折算 ¥，
// 消除"按什么时点汇率结算"的争议。折算假设 quota 基准为 USD，显示货币非 USD
// 时该折算不成立，隐藏折算行避免误导。
function PayoutCell({ w }: { w: Withdrawal }) {
  const { t } = useTranslation()
  const payout = w.amount - w.fee
  const { config, meta } = getCurrencyDisplay()
  const isUsdDisplay = meta.kind === 'currency' && meta.symbol === '$'
  const usd = payout / config.quotaPerUnit
  const cny = w.exchange_rate ? usd * w.exchange_rate : 0
  return (
    <div className='whitespace-nowrap'>
      <span className='font-semibold tabular-nums'>{formatQuota(payout)}</span>
      {cny > 0 && isUsdDisplay && (
        <span
          className='text-muted-foreground ml-1 text-xs tabular-nums'
          title={t('Converted at the exchange rate when requested')}
        >
          ≈ ¥{cny.toFixed(2)}
        </span>
      )}
    </div>
  )
}

// PayeeAccountCell 收款账号 + 一键复制：人工打款要手抄账号到支付宝/网银，
// 复制按钮消灭转写错误。
function PayeeAccountCell({ account }: { account: string }) {
  const { t } = useTranslation()
  return (
    <div className='flex items-center gap-1'>
      <span className='font-mono text-xs'>{account}</span>
      <button
        type='button'
        className='text-muted-foreground hover:text-foreground shrink-0 cursor-pointer p-0.5'
        aria-label={t('Copy')}
        onClick={() => {
          navigator.clipboard
            .writeText(account)
            .then(() => toast.success(t('Copied')))
            .catch(() => toast.error(t('Failed')))
        }}
      >
        <CopyIcon className='size-3.5' />
      </button>
    </div>
  )
}

// WaitingHint 未决单的已等待时长提示；超过 3 天转警示色，帮最老的单浮出水面。
// 每分钟 tick 一次，页面长开时时长也会自动推进。
function WaitingHint({ w }: { w: Withdrawal }) {
  const { t } = useTranslation()
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 60_000)
    return () => clearInterval(timer)
  }, [])
  if (
    w.status !== WITHDRAWAL_STATUS.PENDING &&
    w.status !== WITHDRAWAL_STATUS.PROCESSING
  ) {
    return null
  }
  const hours = Math.floor((now / 1000 - w.created_at) / 3600)
  if (hours < 1) return null
  const text =
    hours >= 24 ? `${Math.floor(hours / 24)}d ${hours % 24}h` : `${hours} h`
  return (
    <div
      className={
        hours >= 72
          ? 'text-xs font-medium text-amber-600 dark:text-amber-400'
          : 'text-muted-foreground text-xs'
      }
    >
      {t('Waited')} {text}
    </div>
  )
}
