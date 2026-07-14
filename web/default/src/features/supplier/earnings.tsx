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

import { getSupplierEarnings } from './api'
import { PayoutAccountCard } from './components/payout-account-card'
import { SettlementSummary } from './settlement-summary'
import {
  ledgerTypeLabelKey,
  SUPPLIER_LEDGER_TYPE,
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
          <SettlementSummary settlement={settlement} loading={loading} />

          <PayoutAccountCard />

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
