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

// Veridrop 真伪检测前端类型（对齐后端 /api/detector/* 契约）。

export interface ApiEnvelope<T> {
  success: boolean
  message?: string
  data?: T
}

export type DetectorProtocol = 'claude' | 'openai' | 'gemini'

export type DetectorMode = 'standard' | 'quick' | 'deep'

export type DetectorVerdict = 'passed' | 'marginal' | 'failed'

export type DetectorResultStatus = 'pass' | 'fail' | 'skip' | 'error'

export type DetectorJobStatus = 'running' | 'done' | 'error'

export interface DetectorResultItem {
  name: string
  display_name: string
  status: DetectorResultStatus
  score: number
  weight: number
  details: string
  duration_ms: number
  error?: string
}

export interface DetectionReport {
  protocol: string
  base_url: string
  api_key_masked: string
  target_model: string
  mode: string
  total_score: number
  verdict: DetectorVerdict
  critical_count: number
  results: DetectorResultItem[]
  summary: string
  self_reported_identity: string
  detected_brands: string[]
}

export interface DetectorJob {
  id: string
  status: DetectorJobStatus
  report?: DetectionReport
  error?: string
}

export interface DetectRequest {
  base_url: string
  api_key: string
  model: string
  protocol?: DetectorProtocol
  mode?: DetectorMode
  include_long_context?: boolean
  include_long_context_extreme?: boolean
}

export interface DetectJobResponse {
  job_id: string
  status_url: string
}

export interface LeaderboardEntry {
  domain: string
  samples: number
  avg_score: number
  min_score: number
  critical_count: number
  last_checked_at: number
}

export interface LeaderboardResult {
  items: LeaderboardEntry[]
  page: number
  page_size: number
}

// 管理端渠道检测记录。
export interface DetectionRecord {
  id: number
  channel_id: number
  verdict: DetectorVerdict
  total_score: number
  critical_count: number
  report?: DetectionReport
  created_at: number
}

export interface DetectionRecordsResult {
  items: DetectionRecord[]
  total: number
}
