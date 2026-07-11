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

import { startApplicationAfterI18n } from './start-application'

describe('application startup after i18n initialization', () => {
  test('starts after locale bundles load', async () => {
    let startCount = 0

    await startApplicationAfterI18n(Promise.resolve(), () => {
      startCount += 1
    })

    assert.equal(startCount, 1)
  })

  test('still starts when a locale chunk fails to load', async () => {
    let startCount = 0

    await startApplicationAfterI18n(
      Promise.reject(new Error('chunk unavailable')),
      () => {
        startCount += 1
      }
    )

    assert.equal(startCount, 1)
  })
})
