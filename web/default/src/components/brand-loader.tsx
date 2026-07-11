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
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

interface BrandLoaderProps {
  className?: string
  message?: string
}

// 品牌加载态：与 index.html 的开屏加载页同一视觉（logo 呼吸光晕 + 离火渐变
// 进度条），用于路由跳转 pending 和页面级加载。容器高度由调用方通过
// className 控制（如 min-h-svh / min-h-40）；小型局部加载请用 LoadingState。
export function BrandLoader({ className, message }: BrandLoaderProps) {
  const { t } = useTranslation()
  const statusMessage = message ?? t('Loading...')

  return (
    <div
      className={cn(
        'flex w-full flex-col items-center justify-center gap-7',
        className
      )}
      role='status'
      aria-live='polite'
    >
      <img
        src='/logo.svg'
        alt=''
        draggable={false}
        className='brand-loader-logo size-14 select-none'
      />
      <div className='brand-loader-track'>
        <div className='brand-loader-fill' />
      </div>
      <p className={message ? 'text-muted-foreground text-sm' : 'sr-only'}>
        {statusMessage}
      </p>
    </div>
  )
}

// 路由跳转/懒加载 chunk 的等待界面，供 createRouter 的
// defaultPendingComponent 使用。渲染在发生挂起的那层 Outlet：控制台内
// 切换时只占内容区（侧边栏保持不动），70vh 兼顾整页与嵌套两种场景的居中感。
export function RoutePendingLoader() {
  return <BrandLoader className='min-h-[70vh]' />
}
