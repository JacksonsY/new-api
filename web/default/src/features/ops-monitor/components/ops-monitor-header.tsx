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
import {
  HeartPulse,
  Maximize2,
  Minimize2,
  RefreshCw,
  RotateCcw,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/design-system/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'

import { REFRESH_INTERVALS } from '../hooks/use-channel-health'

interface OpsMonitorHeaderProps {
  enabled: boolean
  loading: boolean
  lastUpdated: Date | null
  autoRefresh: boolean
  onAutoRefreshChange: (value: boolean) => void
  intervalSec: number
  onIntervalChange: (value: number) => void
  countdown: number
  onRefresh: () => void
  onResetAll: () => void
  hasRows: boolean
  fullscreen: boolean
  onToggleFullscreen: () => void
}

export function OpsMonitorHeader(props: OpsMonitorHeaderProps) {
  const { t } = useTranslation()

  return (
    <div className='bg-card flex flex-col gap-4 rounded-2xl border p-4 sm:p-5'>
      <div className='flex flex-wrap items-start justify-between gap-4'>
        <div className='min-w-0'>
          <h1 className='flex items-center gap-2 text-lg font-bold'>
            <HeartPulse className='text-primary size-5 shrink-0' />
            {t('Ops Monitor')}
          </h1>
          <div className='text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs'>
            <span className='flex items-center gap-1.5'>
              <span
                className={cn(
                  'relative inline-flex size-2 rounded-full',
                  props.loading ? 'bg-muted-foreground' : 'bg-success'
                )}
              />
              {props.loading ? t('Refreshing...') : t('Live')}
            </span>
            <span aria-hidden>·</span>
            <span>
              {t('Last updated')}:{' '}
              {props.lastUpdated ? props.lastUpdated.toLocaleTimeString() : '—'}
            </span>
            {props.autoRefresh && (
              <>
                <span aria-hidden>·</span>
                <span>
                  {t('Next in {{seconds}}s', { seconds: props.countdown })}
                </span>
              </>
            )}
          </div>
        </div>

        <div className='flex flex-wrap items-center gap-2'>
          <label className='text-muted-foreground flex items-center gap-1.5 text-xs font-medium'>
            <Switch
              checked={props.autoRefresh}
              onCheckedChange={props.onAutoRefreshChange}
            />
            {t('Auto refresh')}
          </label>
          <NativeSelect
            size='sm'
            value={String(props.intervalSec)}
            disabled={!props.autoRefresh}
            onChange={(e) => props.onIntervalChange(Number(e.target.value))}
            aria-label={t('Refresh interval')}
          >
            {REFRESH_INTERVALS.map((sec) => (
              <NativeSelectOption key={sec} value={String(sec)}>
                {sec}s
              </NativeSelectOption>
            ))}
          </NativeSelect>

          <div className='bg-border mx-1 hidden h-4 w-px sm:block' />

          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onRefresh}
            disabled={props.loading}
            className='h-8 gap-1.5'
          >
            <RefreshCw
              className={cn('size-3.5', props.loading && 'animate-spin')}
            />
            <span className='hidden sm:inline'>{t('Refresh')}</span>
          </Button>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onResetAll}
            disabled={!props.hasRows}
            className='h-8 gap-1.5'
          >
            <RotateCcw className='size-3.5' />
            <span className='hidden sm:inline'>{t('Clear all')}</span>
          </Button>
          <Button
            type='button'
            variant='outline'
            size='icon'
            onClick={props.onToggleFullscreen}
            className='size-8'
            title={props.fullscreen ? t('Exit fullscreen') : t('Fullscreen')}
          >
            {props.fullscreen ? (
              <Minimize2 className='size-3.5' />
            ) : (
              <Maximize2 className='size-3.5' />
            )}
          </Button>
        </div>
      </div>

      {!props.enabled && (
        <div className='bg-warning/10 text-warning border-warning/20 rounded-lg border px-3 py-2 text-xs'>
          {t('Adaptive routing is disabled; values are stale until enabled.')}
        </div>
      )}
    </div>
  )
}
