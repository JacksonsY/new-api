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
 * LobeHub icon resolver (heavy). Kept in its own module so the whole
 * `@lobehub/icons` namespace — which is imported wholesale because icons are
 * resolved by dynamic name (`LobeIcons[baseKey]`) and therefore cannot be
 * tree-shaken — is split into an on-demand chunk. Rendered via React.lazy from
 * `./lobe-icon`, so provider logos never ship in the initial/entry bundle.
 */
import * as LobeIcons from '@lobehub/icons'

import { CUSTOM_PROVIDER_ICONS } from './custom-provider-icons'
import { LobeIconFallback } from './lobe-icon-fallback'

/**
 * Parse a property value from string to appropriate type
 */
function parseValue(raw: string | undefined | null): string | number | boolean {
  if (raw == null) return true

  let v = String(raw).trim()

  // Remove curly braces
  if (v.startsWith('{') && v.endsWith('}')) {
    v = v.slice(1, -1).trim()
  }

  // Remove quotes
  if (
    (v.startsWith('"') && v.endsWith('"')) ||
    (v.startsWith("'") && v.endsWith("'"))
  ) {
    return v.slice(1, -1)
  }

  // Boolean
  if (v === 'true') return true
  if (v === 'false') return false

  // Number
  if (/^-?\d+(?:\.\d+)?$/.test(v)) return Number(v)

  // Return as string
  return v
}

/**
 * Resolve and render a LobeHub icon by name. `name` is assumed non-empty and
 * trimmed (the wrapper in ./lobe-icon guards empty input before this loads).
 *
 * Supports:
 * - Basic: "OpenAI", "OpenAI.Color"
 * - Chained properties: "OpenAI.Avatar.type={'platform'}"
 */
export function LobeIconImpl(props: { name: string; size: number }) {
  const segments = props.name.split('.')
  const baseKey = segments[0]

  // 自定义图标（@lobehub 未收录的品牌，如 HappyHorse）：按 baseKey 命中即渲染，
  // 忽略 .Color/.Avatar 等变体后缀。
  const CustomIcon = CUSTOM_PROVIDER_ICONS[baseKey]
  if (CustomIcon) {
    return <CustomIcon size={props.size} />
  }

  const BaseIcon = (LobeIcons as Record<string, unknown>)[baseKey] as
    | Record<string, unknown>
    | undefined

  let IconComponent: React.ComponentType<Record<string, unknown>> | undefined
  let propStartIndex: number

  if (BaseIcon && segments.length > 1 && BaseIcon[segments[1]]) {
    IconComponent = BaseIcon[segments[1]] as React.ComponentType<
      Record<string, unknown>
    >
    propStartIndex = 2
  } else {
    IconComponent = (LobeIcons as Record<string, unknown>)[baseKey] as
      | React.ComponentType<Record<string, unknown>>
      | undefined
    propStartIndex = segments.length > 1 && /^[A-Z]/.test(segments[1]) ? 2 : 1
  }

  // Fallback if icon not found
  if (
    !IconComponent ||
    (typeof IconComponent !== 'function' && typeof IconComponent !== 'object')
  ) {
    return (
      <LobeIconFallback
        size={props.size}
        label={props.name.charAt(0).toUpperCase()}
      />
    )
  }

  // Parse chained properties (e.g., "type={'platform'}", "shape='square'")
  const iconProps: Record<string, string | number | boolean> = {}

  for (let i = propStartIndex; i < segments.length; i++) {
    const seg = segments[i]
    if (!seg) continue

    const eqIdx = seg.indexOf('=')
    if (eqIdx === -1) {
      iconProps[seg.trim()] = true
      continue
    }

    const key = seg.slice(0, eqIdx).trim()
    const valRaw = seg.slice(eqIdx + 1).trim()
    iconProps[key] = parseValue(valRaw)
  }

  // Set size if not explicitly specified in the string
  if (iconProps.size == null && props.size != null) {
    iconProps.size = props.size
  }

  return <IconComponent {...iconProps} />
}
