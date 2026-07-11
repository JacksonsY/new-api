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
import {
  ArrowLeftRight,
  ChevronLeft,
  ChevronRight,
  Clock,
  Landmark,
  Percent,
  ReceiptText,
  TrendingUp,
  Wallet,
  type LucideIcon,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/design-system/select'
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from '@/components/design-system/tabs'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { getCurrencyDisplay } from '@/lib/currency'
import { formatQuota, parseQuotaFromDollars } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  agentListCommissions,
  agentListWithdrawals,
  cancelWithdrawal,
  convertCommission,
  createWithdrawal,
} from './api'
import {
  COMMISSION_STATUS,
  WITHDRAWAL_METHOD_LABELS,
  WITHDRAWAL_STATUS,
  type CommissionRecord,
  type CommissionsResult,
  type Withdrawal,
  type WithdrawalMethod,
} from './types'
import { WithdrawalStatusBadge } from './withdrawal-status-badge'

const WD_PAGE_SIZE = 10
const CM_PAGE_SIZE = 8
const SUMMARY_SKELETON_KEYS = ['balance', 'maturing', 'earnings', 'rate']
const COMMISSION_SKELETON_KEYS = [
  'commission-1',
  'commission-2',
  'commission-3',
  'commission-4',
  'commission-5',
]
const WITHDRAWAL_SKELETON_KEYS = [
  'withdrawal-1',
  'withdrawal-2',
  'withdrawal-3',
  'withdrawal-4',
]

// 收款人字段格式校验(非真实身份核验，仅拦截明显填错/乱填；须与 model/withdrawal.go 的后端校验保持一致)。
const PAYEE_NAME_CHARSET_RE = /[\p{Script=Han}A-Za-z]/u
const PAYEE_PHONE_RE = /^1[3-9]\d{9}$/
const PAYEE_EMAIL_RE = /^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$/
const PAYEE_WECHAT_ID_RE = /^[a-zA-Z][-_a-zA-Z0-9]{5,19}$/
const PAYEE_BANK_CARD_RE = /^\d{16,19}$/

function luhnValid(digits: string): boolean {
  let sum = 0
  let double = false
  for (let i = digits.length - 1; i >= 0; i--) {
    let d = digits.charCodeAt(i) - 48
    if (double) {
      d *= 2
      if (d > 9) d -= 9
    }
    sum += d
    double = !double
  }
  return sum % 10 === 0
}

