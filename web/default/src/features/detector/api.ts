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
import { api } from '@/lib/api'

import type {
  ApiEnvelope,
  DetectionRecord,
  DetectionRecordsResult,
  DetectJobResponse,
  DetectorJob,
  DetectRequest,
  LeaderboardResult,
} from './types'

// ---- 公共检测 ----

export async function submitDetection(
  req: DetectRequest
): Promise<ApiEnvelope<DetectJobResponse>> {
  const res = await api.post('/api/detector/detect', req)
  return res.data
}

export async function getDetectionStatus(
  jobId: string
): Promise<ApiEnvelope<DetectorJob>> {
  const res = await api.get(`/api/detector/status/${jobId}`)
  return res.data
}

export async function getLeaderboard(
  page = 1,
  pageSize = 20
): Promise<ApiEnvelope<LeaderboardResult>> {
  const res = await api.get('/api/detector/leaderboard', {
    params: { p: page, page_size: pageSize },
  })
  return res.data
}

// ---- 管理端：渠道验真 ----

export async function verifyChannel(
  channelId: number,
  mode = 'standard'
): Promise<ApiEnvelope<DetectJobResponse>> {
  const res = await api.post(
    `/api/detector/channel/${channelId}`,
    {},
    { params: { mode } }
  )
  return res.data
}

export async function getChannelLatestDetection(
  channelId: number
): Promise<ApiEnvelope<DetectionRecord | null>> {
  const res = await api.get(`/api/detector/channel/${channelId}/latest`)
  return res.data
}

export async function getDetectionRecords(
  channelId: number,
  page = 1
): Promise<ApiEnvelope<DetectionRecordsResult>> {
  const res = await api.get('/api/detector/records', {
    params: { channel_id: channelId, p: page },
  })
  return res.data
}
