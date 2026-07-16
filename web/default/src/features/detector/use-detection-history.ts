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
import { useCallback, useEffect, useRef, useState } from 'react'

import {
  clearHistory as clearStoredHistory,
  loadHistory,
  saveHistory,
} from './history-storage'
import type {
  DetectionHistoryEntry,
  DetectionHistoryRequest,
  DetectionReport,
} from './types'

// makeEntryId builds a collision-resistant id from the timestamp plus a short
// random suffix, so two detections finished in the same millisecond still get
// distinct keys.
function makeEntryId(createdAt: number): string {
  const suffix = Math.random().toString(36).slice(2, 8)
  return `d${createdAt}-${suffix}`
}

// useDetectionhistory keeps the browser-local detection history in React state,
// mirrored to localStorage on every mutation. It never talks to the server.
export function useDetectionHistory() {
  // Lazy initial read so the stored history is available on first paint.
  const [entries, setEntries] = useState<DetectionHistoryEntry[]>(() =>
    loadHistory()
  )

  // Mirror the committed list into a ref so the mutators can read the current
  // entries and persist OUTSIDE the setState updater — keeping the updater pure
  // (React double-invokes updaters under StrictMode; localStorage.setItem must
  // not run there). The effect runs after commit, so between two user-driven
  // mutations the ref is always up to date.
  const entriesRef = useRef(entries)
  useEffect(() => {
    entriesRef.current = entries
  }, [entries])

  // add prepends a freshly completed detection and returns the new entry's id
  // (so the caller can auto-expand it). Persistence may trim to fit the quota;
  // React state is set to exactly what landed on disk.
  const add = useCallback(
    (report: DetectionReport, request: DetectionHistoryRequest): string => {
      const entry: DetectionHistoryEntry = {
        id: makeEntryId(Date.now()),
        created_at: Date.now(),
        report,
        request,
      }
      const persisted = saveHistory([entry, ...entriesRef.current])
      entriesRef.current = persisted
      setEntries(persisted)
      return entry.id
    },
    []
  )

  const remove = useCallback((id: string) => {
    const persisted = saveHistory(
      entriesRef.current.filter((e) => e.id !== id)
    )
    entriesRef.current = persisted
    setEntries(persisted)
  }, [])

  const clear = useCallback(() => {
    clearStoredHistory()
    entriesRef.current = []
    setEntries([])
  }, [])

  return { entries, add, remove, clear }
}
