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
import { ChevronDown, Loader2, ShieldCheck } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Combobox } from '@/components/design-system/combobox'
import type { ComboboxInputOption } from '@/components/design-system/combobox-input'
import { Button } from '@/components/design-system/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/design-system/tabs'
import { Dialog } from '@/components/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { toIntlLocale } from '@/i18n/languages'
import {
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'
import { cn } from '@/lib/utils'

import { getChannelLatestDetection, getDetectionRecords, verifyChannel } from './api'
import { DetectorReportCard, VerdictBadge } from './detector-report'
import { useDetectionPoll } from './use-detection-poll'
import type { DetectionRecord, DetectionReport } from './types'
import { verdictToneClass } from './verdict'

// 管理员渠道验真弹窗：可选模型发起检测（检测/历史双 Tab），历史来自服务端记录。
export function ChannelVerifyDialog({
  channelId,
  channelName,
  channelModels,
  defaultModel,
  open,
  onOpenChange,
}: {
  channelId: number | null
  channelName?: string
  // 渠道逗号分隔的模型列表，用于「选择检测哪个模型」。
  channelModels?: string
  // 渠道默认测试模型（test_model），作为选择器初值。
  defaultModel?: string
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const { phase, report, error, start, reset } = useDetectionPoll()
  const [latest, setLatest] = useState<DetectionReport | null>(null)
  const [loadingLatest, setLoadingLatest] = useState(false)
  const [starting, setStarting] = useState(false)
  const [selectedModel, setSelectedModel] = useState('')
  // 每完成一轮检测就 +1，让历史 Tab 重新拉第一页（即便停留其上也能刷出新记录）。
  const [historyRefresh, setHistoryRefresh] = useState(0)

  const modelOptions = useMemo<ComboboxInputOption[]>(() => {
    if (!channelModels) return []
    return channelModels
      .split(',')
      .map((m) => m.trim())
      .filter(Boolean)
      .map((m) => ({ value: m, label: m }))
  }, [channelModels])

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
      // 初值只从合法选项里取：test_model 在挂载模型列表内才用它，否则退回首个模型；
      // 无选项则留空（传 undefined，后端沿用自身 test_model/首模型默认，不会被校验拒绝）。
      const wanted = defaultModel?.trim()
      const inList = wanted && modelOptions.some((o) => o.value === wanted)
      setSelectedModel(inList ? wanted : (modelOptions[0]?.value ?? ''))
      loadLatest()
    }
  }, [open, channelId, reset, loadLatest, defaultModel, modelOptions])

  // 检测跑完（job done）刷新历史列表。
  useEffect(() => {
    if (phase === 'done') {
      setHistoryRefresh((k) => k + 1)
    }
  }, [phase])

  async function onRun() {
    if (channelId == null) return
    setStarting(true)
    try {
      const res = await verifyChannel(
        channelId,
        'standard',
        selectedModel || undefined
      )
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
      <Tabs defaultValue='detect' className='w-full'>
        <TabsList className='mb-2'>
          <TabsTrigger value='detect'>{t('Detection')}</TabsTrigger>
          <TabsTrigger value='history'>{t('History')}</TabsTrigger>
        </TabsList>

        <TabsContent value='detect'>
          {modelOptions.length > 0 && (
            <div className='mb-3 flex items-center gap-2'>
              <span className='text-muted-foreground shrink-0 text-sm'>
                {t('Model')}
              </span>
              <Combobox
                options={modelOptions}
                value={selectedModel}
                onValueChange={(v) => setSelectedModel(v ?? '')}
                placeholder={t('Select a model to detect')}
              />
            </div>
          )}

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
        </TabsContent>

        <TabsContent value='history'>
          <ChannelDetectionHistory
            channelId={open ? channelId : null}
            refreshKey={historyRefresh}
          />
        </TabsContent>
      </Tabs>
    </Dialog>
  )
}

