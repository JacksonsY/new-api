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
// One channel's live routing health as observed by this instance, enriched
// server-side with the channel's identity (name/type/group/status).
export interface ChannelHealthRow {
  channel_id: number
  name: string
  type: number
  group: string
  status: number
  has_data: boolean
  error_rate: number
  has_ttft: boolean
  ttft_ms: number
  inflight: number
  circuit_open: boolean
  circuit_half_open: boolean
  // Observability enrichments.
  has_latency: boolean
  latency_ms: number
  has_tps: boolean
  tps: number
  last_used_at: number
  last_err_code: number
  last_err_at: number
  cooldown_ms: number
  rpm: number
  tpm: number
  err_429: number
  err_5xx: number
  err_other: number
  weight: number
  test_latency_ms: number
  test_time: number
  balance: number
  has_balance: boolean
  balance_updated_time: number
}

export type ChannelHealthState = 'healthy' | 'degraded' | 'open'

// Fleet-level summary derived from the per-channel rows (see lib/overview).
export interface ChannelHealthOverview {
  total: number
  healthy: number
  degraded: number
  open: number
  disabled: number
  inflightTotal: number
  totalRpm: number
  totalTpm: number
  avgTtftMs: number | null
  avgTpsTokPerSec: number | null
  avgLatencyMs: number | null
  avgErrorRate: number | null
  // 0-100 blended fleet score, or null when no channel has produced samples yet.
  healthScore: number | null
}

export interface ChannelHealthPayload {
  enabled: boolean
  channels: ChannelHealthRow[]
}
