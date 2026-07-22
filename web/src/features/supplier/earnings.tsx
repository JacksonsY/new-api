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
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
import { Button } from '@/components/design-system/button'
import { SectionPageLayout } from '@/components/layout'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { formatQuota } from '@/lib/format'
import { cn } from '@/lib/utils'

import { getSupplierDailyEarnings, getSupplierEarnings } from './api'
import { SettlementSummary } from './settlement-summary'
import {
  ledgerTypeLabelKey,
  SUPPLIER_LEDGER_TYPE,
  type SupplierDailyEarning,
  type SupplierLedger,
  type SupplierSettlement,
} from './types'

const PAGE_SIZE = 10
const SKELETON_KEYS = ['le-1', 'le-2', 'le-3', 'le-4', 'le-5']

// 「我的收益」— 只读：结算汇总 + 流水明细，无提现入口（打款由管理员线下执行）。
export function SupplierEarnings() {
  const { t } = useTranslation()
  const [settlement, setSettlement] = useState<SupplierSettlement | null>(null)
  const [ledger, setLedger] = useState<SupplierLedger[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(true)

  const load = useCallback(
    async (p: number) => {
      setLoading(true)
      try {
        const res = await getSupplierEarnings(p)
        if (res.success && res.data) {
          setSettlement(res.data.settlement)
          setLedger(res.data.ledger || [])
          setTotal(res.data.total || 0)
        } else if (!res.success) {
          toast.error(res.message || t('Failed to load'))
        }
      } catch {
        toast.error(t('Failed to load'))
      } finally {
        setLoading(false)
      }
    },
    [t]
  )

  useEffect(() => {
    load(page)
  }, [load, page])

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('My Earnings')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-7xl flex-col gap-4 sm:gap-5'>
          {/* 收款账户的唯一编辑入口在「收款设置」页;收益页只读,不再内嵌重复编辑器 */}
          <SettlementSummary settlement={settlement} loading={loading} />

          {/* 结算规则透明化:成熟期/打款方式/没收口径,不让"何时拿到钱"靠猜 */}
          <div className='text-muted-foreground rounded-lg border px-4 py-3 text-xs leading-relaxed'>
            {t(
              'Earnings mature 3 days after the request (refund/risk window), then count toward Payable. Payouts are made manually by the platform after reconciliation — there is no self-service withdrawal. Confiscations reduce Payable and always appear in the ledger with a remark.'
            )}
          </div>

          {/* v2 §4.3 经营透明:按渠道×按天明细,口径与结算完全一致——
              供应商可自行核账,不再只能相信平台报的汇总数。 */}
          <DailyEarningsCard />


          <Card data-card-hover='false'>
            <CardHeader>
              <CardTitle className='text-base'>
                {t('Settlement Ledger')}
              </CardTitle>
            </CardHeader>
            <CardContent>
              {loading && (
                <div className='space-y-2'>
                  {SKELETON_KEYS.map((key) => (
                    <Skeleton key={key} className='h-10 w-full' />
                  ))}
                </div>
              )}
              {!loading && ledger.length === 0 && (
                <div className='text-muted-foreground py-10 text-center text-sm'>
                  {t('No records yet')}
                </div>
              )}
              {!loading && ledger.length > 0 && (
                <>
                  <StaticDataTable
                    data={ledger}
                    columns={[
                      {
                        id: 'time',
                        header: t('Time'),
                        cell: (l: SupplierLedger) =>
                          new Date(l.created_at * 1000).toLocaleString(),
                      },
                      {
                        id: 'type',
                        header: t('Type'),
                        cell: (l: SupplierLedger) => t(ledgerTypeLabelKey(l.type)),
                      },
                      {
                        id: 'amount',
                        header: t('Amount'),
                        className: 'text-right',
                        cellClassName: 'text-right font-medium tabular-nums',
                        cell: (l: SupplierLedger) => (
                          <span
                            className={cn(
                              l.type === SUPPLIER_LEDGER_TYPE.CONFISCATION
                                ? 'text-destructive'
                                : 'text-foreground'
                            )}
                          >
                            {l.type === SUPPLIER_LEDGER_TYPE.CONFISCATION
                              ? '-'
                              : ''}
                            {formatQuota(Math.abs(l.quota))}
                          </span>
                        ),
                      },
                      {
                        id: 'voucher',
                        header: t('Voucher'),
                        cellClassName: 'font-mono text-xs',
                        cell: (l: SupplierLedger) => l.voucher || '-',
                      },
                      {
                        id: 'remark',
                        header: t('Remark'),
                        cellClassName: 'text-muted-foreground',
                        cell: (l: SupplierLedger) => l.remark || '-',
                      },
                    ]}
                  />
                  {total > PAGE_SIZE && (
                    <div className='mt-3 flex items-center justify-between border-t pt-3'>
                      <div className='text-muted-foreground text-xs'>
                        {page}/{totalPages}
                      </div>
                      <div className='flex items-center gap-2'>
                        <Button
                          variant='outline'
                          size='sm'
                          className='h-8 w-8 p-0'
                          onClick={() => setPage((p) => p - 1)}
                          disabled={page <= 1}
                        >
                          <ChevronLeft className='h-4 w-4' />
                        </Button>
                        <Button
                          variant='outline'
                          size='sm'
                          className='h-8 w-8 p-0'
                          onClick={() => setPage((p) => p + 1)}
                          disabled={page >= totalPages}
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
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

// v2 §4.3:按渠道×按天毛收入明细。口径 = Σ min(channel_quota, quota),
// 与上方结算汇总同源,供应商可逐日核账。
function DailyEarningsCard() {
  const { t } = useTranslation()
  const [days, setDays] = useState(30)
  const [rows, setRows] = useState<SupplierDailyEarning[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let active = true
    setLoading(true)
    void getSupplierDailyEarnings(days)
      .then((res) => {
        if (!active) return
        if (res.success) setRows(res.data?.items || [])
        else toast.error(res.message || t('Failed to load'))
      })
      .catch(() => {
        if (active) toast.error(t('Failed to load'))
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [days, t])

  return (
    <Card data-card-hover='false'>
      <CardHeader className='flex flex-row items-center justify-between'>
        <CardTitle className='text-base'>
          {t('Daily Earnings by Channel')}
        </CardTitle>
        <div className='flex items-center gap-1'>
          {[7, 30, 90].map((d) => (
            <Button
              key={d}
              size='sm'
              variant={days === d ? 'default' : 'outline'}
              onClick={() => setDays(d)}
            >
              {t('{{count}}d', { count: d })}
            </Button>
          ))}
        </div>
      </CardHeader>
      <CardContent className='p-0'>
        {loading ? (
          <div className='space-y-2 p-4'>
            {SKELETON_KEYS.map((k) => (
              <Skeleton key={k} className='h-8 w-full' />
            ))}
          </div>
        ) : (
          <StaticDataTable
            data={rows}
            empty={rows.length === 0}
            emptyContent={
              <span className='text-muted-foreground text-sm'>
                {t('No earnings in this period.')}
              </span>
            }
            columns={[
              {
                id: 'day',
                header: t('Date'),
                cellClassName: 'text-muted-foreground text-sm',
                cell: (r: SupplierDailyEarning) =>
                  new Date(r.day * 1000).toISOString().slice(0, 10),
              },
              {
                id: 'channel',
                header: t('Channel'),
                cell: (r: SupplierDailyEarning) =>
                  r.channel_name || `#${r.channel_id}`,
              },
              {
                id: 'count',
                header: t('Requests'),
                className: 'text-right',
                cellClassName: 'text-right text-muted-foreground tabular-nums',
                cell: (r: SupplierDailyEarning) => String(r.count),
              },
              {
                id: 'gross',
                header: t('Gross'),
                className: 'text-right',
                cellClassName: 'text-right font-medium tabular-nums',
                cell: (r: SupplierDailyEarning) => formatQuota(r.gross),
              },
            ]}
          />
        )}
      </CardContent>
    </Card>
  )
}
