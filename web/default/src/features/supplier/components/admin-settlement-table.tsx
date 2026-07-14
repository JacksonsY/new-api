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
import { ChevronLeft, ChevronRight } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, StaticDataTable, useDataTable } from '@/components/data-table'
import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { Dialog } from '@/components/dialog'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
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
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { formatQuota, formatTimestamp, parseQuotaFromDollars } from '@/lib/format'
import { cn } from '@/lib/utils'

import {
  adminConfiscateSupplier,
  adminGetSettlement,
  adminListSuppliers,
  adminPaySupplier,
} from '../api'
import { SettlementSummary } from '../settlement-summary'
import {
  ledgerTypeLabelKey,
  payoutMethodLabelKey,
  SUPPLIER_LEDGER_TYPE,
  SUPPLIER_STATUS,
  type SupplierLedger,
  type SupplierSettlement,
  type SupplierUser,
} from '../types'
import {
  useAdminSettlementColumns,
  type SettlementPayoutMode,
} from './admin-settlement-columns'

const route = getRouteApi('/_authenticated/suppliers/settlement/')
const AMOUNT_RE = /^\d+(\.\d{1,2})?$/
const LEDGER_PAGE_SIZE = 10
const LEDGER_SKELETON_KEYS = ['sl-1', 'sl-2', 'sl-3', 'sl-4', 'sl-5']

export function AdminSettlementTable() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [payoutTarget, setPayoutTarget] = useState<{
    user: SupplierUser
    mode: SettlementPayoutMode
  } | null>(null)
  const [ledgerUser, setLedgerUser] = useState<SupplierUser | null>(null)

  const columns = useAdminSettlementColumns({
    onPayout: setPayoutTarget,
    onViewLedger: setLedgerUser,
  })

  const { pagination, onPaginationChange, ensurePageInRange } = useTableUrlState(
    {
      search: route.useSearch(),
      navigate: route.useNavigate(),
      pagination: {
        defaultPage: 1,
        defaultPageSize: 20,
        pageSizeStorageKey: 'supplier-settlement:page-size:v1',
      },
      globalFilter: { enabled: false },
    }
  )

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'settlement-suppliers',
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const res = await adminListSuppliers(
        String(SUPPLIER_STATUS.APPROVED),
        '',
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
    queryClient.invalidateQueries({ queryKey: ['settlement-suppliers'] })

  const { table } = useDataTable({
    data: data?.items || [],
    columns,
    pagination,
    onPaginationChange,
    manualPagination: true,
    totalCount: data?.total || 0,
    ensurePageInRange,
  })

  return (
    <>
      <DataTablePage
        table={table}
        columns={columns}
        tableLabel={t('Settlement')}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No suppliers yet')}
        emptyDescription={t(
          'Approved suppliers with a payable balance will appear here.'
        )}
        skeletonKeyPrefix='settlement-suppliers-skeleton'
        applyHeaderSize
        toolbarProps={null}
      />

      <PayoutDialog
        target={payoutTarget}
        onOpenChange={(open) => {
          if (!open) setPayoutTarget(null)
        }}
        onDone={() => {
          setPayoutTarget(null)
          refresh()
        }}
      />

      <SettlementLedgerDrawer
        user={ledgerUser}
        onOpenChange={(open) => {
          if (!open) setLedgerUser(null)
        }}
      />
    </>
  )
}

