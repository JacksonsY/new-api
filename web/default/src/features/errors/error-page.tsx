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
import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import '@/features/home/premium/premium.css'

type ErrorPageProps = {
  code: string
  title: string
  description: React.ReactNode
  /** Replace the default action pair entirely (e.g. maintenance page). */
  actions?: React.ReactNode
}

// 「离火・白」错误页外壳 —— 与首页/登录页同一套 premium 画布(极光 + 细网格 + 颗粒),
// 巨号火焰渐变状态码居中,浅色/深色随全局主题切换。5 个错误页共用此组件。
export function ErrorPage({
  code,
  title,
  description,
  actions,
}: ErrorPageProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()

  return (
    <div className='pf relative flex min-h-svh flex-col items-center justify-center px-4 py-10'>
      <div className='pf-aurora' aria-hidden />
      <div className='pf-grid' aria-hidden />
      <div className='pf-grain' aria-hidden />

      <div className='relative z-10 flex w-full max-w-md flex-col items-center text-center'>
        {/* 数字作视觉锚点保持 hero 尺度；中文标题/描述刻意收小，避免满屏大字 */}
        <div className='pf-fire-text text-7xl leading-none font-extrabold tracking-tight select-none sm:text-8xl'>
          {code}
        </div>
        <h1 className='mt-5 text-lg font-semibold text-balance sm:text-xl'>
          {title}
        </h1>
        <p className='text-muted-foreground mt-2 text-sm leading-relaxed text-balance'>
          {description}
        </p>
        <div className='mt-7 flex flex-wrap items-center justify-center gap-3'>
          {actions ?? (
            <>
              <button
                type='button'
                className='pf-btn pf-btn-ghost pf-btn-sm'
                onClick={() => history.go(-1)}
              >
                {t('Go Back')}
              </button>
              <button
                type='button'
                className='pf-btn pf-btn-fire pf-btn-sm'
                onClick={() => navigate({ to: '/' })}
              >
                {t('Back to Home')}
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
