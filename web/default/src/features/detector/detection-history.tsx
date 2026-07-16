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
import { ChevronDown, History, RotateCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { toIntlLocale } from '@/i18n/languages'
import {
  formatTimestampRelative,
  formatTimestampToDate,
} from '@/lib/format'
import { cn } from '@/lib/utils'

import { DetectorReportCard, VerdictBadge } from './detector-report'
import type {
  DetectionHistoryEntry,
  DetectionHistoryRequest,
} from './types'
import { verdictToneClass } from './verdict'

// HistoryRow is one collapsed detection: a scannable summary (score, model,
// endpoint, when) that expands to the full report card, plus re-run / delete.
function HistoryRow({
  entry,
  expanded,
  onToggle,
  onRemove,
  onRerun,
}: {
  entry: DetectionHistoryEntry
  expanded: boolean
  onToggle: () => void
  onRemove: () => void
  onRerun?: (req: DetectionHistoryRequest) => void
}) {
  const { t, i18n } = useTranslation()
  const { report } = entry
  return (
    <li className='overflow-hidden'>
      <div className='hover:bg-muted/40 flex items-center gap-3 px-4 py-3 transition-colors'>
        <button
          type='button'
          onClick={onToggle}
          aria-expanded={expanded}
          className='flex min-w-0 flex-1 items-center gap-3 text-left'
        >
          <ChevronDown
            className={cn(
              'text-muted-foreground size-4 shrink-0 transition-transform',
              expanded && 'rotate-180'
            )}
          />
          <span
            className={cn(
              'w-9 shrink-0 text-lg font-semibold tabular-nums',
              verdictToneClass(report.verdict)
            )}
          >
            {Math.round(report.total_score)}
          </span>
          <span className='min-w-0 flex-1'>
            <span className='flex flex-wrap items-center gap-x-2 gap-y-1'>
              <span className='truncate text-sm font-medium'>
                {report.target_model}
              </span>
              <VerdictBadge verdict={report.verdict} />
              {report.critical_count > 0 && (
                <span className='text-destructive text-xs font-medium'>
                  {report.critical_count} {t('Critical Issue')}
                </span>
              )}
            </span>
            <span className='text-muted-foreground mt-0.5 block truncate text-xs'>
              {report.base_url} · {report.protocol} · {report.mode} ·{' '}
              <span
                title={formatTimestampToDate(entry.created_at, 'milliseconds')}
              >
                {formatTimestampRelative(
                  entry.created_at,
                  'milliseconds',
                  // 项目界面语言码是 zhCN/zhTW（非 BCP-47），直接喂 Intl 会抛
                  // RangeError，必须先过 toIntlLocale。
                  toIntlLocale(i18n.language)
                )}
              </span>
            </span>
          </span>
        </button>
        <div className='flex shrink-0 items-center gap-1'>
          {onRerun && (
            <button
              type='button'
              onClick={() => onRerun(entry.request)}
              title={t('Re-run')}
              className='text-muted-foreground hover:text-foreground hover:bg-muted rounded-md p-1.5 transition-colors'
            >
              <RotateCw className='size-4' />
            </button>
          )}
          <button
            type='button'
            onClick={onRemove}
            title={t('Delete')}
            className='text-muted-foreground hover:text-destructive hover:bg-destructive/10 rounded-md p-1.5 transition-colors'
          >
            <Trash2 className='size-4' />
          </button>
        </div>
      </div>
      {expanded && (
        <div className='px-4 pb-4'>
          <DetectorReportCard report={report} />
        </div>
      )}
    </li>
  )
}

// DetectionHistory renders the browser-local list of past detections. Storage
// and mutation live in the parent (useDetectionHistory); this is presentational.
export function DetectionHistory({
  entries,
  expandedId,
  onToggle,
  onRemove,
  onClear,
  onRerun,
}: {
  entries: DetectionHistoryEntry[]
  expandedId: string | null
  onToggle: (id: string) => void
  onRemove: (id: string) => void
  onClear: () => void
  onRerun?: (req: DetectionHistoryRequest) => void
}) {
  const { t } = useTranslation()
  const [confirmClear, setConfirmClear] = useState(false)

  if (entries.length === 0) return null

  return (
    <div className='pf-card pf-static overflow-hidden'>
      <div className='flex items-center justify-between gap-2 border-b px-4 py-2.5'>
        <span className='flex items-center gap-2 text-sm font-medium'>
          <History className='text-muted-foreground size-4' />
          {t('Detection History')}
          <span className='text-muted-foreground tabular-nums'>
            ({entries.length})
          </span>
        </span>
        <button
          type='button'
          onClick={() => setConfirmClear(true)}
          className='text-muted-foreground hover:text-destructive text-xs transition-colors'
        >
          {t('Clear all')}
        </button>
      </div>
      <ul className='divide-border/60 divide-y'>
        {entries.map((entry) => (
          <HistoryRow
            key={entry.id}
            entry={entry}
            expanded={expandedId === entry.id}
            onToggle={() => onToggle(entry.id)}
            onRemove={() => onRemove(entry.id)}
            onRerun={onRerun}
          />
        ))}
      </ul>

      <ConfirmDialog
        open={confirmClear}
        onOpenChange={setConfirmClear}
        destructive
        title={t('Clear all')}
        desc={t(
          'This permanently removes all detection history from this browser. This cannot be undone.'
        )}
        confirmText={t('Clear all')}
        handleConfirm={() => {
          onClear()
          setConfirmClear(false)
        }}
      />
    </div>
  )
}
