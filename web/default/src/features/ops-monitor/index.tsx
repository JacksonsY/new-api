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
  Activity,
  AlertTriangle,
  CheckCircle2,
  Coins,
  Gauge,
  Percent,
  Radio,
  Timer,
  XCircle,
  Zap,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'

import { resetChannelHealth } from './api'
import { ChannelHealthTable } from './components/channel-health-table'
import { HealthScoreRing } from './components/health-score-ring'
import { MetricTile, type MetricTone } from './components/metric-tile'
import { OpsMonitorHeader } from './components/ops-monitor-header'
import { useChannelHealth } from './hooks/use-channel-health'
import { computeOverview } from './lib/overview'

function ttftToneOf(ms: number | null): MetricTone {
  if (ms == null) return 'muted'
  if (ms >= 8000) return 'destructive'
  if (ms >= 2000) return 'warning'
  return 'default'
}

function errorRateToneOf(rate: number | null): MetricTone {
  if (rate == null) return 'muted'
  if (rate >= 0.5) return 'destructive'
  if (rate >= 0.2) return 'warning'
  return 'default'
}

export function OpsMonitor() {
  const { t } = useTranslation()
  const health = useChannelHealth()
  const [fullscreen, setFullscreen] = useState(false)

  const overview = useMemo(
    () => computeOverview(health.rows),
    [health.rows]
  )

  useEffect(() => {
    if (!fullscreen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFullscreen(false)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [fullscreen])

  const handleReset = useCallback(
    async (channelId?: number) => {
      try {
        await resetChannelHealth(channelId)
        toast.success(t('Done'))
        health.refresh()
      } catch {
        toast.error(t('Request failed'))
      }
    },
    [t, health]
  )

  const ttftTone = ttftToneOf(overview.avgTtftMs)
  const errorTone = errorRateToneOf(overview.avgErrorRate)

  const tiles = [
    {
      icon: Radio,
      label: t('Observed channels'),
      value: overview.total,
      tone: 'default' as MetricTone,
      hint:
        overview.disabled > 0
          ? t('{{count}} disabled', { count: overview.disabled })
          : undefined,
    },
    {
      icon: CheckCircle2,
      label: t('Healthy'),
      value: overview.healthy,
      tone: (overview.healthy > 0 ? 'success' : 'muted') as MetricTone,
    },
    {
      icon: AlertTriangle,
      label: t('Half-open'),
      value: overview.degraded,
      tone: (overview.degraded > 0 ? 'warning' : 'muted') as MetricTone,
    },
    {
      icon: XCircle,
      label: t('Circuit open'),
      value: overview.open,
      tone: (overview.open > 0 ? 'destructive' : 'muted') as MetricTone,
    },
    {
      icon: Activity,
      label: t('Total in-flight'),
      value: overview.inflightTotal,
      tone: 'default' as MetricTone,
    },
    {
      icon: Gauge,
      label: t('RPM'),
      value: overview.totalRpm,
      tone: 'default' as MetricTone,
      hint: t('requests / min'),
    },
    {
      icon: Coins,
      label: t('TPM'),
      value:
        overview.totalTpm >= 1000
          ? `${(overview.totalTpm / 1000).toFixed(1)}k`
          : String(overview.totalTpm),
      tone: 'default' as MetricTone,
      hint: t('output tokens / min'),
    },
    {
      icon: Timer,
      label: t('Avg TTFT'),
      value:
        overview.avgTtftMs == null
          ? '—'
          : `${Math.round(overview.avgTtftMs)} ms`,
      tone: ttftTone,
    },
    {
      icon: Zap,
      label: t('Avg TPS'),
      value:
        overview.avgTpsTokPerSec == null
          ? '—'
          : `${overview.avgTpsTokPerSec.toFixed(0)} tok/s`,
      tone: 'default' as MetricTone,
    },
    {
      icon: Percent,
      label: t('Avg error rate'),
      value:
        overview.avgErrorRate == null
          ? '—'
          : `${(overview.avgErrorRate * 100).toFixed(1)}%`,
      tone: errorTone,
    },
  ]

  const content = (
    <div className='space-y-4'>
      <OpsMonitorHeader
        enabled={health.enabled}
        loading={health.loading}
        lastUpdated={health.lastUpdated}
        autoRefresh={health.autoRefresh}
        onAutoRefreshChange={health.setAutoRefresh}
        intervalSec={health.intervalSec}
        onIntervalChange={health.setIntervalSec}
        countdown={health.countdown}
        onRefresh={health.refresh}
        onResetAll={() => void handleReset()}
        hasRows={health.rows.length > 0}
        fullscreen={fullscreen}
        onToggleFullscreen={() => setFullscreen((prev) => !prev)}
      />

      {health.error && (
        <div className='bg-destructive/10 text-destructive border-destructive/20 rounded-lg border px-3 py-2 text-xs'>
          {t('Failed to refresh; showing the last known values.')}
        </div>
      )}

      <div className='grid grid-cols-1 gap-4 lg:grid-cols-[220px_1fr]'>
        <div className='bg-card flex items-center justify-center rounded-2xl border p-6'>
          <HealthScoreRing score={overview.healthScore} />
        </div>
        <div className='grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-4'>
          {tiles.map((tile) => (
            <MetricTile
              key={tile.label}
              icon={tile.icon}
              label={tile.label}
              value={tile.value}
              tone={tile.tone}
              hint={tile.hint}
              loading={health.loading && health.rows.length === 0}
            />
          ))}
        </div>
      </div>

      <ChannelHealthTable
        rows={health.rows}
        onReset={(channelId) => void handleReset(channelId)}
      />
    </div>
  )

  if (fullscreen) {
    return (
      <div className='bg-background fixed inset-0 z-50 overflow-auto p-4 md:p-6'>
        {content}
      </div>
    )
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('Ops Monitor')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>{content}</SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
