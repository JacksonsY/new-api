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
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

type ScoreLevel = 'idle' | 'good' | 'warn' | 'bad'

function scoreLevel(score: number | null): ScoreLevel {
  if (score == null) return 'idle'
  if (score >= 90) return 'good'
  if (score >= 60) return 'warn'
  return 'bad'
}

const LEVEL_TONE: Record<ScoreLevel, string> = {
  idle: 'text-muted-foreground',
  good: 'text-success',
  warn: 'text-warning',
  bad: 'text-destructive',
}

const LEVEL_LABEL: Record<ScoreLevel, string> = {
  idle: 'Idle',
  good: 'Healthy',
  warn: 'At risk',
  bad: 'Degraded',
}

interface HealthScoreRingProps {
  // 0-100, or null when idle (no observed samples yet).
  score: number | null
  size?: number
  strokeWidth?: number
}

// Circular fleet-health gauge: green at >=90, amber at >=60, red below, muted
// when idle. Colour is driven by a currentColor stroke so the arc and the
// centred number always share one tone.
export function HealthScoreRing(props: HealthScoreRingProps) {
  const { t } = useTranslation()
  const size = props.size ?? 132
  const strokeWidth = props.strokeWidth ?? 10
  const radius = (size - strokeWidth) / 2
  const circumference = 2 * Math.PI * radius

  const idle = props.score == null
  const clamped = idle ? 0 : Math.max(0, Math.min(100, props.score as number))
  const dashOffset = idle ? circumference : circumference - (clamped / 100) * circumference

  const level = scoreLevel(props.score)
  const toneClass = LEVEL_TONE[level]
  const conditionLabel = t(LEVEL_LABEL[level])

  return (
    <div className='flex flex-col items-center justify-center gap-3'>
      <div className={cn('relative', toneClass)} style={{ width: size, height: size }}>
        <svg width={size} height={size} className='-rotate-90'>
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            strokeWidth={strokeWidth}
            fill='transparent'
            className='text-muted/40'
            stroke='currentColor'
          />
          <circle
            cx={size / 2}
            cy={size / 2}
            r={radius}
            strokeWidth={strokeWidth}
            fill='transparent'
            stroke='currentColor'
            strokeLinecap='round'
            strokeDasharray={circumference}
            strokeDashoffset={dashOffset}
            className='transition-[stroke-dashoffset] duration-700 ease-out'
          />
        </svg>
        <div className='absolute inset-0 flex flex-col items-center justify-center'>
          <span className='font-mono text-3xl font-bold tabular-nums'>
            {idle ? '—' : clamped}
          </span>
          <span className='text-muted-foreground text-[10px] font-bold tracking-wider uppercase'>
            {t('Health')}
          </span>
        </div>
      </div>
      <div className={cn('text-sm font-semibold', toneClass)}>{conditionLabel}</div>
    </div>
  )
}
