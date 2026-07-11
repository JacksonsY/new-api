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
import { useQuery } from '@tanstack/react-query'
import { VChart } from '@visactor/react-vchart'
import {
  Coins,
  Loader2,
  Network,
  Percent,
  PiggyBank,
  Receipt,
  TrendingUp,
} from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Tabs, TabsList, TabsTrigger } from '@/components/design-system/tabs'
import { Skeleton } from '@/components/ui/skeleton'
import { useTheme } from '@/context/theme-provider'
import { getChannelQuotaDates } from '@/features/dashboard/api'
import {
  TIME_GRANULARITY_OPTIONS,
  TIME_RANGE_PRESETS,
} from '@/features/dashboard/constants'
import { processUserChartData } from '@/features/dashboard/lib'
import type {
  ProcessedUserChartData,
  QuotaDataItem,
} from '@/features/dashboard/types'
import { formatQuota } from '@/lib/format'
import { getRollingDateRange, type TimeGranularity } from '@/lib/time'
import { VCHART_OPTION } from '@/lib/vchart'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

const TOP_OPTIONS = [5, 10, 20, 50]

const CHANNEL_CHARTS: {
  value: string
  labelKey: string
  specKey: keyof ProcessedUserChartData
}[] = [
  {
    value: 'rank',
    labelKey: 'Channel Cost Ranking',
    specKey: 'spec_user_rank',
  },
  {
    value: 'trend',
    labelKey: 'Channel Cost Trend',
    specKey: 'spec_user_trend',
  },
]

