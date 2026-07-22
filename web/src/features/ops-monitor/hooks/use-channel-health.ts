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

import { fetchChannelHealth } from '../api'
import type { ChannelHealthRow } from '../types'

export const REFRESH_INTERVALS = [5, 10, 30, 60] as const

export interface UseChannelHealth {
  rows: ChannelHealthRow[]
  enabled: boolean
  loading: boolean
  error: boolean
  lastUpdated: Date | null
  autoRefresh: boolean
  setAutoRefresh: (value: boolean) => void
  intervalSec: number
  setIntervalSec: (value: number) => void
  countdown: number
  refresh: () => void
}

// Polls the channel-health endpoint on a fixed cadence with a live countdown,
// pausing while the tab is hidden and refreshing on return. A sequence guard
// drops stale in-flight responses so a slow poll can never overwrite a newer
// one.
export function useChannelHealth(): UseChannelHealth {
  const [rows, setRows] = useState<ChannelHealthRow[]>([])
  const [enabled, setEnabled] = useState(true)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null)
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [intervalSec, setIntervalSec] = useState<number>(10)
  const [countdown, setCountdown] = useState<number>(10)
  const [refreshNonce, setRefreshNonce] = useState(0)
  const seqRef = useRef(0)

  const load = useCallback(async () => {
    const seq = ++seqRef.current
    setLoading(true)
    try {
      const data = await fetchChannelHealth()
      if (seq !== seqRef.current) return
      setRows(data.channels)
      setEnabled(data.enabled)
      setError(false)
      setLastUpdated(new Date())
    } catch {
      if (seq !== seqRef.current) return
      setError(true)
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
  }, [])

  const refresh = useCallback(() => {
    setRefreshNonce((n) => n + 1)
    void load()
  }, [load])

  // Initial fetch.
  useEffect(() => {
    void load()
  }, [load])

  // Countdown owns the auto-refresh cadence. `remaining` is a closure local so
  // the tick never leaks side effects into a setState updater. A manual refresh
  // bumps refreshNonce, restarting the effect (and resetting the countdown).
  useEffect(() => {
    if (!autoRefresh) return
    let remaining = intervalSec
    setCountdown(remaining)
    const timer = setInterval(() => {
      remaining -= 1
      if (remaining <= 0) {
        void load()
        remaining = intervalSec
      }
      setCountdown(remaining)
    }, 1000)
    return () => clearInterval(timer)
  }, [autoRefresh, intervalSec, load, refreshNonce])

  // Refresh immediately when the tab becomes visible again.
  useEffect(() => {
    const onVisible = () => {
      if (!document.hidden && autoRefresh) void load()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [autoRefresh, load])

  return {
    rows,
    enabled,
    loading,
    error,
    lastUpdated,
    autoRefresh,
    setAutoRefresh,
    intervalSec,
    setIntervalSec,
    countdown,
    refresh,
  }
}
