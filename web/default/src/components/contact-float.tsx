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
import { Headset, Mail, MessageCircle, Phone } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { SUPPORT_CONTACT } from '@/components/support-contact'

// 右下角客服悬浮按钮：展示平台客服联系方式(SUPPORT_CONTACT)。
// 微信行 hover/focus 展示二维码(配置了图片地址时)。
export function ContactFloat() {
  const { t } = useTranslation()

  const { email, phone, wechat, wechatQr, qq } = SUPPORT_CONTACT

  if (!email && !phone && !wechat && !qq) return null

  return (
    <div className='fixed right-5 bottom-5 z-40 print:hidden'>
      <Popover>
        <PopoverTrigger
          className='bg-primary text-primary-foreground hover:bg-primary/90 flex size-11 items-center justify-center rounded-full shadow-lg transition-colors'
          aria-label={t('Contact Support')}
        >
          <Headset className='size-5' />
        </PopoverTrigger>
        <PopoverContent
          align='end'
          sideOffset={8}
          className='w-64 p-2 text-sm'
        >
          <div className='text-muted-foreground px-2 pt-1 pb-2 text-xs font-medium'>
            {t('Contact Support')}
          </div>
          <div className='flex flex-col'>
            {email && (
              <a
                href={`mailto:${email}`}
                className='hover:bg-muted flex items-center gap-2.5 rounded-md px-2 py-2'
              >
                <Mail className='text-muted-foreground size-4 shrink-0' />
                <span className='truncate'>{email}</span>
              </a>
            )}
            {phone && (
              <a
                href={`tel:${phone.replace(/[^\d+]/g, '')}`}
                className='hover:bg-muted flex items-center gap-2.5 rounded-md px-2 py-2'
              >
                <Phone className='text-muted-foreground size-4 shrink-0' />
                <span className='truncate'>{phone}</span>
              </a>
            )}
            {wechat && (
              <div
                className='group hover:bg-muted relative flex items-center gap-2.5 rounded-md px-2 py-2'
                tabIndex={0}
              >
                <MessageCircle className='text-muted-foreground size-4 shrink-0' />
                <span className='truncate'>
                  {t('WeChat')}: {wechat}
                </span>
                {wechatQr && (
                  <img
                    src={wechatQr}
                    alt={t('WeChat QR Code')}
                    className='bg-background absolute right-full bottom-0 mr-3 hidden w-36 rounded-lg border p-1.5 shadow-lg group-hover:block group-focus-within:block group-focus:block'
                  />
                )}
              </div>
            )}
            {qq && (
              <div className='flex items-center gap-2.5 rounded-md px-2 py-2'>
                <MessageCircle className='text-muted-foreground size-4 shrink-0' />
                <span className='truncate'>QQ: {qq}</span>
              </div>
            )}
          </div>
        </PopoverContent>
      </Popover>
    </div>
  )
}
