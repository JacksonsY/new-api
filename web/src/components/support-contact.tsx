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
import type { CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

// 客服联系方式 — single source of truth for the site-wide footer contact block.
// 合规要求全站页脚展示有效客服联系方式。取值优先级：
// 1) window.__SUPPORT_CONTACT__(部署方可在 index.html / 注入脚本中配置,免重新构建)
// 2) 构建期环境变量 PUBLIC_SUPPORT_*(rsbuild 注入)
// 3) 代码内 fallback(号码留空则该项不展示)
// 占位电话(400-000-0000)会被过滤,不会在生产误展示。
type SupportContactInfo = {
  email: string
  phone: string
  wechat: string
  wechatQr: string
  qq: string
}

declare global {
  interface Window {
    __SUPPORT_CONTACT__?: Partial<SupportContactInfo>
  }
}

const PLACEHOLDER_PHONE = '400-000-0000'

function resolveSupportContact(): SupportContactInfo {
  const env = (import.meta as { env?: Record<string, string | undefined> }).env
  const win =
    (typeof window !== 'undefined' && window.__SUPPORT_CONTACT__) || {}
  const contact: SupportContactInfo = {
    email: win.email ?? env?.PUBLIC_SUPPORT_EMAIL ?? '',
    phone: win.phone ?? env?.PUBLIC_SUPPORT_PHONE ?? '',
    wechat: win.wechat ?? env?.PUBLIC_SUPPORT_WECHAT ?? '',
    wechatQr: win.wechatQr ?? env?.PUBLIC_SUPPORT_WECHAT_QR ?? '',
    qq: win.qq ?? env?.PUBLIC_SUPPORT_QQ ?? '',
  }
  // 占位电话一律不展示,避免误导用户拨打无效号码。
  if (contact.phone.trim() === PLACEHOLDER_PHONE) {
    contact.phone = ''
  }
  return contact
}

export const SUPPORT_CONTACT: SupportContactInfo = resolveSupportContact()

type SupportContactProps = {
  className?: string
  linkClassName?: string
  style?: CSSProperties
  // Render as a horizontal wrapped row (compact bars) instead of a stacked list.
  inline?: boolean
}

export function SupportContact({
  className,
  linkClassName,
  style,
  inline = false,
}: SupportContactProps) {
  const { t } = useTranslation()
  const contact = SUPPORT_CONTACT
  const telHref = `tel:${contact.phone.replace(/[^\d+]/g, '')}`

  return (
    <div
      className={cn(
        'text-sm',
        inline
          ? 'flex flex-wrap items-center gap-x-4 gap-y-1'
          : 'flex flex-col gap-1.5',
        className
      )}
      style={style}
    >
      {contact.email && (
        <a href={`mailto:${contact.email}`} className={linkClassName}>
          {t('Support Email')}: {contact.email}
        </a>
      )}
      {contact.phone && (
        <a href={telHref} className={linkClassName}>
          {t('Support Phone')}: {contact.phone}
        </a>
      )}
      {contact.wechat && (
        <span>
          {t('WeChat')}: {contact.wechat}
        </span>
      )}
    </div>
  )
}
