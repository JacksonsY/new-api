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
  CheckCircle2,
  CircleSlash,
  XCircle,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

import type {
  DetectionReport,
  DetectorResultItem,
  DetectorResultStatus,
  DetectorVerdict,
} from './types'

// 判定徽章：passed=通过 marginal=存疑 failed=不通过。
export function VerdictBadge({ verdict }: { verdict: DetectorVerdict }) {
  const { t } = useTranslation()
  if (verdict === 'passed') {
    return <StatusBadge variant='success'>{t('Passed')}</StatusBadge>
  }
  if (verdict === 'marginal') {
    return <StatusBadge variant='warning'>{t('Marginal')}</StatusBadge>
  }
  return <StatusBadge variant='destructive'>{t('Failed')}</StatusBadge>
}

function verdictToneClass(verdict: DetectorVerdict): string {
  if (verdict === 'passed') return 'text-success'
  if (verdict === 'marginal') return 'text-warning'
  return 'text-destructive'
}

// 分数环：以 SVG 圆环表达 0-100 总分，颜色随判定语义变化。
function ScoreRing({
  score,
  verdict,
}: {
  score: number
  verdict: DetectorVerdict
}) {
  const clamped = Math.max(0, Math.min(100, score))
  const radius = 34
  const circumference = 2 * Math.PI * radius
  const offset = circumference * (1 - clamped / 100)
  return (
    <div className='relative flex size-24 shrink-0 items-center justify-center'>
      <svg className='size-24 -rotate-90' viewBox='0 0 80 80'>
        <circle
          cx='40'
          cy='40'
          r={radius}
          fill='none'
          strokeWidth='6'
          className='stroke-muted'
        />
        <circle
          cx='40'
          cy='40'
          r={radius}
          fill='none'
          strokeWidth='6'
          strokeLinecap='round'
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          className={cn('transition-all', verdictToneClass(verdict))}
          stroke='currentColor'
        />
      </svg>
      <div className='absolute flex flex-col items-center'>
        <span
          className={cn(
            'text-2xl font-semibold tabular-nums',
            verdictToneClass(verdict)
          )}
        >
          {Math.round(clamped)}
        </span>
        <span className='text-muted-foreground text-[11px]'>/ 100</span>
      </div>
    </div>
  )
}

function ResultStatusIcon({ status }: { status: DetectorResultStatus }) {
  if (status === 'pass') {
    return <CheckCircle2 className='text-success size-4 shrink-0' />
  }
  if (status === 'fail') {
    return <XCircle className='text-destructive size-4 shrink-0' />
  }
  if (status === 'error') {
    return <AlertTriangle className='text-warning size-4 shrink-0' />
  }
  return <CircleSlash className='text-muted-foreground size-4 shrink-0' />
}

function DetectorResultRow({ item }: { item: DetectorResultItem }) {
  const { t } = useTranslation()
  return (
    <li className='flex items-start gap-3 py-2.5'>
      <ResultStatusIcon status={item.status} />
      <div className='min-w-0 flex-1'>
        <div className='flex items-center justify-between gap-2'>
          <span className='truncate text-sm font-medium'>
            {item.display_name || item.name}
          </span>
          <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
            {Math.round(item.score)} · {t('Weight')} {item.weight}
          </span>
        </div>
        {item.details && (
          <p className='text-muted-foreground mt-0.5 text-xs leading-relaxed break-words'>
            {item.details}
          </p>
        )}
        {item.error && (
          <p className='text-destructive mt-0.5 text-xs break-words'>
            {item.error}
          </p>
        )}
      </div>
    </li>
  )
}

// 检测报告卡片：分数环 + 判定 + 各探测器证据 + 识别到的品牌。
export function DetectorReportCard({ report }: { report: DetectionReport }) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-col gap-4'>
      <div className='flex items-center gap-4 rounded-lg border p-4'>
        <ScoreRing score={report.total_score} verdict={report.verdict} />
        <div className='min-w-0 flex-1 space-y-1.5'>
          <div className='flex flex-wrap items-center gap-2'>
            <VerdictBadge verdict={report.verdict} />
            {report.critical_count > 0 && (
              <StatusBadge variant='destructive'>
                {report.critical_count} {t('Critical Issue')}
              </StatusBadge>
            )}
          </div>
          <div className='text-muted-foreground text-xs'>
            {report.target_model} · {report.protocol} · {report.mode}
          </div>
          {report.summary && (
            <p className='text-sm leading-relaxed break-words'>
              {report.summary}
            </p>
          )}
        </div>
      </div>

      {(report.self_reported_identity ||
        report.detected_brands.length > 0) && (
        <div className='grid gap-3 rounded-lg border p-4 sm:grid-cols-2'>
          {report.self_reported_identity && (
            <div>
              <div className='text-muted-foreground text-xs font-medium'>
                {t('Thinking Signature')}
              </div>
              <div className='mt-1 text-sm break-words'>
                {report.self_reported_identity}
              </div>
            </div>
          )}
          {report.detected_brands.length > 0 && (
            <div>
              <div className='text-muted-foreground text-xs font-medium'>
                {t('Detected Backend')}
              </div>
              <div className='mt-1 flex flex-wrap gap-1.5'>
                {report.detected_brands.map((brand) => (
                  <Badge
                    key={brand}
                    variant='outline'
                    className='border-destructive/50 text-destructive'
                  >
                    {brand}
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      <div className='rounded-lg border'>
        <div className='border-b px-4 py-2.5 text-sm font-medium'>
          {t('Detector Results')}
        </div>
        <ul className='divide-border/60 divide-y px-4'>
          {report.results.map((item) => (
            <DetectorResultRow key={item.name} item={item} />
          ))}
        </ul>
      </div>
    </div>
  )
}
