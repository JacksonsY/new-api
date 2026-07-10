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
import type {
  ChannelHealthOverview,
  ChannelHealthRow,
  ChannelHealthState,
} from '../types'

export function channelHealthState(row: ChannelHealthRow): ChannelHealthState {
  if (row.circuit_open) return 'open'
  if (row.circuit_half_open) return 'degraded'
  return 'healthy'
}

// A single channel's contribution to the fleet score in [0,1]: a tripped
// circuit is 0, half-open is 0.5, and a closed circuit starts at 1 and is
// docked by its error rate (capped so a fully-erroring-but-not-yet-tripped
// channel never scores worse than a half-open one).
function channelScore(row: ChannelHealthRow): number {
  if (row.circuit_open) return 0
  if (row.circuit_half_open) return 0.5
  const errorPenalty = Math.min(Math.max(row.error_rate, 0), 0.5)
  return 1 - errorPenalty
}

// Derives the fleet-level summary shown in the header ring + stat tiles from the
// raw per-channel rows. Pure and side-effect free so the numbers stay trivially
// auditable. healthScore is null when no channel has produced samples yet (the
// "idle" state), so the ring can show a neutral placeholder instead of 0.
export function computeOverview(
  rows: ChannelHealthRow[]
): ChannelHealthOverview {
  let healthy = 0
  let degraded = 0
  let open = 0
  let disabled = 0
  let inflightTotal = 0
  let rpmTotal = 0
  let tpmTotal = 0
  let ttftSum = 0
  let ttftCount = 0
  let tpsSum = 0
  let tpsCount = 0
  let latencySum = 0
  let latencyCount = 0
  let errorSum = 0
  let errorCount = 0
  let inputTpmSum = 0
  let cacheTpmSum = 0
  let inputTotalSum = 0
  let cacheTotalSum = 0
  let scoreSum = 0
  let observedCount = 0

  for (const row of rows) {
    const state = channelHealthState(row)
    if (state === 'open') open += 1
    else if (state === 'degraded') degraded += 1
    else healthy += 1

    // status 1 = enabled; any other non-zero value means the channel is
    // manually or automatically disabled. 0 = unknown (metadata lookup missed).
    if (row.status !== 0 && row.status !== 1) disabled += 1

    inflightTotal += Math.max(0, row.inflight)
    inputTpmSum += Math.max(0, row.input_tpm)
    cacheTpmSum += Math.max(0, row.cache_tpm)
    inputTotalSum += Math.max(0, row.input_tokens_total)
    cacheTotalSum += Math.max(0, row.cache_read_tokens_total)
    rpmTotal += Math.max(0, row.rpm)
    tpmTotal += Math.max(0, row.tpm)

    if (row.has_ttft) {
      ttftSum += row.ttft_ms
      ttftCount += 1
    }
    if (row.has_tps) {
      tpsSum += row.tps
      tpsCount += 1
    }
    if (row.has_latency) {
      latencySum += row.latency_ms
      latencyCount += 1
    }
    if (row.has_data) {
      errorSum += row.error_rate
      errorCount += 1
      observedCount += 1
    }
    scoreSum += channelScore(row)
  }

  let cacheHitRate: number | null = null
  if (inputTpmSum > 0) {
    cacheHitRate = cacheTpmSum / inputTpmSum
  } else if (inputTotalSum > 0) {
    cacheHitRate = cacheTotalSum / inputTotalSum
  }

  return {
    total: rows.length,
    healthy,
    degraded,
    open,
    disabled,
    inflightTotal,
    totalRpm: rpmTotal,
    totalTpm: tpmTotal,
    avgTtftMs: ttftCount > 0 ? ttftSum / ttftCount : null,
    avgTpsTokPerSec: tpsCount > 0 ? tpsSum / tpsCount : null,
    avgLatencyMs: latencyCount > 0 ? latencySum / latencyCount : null,
    avgErrorRate: errorCount > 0 ? errorSum / errorCount : null,
    cacheHitRate,
    healthScore:
      rows.length > 0 && observedCount > 0
        ? Math.round((scoreSum / rows.length) * 100)
        : null,
  }
}
