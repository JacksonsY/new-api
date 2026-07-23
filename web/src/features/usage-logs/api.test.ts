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

import { buildLogStatsApiPath, buildLogsApiPath } from './api'

describe('usage log API paths', () => {
  test('uses canonical collection routes for administrators', () => {
    assert.equal(buildLogsApiPath('/api/log', true), '/api/log/')
    assert.equal(buildLogsApiPath('/api/task', true), '/api/task/')
  })

  test('uses self routes without a redirect for users', () => {
    assert.equal(buildLogsApiPath('/api/log', false), '/api/log/self')
    assert.equal(buildLogsApiPath('/api/task', false), '/api/task/self')
  })

  test('does not introduce a duplicate slash in statistics routes', () => {
    assert.equal(buildLogStatsApiPath('/api/log', true), '/api/log/stat')
    assert.equal(buildLogStatsApiPath('/api/log', false), '/api/log/self/stat')
  })
})
