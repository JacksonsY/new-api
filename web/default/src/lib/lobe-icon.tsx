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
/**
 * LobeHub Icon Loader
 * Renders provider icons from @lobehub/icons by name (e.g. "OpenAI",
 * "OpenAI.Color", "Claude.Avatar.type={'platform'}").
 *
 * The resolver imports the whole `@lobehub/icons` namespace (icons are looked
 * up by dynamic name, so the library cannot be tree-shaken). To keep hundreds
 * of provider logos out of the initial/entry bundle, the resolver lives in a
 * React.lazy chunk (`./lobe-icon-impl`) that loads on first render.
 */
import { lazy, Suspense } from 'react'

import { LobeIconFallback } from './lobe-icon-fallback'

const LobeIconImpl = lazy(() =>
  import('./lobe-icon-impl').then((m) => ({ default: m.LobeIconImpl }))
)

/**
 * Get LobeHub icon component by name.
 * @param iconName - Icon name/description (e.g., "OpenAI", "OpenAI.Color", "Claude.Avatar")
 * @param size - Icon size (default: 20)
 * @returns Icon element (renders a lightweight placeholder while the icon chunk loads)
 *
 * @example
 * getLobeIcon("OpenAI", 24)
 * getLobeIcon("OpenAI.Color", 20)
 * getLobeIcon("Claude.Avatar.type={'platform'}", 32)
 */
export function getLobeIcon(
  iconName: string | undefined | null,
  size: number = 20
): React.ReactNode {
  const trimmedName = typeof iconName === 'string' ? iconName.trim() : ''

  if (!trimmedName) {
    return <LobeIconFallback size={size} label='?' />
  }

  return (
    <Suspense
      fallback={
        <LobeIconFallback
          size={size}
          label={trimmedName.charAt(0).toUpperCase()}
        />
      }
    >
      <LobeIconImpl name={trimmedName} size={size} />
    </Suspense>
  )
}
