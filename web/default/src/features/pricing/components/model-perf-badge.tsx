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
import { memo } from 'react'
import { useTranslation } from 'react-i18next'

import {
  getSuccessRateDotClass,
  getSuccessRateTextClass,
} from '@/features/performance-metrics/lib/format'
import { cn } from '@/lib/utils'

export type ModelPerfBadgeData = {
  avg_latency_ms: number
  success_rate: number
  avg_tps: number
  recent_success_rates?: number[]
}

function formatCompactNumber(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '—'
  return value > 1 ? String(Math.round(value)) : value.toFixed(1)
}

function formatCompactLatency(ms: number): string {
  if (!Number.isFinite(ms) || ms <= 0) return '—'
  if (ms >= 1_000) return `${formatCompactNumber(ms / 1_000)}s`
  return `${formatCompactNumber(ms)}ms`
}

function formatCompactThroughput(tps: number): string {
  if (!Number.isFinite(tps) || tps <= 0) return '—'
  if (tps >= 1_000) return `${formatCompactNumber(tps / 1_000)}Kt`
  return `${formatCompactNumber(tps)}t`
}


/** 定价表「可用率」列的段条数；与后端汇总带回的逐桶成功率个数一致(24 桶≈一天)。 */
const AVAILABILITY_BAR_COUNT = 24

/**
 * Uptime 风格的可用率单元格:逐桶成功率画成一排竖条,右侧带总成功率。
 * 数据不足 24 桶时左侧以哑色占位,保证条带总宽稳定、新旧模型对齐。
 */
export const AvailabilityBars = memo(function AvailabilityBars(props: {
  perf: ModelPerfBadgeData | undefined
}) {
  const { t } = useTranslation()

  if (!props.perf) {
    return null
  }

  const rates = (props.perf.recent_success_rates ?? []).filter((rate) =>
    Number.isFinite(rate)
  )
  // slot 即桶在固定 24 格时间轴上的位置，天然就是每根竖条的身份。
  const bars = [
    ...Array(Math.max(0, AVAILABILITY_BAR_COUNT - rates.length)).fill(null),
    ...rates.slice(-AVAILABILITY_BAR_COUNT),
  ].map((rate: number | null, slot) => ({ slot, rate }))

  return (
    <div
      className='flex items-center gap-2'
      title={`${t('Average latency')}: ${formatCompactLatency(props.perf.avg_latency_ms)} · ${t('Throughput')}: ${formatCompactThroughput(props.perf.avg_tps)}`}
    >
      <div className='flex items-center gap-[2px]'>
        {bars.map((bar) => (
          <span
            key={bar.slot}
            className={cn(
              'h-3 w-[3px] rounded-full',
              bar.rate == null
                ? 'bg-muted-foreground/15'
                : getSuccessRateDotClass(bar.rate)
            )}
          />
        ))}
      </div>
      <span
        className={cn(
          'text-xs font-medium tabular-nums',
          getSuccessRateTextClass(props.perf.success_rate)
        )}
      >
        {props.perf.success_rate.toFixed(1)}%
      </span>
    </div>
  )
})
