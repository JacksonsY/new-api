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

import { ErrorPage } from './error-page'

type GeneralErrorProps = React.HTMLAttributes<HTMLDivElement> & {
  minimal?: boolean
  error?: unknown
}

function getHttpStatus(error: unknown): number | undefined {
  if (typeof error !== 'object' || error === null) return undefined
  const response = (error as Record<string, unknown>).response
  if (typeof response !== 'object' || response === null) return undefined
  const status = (response as Record<string, unknown>).status
  return typeof status === 'number' ? status : undefined
}

export function GeneralError({
  className,
  minimal = false,
  error,
}: GeneralErrorProps) {
  const { t } = useTranslation()
  const status = getHttpStatus(error)
  const isRateLimited = status === 429
  const title = isRateLimited
    ? t('Too many requests')
    : `${t('Oops! Something went wrong')} ${`:')`}`
  const description = isRateLimited
    ? t('Please wait a moment before trying again.')
    : t('Please try again later.')

  // minimal 模式渲染在应用内容区内部(如局部错误边界),保持朴素行内样式,
  // 不套整页 premium 画布。
  if (minimal) {
    return (
      <div className={cn('h-svh w-full', className)}>
        <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
          <span className='font-medium'>{title}</span>
          <p className='text-muted-foreground text-center'>
            {t('We apologize for the inconvenience.')} <br /> {description}
          </p>
        </div>
      </div>
    )
  }

  return (
    <ErrorPage
      code={String(status ?? 500)}
      title={title}
      description={
        <>
          {t('We apologize for the inconvenience.')} {description}
        </>
      }
    />
  )
}
