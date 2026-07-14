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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { Dialog } from '@/components/dialog'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { useTableUrlState } from '@/hooks/use-table-url-state'

import {
  adminApproveChannel,
  adminListPendingChannels,
  adminRejectChannel,
} from '../api'
import type { SupplierChannel } from '../types'
import { useAdminChannelReviewColumns } from './admin-channel-review-columns'

const route = getRouteApi('/_authenticated/suppliers/review/')

export function AdminChannelReviewTable() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [approveTarget, setApproveTarget] = useState<SupplierChannel | null>(
    null
  )
  const [rejectTarget, setRejectTarget] = useState<SupplierChannel | null>(null)

  const columns = useAdminChannelReviewColumns({
    onApprove: setApproveTarget,
    onReject: setRejectTarget,
  })

  const { pagination, onPaginationChange, ensurePageInRange } = useTableUrlState(
    {
      search: route.useSearch(),
      navigate: route.useNavigate(),
      pagination: {
        defaultPage: 1,
        defaultPageSize: 20,
        pageSizeStorageKey: 'supplier-pending-channels:page-size:v1',
      },
      globalFilter: { enabled: false },
    }
  )

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'pending-channels',
      pagination.pageIndex + 1,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const res = await adminListPendingChannels(
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
    queryClient.invalidateQueries({ queryKey: ['pending-channels'] })

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
        tableLabel={t('Channel Review')}
        isLoading={isLoading}
        isFetching={isFetching}
        emptyTitle={t('No channels pending review')}
        emptyDescription={t(
          'Channels submitted by suppliers will appear here for approval.'
        )}
        skeletonKeyPrefix='pending-channels-skeleton'
        applyHeaderSize
        toolbarProps={null}
      />

      <ApproveDialog
        target={approveTarget}
        onOpenChange={(open) => {
          if (!open) setApproveTarget(null)
        }}
        onDone={() => {
          setApproveTarget(null)
          refresh()
        }}
      />
      <RejectDialog
        target={rejectTarget}
        onOpenChange={(open) => {
          if (!open) setRejectTarget(null)
        }}
        onDone={() => {
          setRejectTarget(null)
          refresh()
        }}
      />
    </>
  )
}

function ApproveDialog({
  target,
  onOpenChange,
  onDone,
}: {
  target: SupplierChannel | null
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const [group, setGroup] = useState('default')
  const [priority, setPriority] = useState('0')
  const [weight, setWeight] = useState('0')
  const [ratio, setRatio] = useState('1')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (target) {
      setGroup('default')
      setPriority('0')
      setWeight('0')
      setRatio(String(target.channel_ratio ?? 1))
    }
  }, [target])

  async function onSubmit() {
    if (!target) return
    setBusy(true)
    try {
      const res = await adminApproveChannel({
        channel_id: target.id,
        group: group.trim() || 'default',
        priority: Number.parseInt(priority, 10) || 0,
        weight: Number.parseInt(weight, 10) || 0,
        channel_ratio: Number.parseFloat(ratio) || 1,
      })
      if (res.success) {
        toast.success(t('Channel approved'))
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

  return (
    <Dialog
      open={!!target}
      onOpenChange={onOpenChange}
      title={t('Approve Channel')}
      description={target ? `${target.name} #${target.id}` : ''}
      contentHeight='auto'
      contentClassName='sm:max-w-md'
      footer={
        <div className='flex justify-end gap-2'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button onClick={onSubmit} disabled={busy}>
            {t('Approve')}
          </Button>
        </div>
      }
    >
      <div className='grid gap-4 py-2'>
        <div className='grid gap-1.5'>
          <Label htmlFor='ap-group'>{t('Group')}</Label>
          <Input
            id='ap-group'
            value={group}
            onChange={(e) => setGroup(e.target.value)}
          />
        </div>
        <div className='grid grid-cols-2 gap-4'>
          <div className='grid gap-1.5'>
            <Label htmlFor='ap-priority'>{t('Priority')}</Label>
            <Input
              id='ap-priority'
              value={priority}
              onChange={(e) => setPriority(e.target.value)}
              inputMode='numeric'
            />
          </div>
          <div className='grid gap-1.5'>
            <Label htmlFor='ap-weight'>{t('Weight')}</Label>
            <Input
              id='ap-weight'
              value={weight}
              onChange={(e) => setWeight(e.target.value)}
              inputMode='numeric'
            />
          </div>
        </div>
        <div className='grid max-w-40 gap-1.5'>
          <Label htmlFor='ap-ratio'>{t('Channel Ratio')}</Label>
          <Input
            id='ap-ratio'
            value={ratio}
            onChange={(e) => setRatio(e.target.value)}
            inputMode='decimal'
          />
        </div>
      </div>
    </Dialog>
  )
}

function RejectDialog({
  target,
  onOpenChange,
  onDone,
}: {
  target: SupplierChannel | null
  onOpenChange: (open: boolean) => void
  onDone: () => void
}) {
  const { t } = useTranslation()
  const [remark, setRemark] = useState('')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (target) setRemark('')
  }, [target])

  async function onSubmit() {
    if (!target) return
    setBusy(true)
    try {
      const res = await adminRejectChannel(target.id, remark.trim())
      if (res.success) {
        toast.success(t('Channel rejected'))
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

  return (
    <Dialog
      open={!!target}
      onOpenChange={onOpenChange}
      title={t('Reject Channel')}
      description={target ? `${target.name} #${target.id}` : ''}
      contentHeight='auto'
      contentClassName='sm:max-w-md'
      footer={
        <div className='flex justify-end gap-2'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button variant='destructive' onClick={onSubmit} disabled={busy}>
            {t('Reject')}
          </Button>
        </div>
      }
    >
      <div className='grid gap-1.5 py-2'>
        <Label htmlFor='rj-remark'>{t('Reason')}</Label>
        <Textarea
          id='rj-remark'
          value={remark}
          onChange={(e) => setRemark(e.target.value)}
          placeholder={t('Explain why this channel is rejected')}
          rows={3}
        />
      </div>
    </Dialog>
  )
}
