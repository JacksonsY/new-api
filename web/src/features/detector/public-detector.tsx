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
import { zodResolver } from '@hookform/resolvers/zod'
import {
  ArrowRight,
  BadgeCheck,
  Braces,
  FileText,
  Fingerprint,
  Info,
  KeyRound,
  Loader2,
  Network,
  Ruler,
  ShieldCheck,
} from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { ComboboxInput } from '@/components/design-system/combobox-input'
import { Input } from '@/components/design-system/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/design-system/select'
import { Tabs, TabsList, TabsTrigger } from '@/components/design-system/tabs'
import { PremiumPublicLayout } from '@/components/layout'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'

import {
  clearActiveJob,
  loadActiveJob,
  saveActiveJob,
} from './active-job-storage'
import { submitDetection } from './api'
import { estimateDetectionMinutes } from './detection-estimate'
import { DetectionHistory } from './detection-history'
import {
  DetectionEmptyState,
  DetectionErrorCard,
  DetectionProgressCard,
} from './detection-status'
import type {
  DetectionHistoryRequest,
  DetectorMode,
  DetectorProtocol,
} from './types'
import { useDetectionHistory } from './use-detection-history'
import { useDetectionPoll } from './use-detection-poll'

const PROTOCOLS: { value: DetectorProtocol; label: string }[] = [
  { value: 'claude', label: 'Claude' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'grok', label: 'Grok' },
]

const MODE_LABELS: Record<DetectorMode, string> = {
  standard: 'Standard',
  quick: 'Quick',
  deep: 'Deep',
}
const MODES: DetectorMode[] = ['standard', 'quick', 'deep']

// 各协议的预设模型建议（与 veridrop.org 现网各协议页保持一致）。选 Claude 只列
// Claude 模型,选 OpenAI 只列 GPT,依此类推;中转用 claude-fable-5-thinking 这类
// 变体名时,直接手输即可。
const PRESET_MODELS_BY_PROTOCOL: Record<DetectorProtocol, string[]> = {
  claude: [
    'claude-fable-5',
    'claude-sonnet-5',
    'claude-opus-4-8',
    'claude-opus-4-7',
    'claude-opus-4-6',
    'claude-sonnet-4-6',
    'claude-haiku-4-5-20251001',
  ],
  openai: [
    'gpt-5.6-sol',
    'gpt-5.6-terra',
    'gpt-5.6-luna',
    'gpt-5.5',
    'gpt-5.4',
    'gpt-5.3-codex',
    'gpt-5.4-nano',
    'gpt-5.4-mini',
  ],
  gemini: [
    'gemini-3.5-flash',
    'gemini-3.1-pro-preview',
    'gemini-3.1-flash-lite',
    'gemini-2.5-pro',
    'gemini-2.5-flash',
    'gemini-2.5-flash-lite',
  ],
  grok: [
    'grok-4.5',
    'grok-4.3',
    'grok-4.20-0309-reasoning',
    'grok-4.20-0309-non-reasoning',
    'grok-4.20-multi-agent-0309',
    'grok-build-0.1',
  ],
}

// 切换协议 tab 时默认选中的模型（各协议现网首选/最新旗舰）。
const DEFAULT_MODEL_BY_PROTOCOL: Record<DetectorProtocol, string> = {
  claude: 'claude-sonnet-5',
  openai: 'gpt-5.6-sol',
  gemini: 'gemini-3.5-flash',
  grok: 'grok-4.5',
}

