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
  Copy,
  Download,
  Plug,
  XCircle,
  Zap,
} from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StatusBadge } from '@/components/status-badge'
import { Badge } from '@/components/ui/badge'
import { copyToClipboard } from '@/lib/copy-to-clipboard'
import { cn } from '@/lib/utils'

import type {
  DetectionReport,
  DetectorDetails,
  DetectorResultItem,
  DetectorResultStatus,
  DetectorVerdict,
} from './types'
import { verdictToneClass } from './verdict'

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

// --- Structured evidence rendering ------------------------------------------
// The backend sends each detector's `details` as a nested object. We render it
// as structured UI (booleans → ✓/✗, `issues` → severity rows, `sub_checks` →
// per-check pass marks, nested objects → indented key/value blocks) rather than
// dumping raw JSON.

type IssueLike = { severity?: string; code?: string; message?: string }

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v)
}

function isIssueLike(v: unknown): v is IssueLike {
  return isPlainObject(v) && ('severity' in v || 'message' in v || 'code' in v)
}

// detailsHasCritical reports whether a detector's evidence carries a critical
// issue, so critical failures can be surfaced above ordinary ones.
function detailsHasCritical(details?: DetectorDetails): boolean {
  if (!details) return false
  const count = details.critical_issue_count
  if (typeof count === 'number' && count > 0) return true
  const issues = details.issues
  return (
    Array.isArray(issues) &&
    issues.some((i) => isPlainObject(i) && i.severity === 'critical')
  )
}

// resultRank orders detectors so the most actionable rows come first: critical
// failures, then other failures, then errors, then passes, then skips. Within a
// rank, Array.sort is stable so the backend's definition order is preserved.
function resultRank(item: DetectorResultItem): number {
  if (item.status === 'fail') return detailsHasCritical(item.details) ? 0 : 1
  if (item.status === 'error') return 2
  if (item.status === 'pass') return 3
  return 4 // skip
}

function issueSeverityVariant(
  severity?: string
): 'destructive' | 'warning' | 'neutral' {
  if (severity === 'critical' || severity === 'major') return 'destructive'
  if (severity === 'minor') return 'warning'
  return 'neutral'
}

// IssueList renders an `issues` array as severity-badged rows, collapsing
// identical issues into one row with a ×N count (e.g. 9× id_prefix_invalid).
function IssueList({ issues }: { issues: unknown[] }) {
  const grouped = new Map<string, { issue: IssueLike; count: number }>()
  for (const raw of issues) {
    const issue: IssueLike = isIssueLike(raw) ? raw : { message: String(raw) }
    const key = `${issue.severity ?? ''}|${issue.code ?? ''}|${issue.message ?? ''}`
    const existing = grouped.get(key)
    if (existing) existing.count += 1
    else grouped.set(key, { issue, count: 1 })
  }
  return (
    <ul className='mt-0.5 space-y-1'>
      {[...grouped.entries()].map(([key, { issue, count }]) => (
        <li key={key} className='flex flex-wrap items-center gap-1.5'>
          <StatusBadge variant={issueSeverityVariant(issue.severity)}>
            {issue.severity ?? 'issue'}
          </StatusBadge>
          <span className='text-muted-foreground break-words'>
            {issue.message ?? issue.code ?? ''}
            {count > 1 && <span className='text-muted-foreground/70'> ×{count}</span>}
          </span>
        </li>
      ))}
    </ul>
  )
}

type EvidenceView = 'simple' | 'json'

// failureHints names the checks a failing detector did not pass, so a failure
// explains itself in the simple view: sub_checks with pass:false, plus top-level
// `*_match` / `*_ok` booleans that are false.
function failureHints(details?: DetectorDetails): string[] {
  if (!details) return []
  const out: string[] = []
  const sub = details.sub_checks
  if (isPlainObject(sub)) {
    for (const [k, v] of Object.entries(sub)) {
      if (isPlainObject(v) && v.pass === false) out.push(k)
    }
  }
  for (const [k, v] of Object.entries(details)) {
    if (v === false && (k.endsWith('_match') || k.endsWith('_ok'))) out.push(k)
  }
  return [...new Set(out)]
}

