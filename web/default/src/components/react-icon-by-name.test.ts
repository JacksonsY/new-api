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

import { resolveReactIcon } from './react-icon-resolver'

describe('legacy payment icon compatibility', () => {
  test('resolves Material Design payment icons saved before pack reduction', async () => {
    assert.equal(
      await resolveReactIcon('MdPayment'),
      await resolveReactIcon('LuCreditCard')
    )
  })

  test('resolves Bootstrap payment icons saved before pack reduction', async () => {
    assert.equal(
      await resolveReactIcon('BsCreditCard'),
      await resolveReactIcon('LuCreditCard')
    )
  })

  test('keeps unknown icons as an empty result', async () => {
    assert.equal(await resolveReactIcon('MdNotAPaymentIcon'), null)
    assert.equal(await resolveReactIcon('UnknownIcon'), null)
  })
})