// ChannelDetectionHistory 拉取并展示某渠道服务端保存的历次检测记录，可展开看完整证据卡。
// 分页「加载更多」累积追加；refreshKey 变化（每完成一轮检测）时重置回第一页。
function ChannelDetectionHistory({
  channelId,
  refreshKey,
}: {
  channelId: number | null
  refreshKey: number
}) {
  const { t, i18n } = useTranslation()
  const [records, setRecords] = useState<DetectionRecord[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [expandedId, setExpandedId] = useState<number | null>(null)
  const mounted = useRef(true)

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
    }
  }, [])

  // load 拉第 page 页：page===1 覆盖，>1 追加。用 mounted ref 防卸载后 setState。
  const load = useCallback(
    (page: number) => {
      if (channelId == null) return
      setLoading(true)
      void getDetectionRecords(channelId, page)
        .then((res) => {
          if (!mounted.current) return
          const data = res.success ? res.data : undefined
          if (data) {
            setTotal(data.total)
            setRecords((prev) =>
              page === 1 ? data.items : [...prev, ...data.items]
            )
          } else if (page === 1) {
            setRecords([])
            setTotal(0)
          }
        })
        .catch(() => {
          if (mounted.current && page === 1) {
            setRecords([])
            setTotal(0)
          }
        })
        .finally(() => {
          if (mounted.current) setLoading(false)
        })
    },
    [channelId]
  )

  // 渠道切换或刷新信号变化 → 重置并拉第一页。
  useEffect(() => {
    setRecords([])
    setTotal(0)
    setExpandedId(null)
    load(1)
  }, [channelId, refreshKey, load])

  const hasMore = records.length < total
  const nextPage = Math.floor(records.length / 20) + 1

  if (loading && records.length === 0) {
    return (
      <div className='space-y-3 py-2'>
        <Skeleton className='h-14 w-full' />
        <Skeleton className='h-14 w-full' />
      </div>
    )
  }

  if (records.length === 0) {
    return (
      <div className='text-muted-foreground py-10 text-center text-sm'>
        {t('No detection record yet. Run a verification to begin.')}
      </div>
    )
  }

  return (
    <>
    <ul className='divide-border/60 max-h-[420px] divide-y overflow-y-auto'>
      {records.map((rec) => {
        const expanded = expandedId === rec.id
        const model = rec.report?.target_model || rec.model
        return (
          <li key={rec.id} className='overflow-hidden'>
            <button
              type='button'
              onClick={() => setExpandedId(expanded ? null : rec.id)}
              aria-expanded={expanded}
              className='hover:bg-muted/40 flex w-full items-center gap-3 px-1 py-3 text-left transition-colors'
            >
              <ChevronDown
                className={cn(
                  'text-muted-foreground size-4 shrink-0 transition-transform',
                  expanded && 'rotate-180'
                )}
              />
              <span
                className={cn(
                  'w-9 shrink-0 text-lg font-semibold tabular-nums',
                  verdictToneClass(rec.verdict)
                )}
              >
                {Math.round(rec.score)}
              </span>
              <span className='min-w-0 flex-1'>
                <span className='flex flex-wrap items-center gap-x-2 gap-y-1'>
                  <span className='truncate text-sm font-medium'>{model}</span>
                  <VerdictBadge verdict={rec.verdict} />
                  {rec.critical_count > 0 && (
                    <span className='text-destructive text-xs font-medium'>
                      {rec.critical_count} {t('Critical Issue')}
                    </span>
                  )}
                </span>
                <span className='text-muted-foreground mt-0.5 block truncate text-xs'>
                  {rec.protocol} · {rec.source} ·{' '}
                  <span title={formatTimestampToDate(rec.created_at, 'seconds')}>
                    {formatTimestampRelative(
                      rec.created_at,
                      'seconds',
                      toIntlLocale(i18n.language)
                    )}
                  </span>
                </span>
              </span>
            </button>
            {expanded && rec.report && (
              <div className='px-1 pb-4'>
                <DetectorReportCard report={rec.report} />
              </div>
            )}
          </li>
        )
      })}
    </ul>
    {hasMore && (
      <div className='pt-3 text-center'>
        <Button
          variant='outline'
          size='sm'
          disabled={loading}
          onClick={() => load(nextPage)}
        >
          {loading ? (
            <Loader2 className='size-4 animate-spin' />
          ) : (
            t('Load more')
          )}
        </Button>
      </div>
    )}
    </>
  )
}
