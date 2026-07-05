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
import { useEffect, useState } from 'react'
import type { IconBaseProps, IconType } from 'react-icons'

type IconPackModule = Record<string, unknown>
type IconPackLoader = () => Promise<IconPackModule>

// Curated to the icon packs that actually make sense for payment-method /
// wallet icons — the only place ReactIconByName is used. `si` (brand logos:
// Alipay, WeChat, Stripe, PayPal, Visa…) and `lu` (the LuCreditCard default)
// are the only packs referenced in code; `fa`/`fa6`/`ri` are kept because they
// carry the other common payment/brand glyphs an operator might type.
//
// Registering a pack here makes the bundler emit its *entire* icon set as a
// chunk (e.g. Game Icons ≈ 6.7 MB, Phosphor ≈ 5.2 MB), which previously shipped
// ~25 MB of never-referenced sets to dist. To support another pack, add its
// loader below and a matching entry in ICON_PACK_CANDIDATES; unknown names
// simply render nothing (see resolveReactIcon), so this degrades gracefully.
const ICON_PACK_LOADERS = {
  fa: () => import('react-icons/fa').then((module) => module as IconPackModule),
  fa6: () =>
    import('react-icons/fa6').then((module) => module as IconPackModule),
  lu: () => import('react-icons/lu').then((module) => module as IconPackModule),
  ri: () => import('react-icons/ri').then((module) => module as IconPackModule),
  si: () => import('react-icons/si').then((module) => module as IconPackModule),
} satisfies Record<string, IconPackLoader>

type IconPackId = keyof typeof ICON_PACK_LOADERS

const ICON_PACK_CACHE = new Map<IconPackId, Promise<IconPackModule>>()

// Prefix → candidate packs, restricted to the curated ICON_PACK_LOADERS above.
// `Fa*` tries fa6 (v6) before fa (v5); other prefixes map to their single pack.
const ICON_PACK_CANDIDATES: Array<[RegExp, IconPackId[]]> = [
  [/^Fa[A-Z0-9]/, ['fa6', 'fa']],
  [/^Lu[A-Z0-9]/, ['lu']],
  [/^Ri[A-Z0-9]/, ['ri']],
  [/^Si[A-Z0-9]/, ['si']],
]

function normalizeIconName(name: string | null | undefined): string | null {
  const trimmed = name?.trim()
  if (!trimmed || !/^[A-Z][A-Za-z0-9]*$/.test(trimmed)) return null
  return trimmed
}

function getCandidatePacks(iconName: string): IconPackId[] {
  return (
    ICON_PACK_CANDIDATES.find(([pattern]) => pattern.test(iconName))?.[1] ?? []
  )
}

function loadIconPack(packId: IconPackId): Promise<IconPackModule> {
  const cached = ICON_PACK_CACHE.get(packId)
  if (cached) return cached

  const promise = ICON_PACK_LOADERS[packId]()
  ICON_PACK_CACHE.set(packId, promise)
  return promise
}

function isIconComponent(value: unknown): value is IconType {
  return typeof value === 'function'
}

async function resolveReactIcon(iconName: string): Promise<IconType | null> {
  for (const packId of getCandidatePacks(iconName)) {
    try {
      const icon = (await loadIconPack(packId))[iconName]
      if (isIconComponent(icon)) return icon
    } catch {
      // Missing chunks or unknown packs should behave the same as unknown names.
    }
  }
  return null
}

type ReactIconByNameProps = IconBaseProps & {
  name?: string | null
}

type ResolvedIconState = {
  iconName: string
  Icon: IconType | null
}

export function ReactIconByName({ name, ...props }: ReactIconByNameProps) {
  const iconName = normalizeIconName(name)
  const [resolvedIcon, setResolvedIcon] = useState<ResolvedIconState | null>(
    null
  )

  useEffect(() => {
    let cancelled = false

    if (!iconName) return

    void resolveReactIcon(iconName).then((Icon) => {
      if (!cancelled) setResolvedIcon({ iconName, Icon })
    })

    return () => {
      cancelled = true
    }
  }, [iconName])

  if (!iconName || resolvedIcon?.iconName !== iconName || !resolvedIcon.Icon) {
    return null
  }

  const Icon = resolvedIcon.Icon

  return <Icon {...props} />
}
