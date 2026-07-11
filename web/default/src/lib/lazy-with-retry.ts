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
import { type ComponentType, lazy, type LazyExoticComponent } from 'react'

// After a deploy, tabs that loaded the previous build still reference the old
// hashed chunk URLs; their next dynamic import 404s and the route error
// boundary takes down the whole page. A full reload fetches the new manifest
// and fixes that, so retry each failed chunk with exactly one reload per tab
// session (tracked in sessionStorage per chunk key to avoid reload loops when
// the import failure has another cause, e.g. the server is down).
const STORAGE_KEY_PREFIX = 'chunk-reload:'

export type ChunkRetryDeps = {
  wasReloaded: (chunkKey: string) => boolean
  /** Returns false when the marker could not be persisted. */
  markReloaded: (chunkKey: string) => boolean
  clearReloaded: (chunkKey: string) => void
  reload: () => void
}

export function createRetryImport(deps: ChunkRetryDeps) {
  return async function retryImport<T>(
    chunkKey: string,
    load: () => Promise<T>
  ): Promise<T> {
    let module: T
    try {
      module = await load()
    } catch (error) {
      // Without a persisted marker a reload could loop forever, so only
      // reload when this tab has not already retried this chunk AND the
      // marker write succeeded. Otherwise surface the original error.
      if (deps.wasReloaded(chunkKey) || !deps.markReloaded(chunkKey)) {
        throw error
      }
      deps.reload()
      // The page is about to unload; stay pending instead of flashing the
      // error boundary during the reload.
      return new Promise<T>(() => {})
    }
    // Successful load consumes the marker so a later deploy in this same tab
    // gets its own one-shot reload.
    deps.clearReloaded(chunkKey)
    return module
  }
}

const retryImport = createRetryImport({
  wasReloaded: (chunkKey) => {
    try {
      return (
        window.sessionStorage.getItem(STORAGE_KEY_PREFIX + chunkKey) === '1'
      )
    } catch {
      // Treat unreadable storage as "already reloaded": never reload blind.
      return true
    }
  },
  markReloaded: (chunkKey) => {
    try {
      window.sessionStorage.setItem(STORAGE_KEY_PREFIX + chunkKey, '1')
      return true
    } catch {
      return false
    }
  },
  clearReloaded: (chunkKey) => {
    try {
      window.sessionStorage.removeItem(STORAGE_KEY_PREFIX + chunkKey)
    } catch {
      /* empty */
    }
  },
  reload: () => window.location.reload(),
})

/**
 * Drop-in replacement for `React.lazy` that reloads the page once when the
 * chunk fails to load (stale build after a deploy).
 * @param chunkKey - Stable identifier for the chunk (used as the retry marker)
 */
// eslint-disable-next-line @typescript-eslint/no-explicit-any
export function lazyWithRetry<T extends ComponentType<any>>(
  chunkKey: string,
  load: () => Promise<{ default: T }>
): LazyExoticComponent<T> {
  return lazy(() => retryImport(chunkKey, load))
}
