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
import { Loader2, ShieldCheck } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/design-system/button'
import { Dialog } from '@/components/dialog'
import { Skeleton } from '@/components/ui/skeleton'

import { getChannelLatestDetection, verifyChannel } from './api'
import { DetectorReportCard } from './detector-report'
import { useDetectionPoll } from './use-detection-poll'
import type { DetectionReport } from './types'

// 管理员渠道验真弹窗：展示上次记录，可发起新一轮检测并轮询结果。
export function ChannelVerifyDialog({
  channelId,
  channelName,
  open,
  onOpenChange,
}: {
  channelId: number | null
  channelName?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const { phase, report, error, start, reset } = useDetectionPoll()
  const [latest, setLatest] = useState<DetectionReport | null>(null)
  const [loadingLatest, setLoadingLatest] = useState(false)
  const [starting, setStarting] = useState(false)

  const loadLatest = useCallback(async () => {
    if (channelId == null) return
    setLoadingLatest(true)
    try {
      const res = await getChannelLatestDetection(channelId)
      if (res.success && res.data?.report) {
        setLatest(res.data.report)
      } else {
        setLatest(null)
      }
    } catch {
      setLatest(null)
    } finally {
      setLoadingLatest(false)
    }
  }, [channelId])

  useEffect(() => {
    if (open && channelId != null) {
      reset()
      setLatest(null)
      loadLatest()
    }
  }, [open, channelId, reset, loadLatest])

  async function onRun() {
    if (channelId == null) return
    setStarting(true)
    try {
      const res = await verifyChannel(channelId, 'standard')
      if (res.success && res.data?.job_id) {
        start(res.data.job_id)
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setStarting(false)
    }
  }

  const running = phase === 'running' || starting
  const shownReport = report ?? latest

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('Verify Authenticity')}
      description={channelName}
      contentHeight='auto'
      contentClassName='sm:max-w-2xl'
      footer={
        <div className='flex justify-end gap-2'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Close')}
          </Button>
          <Button onClick={onRun} disabled={running}>
            {running ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <ShieldCheck className='size-4' />
            )}
            {phase === 'done' || shownReport
              ? t('Re-run Verification')
              : t('Run Verification')}
          </Button>
        </div>
      }
    >
      <div className='min-h-[120px] py-2'>
        {loadingLatest && !shownReport && (
          <div className='space-y-3'>
            <Skeleton className='h-24 w-full' />
            <Skeleton className='h-16 w-full' />
          </div>
        )}

        {running && (
          <div className='text-muted-foreground flex flex-col items-center gap-3 py-10 text-sm'>
            <Loader2 className='size-6 animate-spin' />
            {t('Running detection, this may take a moment...')}
          </div>
        )}

        {phase === 'error' && (
          <div className='text-destructive py-10 text-center text-sm'>
            {error || t('Detection failed')}
          </div>
        )}

        {!running &&
          phase !== 'error' &&
          !loadingLatest &&
          !shownReport && (
            <div className='text-muted-foreground py-10 text-center text-sm'>
              {t('No detection record yet. Run a verification to begin.')}
            </div>
          )}

        {!running && shownReport && (
          <>
            {report == null && latest != null && (
              <div className='text-muted-foreground mb-3 text-xs'>
                {t('Showing the most recent detection result.')}
              </div>
            )}
            <DetectorReportCard report={shownReport} />
          </>
        )}
      </div>
    </Dialog>
  )
}
