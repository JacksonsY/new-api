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
// jzlh-agent 代理入驻审核队列(root):通过时设定分润费率(与手动设代理同界),
// 拒绝必填原因(申请人页面可见,可补材料重申)。
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StaticDataTable } from '@/components/data-table'
import { Dialog } from '@/components/dialog'
import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { SectionPageLayout } from '@/components/layout'
import { LongText } from '@/components/long-text'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Label } from '@/components/ui/label'
import { TitledCard } from '@/components/ui/titled-card'
import { formatTimestamp } from '@/lib/format'

import { adminListAgentApplications, adminReviewAgentApplication } from './api'
import {
  AGENT_APPLICATION_STATUS,
  type AgentApplicationRow,
} from './types'

const QUERY_KEY = 'agent-applications'

function applicationStatusBadge(
  t: (k: string) => string,
  status: number
): { label: string; variant: StatusVariant } {
  if (status === AGENT_APPLICATION_STATUS.PENDING) {
    return { label: t('Pending'), variant: 'warning' }
  }
  if (status === AGENT_APPLICATION_STATUS.APPROVED) {
    return { label: t('Approved'), variant: 'success' }
  }
  return { label: t('Rejected'), variant: 'destructive' }
}

export function AdminAgentApplications() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [approveRow, setApproveRow] = useState<AgentApplicationRow | null>(null)
  const [rejectRow, setRejectRow] = useState<AgentApplicationRow | null>(null)
  const [ratePercent, setRatePercent] = useState('20')
  const [reason, setReason] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: [QUERY_KEY],
    queryFn: async () => {
      const res = await adminListAgentApplications(0, 1, 100)
      if (!res.success) {
        toast.error(res.message || t('Failed to load'))
        return { items: [], defaultRate: 0 }
      }
      const raw = res.data as
        | { items?: AgentApplicationRow[]; default_profit_rate?: number }
        | undefined
      return {
        items: raw?.items || [],
        defaultRate: raw?.default_profit_rate ?? 0,
      }
    },
  })
  const rows = data?.items || []

  // v2 P2:系统设置的默认分润比例预填审批弹窗,减少逐单手填
  const defaultRate = data?.defaultRate ?? 0
  useEffect(() => {
    if (defaultRate > 0) {
      setRatePercent(String(Number((defaultRate * 100).toFixed(1))))
    }
  }, [defaultRate])

  const refresh = () => queryClient.invalidateQueries({ queryKey: [QUERY_KEY] })

  const handleApprove = useCallback(async () => {
    if (!approveRow) return
    const pct = Number.parseFloat(ratePercent)
    if (Number.isNaN(pct) || pct < 0 || pct > 100) {
      toast.error(t('Fee rate must be between 0 and 100'))
      return
    }
    setSubmitting(true)
    try {
      const res = await adminReviewAgentApplication({
        id: approveRow.application.id,
        action: 'approve',
        usage_profit_rate: pct / 100,
      })
      if (!res.success) {
        toast.error(res.message || t('Operation failed'))
        return
      }
      toast.success(t('Approved. The user is now an agent.'))
      setApproveRow(null)
      refresh()
    } finally {
      setSubmitting(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [approveRow, ratePercent, t])

  const handleReject = useCallback(async () => {
    if (!rejectRow) return
    if (!reason.trim()) {
      toast.error(t('Please enter a rejection reason'))
      return
    }
    setSubmitting(true)
    try {
      const res = await adminReviewAgentApplication({
        id: rejectRow.application.id,
        action: 'reject',
        reason: reason.trim(),
      })
      if (!res.success) {
        toast.error(res.message || t('Operation failed'))
        return
      }
      toast.success(t('Rejected'))
      setRejectRow(null)
      setReason('')
      refresh()
    } finally {
      setSubmitting(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rejectRow, reason, t])

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Agent Applications')}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <TitledCard
          title={t('Application Queue')}
          description={t(
            'Pending applications first. Approving sets the user as an agent with the rate you enter.'
          )}
          disableHoverEffect
          contentClassName='p-0'
        >
          <StaticDataTable
            data={rows}
            empty={!isLoading && rows.length === 0}
            emptyContent={
              <span className='text-muted-foreground text-sm'>
                {t('No applications yet.')}
              </span>
            }
            columns={[
              {
                id: 'user',
                header: t('Applicant'),
                cell: (r: AgentApplicationRow) => (
                  <span className='font-medium'>{r.username}</span>
                ),
              },
              {
                id: 'contact',
                header: t('Contact'),
                cell: (r: AgentApplicationRow) => (
                  <LongText className='max-w-[180px]'>
                    {r.application.contact}
                  </LongText>
                ),
              },
              {
                id: 'note',
                header: t('Promotion plan'),
                cell: (r: AgentApplicationRow) => (
                  <LongText className='text-muted-foreground max-w-[260px]'>
                    {r.application.note}
                  </LongText>
                ),
              },
              {
                id: 'status',
                header: t('Status'),
                cell: (r: AgentApplicationRow) => {
                  const s = applicationStatusBadge(t, r.application.status)
                  return <StatusBadge variant={s.variant}>{s.label}</StatusBadge>
                },
              },
              {
                id: 'time',
                header: t('Time'),
                cellClassName: 'text-muted-foreground text-sm',
                cell: (r: AgentApplicationRow) =>
                  formatTimestamp(r.application.updated_time),
              },
              {
                id: 'actions',
                header: t('Actions'),
                cell: (r: AgentApplicationRow) => {
                  if (
                    r.application.status !== AGENT_APPLICATION_STATUS.PENDING
                  ) {
                    return (
                      <span className='text-muted-foreground text-xs'>
                        {r.application.reason || '-'}
                      </span>
                    )
                  }
                  return (
                    <div className='flex flex-wrap gap-2'>
                      <Button size='sm' onClick={() => setApproveRow(r)}>
                        {t('Approve')}
                      </Button>
                      <Button
                        size='sm'
                        variant='outline'
                        onClick={() => setRejectRow(r)}
                      >
                        {t('Reject')}
                      </Button>
                    </div>
                  )
                },
              },
            ]}
          />
        </TitledCard>
      </SectionPageLayout.Content>

      <Dialog
        open={approveRow !== null}
        onOpenChange={(open) => !open && setApproveRow(null)}
        title={t('Approve Application')}
        description={t(
          'Sets {{name}} as an agent. Commission = downstream spend × rate.',
          { name: approveRow?.username ?? '' }
        )}
        contentClassName='sm:max-w-md'
        footer={
          <>
            <Button variant='outline' onClick={() => setApproveRow(null)}>
              {t('Cancel')}
            </Button>
            <Button disabled={submitting} onClick={handleApprove}>
              {t('Approve')}
            </Button>
          </>
        }
      >
        <div className='space-y-2'>
          <Label htmlFor='agent-approve-rate'>{t('Commission Rate')} (%)</Label>
          <Input
            id='agent-approve-rate'
            inputMode='decimal'
            value={ratePercent}
            onChange={(e) => setRatePercent(e.target.value)}
            placeholder='20'
          />
        </div>
      </Dialog>

      <Dialog
        open={rejectRow !== null}
        onOpenChange={(open) => !open && setRejectRow(null)}
        title={t('Reject Application')}
        description={t(
          'The reason is shown to the applicant, who can revise and resubmit.'
        )}
        contentClassName='sm:max-w-md'
        footer={
          <>
            <Button variant='outline' onClick={() => setRejectRow(null)}>
              {t('Cancel')}
            </Button>
            <Button
              variant='destructive'
              disabled={submitting}
              onClick={handleReject}
            >
              {t('Reject')}
            </Button>
          </>
        }
      >
        <div className='space-y-2'>
          <Label htmlFor='agent-reject-reason'>{t('Rejection reason')}</Label>
          <Input
            id='agent-reject-reason'
            value={reason}
            maxLength={255}
            onChange={(e) => setReason(e.target.value)}
          />
        </div>
      </Dialog>
    </SectionPageLayout>
  )
}
