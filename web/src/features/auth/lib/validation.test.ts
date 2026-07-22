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
import { after, before, describe, test } from 'node:test'

import { isSafeInternalRedirect } from './validation'

const ORIGIN = 'https://app.example.com'

type WindowedGlobal = { window?: { location: { origin: string } } }

describe('isSafeInternalRedirect', () => {
  before(() => {
    ;(globalThis as WindowedGlobal).window = { location: { origin: ORIGIN } }
  })

  after(() => {
    ;(globalThis as WindowedGlobal).window = undefined
  })

  test('accepts same-origin paths', () => {
    assert.equal(isSafeInternalRedirect('/dashboard'), true)
    assert.equal(isSafeInternalRedirect('/agent/apply?from=x'), true)
    assert.equal(isSafeInternalRedirect(`${ORIGIN}/console/token`), true)
  })

  // The caller decodes before we see it (URLSearchParams / router search), so
  // the percent-encoded payloads `%2F%5Cevil.com` arrive here as `/\evil.com`.
  // Each of these resolves to the `evil.com` origin under the WHATWG URL parser
  // and must be rejected — a `startsWith('//')` blocklist misses them all.
  test('rejects protocol-relative and backslash bypasses', () => {
    assert.equal(isSafeInternalRedirect('//evil.com'), false)
    assert.equal(isSafeInternalRedirect('/\\evil.com'), false)
    assert.equal(isSafeInternalRedirect('/\\/evil.com'), false)
    assert.equal(isSafeInternalRedirect('\\\\evil.com'), false)
  })

  test('rejects absolute off-origin URLs and non-navigable schemes', () => {
    assert.equal(isSafeInternalRedirect('https://evil.com'), false)
    assert.equal(isSafeInternalRedirect('http://evil.com/path'), false)
    assert.equal(isSafeInternalRedirect('javascript:alert(1)'), false)
  })

  test('rejects empty and nullish targets', () => {
    assert.equal(isSafeInternalRedirect(''), false)
    assert.equal(isSafeInternalRedirect(null), false)
    assert.equal(isSafeInternalRedirect(undefined), false)
  })
})
