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
import { useMemo, useState } from 'react'
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

import { submitDetection } from './api'
import { DetectorReportCard } from './detector-report'
import type { DetectorMode, DetectorProtocol } from './types'
import { useDetectionPoll } from './use-detection-poll'

const PROTOCOLS: { value: DetectorProtocol; label: string }[] = [
  { value: 'claude', label: 'Claude' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
]

const MODE_LABELS: Record<DetectorMode, string> = {
  standard: 'Standard',
  quick: 'Quick',
  deep: 'Deep',
}
const MODES: DetectorMode[] = ['standard', 'quick', 'deep']

// 模型输入的预设建议（点击/聚焦弹出;自定义名照样可直接输入)。
const PRESET_MODELS = [
  'claude-opus-4-8',
  'claude-opus-4-7',
  'claude-sonnet-4-6',
  'claude-haiku-4-5',
  'gpt-5.5',
  'gpt-5.4',
  'gpt-5.4-mini',
  'gemini-3-pro-preview',
  'gemini-3-flash-preview',
  'gemini-2.5-pro',
  'gemini-2.5-flash',
]

// 公共真伪检测页（免登录）。表单走项目标准 react-hook-form + zod + Form 原语；
// 外观走首页同一套 premium 设计语言（PremiumPublicLayout 已提供 .pf 背景层，
// 这里直接用 .pf-* 设计 token，与首页排版一致）。
export function PublicDetector() {
  const { t } = useTranslation()
  const { phase, report, error, start, reset } = useDetectionPoll()
  const [submitting, setSubmitting] = useState(false)

  const formSchema = useMemo(
    () =>
      z.object({
        protocol: z.enum(['claude', 'openai', 'gemini']),
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

  async function onSubmit(values: DetectorFormValues) {
    reset()
    setSubmitting(true)
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
        start(res.data.job_id)
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
      <section className='mx-auto w-full max-w-4xl px-6 pt-20 pb-8 text-center sm:pt-24'>
        {/* Hero — same vocabulary as the homepage hero */}
        <span className='pf-pill mb-5'>
          <span
            className='size-1.5 rounded-full'
            style={{ background: 'var(--pf-grad)' }}
          />
          {t('Model Detection')}
        </span>
        <h1
          className='mx-auto text-3xl font-bold tracking-tight sm:text-4xl md:text-5xl'
          style={{ color: 'var(--pf-ink)' }}
        >
          {t('Is your relay serving the')}{' '}
          <span className='pf-fire-text whitespace-nowrap'>
            {t('real model?')}
          </span>
        </h1>
        <p className='pf-lead mx-auto mt-5 max-w-xl'>
          {t(
            'Probe any upstream endpoint and get a field-level, protocol-level verdict on whether it is the genuine Claude / OpenAI / Gemini model it claims to be.'
          )}
        </p>
        <div className='mt-7 flex flex-wrap items-center justify-center gap-2.5'>
          {trustChips.map((c) => (
            <span key={c.label} className='pf-chip'>
              <c.icon className='size-4' style={{ color: 'var(--pf-fire)' }} />
              {c.label}
            </span>
          ))}
        </div>

        {/* Form card */}
        <div className='pf-card mx-auto mt-8 max-w-2xl p-6 text-left sm:p-8'>
          <Form {...form}>
            <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-4'>
              <FormField
                control={form.control}
                name='protocol'
                render={({ field }) => (
                  <FormItem>
                    <Tabs value={field.value} onValueChange={field.onChange}>
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
                          options={PRESET_MODELS.map((m) => ({
                            value: m,
                            label: m,
                          }))}
                          value={field.value}
                          onValueChange={field.onChange}
                          allowCustomValue
                          placeholder={t('e.g. gpt-4o, gemini-2.5-flash')}
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

        {phase === 'error' && (
          <div className='text-destructive mx-auto mt-6 max-w-2xl rounded-xl border px-4 py-6 text-sm'>
            {error || t('Detection failed')}
          </div>
        )}

        {phase === 'done' && report && (
          <div className='mx-auto mt-8 max-w-2xl text-left'>
            <h2 className='mb-3 text-base font-semibold'>
              {t('Detection Result')}
            </h2>
            <DetectorReportCard report={report} />
          </div>
        )}
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
