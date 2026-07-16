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
import type { DetectionHistoryRequest } from './types'

// The in-flight detection job, persisted so a page refresh can resume polling
// instead of losing a 1–10+ minute run (and the upstream tokens it burned).
// sessionStorage on purpose: survives F5 in the same tab, dies with the tab —
// no cross-tab races, nothing lingers. The API key is never stored.
export interface ActiveDetectionJob {
  job_id: string
  started_at: number // Unix ms, when the run was submitted
  request: DetectionHistoryRequest
}

const STORAGE_KEY = 'jzlh:detector:active-job:v1'

// Backend jobs are pruned after ~1h, and the longest legitimate poll window
// (extreme long-context) is ~20 min. Entries older than this are dead weight —
// resuming them could only produce a stale "job not found" error.
const MAX_RESUME_AGE_MS = 25 * 60 * 1000

function isValidJob(v: unknown): v is ActiveDetectionJob {
  if (typeof v !== 'object' || v === null) return false
  const job = v as Record<string, unknown>
  if (typeof job.job_id !== 'string' || job.job_id === '') return false
  if (typeof job.started_at !== 'number') return false
  const req = job.request as Record<string, unknown> | undefined
  return (
    typeof req === 'object' &&
    req !== null &&
    typeof req.base_url === 'string' &&
    typeof req.model === 'string' &&
    typeof req.protocol === 'string' &&
    typeof req.mode === 'string'
  )
}

// loadActiveJob returns the resumable job, or null. Corrupt or expired entries
// are cleared so they are not re-examined on every mount.
export function loadActiveJob(): ActiveDetectionJob | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed: unknown = JSON.parse(raw)
    if (!isValidJob(parsed) || Date.now() - parsed.started_at > MAX_RESUME_AGE_MS) {
      sessionStorage.removeItem(STORAGE_KEY)
      return null
    }
    return parsed
  } catch {
    clearActiveJob()
    return null
  }
}

export function saveActiveJob(job: ActiveDetectionJob): void {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(job))
  } catch {
    // Private mode / quota — resume just won't be available for this run.
  }
}

export function clearActiveJob(): void {
  try {
    sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}
