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
  AlertTriangle,
  BadgeCheck,
  Braces,
  Fingerprint,
  Loader2,
  Radar,
  RotateCw,
} from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import {
  estimateDetectionMinutes,
  estimateDetectionSeconds,
} from './detection-estimate'
import type { DetectionHistoryRequest } from './types'

// Probe stages shown while a run is in flight. The backend reports no
// incremental progress (the job poll is running/done), so the stage is derived
// from the asymptotic progress estimate — informative pacing, not telemetry.
const STAGES = [
  'Connecting to the endpoint',
  'Verifying model identity',
  'Checking protocol compliance',
  'Auditing token usage',
  'Fingerprinting behavior',
  'Running security probes',
  'Aggregating the verdict',
]

function formatClock(totalSeconds: number): string {
  const m = Math.floor(totalSeconds / 60)
  const s = totalSeconds % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

// DetectionProgressCard is the live panel shown while a detection runs — a
// run takes 1–10+ minutes and previously the only feedback was the submit
// button's spinner. Shows elapsed time, an asymptotic progress estimate
// against the mode's expected duration, and the derived probe stage.
// `startedAt` anchors the clock to the real submit time, so a page refresh
// that resumes an in-flight job keeps counting instead of restarting at 0:00.
export function DetectionProgressCard({
  request,
  startedAt,
}: {
  request: DetectionHistoryRequest | null
  startedAt?: number
}) {
  const { t } = useTranslation()
  const startRef = useRef(startedAt ?? Date.now())
  const [elapsed, setElapsed] = useState(() =>
    Math.max(0, Math.floor((Date.now() - startRef.current) / 1000))
  )

  useEffect(() => {
    const id = setInterval(
      () => setElapsed(Math.floor((Date.now() - startRef.current) / 1000)),
      1000
    )
    return () => clearInterval(id)
  }, [])

  const expected = request ? estimateDetectionSeconds(request) : 120
  // Asymptotic fill: ~85% at the expected duration, crawling toward 99 after —
  // never "done" until the poll actually completes.
  const pct = Math.min(99, 100 * (1 - Math.exp((-1.9 * elapsed) / expected)))
  const stage =
    STAGES[Math.min(STAGES.length - 1, Math.floor((pct / 100) * STAGES.length))]
  const longContext = Boolean(
    request?.include_long_context || request?.include_long_context_extreme
  )

  return (
    <div className='pf-card pf-static p-5 sm:p-6'>
      <div className='flex items-center justify-between gap-3'>
        <div className='flex min-w-0 items-center gap-3'>
          <span
            className='flex size-10 shrink-0 items-center justify-center rounded-xl'
            style={{
              background: 'rgba(255,255,255,0.9)',
              border: '1px solid var(--pf-line-2)',
            }}
          >
            <Loader2
              className='size-5 animate-spin'
              style={{ color: 'var(--pf-fire)' }}
            />
          </span>
          <div className='min-w-0'>
            <div className='text-sm font-semibold'>
              {t('Detection in progress')}
            </div>
            {request && (
              <div className='text-muted-foreground truncate text-xs'>
                {request.model} · {request.base_url}
              </div>
            )}
          </div>
        </div>
        <div className='shrink-0 text-right'>
          <div className='text-lg font-semibold tabular-nums'>
            {formatClock(elapsed)}
          </div>
          <div className='text-muted-foreground text-[11px]'>
            {t('Elapsed')}
          </div>
        </div>
      </div>

      <div className='mt-4'>
        <div
          role='progressbar'
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={Math.round(pct)}
          className='h-1.5 overflow-hidden rounded-full'
          style={{ background: 'var(--pf-line-2)' }}
        >
          <div
            className='h-full rounded-full transition-[width] duration-1000 ease-linear'
            style={{ width: `${pct}%`, background: 'var(--pf-grad)' }}
          />
        </div>
        <div className='text-muted-foreground mt-2 flex flex-wrap items-center justify-between gap-x-3 gap-y-1 text-xs'>
          <span className='flex items-center gap-1.5'>
            <span
              className='size-1.5 animate-pulse rounded-full'
              style={{ background: 'var(--pf-grad)' }}
            />
            {t(stage)}
          </span>
          <span>
            {t('Usually finishes in about {{minutes}} min', {
              minutes: request ? estimateDetectionMinutes(request) : 2,
            })}
          </span>
        </div>
        {longContext && (
          <p className='text-muted-foreground mt-2 text-xs'>
            {t('Long-context probing may take several minutes.')}
          </p>
        )}
      </div>
    </div>
  )
}

// DetectionErrorCard surfaces a failed/timed-out run with a one-click retry
// (the form still holds the submitted values, including the key).
export function DetectionErrorCard({
  message,
  onRetry,
}: {
  message: string | null
  onRetry: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className='pf-card pf-static p-5 sm:p-6'>
      <div className='flex items-start gap-3'>
        <span className='bg-destructive/10 flex size-10 shrink-0 items-center justify-center rounded-xl'>
          <AlertTriangle className='text-destructive size-5' />
        </span>
        <div className='min-w-0 flex-1'>
          <div className='text-sm font-semibold'>{t('Detection failed')}</div>
          <p className='text-muted-foreground mt-1 text-sm break-words'>
            {message || t('Detection failed')}
          </p>
        </div>
        <button
          type='button'
          onClick={onRetry}
          className='pf-btn pf-btn-ghost pf-btn-sm shrink-0'
        >
          <RotateCw className='size-3.5' />
          {t('Retry')}
        </button>
      </div>
    </div>
  )
}

// DetectionEmptyState fills the results column before the first run. flex-1 +
// justify-center: on desktop the column stretches to the form card's height,
// so the empty card matches it and centers its content vertically.
export function DetectionEmptyState() {
  const { t } = useTranslation()
  const hints = [
    { icon: BadgeCheck, label: t('Model identity') },
    { icon: Braces, label: t('Protocol compliance') },
    { icon: Fingerprint, label: t('Usage fingerprint') },
  ]
  return (
    <div className='pf-card pf-static flex flex-1 flex-col items-center justify-center px-6 py-12 text-center sm:py-16'>
      <span
        className='flex size-14 items-center justify-center rounded-2xl'
        style={{
          background: 'rgba(255,255,255,0.9)',
          border: '1px solid var(--pf-line-2)',
        }}
      >
        <Radar className='size-7' style={{ color: 'var(--pf-fire)' }} />
      </span>
      <h3
        className='mt-4 text-base font-bold'
        style={{ color: 'var(--pf-ink)' }}
      >
        {t('Your report will appear here')}
      </h3>
      <p className='text-muted-foreground mt-1.5 max-w-sm text-sm leading-relaxed'>
        {t(
          'Point the form at any relay endpoint and run a detection to get a field-level authenticity report with per-probe evidence.'
        )}
      </p>
      <div className='mt-5 flex flex-wrap items-center justify-center gap-2'>
        {hints.map((h) => (
          <span key={h.label} className='pf-pill'>
            <h.icon className='size-3.5' style={{ color: 'var(--pf-fire)' }} />
            {h.label}
          </span>
        ))}
      </div>
    </div>
  )
}