// 公共真伪检测页（免登录），工作台式布局：左栏表单、右栏实时状态 + 浏览器本地
// 历史。表单走项目标准 react-hook-form + zod + Form 原语；外观走首页同一套
// premium 设计语言（PremiumPublicLayout 提供 .pf 背景层与 .pf-* token）。
export function PublicDetector() {
  const { t } = useTranslation()
  const { phase, report, error, start, reset } = useDetectionPoll()
  const [submitting, setSubmitting] = useState(false)
  // 浏览器本地检测历史（不落服务器）。刚跑完的结果即最新一条，自动展开；
  // 旧记录可点开重看 / 删除 / 清空 / 一键重测。
  const { entries, add, remove, clear } = useDetectionHistory()
  const [expandedId, setExpandedId] = useState<string | null>(null)
  // 本次运行的请求参数（不含密钥）。进度面板渲染它，完成后配对报告入历史。
  const [activeRequest, setActiveRequest] =
    useState<DetectionHistoryRequest | null>(null)
  // 本次运行的真实提交时刻——进度面板计时锚点（刷新续接后接着计，不清零）。
  const [activeStartedAt, setActiveStartedAt] = useState<number | null>(null)
  // 已存入历史的报告引用，避免 effect 重跑时重复入库。
  const savedReportRef = useRef<unknown>(null)
  // 本实例已挂接的 job id：续接的 UI 副作用（回填表单/提示）按 job 只做一次；
  // start() 则每次 effect 都要调——StrictMode 卸载清理会停掉轮询，必须重新拉起。
  const attachedJobRef = useRef<string | null>(null)
  // 重测时把视图滚回表单；提交后（移动端）把视图滚到状态面板。
  const formCardRef = useRef<HTMLDivElement | null>(null)
  const resultsRef = useRef<HTMLDivElement | null>(null)

  const formSchema = useMemo(
    () =>
      z.object({
        protocol: z.enum(['claude', 'openai', 'gemini', 'grok']),
        base_url: z
          .string()
          .trim()
          .min(1, t('Please enter the base URL'))
          .refine(
            (v) => {
              try {
                new URL(v)
                return true
              } catch {
                return false
              }
            },
            { message: t('Please enter a valid URL') }
          ),
        api_key: z.string().trim().min(1, t('Please enter the API key')),
        model: z.string().trim().min(1, t('Please enter the model')),
        mode: z.enum(['standard', 'quick', 'deep']),
        include_long_context: z.boolean(),
        include_long_context_extreme: z.boolean(),
      }),
    [t]
  )

  type DetectorFormValues = z.infer<typeof formSchema>

  const form = useForm<DetectorFormValues>({
    resolver: zodResolver(formSchema),
    defaultValues: {
      protocol: 'claude',
      base_url: '',
      api_key: '',
      model: '',
      mode: 'standard',
      include_long_context: false,
      include_long_context_extreme: false,
    },
  })

  const running = phase === 'running' || submitting
  const selectedProtocol = form.watch('protocol')
  // 预计耗时提示随模式与长上下文选项联动。
  const estimatedMinutes = estimateDetectionMinutes({
    mode: form.watch('mode'),
    include_long_context: form.watch('include_long_context'),
    include_long_context_extreme: form.watch('include_long_context_extreme'),
  })

  // 检测完成后把报告 + 请求参数存入浏览器本地历史，并自动展开这条最新结果。
  // savedReportRef 防止 effect 重跑造成重复入库（每次完成都是新的报告对象引用）。
  useEffect(() => {
    if (phase !== 'done' || !report) return
    if (savedReportRef.current === report) return
    savedReportRef.current = report
    const request: DetectionHistoryRequest = activeRequest ?? {
      protocol: (report.protocol === 'anthropic'
        ? 'claude'
        : report.protocol) as DetectorProtocol,
      base_url: report.base_url,
      model: report.target_model,
      mode: (report.mode as DetectorMode) ?? 'standard',
      include_long_context: false,
      include_long_context_extreme: false,
    }
    const id = add(report, request)
    setExpandedId(id)
  }, [phase, report, add, activeRequest])

  // 刷新续接：sessionStorage 里有未完成的 job（同 tab、25 分钟内）时恢复轮询与
  // 进度面板，并把请求参数回填表单（密钥除外，密钥永不落盘）。后端 job 内存保活
  // 约 1 小时，窗口内首次轮询即可拿到进行中/已完成状态；服务端重启则正常走
  // "job not found" 错误卡。start() 幂等（内部先 stop），每次 effect 执行都调，
  // 这样 StrictMode 双挂载在两次之间停掉的轮询会被第二次执行重新拉起。
  useEffect(() => {
    const active = loadActiveJob()
    if (!active) return
    if (attachedJobRef.current !== active.job_id) {
      attachedJobRef.current = active.job_id
      setActiveRequest(active.request)
      setActiveStartedAt(active.started_at)
      form.setValue('protocol', active.request.protocol)
      form.setValue('base_url', active.request.base_url)
      form.setValue('model', active.request.model)
      form.setValue('mode', active.request.mode)
      form.setValue('include_long_context', active.request.include_long_context)
      form.setValue(
        'include_long_context_extreme',
        active.request.include_long_context_extreme
      )
      toast.info(t('Resumed the in-flight detection'))
    }
    start(active.job_id, {
      longRun:
        active.request.include_long_context ||
        active.request.include_long_context_extreme,
    })
  }, [form, start, t])

  // 运行到达终态（完成/失败）后，续接条目即失效。
  useEffect(() => {
    if (phase === 'done' || phase === 'error') clearActiveJob()
  }, [phase])

  function handleRerun(req: DetectionHistoryRequest) {
    reset()
    // 放弃可能在跑的旧 job，刷新后不再复活它。
    clearActiveJob()
    form.setValue('protocol', req.protocol)
    form.setValue('base_url', req.base_url)
    form.setValue('model', req.model)
    form.setValue('mode', req.mode)
    form.setValue('include_long_context', req.include_long_context)
    form.setValue('include_long_context_extreme', req.include_long_context_extreme)
    // API Key 从不落盘，重测须重新输入。
    form.setValue('api_key', '')
    formCardRef.current?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }

  async function onSubmit(values: DetectorFormValues) {
    reset()
    clearActiveJob()
    setSubmitting(true)
    // 记住这次请求的可复现参数（不含密钥）：进度面板展示 + 完成后配对报告入库
    // + 落盘 sessionStorage 供刷新续接。
    const request: DetectionHistoryRequest = {
      protocol: values.protocol,
      base_url: values.base_url,
      model: values.model,
      mode: values.mode,
      include_long_context: values.include_long_context,
      include_long_context_extreme: values.include_long_context_extreme,
    }
    setActiveRequest(request)
    try {
      const res = await submitDetection({
        base_url: values.base_url,
        api_key: values.api_key,
        model: values.model,
        protocol: values.protocol,
        mode: values.mode,
        include_long_context: values.include_long_context,
        include_long_context_extreme: values.include_long_context_extreme,
      })
      if (res.success && res.data?.job_id) {
        const startedAt = Date.now()
        setActiveStartedAt(startedAt)
        // 标记本实例已挂接该 job，续接 effect 重跑（如切换语言）时不再回填/提示。
        attachedJobRef.current = res.data.job_id
        // 落盘续接条目：本 tab 内刷新页面可无缝恢复这次运行。
        saveActiveJob({
          job_id: res.data.job_id,
          started_at: startedAt,
          request,
        })
        // 长上下文档位合法耗时可达十几分钟，放宽轮询上限。
        start(res.data.job_id, {
          longRun:
            values.include_long_context ||
            values.include_long_context_extreme,
        })
        // 移动端表单较长，提交后把状态面板带进视野（桌面端已可见则无操作）。
        resultsRef.current?.scrollIntoView({
          behavior: 'smooth',
          block: 'nearest',
        })
      } else {
        toast.error(res.message || t('Failed'))
      }
    } catch {
      toast.error(t('Failed'))
    } finally {
      setSubmitting(false)
    }
  }

  const trustChips = [
    { icon: Fingerprint, label: t('Cryptographic signature check') },
    { icon: Network, label: t('Swapped-core fingerprinting') },
    { icon: ShieldCheck, label: t('SSRF-guarded probing') },
  ]

  const checks = [
    {
      icon: BadgeCheck,
      title: t('Model identity'),
      desc: t('Asks who it is and flags non-official backends like AWS or OpenAI.'),
    },
    {
      icon: Fingerprint,
      title: t('Usage fingerprint'),
      desc: t('Catches swapped-core relays leaking Anthropic-native usage fields.'),
    },
    {
      icon: KeyRound,
      title: t('Thinking signature'),
      desc: t('Verifies the server-signed reasoning signature is present and forwarded.'),
    },
    {
      icon: Braces,
      title: t('Protocol compliance'),
      desc: t('Checks field shapes, IDs, and streaming against the official spec.'),
    },
    {
      icon: FileText,
      title: t('Capability stripping'),
      desc: t('Detects removed PDF, tool use, or structured-output support.'),
    },
    {
      icon: Ruler,
      title: t('Long context'),
      desc: t('Optionally probes the real context window with needle-in-haystack.'),
    },
  ]

  return (
    <PremiumPublicLayout>
      {/* Hero — same vocabulary as the homepage hero, slimmed for the workbench */}
      <section className='mx-auto w-full max-w-3xl px-6 pt-16 pb-8 text-center sm:pt-20'>
        <span className='pf-pill mb-5'>
          <span
            className='size-1.5 rounded-full'
            style={{ background: 'var(--pf-grad)' }}
          />
          {t('Model Detection')}
        </span>
        <h1
          className='mx-auto text-3xl font-bold tracking-tight sm:text-4xl'
          style={{ color: 'var(--pf-ink)' }}
        >
          {t('Is your relay serving the')}{' '}
          <span className='pf-fire-text whitespace-nowrap'>
            {t('real model?')}
          </span>
        </h1>
        <p className='pf-lead mx-auto mt-4 max-w-xl'>
          {t(
            'Probe any upstream endpoint and get a field-level, protocol-level verdict on whether it is the genuine Claude / OpenAI / Gemini model it claims to be.'
          )}
        </p>
        <div className='mt-6 flex flex-wrap items-center justify-center gap-2.5'>
          {trustChips.map((c) => (
            <span key={c.label} className='pf-chip'>
              <c.icon className='size-4' style={{ color: 'var(--pf-fire)' }} />
              {c.label}
            </span>
          ))}
        </div>
      </section>

      {/* Workbench — form on the left, live status + local history on the right */}
      <section className='relative z-10 mx-auto w-full max-w-6xl px-4 pb-12 sm:px-6'>
        {/* 网格默认 items-stretch:右列(空态)拉伸到与表单同高;表单卡自身
            self-start,展开长报告时不被反向拉长。 */}
        <div className='flex flex-col gap-6 lg:grid lg:grid-cols-[400px_minmax(0,1fr)]'>
          <div
            ref={formCardRef}
            className='pf-card pf-static scroll-mt-24 p-5 text-left sm:p-6 lg:self-start'
          >
            <Form {...form}>
              <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-4'>
                <FormField
                  control={form.control}
                  name='protocol'
                  render={({ field }) => (
                    <FormItem>
                      <Tabs
                        value={field.value}
                        onValueChange={(v: string) => {
                          field.onChange(v)
                          // Reset the model to the selected protocol's default so a
                          // Claude model never lingers on the OpenAI/Gemini tab.
                          form.setValue(
                            'model',
                            DEFAULT_MODEL_BY_PROTOCOL[v as DetectorProtocol],
                            { shouldValidate: false }
                          )
                        }}
                      >
                        <TabsList className='w-full'>
                          {PROTOCOLS.map((p) => (
                            <TabsTrigger
                              key={p.value}
                              value={p.value}
                              className='flex-1'
                            >
                              {p.label}
                            </TabsTrigger>
                          ))}
                        </TabsList>
                      </Tabs>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='base_url'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Base URL')}</FormLabel>
                      <FormControl>
                        <Input {...field} placeholder='https://api.example.com' />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='api_key'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('API Key')}</FormLabel>
                      <FormControl>
                        <Input {...field} type='password' autoComplete='off' />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <div className='grid gap-3 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='model'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Model')}</FormLabel>
                        <FormControl>
                          <ComboboxInput
                            options={(
                              PRESET_MODELS_BY_PROTOCOL[selectedProtocol] ?? []
                            ).map((m) => ({
                              value: m,
                              label: m,
                            }))}
                            value={field.value}
                            onValueChange={field.onChange}
                            allowCustomValue
                            placeholder={
                              DEFAULT_MODEL_BY_PROTOCOL[selectedProtocol]
                            }
                          />
                        </FormControl>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='mode'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Mode')}</FormLabel>
                        <Select value={field.value} onValueChange={field.onChange}>
                          <FormControl>
                            <SelectTrigger className='w-full'>
                              <SelectValue />
                            </SelectTrigger>
                          </FormControl>
                          <SelectContent alignItemWithTrigger={false}>
                            {MODES.map((m) => (
                              <SelectItem key={m} value={m}>
                                {t(MODE_LABELS[m])}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t('Usually finishes in about {{minutes}} min', {
                    minutes: estimatedMinutes,
                  })}
                </p>

                <FormField
                  control={form.control}
                  name='include_long_context'
                  render={({ field }) => (
                    <FormItem>
                      <label className='border-border/50 hover:bg-muted/40 flex cursor-pointer items-start gap-2.5 rounded-xl border p-3 transition-colors'>
                        <FormControl>
                          <Checkbox
                            checked={field.value}
                            onCheckedChange={field.onChange}
                            className='mt-0.5'
                          />
                        </FormControl>
                        <span className='text-sm'>
                          {t('Include long-context probe (extra cost)')}
                          <span className='text-muted-foreground mt-0.5 block text-xs'>
                            {t(
                              'Sends a large prompt to test context handling. This consumes more tokens.'
                            )}
                          </span>
                        </span>
                      </label>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='include_long_context_extreme'
                  render={({ field }) => (
                    <FormItem>
                      <label className='border-border/50 hover:bg-muted/40 flex cursor-pointer items-start gap-2.5 rounded-xl border p-3 transition-colors'>
                        <FormControl>
                          <Checkbox
                            checked={field.value}
                            onCheckedChange={field.onChange}
                            className='mt-0.5'
                          />
                        </FormControl>
                        <span className='text-sm'>
                          {t('Extreme: probe the model’s full context (extra cost)')}
                          <span className='text-muted-foreground mt-0.5 block text-xs'>
                            {t(
                              'Probes adaptively up to the model’s advertised limit (~950k on 1M models) to catch "advertised 1M, actually 200k" fraud that the standard tiers miss.'
                            )}
                          </span>
                        </span>
                      </label>
                    </FormItem>
                  )}
                />

                <div className='text-muted-foreground bg-muted/40 flex items-start gap-2 rounded-xl px-3 py-2.5 text-xs leading-relaxed'>
                  <Info className='mt-0.5 size-3.5 shrink-0' />
                  <span>
                    {t(
                      'The detector calls the endpoint you provide from the server. Only test endpoints you are authorized to use. Private or internal addresses may be blocked, and each run consumes upstream tokens.'
                    )}
                  </span>
                </div>

                <button
                  type='submit'
                  className='pf-btn pf-btn-fire w-full justify-center'
                  disabled={running}
                >
                  {running ? (
                    <>
                      <Loader2 className='size-4 animate-spin' />
                      {t('Detecting...')}
                    </>
                  ) : (
                    t('Run Detection')
                  )}
                </button>
              </form>
            </Form>
          </div>

          <div
            ref={resultsRef}
            className='flex min-w-0 scroll-mt-24 flex-col gap-4 text-left'
          >
            {phase === 'running' && (
              <DetectionProgressCard
                request={activeRequest}
                startedAt={activeStartedAt ?? undefined}
              />
            )}
            {phase === 'error' && (
              <DetectionErrorCard
                message={error}
                onRetry={() => form.handleSubmit(onSubmit)()}
              />
            )}
            {entries.length > 0 ? (
              <DetectionHistory
                entries={entries}
                expandedId={expandedId}
                onToggle={(id) =>
                  setExpandedId((cur) => (cur === id ? null : id))
                }
                onRemove={remove}
                onClear={() => {
                  clear()
                  setExpandedId(null)
                }}
                onRerun={handleRerun}
              />
            ) : (
              phase !== 'running' &&
              phase !== 'error' && <DetectionEmptyState />
            )}
          </div>
        </div>
      </section>

      {/* What we check */}
      <section className='relative z-10 px-6 py-8 md:py-10'>
        <div className='mx-auto max-w-6xl'>
          <div className='mx-auto mb-7 max-w-2xl text-center'>
            <span className='pf-eyebrow mb-3'>{t('What we check')}</span>
            <h2
              className='text-2xl font-bold tracking-tight sm:text-3xl'
              style={{ color: 'var(--pf-ink)' }}
            >
              {t('Signals a fake relay')}{' '}
              <span className='pf-fire-text'>{t("can't hide.")}</span>
            </h2>
          </div>
          <div className='grid grid-cols-1 gap-3.5 sm:grid-cols-2 lg:grid-cols-3'>
            {checks.map((c) => {
              const Icon = c.icon
              return (
                <div key={c.title} className='pf-card flex flex-col gap-2.5 p-5'>
                  <span
                    className='inline-flex size-10 items-center justify-center rounded-xl'
                    style={{
                      background: 'rgba(255,255,255,0.9)',
                      border: '1px solid var(--pf-line-2)',
                    }}
                  >
                    <Icon className='size-5' style={{ color: 'var(--pf-fire)' }} />
                  </span>
                  <h3
                    className='text-base font-bold'
                    style={{ color: 'var(--pf-ink)' }}
                  >
                    {c.title}
                  </h3>
                  <p
                    className='text-sm leading-relaxed'
                    style={{ color: 'var(--pf-ink-2)' }}
                  >
                    {c.desc}
                  </p>
                </div>
              )
            })}
          </div>
        </div>
      </section>

      {/* Conversion CTA */}
      <section className='relative z-10 px-6 py-10 md:py-12'>
        <div className='pf-card pf-card-fire mx-auto max-w-3xl overflow-hidden px-8 py-10 text-center md:px-12'>
          <span className='pf-eyebrow mb-4'>{t('Partner Program')}</span>
          <h2
            className='mx-auto max-w-[18ch] text-2xl font-bold tracking-tight sm:text-3xl'
            style={{ color: 'var(--pf-ink)' }}
          >
            {t('Run a relay?')}{' '}
            <span className='pf-fire-text'>{t('Get it trusted.')}</span>
          </h2>
          <p className='pf-lead mx-auto mt-5 max-w-xl'>
            {t(
              'List your channels on the platform and let genuine quality stand out.'
            )}
          </p>
          <div className='mt-9 flex flex-wrap items-center justify-center gap-3'>
            <a href='/supplier/apply' className='pf-btn pf-btn-fire'>
              {t('Become a supplier')}
              <ArrowRight className='size-4' />
            </a>
            <a href='/partners' className='pf-btn pf-btn-ghost'>
              {t('Partner Program')}
            </a>
          </div>
        </div>
      </section>
    </PremiumPublicLayout>
  )
}
