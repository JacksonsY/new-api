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
import type { IconType } from 'react-icons'

type IconPackModule = Record<string, unknown>
type IconPackLoader = () => Promise<IconPackModule>

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

const ICON_PACK_CANDIDATES: Array<[RegExp, IconPackId[]]> = [
  [/^Fa[A-Z0-9]/, ['fa6', 'fa']],
  [/^Lu[A-Z0-9]/, ['lu']],
  [/^Ri[A-Z0-9]/, ['ri']],
  [/^Si[A-Z0-9]/, ['si']],
]

// Keep common pre-reduction payment icon names working by mapping them to
// equivalent icons in the already-supported Lucide pack. Importing md/bs even
// through a small re-export module still makes bundlers retain their full sets.
const LEGACY_PAYMENT_ICON_ALIASES: Record<string, string> = {
  BsBank: 'LuLandmark',
  BsBank2: 'LuLandmark',
  BsCash: 'LuBanknote',
  BsCashCoin: 'LuHandCoins',
  BsCashStack: 'LuBanknote',
  BsCoin: 'LuCoins',
  BsCreditCard: 'LuCreditCard',
  BsCreditCard2Back: 'LuCreditCard',
  BsCreditCard2BackFill: 'LuCreditCard',
  BsCreditCard2Front: 'LuCreditCard',
  BsCreditCard2FrontFill: 'LuCreditCard',
  BsCreditCardFill: 'LuCreditCard',
  BsCurrencyDollar: 'LuCircleDollarSign',
  BsFillCreditCard2BackFill: 'LuCreditCard',
  BsFillCreditCard2FrontFill: 'LuCreditCard',
  BsFillCreditCardFill: 'LuCreditCard',
  BsFillPiggyBankFill: 'LuPiggyBank',
  BsFillWalletFill: 'LuWallet',
  BsPaypal: 'LuWalletCards',
  BsPiggyBank: 'LuPiggyBank',
  BsPiggyBankFill: 'LuPiggyBank',
  BsQrCode: 'LuQrCode',
  BsQrCodeScan: 'LuScanQrCode',
  BsStripe: 'LuCreditCard',
  BsWallet: 'LuWallet',
  BsWallet2: 'LuWalletCards',
  BsWalletFill: 'LuWallet',
  MdAccountBalance: 'LuLandmark',
  MdAccountBalanceWallet: 'LuWalletCards',
  MdAddCard: 'LuCreditCard',
  MdAttachMoney: 'LuDollarSign',
  MdCreditCard: 'LuCreditCard',
  MdCreditCardOff: 'LuCircleOff',
  MdCurrencyExchange: 'LuCoins',
  MdMoney: 'LuBanknote',
  MdOutlineAccountBalance: 'LuLandmark',
  MdOutlineAccountBalanceWallet: 'LuWalletCards',
  MdOutlineAddCard: 'LuCreditCard',
  MdOutlineAttachMoney: 'LuDollarSign',
  MdOutlineCreditCard: 'LuCreditCard',
  MdOutlineCreditCardOff: 'LuCircleOff',
  MdOutlineCurrencyExchange: 'LuCoins',
  MdOutlineMoney: 'LuBanknote',
  MdOutlinePayment: 'LuCreditCard',
  MdOutlinePayments: 'LuWalletCards',
  MdOutlineQrCode: 'LuQrCode',
  MdOutlineQrCode2: 'LuQrCode',
  MdOutlineQrCodeScanner: 'LuScanQrCode',
  MdOutlineWallet: 'LuWallet',
  MdPayment: 'LuCreditCard',
  MdPayments: 'LuWalletCards',
  MdQrCode: 'LuQrCode',
  MdQrCode2: 'LuQrCode',
  MdQrCodeScanner: 'LuScanQrCode',
  MdWallet: 'LuWallet',
}

export function normalizeIconName(
  name: string | null | undefined
): string | null {
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

export async function resolveReactIcon(
  iconName: string
): Promise<IconType | null> {
  const resolvedName = LEGACY_PAYMENT_ICON_ALIASES[iconName] ?? iconName

  for (const packId of getCandidatePacks(resolvedName)) {
    try {
      const icon = (await loadIconPack(packId))[resolvedName]
      if (isIconComponent(icon)) return icon
    } catch {
      // Missing chunks or unknown packs should behave like unknown names.
    }
  }
  return null
}
