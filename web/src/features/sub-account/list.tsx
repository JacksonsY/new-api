// jzlh-sub 团队管理（子账号列表）页——对齐 302 截图1。
import {
  Copy,
  Pencil,
  Plus,
  RotateCw,
  Search,
  Trash2,
} from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Button } from '@/components/design-system/button'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/design-system/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/design-system/table'
import { useAuthStore } from '@/stores/auth-store'

import { deleteSubAccount, listSubAccounts } from './api'
import { CreateSubAccountDialog } from './create-dialog'
import { EditSubAccountDialog } from './edit-dialog'
import {
  ROLE_PRESET_ADMIN,
  type SubAccountSummary,
  type SubAccountView,
} from './types'

function usd(v: number, unlimitedLabel: string): string {
  if (v < 0) return unlimitedLabel
  return `$${v.toFixed(2)}`
}

export function SubAccountPage() {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  const canGrantAdmin = !user?.parent_id // 仅 Owner(主号) 能授予管理员/高权限

  const [items, setItems] = useState<SubAccountView[]>([])
  const [summary, setSummary] = useState<SubAccountSummary | null>(null)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [editTarget, setEditTarget] = useState<SubAccountView | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<SubAccountView | null>(null)

  const load = useCallback(
    async (p = page, kw = search) => {
      setLoading(true)
      try {
        const res = await listSubAccounts(p, kw)
        if (res.success && res.data) {
          setItems(res.data.items)
          setTotal(res.data.total)
          setPage(res.data.page)
          setSummary(res.data.summary ?? null)
        }
      } catch {
        setItems([])
      } finally {
        setLoading(false)
      }
    },
    [page, search]
  )

  useEffect(() => {
    void load(1, '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function copyCredentials(row: SubAccountView) {
    const text = row.initial_password
      ? `${row.email} / ${row.initial_password}`
      : row.email
    try {
      await navigator.clipboard.writeText(text)
      toast.success(t('Copied'))
    } catch {
      toast.error(t('Failed'))
    }
  }

  async function confirmDelete() {
    if (!deleteTarget) return
    const res = await deleteSubAccount(deleteTarget.id)
    if (res.success) {
      toast.success(t('Deleted'))
      void load()
    } else {
      toast.error(res.message || t('Failed'))
    }
    setDeleteTarget(null)
  }

  return (
    <div className='space-y-4 p-4'>
      <div className='flex items-center justify-between'>
        <div>
          <h1 className='text-lg font-semibold'>{t('Team Management')}</h1>
          <p className='text-muted-foreground text-sm'>
            {t('Sub-accounts')}: {total}
          </p>
        </div>
        <Button onClick={() => setCreateOpen(true)}>
          <Plus className='size-4' />
          {t('Create Sub-account')}
        </Button>
      </div>

      <div className='grid gap-3 sm:grid-cols-3'>
        <div className='rounded-lg border p-4'>
          <div className='text-muted-foreground text-xs'>
            {t('Account Info')}
          </div>
          <div className='mt-1 truncate text-sm font-medium'>
            {summary?.main_email ?? user?.email ?? '-'}
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            {t('Sub-accounts')}: {total}
          </div>
        </div>
        <div className='rounded-lg border p-4'>
          <div className='text-muted-foreground text-xs'>
            {t('Sub-account Consumption')}
          </div>
          <div className='mt-1 text-lg font-semibold tabular-nums'>
            ${(summary?.sub_used_usd ?? 0).toFixed(2)}
          </div>
        </div>
        <div className='rounded-lg border p-4'>
          <div className='text-muted-foreground text-xs'>{t('Balance')}</div>
          <div className='mt-1 text-lg font-semibold tabular-nums'>
            ${(summary?.balance_usd ?? 0).toFixed(2)}
          </div>
          <div className='text-muted-foreground mt-1 text-xs'>
            {t('Shared pool (main account wallet)')}
          </div>
        </div>
      </div>

      <div className='flex items-center gap-2'>
        <div className='relative max-w-xs flex-1'>
          <Search className='text-muted-foreground absolute left-2 top-1/2 size-4 -translate-y-1/2' />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && load(1, search)}
            placeholder={t('Sub-account email')}
            className='pl-8'
          />
        </div>
        <Button variant='outline' onClick={() => load(1, search)}>
          {t('Search')}
        </Button>
        <Button variant='outline' size='icon' onClick={() => load()}>
          <RotateCw className='size-4' />
        </Button>
      </div>

      <div className='overflow-x-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Sub-account')}</TableHead>
              <TableHead>{t('Username')}</TableHead>
              <TableHead>{t('Note')}</TableHead>
              <TableHead>{t('Consumption')}</TableHead>
              <TableHead>{t('Quota')}</TableHead>
              <TableHead className='text-right'>{t('Actions')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className='text-muted-foreground py-8 text-center'
                >
                  {loading ? t('Loading...') : t('No sub-accounts yet')}
                </TableCell>
              </TableRow>
            )}
            {items.map((row) => (
              <TableRow key={row.id}>
                <TableCell className='min-w-[240px]'>
                  <div className='text-sm font-medium'>{row.email}</div>
                  {row.initial_password && (
                    <div className='text-muted-foreground text-xs'>
                      {t('Initial password')}: {row.initial_password}
                    </div>
                  )}
                  {row.role_preset === ROLE_PRESET_ADMIN && (
                    <Badge variant='secondary' className='mt-1'>
                      {t('Administrator')}
                    </Badge>
                  )}
                  {row.status === 2 && (
                    <Badge variant='outline' className='mt-1 ml-1'>
                      {t('Disabled')}
                    </Badge>
                  )}
                </TableCell>
                <TableCell>{row.username}</TableCell>
                <TableCell className='text-muted-foreground'>
                  {row.note || '-'}
                </TableCell>
                <TableCell className='text-xs whitespace-nowrap'>
                  <div>
                    {t('Total')}: ${row.total_used_usd.toFixed(2)}
                  </div>
                  <div>
                    {t('Monthly')}: ${row.month_used_usd.toFixed(2)}
                  </div>
                  <div>
                    {t('Daily')}: ${row.day_used_usd.toFixed(2)}
                  </div>
                </TableCell>
                <TableCell className='text-xs whitespace-nowrap'>
                  <div>
                    {t('Total')}: {usd(row.total_limit_usd, t('Unlimited'))}
                  </div>
                  <div>
                    {t('Monthly')}: {usd(row.month_limit_usd, t('Unlimited'))}
                  </div>
                  <div>
                    {t('Daily')}: {usd(row.day_limit_usd, t('Unlimited'))}
                  </div>
                </TableCell>
                <TableCell>
                  <div className='flex justify-end gap-1'>
                    <Button
                      variant='ghost'
                      size='icon'
                      title={t('Copy')}
                      onClick={() => copyCredentials(row)}
                    >
                      <Copy className='size-4' />
                    </Button>
                    <Button
                      variant='ghost'
                      size='icon'
                      title={t('Edit')}
                      onClick={() => setEditTarget(row)}
                    >
                      <Pencil className='size-4' />
                    </Button>
                    <Button
                      variant='ghost'
                      size='icon'
                      title={t('Delete')}
                      onClick={() => setDeleteTarget(row)}
                    >
                      <Trash2 className='text-destructive size-4' />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>

      {total > items.length && (
        <div className='flex justify-end gap-2'>
          <Button
            variant='outline'
            size='sm'
            disabled={page <= 1}
            onClick={() => load(page - 1, search)}
          >
            {t('Previous')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            onClick={() => load(page + 1, search)}
          >
            {t('Next')}
          </Button>
        </div>
      )}

      <CreateSubAccountDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onSuccess={() => load(1, '')}
        canGrantAdmin={canGrantAdmin}
      />
      <EditSubAccountDialog
        target={editTarget}
        open={editTarget != null}
        onOpenChange={(o) => !o && setEditTarget(null)}
        onSuccess={() => load()}
        canGrantAdmin={canGrantAdmin}
      />
      <ConfirmDialog
        open={deleteTarget != null}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
        destructive
        title={t('Delete Sub-account')}
        desc={t(
          'This will disable the sub-account and its API keys. Continue?'
        )}
        confirmText={t('Delete')}
        handleConfirm={confirmDelete}
      />
    </div>
  )
}
