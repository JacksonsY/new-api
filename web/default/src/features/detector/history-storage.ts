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

// 公共检测历史的浏览器本地持久化。刻意只用 localStorage，绝不触达服务器数据库
// ——用户在自己的浏览器里保留检测过的中转记录，换设备/清缓存即失，符合"仅本地"
// 的预期。写入用带版本号的信封，方便未来演进结构；条数有上限，配额溢出时逐条丢弃
// 最旧记录后重试，避免一次大报告把整段历史写崩。

import type { DetectionHistoryEntry } from './types'

const STORAGE_KEY = 'jzlh:detector:history:v1'
const STORAGE_VERSION = 1
// 单机保留的检测记录条数上限。每条报告可能含 30+ 检测器的完整证据，取 30 条在
// "够用回看"和"不撑爆 5MB localStorage 配额"之间平衡。
const MAX_ENTRIES = 30

interface HistoryEnvelope {
  version: number
  entries: DetectionHistoryEntry[]
}

function isEntry(v: unknown): v is DetectionHistoryEntry {
  if (typeof v !== 'object' || v === null) return false
  const e = v as Record<string, unknown>
  return (
    typeof e.id === 'string' &&
    typeof e.created_at === 'number' &&
    typeof e.report === 'object' &&
    e.report !== null &&
    typeof e.request === 'object' &&
    e.request !== null
  )
}

// loadHistory returns the stored entries newest-first, tolerating a missing,
// corrupt, or foreign-shaped payload by returning an empty list.
export function loadHistory(): DetectionHistoryEntry[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as Partial<HistoryEnvelope>
    if (!parsed || !Array.isArray(parsed.entries)) return []
    return parsed.entries.filter(isEntry)
  } catch {
    return []
  }
}

// persist writes the list, dropping the oldest entries until it fits under the
// localStorage quota (a single detection can be large). Returns the list that
// actually landed on disk so the caller's in-memory state matches storage.
function persist(entries: DetectionHistoryEntry[]): DetectionHistoryEntry[] {
  let current = entries.slice(0, MAX_ENTRIES)
  while (current.length > 0) {
    try {
      const envelope: HistoryEnvelope = {
        version: STORAGE_VERSION,
        entries: current,
      }
      localStorage.setItem(STORAGE_KEY, JSON.stringify(envelope))
      return current
    } catch {
      // Quota exceeded or storage unavailable: shed the oldest entry and retry.
      current = current.slice(0, -1)
    }
  }
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // Storage entirely unavailable (private mode / disabled): nothing to do.
  }
  return []
}

// saveHistory replaces the whole list (already newest-first) and returns what
// was actually persisted after any quota-driven trimming.
export function saveHistory(
  entries: DetectionHistoryEntry[]
): DetectionHistoryEntry[] {
  return persist(entries)
}

// clearHistory removes every stored entry.
export function clearHistory(): void {
  try {
    localStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}

export { MAX_ENTRIES }
