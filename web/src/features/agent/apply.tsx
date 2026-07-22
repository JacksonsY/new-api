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
// jzlh-agent 代理自助入驻:与供应商入驻对称的"申请→审核"漏斗。
// 此前代理只能 root 手动指派,普通用户根本不知道代理体系存在——增长渠道
// 的漏斗反而是全站最窄的,倒挂。
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Clock, Handshake, TriangleAlert } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { TitledCard } from '@/components/ui/titled-card'

import { getAgentApplication, submitAgentApplication } from './api'
import { AGENT_APPLICATION_STATUS } from './types'

const APPLICATION_QUERY_KEY = 'agent-application'

export function AgentApply() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [contact, setContact] = useState('')
  const [note, setNote] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const { data: app, isLoading } = useQuery({
    queryKey: [APPLICATION_QUERY_KEY],
    queryFn: async () => {
      const res = await getAgentApplication()
      if (!res.success) {
        toast.error(res.message || t('Failed to load'))
        return null
      }
      return res.data ?? null
    },
  })

  // 已有申请时预填材料,便于补充后重新提交
  useEffect(() => {
    if (app) {
      setContact(app.contact)
      setNote(app.note)
    }
  }, [app])

  const handleSubmit = async () => {
    setSubmitting(true)
    try {
      const res = await submitAgentApplication(contact.trim(), note.trim())
      if (!res.success) {
        toast.error(res.message || t('Failed to submit'))
        return
      }
      toast.success(t('Application submitted'))
      await queryClient.invalidateQueries({ queryKey: [APPLICATION_QUERY_KEY] })
    } finally {
      setSubmitting(false)
    }
  }

  const isPending = app?.status === AGENT_APPLICATION_STATUS.PENDING
  const isRejected = app?.status === AGENT_APPLICATION_STATUS.REJECTED

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Become an Agent')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='mx-auto w-full max-w-2xl space-y-4'>
          {isLoading ? <Skeleton className='h-48 w-full' /> : null}

          {!isLoading && (
            <>
              {isPending && (
                <Alert>
                  <Clock className='size-4' />
                  <AlertDescription>
                    {t(
                      'Your application is under review. You can update the materials below and resubmit.'
                    )}
                  </AlertDescription>
                </Alert>
              )}
              {isRejected && (
                <Alert variant='destructive'>
                  <TriangleAlert className='size-4' />
                  <AlertDescription>
                    {t('Your application was rejected: {{reason}}. You can revise and resubmit.', {
                      reason: app?.reason || '-',
                    })}
                  </AlertDescription>
                </Alert>
              )}

              <TitledCard
                title={t('Agent Program')}
                description={t(
                  'Share your invite link; when invited users spend, you earn a commission you can withdraw as cash.'
                )}
                icon={<Handshake className='text-primary size-5' />}
                disableHoverEffect
              >
                <div className='space-y-4'>
                  <div className='space-y-2'>
                    <Label htmlFor='agent-apply-contact'>{t('Contact')}</Label>
                    <Input
                      id='agent-apply-contact'
                      value={contact}
                      maxLength={191}
                      onChange={(e) => setContact(e.target.value)}
                      placeholder={t('Telegram / WeChat / Email for review contact')}
                    />
                  </div>
                  <div className='space-y-2'>
                    <Label htmlFor='agent-apply-note'>
                      {t('Promotion plan')}
                    </Label>
                    <Textarea
                      id='agent-apply-note'
                      value={note}
                      rows={5}
                      maxLength={2000}
                      onChange={(e) => setNote(e.target.value)}
                      placeholder={t(
                        'Where and how will you promote? Channels, audience, expected scale.'
                      )}
                    />
                  </div>
                  <Button
                    className='w-full'
                    disabled={submitting}
                    onClick={handleSubmit}
                  >
                    {isPending || isRejected
                      ? t('Resubmit Application')
                      : t('Submit Application')}
                  </Button>
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'The commission rate is set by the platform during review. You will see the agent center in the sidebar once approved.'
                    )}
                  </p>
                </div>
              </TitledCard>
            </>
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
