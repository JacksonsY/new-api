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

import { getDetectionStatus } from './api'
import type { DetectionReport } from './types'

export type DetectionPhase = 'idle' | 'running' | 'done' | 'error'

const POLL_INTERVAL_MS = 2000
const MAX_POLLS = 150 // ~5 minutes safety cap

// 轮询检测任务直至完成/失败。调用方拿到 job_id 后调用 start(jobId)。
export function useDetectionPoll() {
  const [phase, setPhase] = useState<DetectionPhase>('idle')
  const [report, setReport] = useState<DetectionReport | null>(null)
  const [error, setError] = useState<string | null>(null)
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const activeRef = useRef(false)

  const stop = useCallback(() => {
    activeRef.current = false
    if (timerRef.current) {
      clearTimeout(timerRef.current)
      timerRef.current = null
    }
  }, [])

  const reset = useCallback(() => {
    stop()
    setPhase('idle')
    setReport(null)
    setError(null)
  }, [stop])

  const start = useCallback(
    (jobId: string) => {
      stop()
      activeRef.current = true
      setPhase('running')
      setReport(null)
      setError(null)

      let polls = 0
      const poll = async () => {
        if (!activeRef.current) return
        polls += 1
        try {
          const res = await getDetectionStatus(jobId)
          if (!activeRef.current) return
          if (!res.success || !res.data) {
            setPhase('error')
            setError(res.message || 'Detection failed')
            activeRef.current = false
            return
          }
          const job = res.data
          if (job.status === 'running') {
            if (polls >= MAX_POLLS) {
              setPhase('error')
              setError('Detection timed out')
              activeRef.current = false
              return
            }
            timerRef.current = setTimeout(poll, POLL_INTERVAL_MS)
            return
          }
          if (job.status === 'done') {
            setReport(job.report ?? null)
            setPhase('done')
            activeRef.current = false
            return
          }
          // error
          setPhase('error')
          setError(job.error || 'Detection failed')
          activeRef.current = false
        } catch {
          if (!activeRef.current) return
          setPhase('error')
          setError('Detection failed')
          activeRef.current = false
        }
      }

      timerRef.current = setTimeout(poll, POLL_INTERVAL_MS)
    },
    [stop]
  )

  useEffect(() => stop, [stop])

  return { phase, report, error, start, reset }
}