// compactDetail renders a detector's notable top-level scalar fields on one line
// — the last-resort evidence for a failing detector that has neither issues nor
// named checks (e.g. basic_request's finish_reason, a behavior probe's stop_reason).
function compactDetail(details?: DetectorDetails): string | null {
  if (!details) return null
  const skip = new Set(['issues', 'sub_checks', 'summary', 'evaluation_zh'])
  const parts: string[] = []
  for (const [k, v] of Object.entries(details)) {
    if (skip.has(k)) continue
    if (typeof v === 'string' && v.length > 0 && v.length <= 120) {
      parts.push(`${k}: ${v}`)
    } else if (typeof v === 'number' || typeof v === 'boolean') {
      parts.push(`${k}: ${v}`)
    }
    if (parts.length >= 4) break
  }
  return parts.length ? parts.join(' · ') : null
}

// SimpleEvidence is the default, human-readable view: it surfaces only what a
// person needs to act on — the plain-language issue message(s) for a failing
// detector, or the reason a detector was skipped — and stays clean (silent) for
// a passing one. The raw fields live behind the JSON toggle instead.
function SimpleEvidence({ item }: { item: DetectorResultItem }) {
  const details = item.details
  const issues = details?.issues
  if (Array.isArray(issues) && issues.length > 0) {
    return <IssueList issues={issues} />
  }
  // A detector may provide a one-line human summary worth showing even on a pass:
  // an explicit `summary` (e.g. the speed benchmark's TTFT / tok-s) or an
  // `evaluation_zh` assessment (token billing, structured output).
  let summary: string | null = null
  if (typeof details?.summary === 'string') {
    summary = details.summary
  } else if (typeof details?.evaluation_zh === 'string') {
    summary = details.evaluation_zh
  }
  if (summary) {
    return (
      <p className='text-foreground/80 mt-0.5 text-xs break-words'>{summary}</p>
    )
  }
  // A failing detector with no issue/summary still explains itself: the checks it
  // did not pass, or (last resort) a compact line of its scalar evidence.
  if (item.status === 'fail' || item.status === 'error') {
    const hints = failureHints(details)
    if (hints.length > 0) {
      return (
        <p className='text-destructive/90 mt-0.5 flex items-start gap-1 text-xs'>
          <XCircle className='mt-px size-3 shrink-0' />
          <span className='break-words'>{hints.join(', ')}</span>
        </p>
      )
    }
    const compact = compactDetail(details)
    if (compact) {
      return (
        <p className='text-muted-foreground mt-0.5 text-xs break-words'>
          {compact}
        </p>
      )
    }
  }
  if (item.status === 'skip') {
    const reason = typeof details?.reason === 'string' ? details.reason : null
    return reason ? (
      <p className='text-muted-foreground mt-0.5 text-xs break-words'>{reason}</p>
    ) : null
  }
  return null
}

// JsonEvidence is the opt-in raw view: the detector's full `details` object,
// pretty-printed. Wide content scrolls inside its own box.
function JsonEvidence({ details }: { details?: DetectorDetails }) {
  if (!details || Object.keys(details).length === 0) return null
  return (
    <pre className='bg-muted/50 text-muted-foreground mt-1 max-h-80 overflow-auto rounded-md p-2 font-mono text-[11px] leading-snug'>
      {JSON.stringify(details, null, 2)}
    </pre>
  )
}

