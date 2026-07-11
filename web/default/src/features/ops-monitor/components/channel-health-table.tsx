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
import { ArrowDown, ArrowUp, RotateCcw, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/design-system/button'
import { Input } from '@/components/design-system/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { cn } from '@/lib/utils'

import { channelHealthState } from '../lib/overview'
import type { ChannelHealthRow, ChannelHealthState } from '../types'

type SortKey =
  | 'channel'
  | 'group'
  | 'state'
  | 'weight'
  | 'ttft'
  | 'tps'
  | 'latency'
  | 'error'
  | 'rpm'
  | 'cache'
  | 'inflight'
  | 'test'
type StateFilter = 'all' | ChannelHealthState

const STATE_RANK: Record<ChannelHealthState, number> = {
  open: 2,
  degraded: 1,
  healthy: 0,
}

const STATE_BADGE: Record<ChannelHealthState, string> = {
  healthy: 'bg-success/10 text-success border-success/20',
  degraded: 'bg-warning/10 text-warning border-warning/20',
  open: 'bg-destructive/10 text-destructive border-destructive/20',
}

function stateLabelKey(state: ChannelHealthState): string {
  if (state === 'open') return 'Circuit open'
  if (state === 'degraded') return 'Half-open'
  return 'Healthy'
}

// 行级缓存命中率：优先近 60s 窗口，渠道空闲时回落到自启动累计；均无输入时返回 null。
function cacheHitRateOf(row: ChannelHealthRow): number | null {
  if (row.input_tpm > 0) return row.cache_tpm / row.input_tpm
  if (row.input_tokens_total > 0) {
    return row.cache_read_tokens_total / row.input_tokens_total
  }
  return null
}

function ttftTone(ttftMs: number): string {
  if (ttftMs >= 8000) return 'text-destructive'
  if (ttftMs >= 2000) return 'text-warning'
  return 'text-foreground'
}

function errorTone(rate: number): string {
  if (rate >= 0.5) return 'text-destructive'
  if (rate >= 0.2) return 'text-warning'
  return 'text-foreground'
}

function weightTone(weight: number): string {
  if (weight >= 0.8) return 'text-success'
  if (weight >= 0.4) return 'text-warning'
  return 'text-destructive'
}

function formatMs(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`
}

function formatClock(unixSec: number): string {
  if (!unixSec) return '—'
  return new Date(unixSec * 1000).toLocaleTimeString()
}

interface SortHeaderProps {
  label: string
  columnKey: SortKey
  align?: 'left' | 'right'
  activeKey: SortKey
  asc: boolean
  onSort: (key: SortKey) => void
}

function SortHeader(props: SortHeaderProps) {
  const active = props.activeKey === props.columnKey
  return (
    <th
      className={cn(
        'py-2 font-medium whitespace-nowrap',
        props.align === 'right' ? 'pl-3 text-right' : 'pr-3 text-left'
      )}
    >
      <button
        type='button'
        onClick={() => props.onSort(props.columnKey)}
        className={cn(
          'hover:text-foreground inline-flex items-center gap-1 transition-colors',
          props.align === 'right' && 'flex-row-reverse',
          active ? 'text-foreground' : 'text-muted-foreground'
        )}
      >
        {props.label}
        {active &&
          (props.asc ? (
            <ArrowUp className='size-3' />
          ) : (
            <ArrowDown className='size-3' />
          ))}
      </button>
    </th>
  )
}

interface ChannelHealthTableProps {
  rows: ChannelHealthRow[]
  onReset: (channelId: number) => void
}

export function ChannelHealthTable(props: ChannelHealthTableProps) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const [stateFilter, setStateFilter] = useState<StateFilter>('all')
  const [sortKey, setSortKey] = useState<SortKey>('state')
  const [sortAsc, setSortAsc] = useState(false)

  const visibleRows = useMemo(() => {
    const query = search.trim().toLowerCase()
    const filtered = props.rows.filter((row) => {
      if (stateFilter !== 'all' && channelHealthState(row) !== stateFilter) {
        return false
      }
      if (!query) return true
      return (
        row.name.toLowerCase().includes(query) ||
        String(row.channel_id).includes(query) ||
        row.group.toLowerCase().includes(query)
      )
    })

    const dir = sortAsc ? 1 : -1
    return [...filtered].sort((a, b) => {
      let cmp = 0
      switch (sortKey) {
        case 'channel':
          cmp = a.channel_id - b.channel_id
          break
        case 'group':
          cmp = a.group.localeCompare(b.group)
          break
        case 'state':
          cmp =
            STATE_RANK[channelHealthState(a)] -
            STATE_RANK[channelHealthState(b)]
          break
        case 'weight':
          cmp = a.weight - b.weight
          break
        case 'ttft':
          cmp = (a.has_ttft ? a.ttft_ms : -1) - (b.has_ttft ? b.ttft_ms : -1)
          break
        case 'tps':
          cmp = (a.has_tps ? a.tps : -1) - (b.has_tps ? b.tps : -1)
          break
        case 'latency':
          cmp =
            (a.has_latency ? a.latency_ms : -1) -
            (b.has_latency ? b.latency_ms : -1)
          break
        case 'error':
          cmp =
            (a.has_data ? a.error_rate : -1) - (b.has_data ? b.error_rate : -1)
          break
        case 'rpm':
          cmp = a.rpm - b.rpm
          break
        case 'cache':
          cmp = (cacheHitRateOf(a) ?? -1) - (cacheHitRateOf(b) ?? -1)
          break
        case 'inflight':
          cmp = a.inflight - b.inflight
          break
        case 'test':
          cmp = a.test_latency_ms - b.test_latency_ms
          break
      }
      if (cmp === 0) cmp = a.channel_id - b.channel_id
      return cmp * dir
    })
  }, [props.rows, search, stateFilter, sortKey, sortAsc])

  const toggleSort = (key: SortKey) => {
    if (key === sortKey) {
      setSortAsc((prev) => !prev)
    } else {
      setSortKey(key)
      setSortAsc(false)
    }
  }

  return (
    <div className='bg-card rounded-2xl border'>
      <div className='flex flex-wrap items-center gap-2 border-b p-3 sm:p-4'>
        <div className='relative min-w-[180px] flex-1'>
          <Search className='text-muted-foreground/60 absolute top-1/2 left-2.5 size-4 -translate-y-1/2' />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder={t('Search channels')}
            className='h-8 pl-8'
          />
        </div>
        <NativeSelect
          size='sm'
          value={stateFilter}
          onChange={(e) => setStateFilter(e.target.value as StateFilter)}
        >
          <NativeSelectOption value='all'>{t('All states')}</NativeSelectOption>
          <NativeSelectOption value='healthy'>
            {t('Healthy')}
          </NativeSelectOption>
          <NativeSelectOption value='degraded'>
            {t('Half-open')}
          </NativeSelectOption>
          <NativeSelectOption value='open'>
            {t('Circuit open')}
          </NativeSelectOption>
        </NativeSelect>
        <span className='text-muted-foreground text-xs'>
          {t('{{count}} channels', { count: visibleRows.length })}
        </span>
      </div>

      {visibleRows.length > 0 ? (
        <div className='overflow-x-auto'>
          <table className='w-full text-sm'>
            <thead>
              <tr className='text-muted-foreground border-b'>
                <SortHeader
                  label={t('Channel')}
                  columnKey='channel'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('Group')}
                  columnKey='group'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('State')}
                  columnKey='state'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('Weight')}
                  columnKey='weight'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('TTFT (ms)')}
                  columnKey='ttft'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('TPS')}
                  columnKey='tps'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('Latency')}
                  columnKey='latency'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('Error rate')}
                  columnKey='error'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('RPM')}
                  columnKey='rpm'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('Cache hit')}
                  columnKey='cache'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('In-flight')}
                  columnKey='inflight'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <SortHeader
                  label={t('Endpoint (ms)')}
                  columnKey='test'
                  align='right'
                  activeKey={sortKey}
                  asc={sortAsc}
                  onSort={toggleSort}
                />
                <th className='py-2 pl-3' />
              </tr>
            </thead>
            <tbody>
              {visibleRows.map((row) => {
                const state = channelHealthState(row)
                const errTotal = row.err_429 + row.err_5xx + row.err_other
                const channelTitleParts: string[] = []
                if (row.last_used_at) {
                  channelTitleParts.push(
                    `${t('Last used')}: ${formatClock(row.last_used_at)}`
                  )
                }
                if (row.has_balance) {
                  channelTitleParts.push(
                    `${t('Balance')}: $${row.balance.toFixed(2)}`
                  )
                }
                return (
                  <tr
                    key={row.channel_id}
                    className='border-b last:border-0 [&>td]:px-0 [&>td]:py-2'
                  >
                    <td
                      className='pr-3'
                      title={channelTitleParts.join(' · ') || undefined}
                    >
                      <div className='flex flex-col'>
                        <span className='text-foreground font-medium whitespace-nowrap'>
                          {row.name || `#${row.channel_id}`}
                        </span>
                        <span className='text-muted-foreground/70 text-xs'>
                          #{row.channel_id}
                        </span>
                      </div>
                    </td>
                    <td className='text-muted-foreground pr-3 whitespace-nowrap'>
                      {row.group || '-'}
                    </td>
                    <td className='pr-3'>
                      <span
                        className={cn(
                          'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap',
                          STATE_BADGE[state]
                        )}
                        title={
                          row.last_err_code
                            ? `${t('Last error')}: ${row.last_err_code} · ${formatClock(row.last_err_at)}`
                            : undefined
                        }
                      >
                        {t(stateLabelKey(state))}
                        {state === 'open' && row.cooldown_ms > 0 && (
                          <span className='ml-1 opacity-80'>
                            · {Math.ceil(row.cooldown_ms / 1000)}s
                          </span>
                        )}
                      </span>
                    </td>
                    <td
                      className={cn(
                        'pl-3 text-right font-mono tabular-nums',
                        weightTone(row.weight)
                      )}
                    >
                      {`${Math.round(row.weight * 100)}%`}
                    </td>
                    <td
                      className={cn(
                        'pl-3 text-right font-mono tabular-nums',
                        row.has_ttft
                          ? ttftTone(row.ttft_ms)
                          : 'text-muted-foreground'
                      )}
                    >
                      {row.has_ttft ? Math.round(row.ttft_ms) : '—'}
                    </td>
                    <td className='text-foreground pl-3 text-right font-mono tabular-nums'>
                      {row.has_tps ? row.tps.toFixed(0) : '—'}
                    </td>
                    <td className='text-muted-foreground pl-3 text-right font-mono tabular-nums'>
                      {row.has_latency ? formatMs(row.latency_ms) : '—'}
                    </td>
                    <td
                      className={cn(
                        'pl-3 text-right font-mono tabular-nums',
                        row.has_data
                          ? errorTone(row.error_rate)
                          : 'text-muted-foreground'
                      )}
                      title={
                        errTotal > 0
                          ? `429: ${row.err_429} · 5xx: ${row.err_5xx} · ${t('Other')}: ${row.err_other}`
                          : undefined
                      }
                    >
                      {row.has_data
                        ? `${(row.error_rate * 100).toFixed(1)}%`
                        : '—'}
                    </td>
                    <td
                      className='text-foreground pl-3 text-right font-mono tabular-nums'
                      title={`TPM: ${row.tpm}`}
                    >
                      {row.rpm}
                    </td>
                    <td
                      className='text-foreground pl-3 text-right font-mono tabular-nums'
                      title={`60s: ${row.cache_tpm}/${row.input_tpm} · total: ${row.cache_read_tokens_total}/${row.input_tokens_total}`}
                    >
                      {(() => {
                        const rate = cacheHitRateOf(row)
                        return rate == null
                          ? '—'
                          : `${(rate * 100).toFixed(0)}%`
                      })()}
                    </td>
                    <td className='text-foreground pl-3 text-right font-mono tabular-nums'>
                      {row.inflight}
                    </td>
                    <td
                      className='text-muted-foreground pl-3 text-right font-mono tabular-nums'
                      title={
                        row.test_time
                          ? `${t('Tested')}: ${formatClock(row.test_time)}`
                          : undefined
                      }
                    >
                      {row.test_latency_ms > 0 ? row.test_latency_ms : '—'}
                    </td>
                    <td className='pl-3 text-right'>
                      <Button
                        type='button'
                        variant='ghost'
                        size='sm'
                        className='text-muted-foreground hover:text-foreground h-7 gap-1'
                        onClick={() => props.onReset(row.channel_id)}
                        title={t('Reset learned health for this channel')}
                      >
                        <RotateCcw className='size-3.5' />
                        <span className='hidden sm:inline'>{t('Reset')}</span>
                      </Button>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <div className='text-muted-foreground py-12 text-center text-sm'>
          {props.rows.length === 0
            ? t('No channels observed yet')
            : t('No channels match the current filter')}
        </div>
      )}
    </div>
  )
}
