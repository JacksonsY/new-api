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
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { Skeleton } from '@/components/ui/skeleton'
import { useSystemConfig } from '@/hooks/use-system-config'

import '@/features/home/premium/premium.css'

type AuthLayoutProps = {
  children: React.ReactNode
}

// Centered glass card on the 「离火・白」 premium surface — identical atmosphere
// to the landing page (aurora + hairline grid + grain), following the app's
// light/dark theme (`.pf` = light 离火·白, `html.dark .pf` = 玄夜 dark variant).
export function AuthLayout({ children }: AuthLayoutProps) {
  const { t } = useTranslation()
  const { systemName, logo, loading } = useSystemConfig()

  return (
    <div className='pf relative flex min-h-svh flex-col items-center justify-center px-4 py-10'>
      <div className='pf-aurora' aria-hidden />
      <div className='pf-grid' aria-hidden />
      <div className='pf-grain' aria-hidden />

      <div className='relative z-10 flex w-full max-w-[420px] flex-col items-center'>
        {/* Brand wordmark above the card */}
        <Link
          to='/'
          className='mb-8 flex items-center gap-2.5 transition-opacity hover:opacity-80'
        >
          {loading ? (
            <Skeleton className='size-9 rounded-xl' />
          ) : (
            <img
              src={logo}
              alt={t('Logo')}
              className='size-9 rounded-xl object-cover'
            />
          )}
          {loading ? (
            <Skeleton className='h-6 w-28' />
          ) : (
            <span className='pf-fire-text text-2xl font-bold tracking-tight'>
              {systemName}
            </span>
          )}
        </Link>

        {/* Glass auth card */}
        <div className='pf-card w-full p-7 sm:p-9'>{children}</div>
      </div>
    </div>
  )
}
