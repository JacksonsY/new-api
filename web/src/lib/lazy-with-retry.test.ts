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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { type ChunkRetryDeps, createRetryImport } from './lazy-with-retry'

function createMemoryDeps() {
  const markers = new Set<string>()
  let reloads = 0
  const deps: ChunkRetryDeps = {
    wasReloaded: (chunkKey) => markers.has(chunkKey),
    markReloaded: (chunkKey) => {
      markers.add(chunkKey)
      return true
    },
    clearReloaded: (chunkKey) => {
      markers.delete(chunkKey)
    },
    reload: () => {
      reloads += 1
    },
  }
  return { deps, markers, getReloads: () => reloads }
}

describe('retrying chunk imports with a one-shot reload', () => {
  test('passes a successful load through without reloading', async () => {
    const { deps, getReloads } = createMemoryDeps()
    const retryImport = createRetryImport(deps)

    const module = await retryImport('markdown', async () => ({
      default: 'markdown-component',
    }))

    assert.deepEqual(module, { default: 'markdown-component' })
    assert.equal(getReloads(), 0)
  })

  test('first failure marks the chunk and triggers exactly one reload', async () => {
    const { deps, markers, getReloads } = createMemoryDeps()
    const retryImport = createRetryImport(deps)

    let settled = false
    const pending = retryImport('markdown', async () => {
      throw new Error('chunk 404')
    })
    void pending.then(
      () => {
        settled = true
      },
      () => {
        settled = true
      }
    )
    await new Promise((resolve) => setImmediate(resolve))

    assert.equal(getReloads(), 1)
    assert.ok(markers.has('markdown'))
    // While the reload is in flight the promise must stay pending so the
    // error boundary does not flash before the page unloads.
    assert.equal(settled, false)
  })

  test('failure after a reload rethrows the original error instead of looping', async () => {
    const { deps, getReloads } = createMemoryDeps()
    const retryImport = createRetryImport(deps)
    deps.markReloaded('markdown')

    await assert.rejects(
      retryImport('markdown', async () => {
        throw new Error('chunk 404')
      }),
      /chunk 404/
    )
    assert.equal(getReloads(), 0)
  })

  test('rethrows without reloading when the marker cannot be persisted', async () => {
    const { deps, getReloads } = createMemoryDeps()
    deps.markReloaded = () => false
    const retryImport = createRetryImport(deps)

    await assert.rejects(
      retryImport('markdown', async () => {
        throw new Error('chunk 404')
      }),
      /chunk 404/
    )
    assert.equal(getReloads(), 0)
  })

  test('successful load clears the marker so the next deploy can retry again', async () => {
    const { deps, markers } = createMemoryDeps()
    const retryImport = createRetryImport(deps)
    deps.markReloaded('markdown')

    await retryImport('markdown', async () => ({ default: 'ok' }))

    assert.equal(markers.has('markdown'), false)
  })

  test('markers are tracked per chunk key', async () => {
    const { deps, getReloads } = createMemoryDeps()
    const retryImport = createRetryImport(deps)
    deps.markReloaded('markdown')

    // 'lobe-icon' has no marker yet, so it still gets its one-shot reload.
    void retryImport('lobe-icon', async () => {
      throw new Error('chunk 404')
    })
    await new Promise((resolve) => setImmediate(resolve))

    assert.equal(getReloads(), 1)
  })
})
