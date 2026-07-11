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

import { createInstance } from 'i18next'
import { createElement } from 'react'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider } from 'react-i18next'

import { BrandLoader, RoutePendingLoader } from './brand-loader'

async function renderLoader(loader: typeof BrandLoader): Promise<string> {
  const i18n = createInstance()
  await i18n.init({
    lng: 'en',
    resources: { en: { translation: { 'Loading...': 'Loading...' } } },
  })

  return renderToStaticMarkup(
    createElement(
      I18nextProvider,
      { i18n },
      createElement(loader, { className: 'test-loader' })
    )
  )
}

describe('brand loading status', () => {
  test('announces a translated default message', async () => {
    const markup = await renderLoader(BrandLoader)

    assert.match(markup, /role="status"/)
    assert.match(markup, /aria-live="polite"/)
    assert.match(markup, />Loading\.\.\.<\/p>/)
  })

  test('route pending loader inherits the accessible default', async () => {
    const markup = await renderLoader(RoutePendingLoader)

    assert.match(markup, /role="status"/)
    assert.match(markup, />Loading\.\.\.<\/p>/)
  })
})