// 「代理钱包」— 分润汇总 / 提现申请 / 转额度 / 申请记录。
export function AgentWallet() {
  const { t } = useTranslation()
  const graceOnly = useAuthStore(
    (state) =>
      Boolean(state.auth.user?.agent_grace_access) &&
      !state.auth.user?.agent_type
  )
  const [summary, setSummary] = useState<CommissionsResult | null>(null)
  const [summaryLoading, setSummaryLoading] = useState(true)

  const [commissions, setCommissions] = useState<CommissionRecord[]>([])
  const [cmTotal, setCmTotal] = useState(0)
  const [cmPage, setCmPage] = useState(1)
  const [cmLoading, setCmLoading] = useState(true)

  const [withdrawals, setWithdrawals] = useState<Withdrawal[]>([])
  const [wdTotal, setWdTotal] = useState(0)
  const [wdPage, setWdPage] = useState(1)
  const [historyLoading, setHistoryLoading] = useState(true)

  const [convertAmount, setConvertAmount] = useState('')
  const [wdAmount, setWdAmount] = useState('')
  const [wdMethod, setWdMethod] = useState<WithdrawalMethod>('alipay')
  const [payeeName, setPayeeName] = useState('')
  const [payeeAccount, setPayeeAccount] = useState('')
  const [remark, setRemark] = useState('')
  const [busy, setBusy] = useState(false)
  const [cancelTarget, setCancelTarget] = useState<Withdrawal | null>(null)

  // 汇总与分润流水来自同一接口(每页都带汇总字段)，一次请求同时刷新两块。
  const loadCommissions = useCallback(
    async (page: number) => {
      setCmLoading(true)
      try {
        const res = await agentListCommissions(page, CM_PAGE_SIZE)
        if (res.success && res.data) {
          setSummary(res.data)
          setCommissions(res.data.items || [])
          setCmTotal(res.data.total || 0)
        } else if (!res.success) {
          toast.error(res.message || t('Failed to load'))
        }
      } catch {
        toast.error(t('Failed to load'))
      } finally {
        setSummaryLoading(false)
        setCmLoading(false)
      }
    },
    [t]
  )

  const loadWithdrawals = useCallback(
    async (page: number) => {
      setHistoryLoading(true)
      try {
        const res = await agentListWithdrawals(page, WD_PAGE_SIZE)
        if (res.success && res.data) {
          setWithdrawals(res.data.items || [])
          setWdTotal(res.data.total || 0)
        } else if (!res.success) {
          toast.error(res.message || t('Failed to load'))
        }
      } catch {
        toast.error(t('Failed to load'))
      } finally {
        setHistoryLoading(false)
      }
    },
    [t]
  )

  useEffect(() => {
    loadCommissions(cmPage)
  }, [loadCommissions, cmPage])

  useEffect(() => {
    loadWithdrawals(wdPage)
  }, [loadWithdrawals, wdPage])

  const balance = summary?.commission_quota || 0
  const canAct = balance > 0
  const { meta: currencyMeta } = getCurrencyDisplay()
  const currencyHint =
    currencyMeta.kind === 'tokens' ? '' : ` (${currencyMeta.symbol})`
  const ratePct = summary
    ? `${((summary.usage_profit_rate || 0) * 100).toFixed(1)}%`
    : '-'
  const totalPages = Math.max(1, Math.ceil(wdTotal / WD_PAGE_SIZE))
  const cmTotalPages = Math.max(1, Math.ceil(cmTotal / CM_PAGE_SIZE))
  const withdrawMinQuota = summary?.withdraw_min_quota || 0
  const withdrawFeeRate = summary?.withdraw_fee_rate || 0
  let payeeAccountPlaceholder = t('Bank card number')
  if (wdMethod === 'alipay') {
    payeeAccountPlaceholder = t('Phone number or email')
  } else if (wdMethod === 'wxpay') {
    payeeAccountPlaceholder = t('WeChat ID or phone number')
  }

  // 与后端 model/withdrawal.go 一致：金额为正数、最多两位小数(quota 为整数存储)。
  const AMOUNT_RE = /^\d+(\.\d{1,2})?$/

  function parseAmount(v: string): number | null {
    const trimmed = v.trim()
    if (!AMOUNT_RE.test(trimmed)) {
      toast.error(t('Amount must be a positive number with up to 2 decimals'))
      return null
    }
    const q = parseQuotaFromDollars(Number.parseFloat(trimmed))
    if (!q || q <= 0) {
      toast.error(t('Please enter a valid amount'))
      return null
    }
    if (q > balance) {
      toast.error(t('Amount exceeds withdrawable balance'))
      return null
    }
    return q
  }

  function validatePayeeName(name: string): boolean {
    const length = [...name].length
    if (length < 2 || length > 30) {
      toast.error(t('Payee name must be 2-30 characters'))
      return false
    }
    if (!PAYEE_NAME_CHARSET_RE.test(name)) {
      toast.error(t("Payee name doesn't look valid"))
      return false
    }
    return true
  }

  function validatePayeeAccount(
    method: WithdrawalMethod,
    account: string
  ): boolean {
    if (method === 'alipay') {
      if (PAYEE_PHONE_RE.test(account) || PAYEE_EMAIL_RE.test(account)) {
        return true
      }
      toast.error(t('Alipay account must be a phone number or email'))
      return false
    }
    if (method === 'wxpay') {
      if (PAYEE_PHONE_RE.test(account) || PAYEE_WECHAT_ID_RE.test(account)) {
        return true
      }
      toast.error(t("WeChat account doesn't look valid"))
      return false
    }
    if (PAYEE_BANK_CARD_RE.test(account) && luhnValid(account)) {
      return true
    }
    toast.error(t("Bank card number doesn't look valid"))
    return false
  }

  // 回到第 1 页并刷新；cmPage 已是 1 时 effect 不会重跑,需手动加载,避免双请求。
  function refreshCommissions() {
    if (cmPage === 1) {
      loadCommissions(1)
    } else {
      setCmPage(1)
    }
  }

  async function onCancelWithdrawal() {
    if (!cancelTarget) return
    setBusy(true)
    try {
      const res = await cancelWithdrawal(cancelTarget.id)
      if (res.success) {
        toast.success(t('Withdrawal cancelled'))
        setCancelTarget(null)
        refreshCommissions()
        loadWithdrawals(wdPage)
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setBusy(false)
    }
  }

  async function onConvert() {
    const amount = parseAmount(convertAmount)
    if (amount === null) return
    setBusy(true)
    try {
      const res = await convertCommission(amount)
      if (res.success) {
        toast.success(t('Converted to API quota'))
        setConvertAmount('')
        refreshCommissions()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setBusy(false)
    }
  }

  async function onWithdraw() {
    const amount = parseAmount(wdAmount)
    if (amount === null) return
    if (withdrawMinQuota > 0 && amount < withdrawMinQuota) {
      toast.error(
        `${t('Amount is below the minimum withdrawal')} (${formatQuota(withdrawMinQuota)})`
      )
      return
    }
    const trimmedName = payeeName.trim()
    const trimmedAccount = payeeAccount.trim()
    if (!trimmedName || !trimmedAccount) {
      toast.error(t('Please enter payee name and account'))
      return
    }
    if (!validatePayeeName(trimmedName)) return
    if (!validatePayeeAccount(wdMethod, trimmedAccount)) return
    setBusy(true)
    try {
      const res = await createWithdrawal({
        amount,
        method: wdMethod,
        payee_name: trimmedName,
        payee_account: trimmedAccount,
        remark: remark.trim(),
      })
      if (res.success) {
        toast.success(t('Withdrawal request submitted'))
        setWdAmount('')
        setPayeeName('')
        setPayeeAccount('')
        setRemark('')
        refreshCommissions()
        if (wdPage === 1) {
          loadWithdrawals(1)
        } else {
          setWdPage(1)
        }
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
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Agent Wallet')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
          {/* 汇总统计 */}
          <div className='overflow-hidden rounded-lg border'>
            {summaryLoading ? (
              <div className='divide-border/60 grid grid-cols-2 divide-x divide-y sm:grid-cols-4 sm:divide-y-0'>
                {SUMMARY_SKELETON_KEYS.map((key) => (
                  <div key={key} className='px-2.5 py-3 sm:px-5 sm:py-4'>
                    <Skeleton className='h-3.5 w-16' />
                    <Skeleton className='mt-2 h-6 w-20' />
                  </div>
                ))}
              </div>
            ) : (
              <div className='divide-border/60 grid grid-cols-2 divide-x divide-y sm:grid-cols-4 sm:divide-y-0'>
                <StatCell
                  icon={Wallet}
                  label={t('Withdrawable Commission')}
                  value={formatQuota(balance)}
                  emphasize
                />
                <StatCell
                  icon={Clock}
                  label={t('Maturing')}
                  value={formatQuota(summary?.commission_pending_quota || 0)}
                  description={t(
                    'Held during a short settlement window before becoming withdrawable.'
                  )}
                />
                <StatCell
                  icon={TrendingUp}
                  label={t('Total Earnings')}
                  value={formatQuota(summary?.commission_history_quota || 0)}
                />
                <StatCell
                  icon={Percent}
                  label={t('Commission Rate')}
                  value={ratePct}
                />
              </div>
            )}
          </div>

          {/* 分润出口(提现/转换 Tabs) + 分润明细流水 */}
          <div className='grid gap-4 lg:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)]'>
            <Card data-card-hover='false'>
              <CardContent className='pt-4'>
                <Tabs defaultValue='withdraw'>
                  <TabsList className='mb-3'>
                    <TabsTrigger value='withdraw'>
                      <Landmark className='size-3.5' />
                      {t('Request Withdrawal')}
                    </TabsTrigger>
                    {!graceOnly && (
                      <TabsTrigger value='convert'>
                        <ArrowLeftRight className='size-3.5' />
                        {t('Convert to API quota')}
                      </TabsTrigger>
                    )}
                  </TabsList>

                  <TabsContent value='withdraw'>
                    <p className='text-muted-foreground mb-3 text-sm'>
                      {t(
                        'Submit a withdrawal request; admin reviews and pays out.'
                      )}
                    </p>
                    <div className='grid gap-3 sm:grid-cols-2'>
                      <div className='grid gap-1.5'>
                        <Label htmlFor='aw-amount'>
                          {t('Withdraw amount')}
                          {currencyHint}
                        </Label>
                        <Input
                          id='aw-amount'
                          value={wdAmount}
                          onChange={(e) => setWdAmount(e.target.value)}
                          inputMode='decimal'
                          disabled={!canAct}
                        />
                        <p className='text-muted-foreground text-xs'>
                          {t('Available')}: {formatQuota(balance)}
                          {withdrawMinQuota > 0 && (
                            <>
                              {' · '}
                              {t('Minimum')}: {formatQuota(withdrawMinQuota)}
                            </>
                          )}
                          {withdrawFeeRate > 0 && (
                            <>
                              {' · '}
                              {t('Fee')}: {(withdrawFeeRate * 100).toFixed(1)}%
                            </>
                          )}
                        </p>
                      </div>
                      {/* content-start: 行内另一格更高(带说明文字)时，阻止网格把
                          多余高度分摊给本格行，避免下拉框整体下坠错位 */}
                      <div className='grid content-start gap-1.5'>
                        <Label>{t('Method')}</Label>
                        <Select
                          value={wdMethod}
                          onValueChange={(v) =>
                            setWdMethod(v as WithdrawalMethod)
                          }
                          disabled={!canAct}
                        >
                          <SelectTrigger className='w-full'>
                            <SelectValue>
                              {t(WITHDRAWAL_METHOD_LABELS[wdMethod])}
                            </SelectValue>
                          </SelectTrigger>
                          <SelectContent alignItemWithTrigger={false}>
                            {(
                              Object.keys(
                                WITHDRAWAL_METHOD_LABELS
                              ) as WithdrawalMethod[]
                            ).map((method) => (
                              <SelectItem key={method} value={method}>
                                {t(WITHDRAWAL_METHOD_LABELS[method])}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                      <div className='grid gap-1.5'>
                        <Label htmlFor='aw-name'>{t('Payee Name')}</Label>
                        <Input
                          id='aw-name'
                          value={payeeName}
                          onChange={(e) => setPayeeName(e.target.value)}
                          disabled={!canAct}
                        />
                      </div>
                      <div className='grid gap-1.5'>
                        <Label htmlFor='aw-account'>{t('Payee Account')}</Label>
                        <Input
                          id='aw-account'
                          value={payeeAccount}
                          onChange={(e) => setPayeeAccount(e.target.value)}
                          placeholder={payeeAccountPlaceholder}
                          disabled={!canAct}
                        />
                      </div>
                      <div className='grid gap-1.5 sm:col-span-2'>
                        <Label htmlFor='aw-remark'>
                          {t('Remark (optional)')}
                        </Label>
                        <Input
                          id='aw-remark'
                          value={remark}
                          onChange={(e) => setRemark(e.target.value)}
                          disabled={!canAct}
                        />
                      </div>
                      <div className='pt-1 sm:col-span-2'>
                        <Button
                          className='w-full sm:w-auto'
                          onClick={onWithdraw}
                          disabled={busy || !canAct}
                        >
                          {t('Request Withdrawal')}
                        </Button>
                      </div>
                    </div>
                  </TabsContent>

                  {!graceOnly && (
                    <TabsContent value='convert'>
                      <p className='text-muted-foreground mb-3 text-sm'>
                        {t(
                          'Convert commission into your own API quota instantly, no review needed.'
                        )}
                      </p>
                      <div className='grid gap-3'>
                        <div className='grid gap-1.5 sm:max-w-sm'>
                          <Label htmlFor='aw-convert'>
                            {t('Amount')}
                            {currencyHint}
                          </Label>
                          <Input
                            id='aw-convert'
                            value={convertAmount}
                            onChange={(e) => setConvertAmount(e.target.value)}
                            inputMode='decimal'
                            disabled={!canAct}
                          />
                          <p className='text-muted-foreground text-xs'>
                            {t('Available')}: {formatQuota(balance)}
                          </p>
                        </div>
                        <p className='text-muted-foreground/70 bg-muted/40 rounded-md px-3 py-2 text-xs leading-relaxed'>
                          {t(
                            'Converted quota is credited to your account immediately and can be used for API calls right away.'
                          )}
                        </p>
                        <div className='pt-1'>
                          <Button
                            variant='outline'
                            className='w-full sm:w-auto'
                            onClick={onConvert}
                            disabled={busy || !canAct}
                          >
                            {t('Convert')}
                          </Button>
                        </div>
                      </div>
                    </TabsContent>
                  )}
                </Tabs>
              </CardContent>
            </Card>

            <Card data-card-hover='false' className='flex flex-col'>
              <CardHeader>
                <CardTitle className='flex items-center gap-2 text-base'>
                  <ReceiptText className='text-muted-foreground size-4' />
                  {t('Commission Details')}
                </CardTitle>
              </CardHeader>
              <CardContent className='flex flex-1 flex-col'>
                {cmLoading && (
                  <div className='space-y-2'>
                    {COMMISSION_SKELETON_KEYS.map((key) => (
                      <Skeleton key={key} className='h-9 w-full' />
                    ))}
                  </div>
                )}
                {!cmLoading && commissions.length === 0 && (
                  <div className='text-muted-foreground flex flex-1 items-center justify-center py-10 text-sm'>
                    {t('No commissions yet')}
                  </div>
                )}
                {!cmLoading && commissions.length > 0 && (
                  <>
                    <ul className='divide-border/60 divide-y'>
                      {commissions.map((c) => (
                        <li
                          key={c.id}
                          className='flex items-center justify-between gap-3 py-2'
                        >
                          <div className='min-w-0'>
                            <div className='truncate text-sm'>
                              {c.from_username || `#${c.from_user_id}`}
                            </div>
                            <div className='text-muted-foreground text-xs'>
                              {new Date(c.created_at * 1000).toLocaleString()}
                            </div>
                          </div>
                          <div className='flex shrink-0 items-center gap-2'>
                            {c.status === COMMISSION_STATUS.PENDING && (
                              <Badge variant='secondary'>{t('Maturing')}</Badge>
                            )}
                            <span
                              className={cn(
                                'font-mono text-sm font-semibold tabular-nums',
                                c.quota < 0
                                  ? 'text-destructive'
                                  : 'text-foreground'
                              )}
                            >
                              {c.quota < 0 ? '-' : '+'}
                              {formatQuota(Math.abs(c.quota))}
                            </span>
                          </div>
                        </li>
                      ))}
                    </ul>
                    {cmTotal > CM_PAGE_SIZE && (
                      <div className='mt-auto flex items-center justify-between border-t pt-3'>
                        <div className='text-muted-foreground text-xs'>
                          {cmPage}/{cmTotalPages}
                        </div>
                        <div className='flex items-center gap-2'>
                          <Button
                            variant='outline'
                            size='sm'
                            className='h-8 w-8 p-0'
                            onClick={() => setCmPage((p) => p - 1)}
                            disabled={cmPage <= 1}
                          >
                            <ChevronLeft className='h-4 w-4' />
                          </Button>
                          <Button
                            variant='outline'
                            size='sm'
                            className='h-8 w-8 p-0'
                            onClick={() => setCmPage((p) => p + 1)}
                            disabled={cmPage >= cmTotalPages}
                          >
                            <ChevronRight className='h-4 w-4' />
                          </Button>
                        </div>
                      </div>
                    )}
                  </>
                )}
              </CardContent>
            </Card>
          </div>

          {/* 申请记录 */}
          <Card data-card-hover='false'>
            <CardHeader>
              <CardTitle className='text-base'>{t('My Withdrawals')}</CardTitle>
            </CardHeader>
            <CardContent>
              {historyLoading && (
                <div className='space-y-2'>
                  {WITHDRAWAL_SKELETON_KEYS.map((key) => (
                    <Skeleton key={key} className='h-10 w-full' />
                  ))}
                </div>
              )}
              {!historyLoading && withdrawals.length === 0 && (
                <div className='text-muted-foreground py-10 text-center text-sm'>
                  {t('No withdrawals yet')}
                </div>
              )}
              {!historyLoading && withdrawals.length > 0 && (
                <>
                  <StaticDataTable
                    data={withdrawals}
                    columns={[
                      {
                        id: 'time',
                        header: t('Time'),
                        cell: (w: Withdrawal) =>
                          new Date(w.created_at * 1000).toLocaleString(),
                      },
                      {
                        id: 'amount',
                        header: t('Amount'),
                        className: 'text-right',
                        cellClassName: 'text-right font-medium',
                        cell: (w: Withdrawal) => formatQuota(w.amount),
                      },
                      {
                        id: 'fee',
                        header: t('Fee'),
                        className: 'text-right',
                        cellClassName: 'text-muted-foreground text-right',
                        cell: (w: Withdrawal) => formatQuota(w.fee),
                      },
                      {
                        id: 'method',
                        header: t('Method'),
                        cell: (w: Withdrawal) =>
                          t(WITHDRAWAL_METHOD_LABELS[w.method] ?? w.method),
                      },
                      {
                        id: 'payee',
                        header: t('Payee Name'),
                        cell: (w: Withdrawal) => w.payee_name,
                      },
                      {
                        id: 'account',
                        header: t('Payee Account'),
                        cellClassName: 'font-mono text-xs',
                        cell: (w: Withdrawal) => w.payee_account,
                      },
                      {
                        id: 'remark',
                        header: t('Remark'),
                        cellClassName: 'text-muted-foreground',
                        cell: (w: Withdrawal) =>
                          [w.remark, w.admin_remark]
                            .filter(Boolean)
                            .join(' / ') || '-',
                      },
                      {
                        id: 'status',
                        header: t('Status'),
                        cell: (w: Withdrawal) => (
                          <WithdrawalStatusBadge status={w.status} />
                        ),
                      },
                      {
                        id: 'ops',
                        header: '',
                        cell: (w: Withdrawal) =>
                          w.status === WITHDRAWAL_STATUS.PENDING ? (
                            <Button
                              variant='ghost'
                              size='sm'
                              className='text-muted-foreground hover:text-destructive h-7 px-2 text-xs'
                              onClick={() => setCancelTarget(w)}
                            >
                              {t('Cancel')}
                            </Button>
                          ) : null,
                      },
                    ]}
                  />
                  {wdTotal > WD_PAGE_SIZE && (
                    <div className='mt-3 flex flex-col items-center gap-3 border-t pt-3 sm:flex-row sm:justify-between'>
                      <div className='text-muted-foreground text-xs sm:text-sm'>
                        {t('Showing')} {(wdPage - 1) * WD_PAGE_SIZE + 1}-
                        {Math.min(wdPage * WD_PAGE_SIZE, wdTotal)} {t('of')}{' '}
                        {wdTotal}
                      </div>
                      <div className='flex items-center gap-2'>
                        <Button
                          variant='outline'
                          size='sm'
                          className='h-8 w-8 p-0'
                          onClick={() => setWdPage((p) => p - 1)}
                          disabled={wdPage <= 1}
                        >
                          <ChevronLeft className='h-4 w-4' />
                        </Button>
                        <div className='text-muted-foreground flex items-center gap-1 text-sm'>
                          <span className='font-medium'>{wdPage}</span>
                          <span>/</span>
                          <span>{totalPages}</span>
                        </div>
                        <Button
                          variant='outline'
                          size='sm'
                          className='h-8 w-8 p-0'
                          onClick={() => setWdPage((p) => p + 1)}
                          disabled={wdPage >= totalPages}
                        >
                          <ChevronRight className='h-4 w-4' />
                        </Button>
                      </div>
                    </div>
                  )}
                </>
              )}
            </CardContent>
          </Card>
        </div>

        <AlertDialog
          open={!!cancelTarget}
          onOpenChange={(open) => {
            if (!open) setCancelTarget(null)
          }}
        >
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t('Cancel withdrawal?')}</AlertDialogTitle>
              <AlertDialogDescription>
                {cancelTarget && (
                  <>
                    {formatQuota(cancelTarget.amount)}
                    {' — '}
                    {t(
                      'The amount will be returned to your withdrawable balance.'
                    )}
                  </>
                )}
              </AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel disabled={busy}>{t('Back')}</AlertDialogCancel>
              <AlertDialogAction
                variant='destructive'
                onClick={onCancelWithdrawal}
                disabled={busy}
              >
                {t('Cancel withdrawal')}
              </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function StatCell({
  icon: Icon,
  label,
  value,
  description,
  emphasize,
}: {
  icon: LucideIcon
  label: string
  value: string
  description?: string
  emphasize?: boolean
}) {
  return (
    <div className='px-2.5 py-3 sm:px-5 sm:py-4'>
      <div className='flex items-start gap-1.5'>
        <Icon className='text-muted-foreground/60 mt-0.5 size-3.5 shrink-0' />
        <div className='text-muted-foreground line-clamp-2 text-[11px] leading-snug font-medium tracking-wider uppercase sm:text-xs'>
          {label}
        </div>
      </div>
      <div
        className={cn(
          'mt-1.5 truncate font-mono text-sm font-bold tracking-tight tabular-nums sm:mt-2 sm:text-2xl',
          emphasize ? 'text-primary' : 'text-foreground'
        )}
      >
        {value}
      </div>
      {description && (
        <div className='text-muted-foreground/60 mt-1 hidden text-xs md:block'>
          {description}
        </div>
      )}
    </div>
  )
}