function DetectorResultRow({
  item,
  view,
}: {
  item: DetectorResultItem
  view: EvidenceView
}) {
  return (
    <li className='flex items-start gap-3 py-2.5'>
      <ResultStatusIcon status={item.status} />
      <div className='min-w-0 flex-1'>
        <div className='flex items-center justify-between gap-2'>
          <span className='truncate text-sm font-medium'>
            {item.display_name || item.name}
          </span>
          <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
            {Math.round(item.score)}
            {item.duration_ms >= 100 && (
              <span className='text-muted-foreground/60'>
                {' '}
                · {(item.duration_ms / 1000).toFixed(1)}s
              </span>
            )}
          </span>
        </div>
        {view === 'json' ? (
          <JsonEvidence details={item.details} />
        ) : (
          <SimpleEvidence item={item} />
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

// SegmentedControl is the compact pill-toggle used for both the status filter
// and the Simple/JSON evidence view switch.
function SegmentedControl<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T
  onChange: (v: T) => void
  options: { value: T; label: string }[]
}) {
  return (
    <div className='border-border inline-flex overflow-hidden rounded-md border text-xs'>
      {options.map((opt, i) => (
        <button
          key={opt.value}
          type='button'
          onClick={() => onChange(opt.value)}
          className={cn(
            'px-2.5 py-1 whitespace-nowrap transition-colors',
            i > 0 && 'border-border border-l',
            value === opt.value
              ? 'bg-primary text-primary-foreground'
              : 'text-muted-foreground hover:bg-muted'
          )}
        >
          {opt.label}
        </button>
      ))}
    </div>
  )
}

type ResultFilter = 'all' | 'failed' | 'passed' | 'skipped'

function matchesFilter(item: DetectorResultItem, filter: ResultFilter): boolean {
  if (filter === 'failed') return item.status === 'fail' || item.status === 'error'
  if (filter === 'passed') return item.status === 'pass'
  if (filter === 'skipped') return item.status === 'skip'
  return true
}

// triggerReportDownload saves the full report as a JSON file — shareable
// evidence for a relay operator or a group chat.
function triggerReportDownload(report: DetectionReport) {
  const blob = new Blob([JSON.stringify(report, null, 2)], {
    type: 'application/json',
  })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `detection-${report.target_model}-${Date.now()}.json`
  a.click()
  URL.revokeObjectURL(url)
}

// 检测报告卡片：分数环 + 判定 + 复制/下载 + 状态筛选的各探测器证据。
export function DetectorReportCard({ report }: { report: DetectionReport }) {
  const { t } = useTranslation()
  const [view, setView] = useState<EvidenceView>('simple')
  const [filter, setFilter] = useState<ResultFilter>('all')
  // Backend omits empty `detected_brands`/`results` (json ,omitempty), so guard
  // against undefined before reading .length / .map.
  const detectedBrands = report.detected_brands ?? []
  const results = report.results ?? []
  // Surface the most actionable detectors first (critical failures → … → skips);
  // with full-mode runs now returning 30+ detectors, definition order buries the
  // failures a user most wants to see.
  const sortedResults = [...results].sort((a, b) => resultRank(a) - resultRank(b))
  const passCount = results.filter((r) => r.status === 'pass').length
  const failCount = results.filter(
    (r) => r.status === 'fail' || r.status === 'error'
  ).length
  const skipCount = results.filter((r) => r.status === 'skip').length
  const visibleResults = sortedResults.filter((r) => matchesFilter(r, filter))
  // Only offer filters that select something; `all` is always present.
  const filterOptions: { value: ResultFilter; label: string }[] = [
    { value: 'all', label: `${t('All')} ${results.length}` },
    ...(failCount > 0
      ? [{ value: 'failed' as const, label: `${t('Failed')} ${failCount}` }]
      : []),
    ...(passCount > 0
      ? [{ value: 'passed' as const, label: `${t('Passed')} ${passCount}` }]
      : []),
    ...(skipCount > 0
      ? [{ value: 'skipped' as const, label: `${t('Skipped')} ${skipCount}` }]
      : []),
  ]
  // The speed detector always passes, so it sorts to the bottom; surface its
  // one-line summary (TTFT / tok-s) in the header where a user looks first.
  const speedResult = results.find((r) => r.name === 'speed')
  const speedSummary =
    typeof speedResult?.details?.summary === 'string'
      ? speedResult.details.summary
      : null

  async function handleCopy() {
    const ok = await copyToClipboard(JSON.stringify(report, null, 2))
    if (ok) toast.success(t('Copied to clipboard'))
    else toast.error(t('Copy failed'))
  }

  return (
    <div className='flex flex-col gap-4'>
      <div className='flex items-start gap-4 rounded-lg border p-4'>
        <ScoreRing score={report.total_score} verdict={report.verdict} />
        <div className='min-w-0 flex-1 space-y-1.5'>
          <div className='flex flex-wrap items-center gap-2'>
            <VerdictBadge verdict={report.verdict} />
            {report.critical_count > 0 && (
              <StatusBadge variant='destructive'>
                {report.critical_count} {t('Critical Issue')}
              </StatusBadge>
            )}
            {report.backend_origin && (
              <StatusBadge
                variant={
                  report.backend_origin.includes('疑似') ? 'destructive' : 'info'
                }
              >
                {report.backend_origin}
              </StatusBadge>
            )}
            <span className='ml-auto flex shrink-0 items-center gap-0.5'>
              <button
                type='button'
                onClick={handleCopy}
                title={t('Copy report JSON')}
                className='text-muted-foreground hover:text-foreground hover:bg-muted rounded-md p-1.5 transition-colors'
              >
                <Copy className='size-4' />
              </button>
              <button
                type='button'
                onClick={() => triggerReportDownload(report)}
                title={t('Download report')}
                className='text-muted-foreground hover:text-foreground hover:bg-muted rounded-md p-1.5 transition-colors'
              >
                <Download className='size-4' />
              </button>
            </span>
          </div>
          <div className='text-muted-foreground text-xs'>
            {report.target_model} · {report.protocol} · {report.mode}
          </div>
          {report.probed_endpoint && (
            <div className='text-muted-foreground flex items-center gap-1.5 text-xs'>
              <Plug className='size-3.5 shrink-0' />
              <span>{t('Probed endpoint')}</span>
              <code className='bg-muted rounded px-1 py-0.5 font-mono text-[11px] break-all'>
                {report.probed_endpoint}
              </code>
            </div>
          )}
          {speedSummary && (
            <div className='text-foreground/80 flex items-center gap-1.5 text-xs'>
              <Zap className='text-warning size-3.5 shrink-0' />
              <span className='break-words'>{speedSummary}</span>
            </div>
          )}
          {report.summary && (
            <p className='text-sm leading-relaxed break-words'>
              {report.summary}
            </p>
          )}
        </div>
      </div>

      {(report.self_reported_identity || detectedBrands.length > 0) && (
        <div className='grid gap-3 rounded-lg border p-4 sm:grid-cols-2'>
          {report.self_reported_identity && (
            <div>
              <div className='text-muted-foreground text-xs font-medium'>
                {t('Self-reported Identity')}
              </div>
              <div className='mt-1 text-sm break-words'>
                {report.self_reported_identity}
              </div>
            </div>
          )}
          {detectedBrands.length > 0 && (
            <div>
              <div className='text-muted-foreground text-xs font-medium'>
                {t('Detected Backend')}
              </div>
              <div className='mt-1 flex flex-wrap gap-1.5'>
                {detectedBrands.map((brand) => (
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
        <div className='flex flex-wrap items-center justify-between gap-2 border-b px-4 py-2.5'>
          <span className='text-sm font-medium'>{t('Detector Results')}</span>
          <div className='flex flex-wrap items-center gap-2'>
            <SegmentedControl
              value={filter}
              onChange={setFilter}
              options={filterOptions}
            />
            <SegmentedControl
              value={view}
              onChange={setView}
              options={[
                { value: 'simple', label: t('Simple') },
                { value: 'json', label: 'JSON' },
              ]}
            />
          </div>
        </div>
        <ul className='divide-border/60 divide-y px-4'>
          {visibleResults.map((item) => (
            <DetectorResultRow key={item.name} item={item} view={view} />
          ))}
        </ul>
      </div>
    </div>
  )
}