function PayoutDialog({
  target,
  onOpenChange,
  onDone,
}: {
  target: { user: SupplierUser; mode: SettlementPayoutMode } | null
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const [amount, setAmount] = useState('')
  const [voucher, setVoucher] = useState('')
  const [remark, setRemark] = useState('')
  const [busy, setBusy] = useState(false)
  const isPay = target?.mode === 'pay'

  useEffect(() => {
    setAmount('')
    setVoucher('')
    setRemark('')
  }, [target])

  function parseAmount(): number | null {
    const trimmed = amount.trim()
    if (!AMOUNT_RE.test(trimmed)) {
      toast.error(t('Amount must be a positive number with up to 2 decimals'))
      return null
    }
    const q = parseQuotaFromDollars(Number.parseFloat(trimmed))
    if (!q || q <= 0) {
      toast.error(t('Please enter a valid amount'))
      return null
    }
    return q
  }

  async function onSubmit() {
    if (!target) return
    const q = parseAmount()
    if (q === null) return
    setBusy(true)
    try {
      const res = isPay
        ? await adminPaySupplier({
            user_id: target.user.id,
            amount: q,
            voucher: voucher.trim(),
            remark: remark.trim(),
          })
        : await adminConfiscateSupplier(target.user.id, q, remark.trim())
      if (res.success) {
        toast.success(isPay ? t('Marked as paid') : t('Confiscated'))
        onDone()
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setBusy(false)
    }
  }

  const payable = target?.user.supplier_payable_quota || 0

  return (
    <Dialog
      open={!!target}
      onOpenChange={onOpenChange}
      title={isPay ? t('Mark as Paid') : t('Confiscate')}
      description={target ? `${target.user.username} #${target.user.id}` : ''}
      contentHeight='auto'
      contentClassName='sm:max-w-md'
      footer={
        <div className='flex justify-end gap-2'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button
            variant={isPay ? 'default' : 'destructive'}
            onClick={onSubmit}
            disabled={busy}
          >
            {isPay ? t('Mark as Paid') : t('Confiscate')}
          </Button>
        </div>
      }
    >
      <div className='grid gap-4 py-2'>
        {isPay && target?.user.supplier_payout_account && (
          <div className='bg-muted/40 grid gap-1 rounded-md p-3 text-sm'>
            <div className='text-muted-foreground mb-0.5 text-xs font-medium'>
              {t('Transfer to')}
            </div>
            <div className='flex justify-between gap-3'>
              <span className='text-muted-foreground'>{t('Payout Method')}</span>
              <span className='font-medium'>
                {t(payoutMethodLabelKey(target.user.supplier_payout_method))}
              </span>
            </div>
            <div className='flex justify-between gap-3'>
              <span className='text-muted-foreground'>{t('Payout Account')}</span>
              <span className='font-medium break-all text-right'>
                {target.user.supplier_payout_account}
              </span>
            </div>
            <div className='flex justify-between gap-3'>
              <span className='text-muted-foreground'>
                {t('Account Holder Name')}
              </span>
              <span className='font-medium break-all text-right'>
                {target.user.supplier_payout_name || '-'}
              </span>
            </div>
            {target.user.supplier_contact && (
              <div className='flex justify-between gap-3'>
                <span className='text-muted-foreground'>{t('Contact')}</span>
                <span className='font-medium break-all text-right'>
                  {target.user.supplier_contact}
                </span>
              </div>
            )}
          </div>
        )}
        <div className='grid gap-1.5'>
          <Label htmlFor='po-amount'>{t('Amount')}</Label>
          <Input
            id='po-amount'
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            inputMode='decimal'
          />
          <p className='text-muted-foreground text-xs'>
            {t('Payable')}: {formatQuota(payable)}
          </p>
        </div>
        {isPay && (
          <div className='grid gap-1.5'>
            <Label htmlFor='po-voucher'>{t('Voucher')}</Label>
            <Input
              id='po-voucher'
              value={voucher}
              onChange={(e) => setVoucher(e.target.value)}
              placeholder={t('Transaction ID or payout reference')}
            />
          </div>
        )}
        <div className='grid gap-1.5'>
          <Label htmlFor='po-remark'>{t('Remark (optional)')}</Label>
          <Textarea
            id='po-remark'
            value={remark}
            onChange={(e) => setRemark(e.target.value)}
            rows={2}
          />
        </div>
      </div>
    </Dialog>
  )
}

// 单个供应商的结算明细：结算汇总 + 流水（只读，管理员核对用）。
function SettlementLedgerDrawer({
  user,
  onOpenChange,
}: {
  user: SupplierUser | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const [page, setPage] = useState(1)
  const [loading, setLoading] = useState(false)
  const [settlement, setSettlement] = useState<SupplierSettlement | null>(null)
  const [ledger, setLedger] = useState<SupplierLedger[]>([])
  const [total, setTotal] = useState(0)

  useEffect(() => {
    setPage(1)
  }, [user])

  useEffect(() => {
    if (!user) return
    let cancelled = false
    const load = async () => {
      setLoading(true)
      try {
        const res = await adminGetSettlement(user.id, page, LEDGER_PAGE_SIZE)
        if (cancelled) return
        if (res.success && res.data) {
          setSettlement(res.data.settlement)
          setLedger(res.data.ledger || [])
          setTotal(res.data.total || 0)
        } else if (!res.success) {
          toast.error(res.message || t('Failed to load'))
        }
      } catch {
        if (!cancelled) toast.error(t('Failed to load'))
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void load()
    return () => {
      cancelled = true
    }
  }, [user, page, t])

  const totalPages = Math.max(1, Math.ceil(total / LEDGER_PAGE_SIZE))

  return (
    <Sheet open={!!user} onOpenChange={onOpenChange}>
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[560px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>{t('Settlement Ledger')}</SheetTitle>
          <SheetDescription>
            {user ? `${user.username} #${user.id}` : ''}
          </SheetDescription>
        </SheetHeader>

        <div className={sideDrawerFormClassName()}>
          <SideDrawerSection>
            <div className='grid gap-4'>
              <SettlementSummary settlement={settlement} loading={loading} />

              {loading && (
                <div className='space-y-2'>
                  {LEDGER_SKELETON_KEYS.map((key) => (
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
                          formatTimestamp(l.created_at),
                      },
                      {
                        id: 'type',
                        header: t('Type'),
                        cell: (l: SupplierLedger) =>
                          t(ledgerTypeLabelKey(l.type)),
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
                  {total > LEDGER_PAGE_SIZE && (
                    <div className='flex items-center justify-between border-t pt-3'>
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
            </div>
          </SideDrawerSection>
        </div>

        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
