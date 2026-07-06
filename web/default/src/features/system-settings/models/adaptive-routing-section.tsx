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
import { useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'

import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

import { ChannelHealthDialog } from './channel-health-dialog'

const adaptiveRoutingSchema = z.object({
  enabled: z.boolean(),
  alpha: z.coerce.number().min(0.01).max(1),
  ttftRefMs: z.coerce.number().min(0),
  ttftPenalty: z.coerce.number().min(0).max(1),
  errorPenalty: z.coerce.number().min(0).max(1),
  healthFloor: z.coerce.number().min(0).max(1),
  inflightPenalty: z.coerce.number().min(0).max(1),
  topK: z.coerce.number().int().min(0),
  circuitEnabled: z.boolean(),
  openThreshold: z.coerce.number().int().min(1),
  cooldownSeconds: z.coerce.number().int().min(0),
  halfOpenFactor: z.coerce.number().min(0).max(1),
  escapeEnabled: z.boolean(),
  escapeTtftMs: z.coerce.number().min(0),
  escapeErrorRate: z.coerce.number().min(0).max(1),
})

type AdaptiveRoutingFormValues = z.output<typeof adaptiveRoutingSchema>
type AdaptiveRoutingFormInput = z.input<typeof adaptiveRoutingSchema>

// optionKeyByField maps each flat form field to its backend option key. Kept
// separate so react-hook-form never sees dotted (nested-path) field names.
const optionKeyByField = {
  enabled: 'adaptive_routing_setting.enabled',
  alpha: 'adaptive_routing_setting.alpha',
  ttftRefMs: 'adaptive_routing_setting.ttft_ref_ms',
  ttftPenalty: 'adaptive_routing_setting.ttft_penalty',
  errorPenalty: 'adaptive_routing_setting.error_penalty',
  healthFloor: 'adaptive_routing_setting.health_floor',
  inflightPenalty: 'adaptive_routing_setting.inflight_penalty',
  topK: 'adaptive_routing_setting.top_k',
  circuitEnabled: 'adaptive_routing_setting.circuit_enabled',
  openThreshold: 'adaptive_routing_setting.open_threshold',
  cooldownSeconds: 'adaptive_routing_setting.cooldown_seconds',
  halfOpenFactor: 'adaptive_routing_setting.half_open_factor',
  escapeEnabled: 'adaptive_routing_setting.escape_enabled',
  escapeTtftMs: 'adaptive_routing_setting.escape_ttft_ms',
  escapeErrorRate: 'adaptive_routing_setting.escape_error_rate',
} as const satisfies Record<keyof AdaptiveRoutingFormValues, string>

export type AdaptiveRoutingDefaultValues = {
  'adaptive_routing_setting.enabled': boolean
  'adaptive_routing_setting.alpha': number
  'adaptive_routing_setting.ttft_ref_ms': number
  'adaptive_routing_setting.ttft_penalty': number
  'adaptive_routing_setting.error_penalty': number
  'adaptive_routing_setting.health_floor': number
  'adaptive_routing_setting.inflight_penalty': number
  'adaptive_routing_setting.top_k': number
  'adaptive_routing_setting.circuit_enabled': boolean
  'adaptive_routing_setting.open_threshold': number
  'adaptive_routing_setting.cooldown_seconds': number
  'adaptive_routing_setting.half_open_factor': number
  'adaptive_routing_setting.escape_enabled': boolean
  'adaptive_routing_setting.escape_ttft_ms': number
  'adaptive_routing_setting.escape_error_rate': number
}

const buildFormDefaults = (
  defaults: AdaptiveRoutingDefaultValues
): AdaptiveRoutingFormInput => ({
  enabled: defaults['adaptive_routing_setting.enabled'] ?? false,
  alpha: defaults['adaptive_routing_setting.alpha'] ?? 0.2,
  ttftRefMs: defaults['adaptive_routing_setting.ttft_ref_ms'] ?? 2000,
  ttftPenalty: defaults['adaptive_routing_setting.ttft_penalty'] ?? 0.5,
  errorPenalty: defaults['adaptive_routing_setting.error_penalty'] ?? 0.8,
  healthFloor: defaults['adaptive_routing_setting.health_floor'] ?? 0.05,
  inflightPenalty:
    defaults['adaptive_routing_setting.inflight_penalty'] ?? 0.5,
  topK: defaults['adaptive_routing_setting.top_k'] ?? 0,
  circuitEnabled: defaults['adaptive_routing_setting.circuit_enabled'] ?? true,
  openThreshold: defaults['adaptive_routing_setting.open_threshold'] ?? 5,
  cooldownSeconds: defaults['adaptive_routing_setting.cooldown_seconds'] ?? 30,
  halfOpenFactor: defaults['adaptive_routing_setting.half_open_factor'] ?? 0.3,
  escapeEnabled: defaults['adaptive_routing_setting.escape_enabled'] ?? true,
  escapeTtftMs: defaults['adaptive_routing_setting.escape_ttft_ms'] ?? 8000,
  escapeErrorRate:
    defaults['adaptive_routing_setting.escape_error_rate'] ?? 0.5,
})

type AdaptiveRoutingSectionProps = {
  defaultValues: AdaptiveRoutingDefaultValues
}

export function AdaptiveRoutingSection({
  defaultValues,
}: AdaptiveRoutingSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const [healthOpen, setHealthOpen] = useState(false)

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )
  const baselineRef = useRef<AdaptiveRoutingFormValues>(
    formDefaults as AdaptiveRoutingFormValues
  )

  const form = useForm<
    AdaptiveRoutingFormInput,
    unknown,
    AdaptiveRoutingFormValues
  >({
    resolver: zodResolver(adaptiveRoutingSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  const onSubmit = async (values: AdaptiveRoutingFormValues) => {
    const fields = Object.keys(optionKeyByField) as Array<
      keyof AdaptiveRoutingFormValues
    >
    const changed = fields.filter(
      (field) => values[field] !== baselineRef.current[field]
    )
    if (changed.length === 0) {
      toast.info(t('No changes to save'))
      return
    }
    for (const field of changed) {
      await updateOption.mutateAsync({
        key: optionKeyByField[field],
        value: values[field],
      })
    }
    baselineRef.current = values
  }

  return (
    <SettingsSection title={t('Adaptive Routing')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <div className='flex min-w-0 flex-col gap-4'>
            <FormField
              control={form.control}
              name='enabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable adaptive routing')}</FormLabel>
                    <FormDescription>
                      {t(
                        'Passively score channels by observed first-token latency and channel-fault error rate (learned from real traffic, no upstream probing) and steer traffic within each priority layer. Affinity stays authoritative.'
                      )}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
            <div>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={() => setHealthOpen(true)}
              >
                {t('View live channel health')}
              </Button>
            </div>
          </div>

          <ChannelHealthDialog
            open={healthOpen}
            onOpenChange={setHealthOpen}
          />

          <Separator />

          <div className='flex min-w-0 flex-col gap-4'>
            <div className='flex flex-col gap-1'>
              <h4 className='text-sm font-medium'>{t('Health scoring')}</h4>
              <p className='text-sm text-muted-foreground'>
                {t(
                  'Scoring only penalizes slow/erroring channels (never rewards a fast one) to avoid stampeding onto the momentary leader.'
                )}
              </p>
            </div>
            <div className='grid min-w-0 gap-6 lg:grid-cols-3'>
              <FormField
                control={form.control}
                name='alpha'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('EWMA smoothing (alpha)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0.01}
                        max={1}
                        step={0.05}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Weight of the newest sample (0.2 = react over ~5 samples). Higher reacts faster but is noisier.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='ttftRefMs'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('TTFT reference (ms)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={100}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'A channel is only penalized once its first-token latency EWMA exceeds this reference.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='ttftPenalty'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('TTFT penalty (0-1)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={1}
                        step={0.05}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Maximum weight reduction from slow TTFT.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='errorPenalty'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Error penalty (0-1)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={1}
                        step={0.05}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Weight reduction per unit of channel-fault error rate.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='healthFloor'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Health floor (0-1)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={1}
                        step={0.01}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Minimum weight multiplier, so a degraded channel is de-weighted but never fully starved (keeps passive recovery traffic).'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='inflightPenalty'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('In-flight penalty (0-1)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={1}
                        step={0.05}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'De-weight a channel by (1 + penalty x in-flight requests) — the "peak" in Peak-EWMA, avoiding a fast-but-swamped channel. 0 disables load-awareness. Counts are per-instance.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='topK'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Top-K candidates')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Keep only the K highest-weighted candidates before the weighted-random pick. 0 = keep all.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </div>

          <Separator />

          <div className='flex min-w-0 flex-col gap-4'>
            <div className='flex flex-col gap-1'>
              <h4 className='text-sm font-medium'>{t('Circuit breaker')}</h4>
              <p className='text-sm text-muted-foreground'>
                {t(
                  'Opens a channel after consecutive channel-fault failures, then recovers from real traffic after a cooldown (no synthetic probe).'
                )}
              </p>
            </div>
            <div className='grid min-w-0 gap-6 lg:grid-cols-3'>
              <FormField
                control={form.control}
                name='circuitEnabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Enable circuit breaker')}</FormLabel>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />

              <FormField
                control={form.control}
                name='openThreshold'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Open threshold')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={1}
                        step={1}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Consecutive channel-fault failures that open a channel.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='cooldownSeconds'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Cooldown (seconds)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={5}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('How long a channel stays open before half-opening.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='halfOpenFactor'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Half-open factor (0-1)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={1}
                        step={0.05}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Weight multiplier while half-open, so only a trickle of real traffic probes recovery.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </div>

          <Separator />

          <div className='flex min-w-0 flex-col gap-4'>
            <div className='flex flex-col gap-1'>
              <h4 className='text-sm font-medium'>{t('Affinity escape')}</h4>
              <p className='text-sm text-muted-foreground'>
                {t(
                  'Abandon an affinity-anchored channel only when it degrades past these absolute thresholds. Blunt on purpose: leaving forfeits the warm upstream prompt cache.'
                )}
              </p>
            </div>
            <div className='grid min-w-0 gap-6 lg:grid-cols-3'>
              <FormField
                control={form.control}
                name='escapeEnabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Escape on health degradation')}</FormLabel>
                      <FormDescription>
                        {t(
                          'When off, affinity only escapes on hard channel disable.'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />

              <FormField
                control={form.control}
                name='escapeTtftMs'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Escape TTFT (ms)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        step={500}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Abandon the anchor when its TTFT EWMA exceeds this value.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='escapeErrorRate'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Escape error rate (0-1)')}</FormLabel>
                    <FormControl>
                      <Input
                        type='number'
                        min={0}
                        max={1}
                        step={0.05}
                        {...safeNumberFieldProps(field)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t(
                        'Abandon the anchor when its error-rate EWMA exceeds this value.'
                      )}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </div>
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
