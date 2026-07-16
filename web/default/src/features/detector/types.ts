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

export type DetectorProtocol = 'claude' | 'openai' | 'gemini' | 'grok'

export type DetectorMode = 'standard' | 'quick' | 'deep'

export type DetectorVerdict = 'passed' | 'marginal' | 'failed'

export type DetectorResultStatus = 'pass' | 'fail' | 'skip' | 'error'

export type DetectorJobStatus = 'running' | 'done' | 'error'

// Per-detector evidence: the backend sends `details` as an object
// (map[string]interface{}), tagged `,omitempty`, so it is an object or absent —
// never a bare string. Values are scalars or nested arrays/objects (e.g. issues).
export type DetectorDetails = Record<string, unknown>

export interface DetectorResultItem {
  name: string
  display_name: string
  status: DetectorResultStatus
  score: number
  weight: number
  details?: DetectorDetails
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
  results?: DetectorResultItem[]
  summary: string
  // Backend tags these `,omitempty`, so they are absent when empty (e.g. a
  // genuine relay with no non-official brands). Optional here to match the wire.
  self_reported_identity?: string
  detected_brands?: string[]
  run_error?: string
  // Claude 真实后端溯源(Anthropic 直连 / Bedrock / Vertex / 疑似伪装),
  // 由 backend_origin 检测器判定。仅 anthropic 协议下出现。
  backend_origin?: string
  // 本次共享探针实际打的主端点路径(Claude /v1/messages、Gemini 原生
  // generateContent、OpenAI/Grok 的 /v1/responses 或 /v1/chat/completions)。
  probed_endpoint?: string
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

// 公共检测页的本地历史记录（仅存浏览器 localStorage，不落服务器数据库）。
// 存下完整报告 + 复现所需的请求参数（不含 API Key，密钥永不落盘），
// 以便旧记录能重看、按需一键重测（重测时需重新输入密钥）。
export interface DetectionHistoryRequest {
  protocol: DetectorProtocol
  base_url: string
  model: string
  mode: DetectorMode
  include_long_context: boolean
  include_long_context_extreme: boolean
}

export interface DetectionHistoryEntry {
  id: string
  created_at: number // Unix 毫秒
  report: DetectionReport
  request: DetectionHistoryRequest
}
