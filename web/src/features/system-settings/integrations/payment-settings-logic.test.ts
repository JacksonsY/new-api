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

import { resolveEpayProtocolVersion } from './payment-settings-logic'

describe('Epay protocol status', () => {
  test('shows a valid detected protocol when available', () => {
    assert.equal(resolveEpayProtocolVersion('v2', 'v1'), 'v2')
  })

  test('falls back to the configured protocol before a valid detection', () => {
    assert.equal(resolveEpayProtocolVersion(undefined, 'v2'), 'v2')
    assert.equal(resolveEpayProtocolVersion('unknown', 'v1'), 'v1')
  })
})
