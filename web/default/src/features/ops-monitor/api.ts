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

import type { ChannelHealthPayload, ChannelHealthRow } from './types'

export async function fetchChannelHealth(): Promise<ChannelHealthPayload> {
  const res = await api.get('/api/channel_health', {
    disableDuplicate: true,
  } as Record<string, unknown>)
  const body = res.data
  if (!body?.success) {
    throw new Error(body?.message || 'request failed')
  }
  return {
    enabled: Boolean(body.data?.enabled),
    channels: (body.data?.channels as ChannelHealthRow[]) ?? [],
  }
}

// Clears learned health + circuit trip. Omit channelId to reset every channel
// (local + cluster-wide).
export async function resetChannelHealth(channelId?: number): Promise<void> {
  const res = await api.delete('/api/channel_health', {
    params: channelId ? { channel_id: channelId } : undefined,
  })
  if (!res.data?.success) {
    throw new Error(res.data?.message || 'request failed')
  }
}
