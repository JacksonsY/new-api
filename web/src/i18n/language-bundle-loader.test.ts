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

import { createLanguageBundleLoader } from './language-bundle-loader'

describe('language bundle loading', () => {
  test('deduplicates concurrent loads for the same language', async () => {
    let releaseBundle: () => void = () => {}
    const bundleGate = new Promise<void>((resolve) => {
      releaseBundle = resolve
    })
    let loadCount = 0
    const loadBundle = async () => {
      loadCount += 1
      await bundleGate
      return { default: { translation: {} } }
    }
    const addResourceBundle = () => undefined
    const loadLanguage = createLanguageBundleLoader(
      { en: loadBundle },
      addResourceBundle
    )

    const first = loadLanguage('en')
    const second = loadLanguage('en')

    assert.equal(first, second)
    assert.equal(loadCount, 1)

    releaseBundle()
    await first
  })

  test('evicts a rejected load so the language can be retried', async () => {
    let loadCount = 0
    const loadBundle = async () => {
      loadCount += 1
      if (loadCount === 1) throw new Error('chunk unavailable')
      return { default: { translation: { Ready: 'Ready' } } }
    }
    const addedBundles: Array<[string, Record<string, string>]> = []
    const loadLanguage = createLanguageBundleLoader(
      { en: loadBundle },
      (language, translation) => {
        addedBundles.push([language, translation])
      }
    )

    await assert.rejects(loadLanguage('en'), /chunk unavailable/)
    await loadLanguage('en')

    assert.equal(loadCount, 2)
    assert.deepEqual(addedBundles, [['en', { Ready: 'Ready' }]])
  })
})
