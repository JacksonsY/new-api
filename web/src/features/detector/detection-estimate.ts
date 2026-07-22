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

// Shared duration estimates for a detection run — used by the form hint and
// the live progress panel. Kept out of the component files so Fast Refresh
// keeps working there.

export interface DetectionDurationInput {
  mode: string
  include_long_context: boolean
  include_long_context_extreme: boolean
}

// estimateDetectionSeconds mirrors the backend's per-mode run budget (quick
// 60s / standard 120s / deep 180s) plus a long-context allowance — the backend
// exempts long-context runs from the overall timeout because their tiers
// legitimately take minutes. Cosmetic pacing only, never enforced.
export function estimateDetectionSeconds(req: DetectionDurationInput): number {
  let base = 120
  if (req.mode === 'quick') base = 60
  if (req.mode === 'deep') base = 180
  if (req.include_long_context_extreme) return base + 600
  if (req.include_long_context) return base + 240
  return base
}

export function estimateDetectionMinutes(req: DetectionDurationInput): number {
  return Math.max(1, Math.round(estimateDetectionSeconds(req) / 60))
}