// 渠道维度成本统计（仅管理员）：从预聚合表读取渠道成本时间序列，
// 复用用户统计页的图表处理逻辑（渠道名 → username，渠道成本 → quota）。
export function ChannelCharts() {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  const [selectedRange, setSelectedRange] = useState<number>(1)
  const [timeGranularity, setTimeGranularity] =
    useState<TimeGranularity>('hour')
  const [topN, setTopN] = useState<number>(10)

  const timeRange = useMemo(() => {
    const { start, end } = getRollingDateRange(selectedRange)
    return {
      start_timestamp: Math.floor(start.getTime() / 1000),
      end_timestamp: Math.floor(end.getTime() / 1000),
    }
  }, [selectedRange])

  const handleRangeChange = useCallback((days: number) => {
    setSelectedRange(days)
  }, [])

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(resolvedTheme === 'dark' ? 'dark' : 'light')
      setThemeReady(true)
    }
    updateTheme()
  }, [resolvedTheme])

  const { data, isLoading } = useQuery({
    queryKey: ['dashboard', 'channel-quota', timeRange],
    queryFn: () => getChannelQuotaDates(timeRange),
    select: (res) => (res.success ? res.data : null),
    staleTime: 60_000,
  })

  const { mappedData, totals } = useMemo(() => {
    const points = data?.points ?? []
    const channels = data?.channels ?? []
    const nameById = new Map(
      channels.map((c) => [c.channel_id, c.channel_name || `#${c.channel_id}`])
    )
    const mapped: QuotaDataItem[] = points.map((p) => ({
      username: nameById.get(p.channel_id) || `#${p.channel_id}`,
      created_at: p.created_at,
      quota: p.channel_quota ?? 0,
      count: p.count ?? 0,
    }))
    const summed = points.reduce(
      (acc, p) => {
        acc.quota += p.quota ?? 0
        acc.channelQuota += p.channel_quota ?? 0
        return acc
      },
      { quota: 0, channelQuota: 0 }
    )
    return { mappedData: mapped, totals: summed }
  }, [data])

  const chartData = useMemo(() => {
    const cd = processUserChartData(
      isLoading ? [] : mappedData,
      timeGranularity,
      t,
      topN
    )
    cd.spec_user_rank.title = {
      ...cd.spec_user_rank.title,
      text: t('Channel Cost Ranking'),
    }
    cd.spec_user_trend.title = {
      ...cd.spec_user_trend.title,
      text: t('Channel Cost Trend'),
    }
    return cd
  }, [mappedData, isLoading, timeGranularity, t, topN])

  const summaryCards = [
    {
      title: t('Total Channel Cost'),
      value: formatQuota(totals.channelQuota),
      icon: Coins,
    },
    {
      title: t('Total Raw Cost'),
      value: formatQuota(totals.quota),
      icon: Receipt,
    },
    {
      title: t('Overall Ratio'),
      value:
        totals.quota !== 0
          ? `${(totals.channelQuota / totals.quota).toFixed(2)}x`
          : '-',
      icon: Percent,
    },
    {
      title: t('Channel cost saving'),
      value: formatQuota(totals.quota - totals.channelQuota),
      icon: PiggyBank,
    },
  ]

  return (
    <div className='space-y-3'>
      <div className='flex items-center gap-1.5 overflow-x-auto pb-1 sm:gap-2'>
        <Tabs
          value={String(selectedRange)}
          onValueChange={(value) => handleRangeChange(Number(value))}
          className='shrink-0'
        >
          <TabsList>
            {TIME_RANGE_PRESETS.map((preset) => (
              <TabsTrigger
                key={preset.days}
                value={String(preset.days)}
                className='px-2.5 text-xs'
              >
                {t(preset.label)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <Tabs
          value={timeGranularity}
          onValueChange={(value) =>
            setTimeGranularity(value as TimeGranularity)
          }
          className='shrink-0'
        >
          <TabsList>
            {TIME_GRANULARITY_OPTIONS.map((opt) => (
              <TabsTrigger
                key={opt.value}
                value={opt.value}
                className='px-2.5 text-xs'
              >
                {t(opt.label)}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        <Tabs
          value={String(topN)}
          onValueChange={(value) => setTopN(Number(value))}
          className='shrink-0'
        >
          <TabsList>
            <span className='text-muted-foreground px-2 text-xs font-medium whitespace-nowrap'>
              {t('Top Channels')}
            </span>
            {TOP_OPTIONS.map((n) => (
              <TabsTrigger key={n} value={String(n)} className='px-2.5 text-xs'>
                {t('Top {{count}}', { count: n })}
              </TabsTrigger>
            ))}
          </TabsList>
        </Tabs>

        {isLoading && (
          <Loader2 className='text-muted-foreground size-4 animate-spin' />
        )}
      </div>

      <div className='grid grid-cols-2 gap-3 lg:grid-cols-4'>
        {summaryCards.map((card) => {
          const Icon = card.icon
          return (
            <div key={card.title} className='rounded-lg border p-4'>
              <div className='text-muted-foreground flex items-center gap-1.5 text-xs font-medium'>
                <Icon className='text-muted-foreground/60 size-3.5 shrink-0' />
                <span className='truncate'>{card.title}</span>
              </div>
              {isLoading ? (
                <Skeleton className='mt-2 h-7 w-20' />
              ) : (
                <div className='text-foreground mt-1.5 font-mono text-xl font-semibold tabular-nums sm:text-2xl'>
                  {card.value}
                </div>
              )}
            </div>
          )
        })}
      </div>

      <div className='grid gap-3'>
        {CHANNEL_CHARTS.map((chart) => {
          const Icon = chart.value === 'rank' ? Network : TrendingUp
          const spec = chartData[chart.specKey]
          return (
            <div
              key={chart.value}
              className='overflow-hidden rounded-lg border'
            >
              <div className='flex w-full items-center gap-2 border-b px-3 py-2 sm:px-5 sm:py-3'>
                <Icon className='text-muted-foreground/60 size-4' />
                <div className='text-sm font-semibold'>{t(chart.labelKey)}</div>
              </div>
              <div className='h-[300px] p-1.5 sm:h-96 sm:p-2'>
                {isLoading ? (
                  <Skeleton className='h-full w-full' />
                ) : (
                  themeReady &&
                  spec && (
                    <VChart
                      key={`channel-${chart.value}-${topN}-${resolvedTheme}`}
                      spec={{
                        ...spec,
                        theme: resolvedTheme === 'dark' ? 'dark' : 'light',
                        background: 'transparent',
                      }}
                      option={VCHART_OPTION}
                    />
                  )
                )}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
