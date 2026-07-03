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
// 合规要求全站页脚展示有效客服联系方式。以下为占位默认值,部署方上线前请替换
// 为真实的客服邮箱 / 电话 / 微信(号码留空则该项不展示)。
export const SUPPORT_CONTACT: {
  email: string
  phone: string
  wechat: string
  wechatQr: string
  qq: string
} = {
  email: 'support@jzlh99.com',
  phone: '400-000-0000',
  wechat: '', // 留空则不展示;需要时填微信号即可
  wechatQr: '', // 微信二维码图片 URL,联系悬浮组件 hover 展示
  qq: '',
}

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
