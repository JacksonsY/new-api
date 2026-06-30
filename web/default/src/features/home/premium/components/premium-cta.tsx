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
import { useRef } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useReveal } from '../lib'

export function PremiumCTA({ isAuthenticated }: { isAuthenticated: boolean }) {
  const { t } = useTranslation()
  const root = useRef<HTMLElement>(null)
  useReveal(root)

  return (
    <section ref={root} className='relative z-10 px-6 py-20 md:py-28'>
      <div
        data-reveal
        className='pf-card relative mx-auto max-w-5xl overflow-hidden px-8 py-20 text-center md:px-12'
      >
        {/* fire wash behind the panel */}
        <div
          aria-hidden
          className='pointer-events-none absolute inset-0 -z-10 opacity-90'
          style={{
            background:
              'radial-gradient(ellipse 60% 80% at 50% 120%, rgba(240,69,46,0.22), transparent 60%), radial-gradient(ellipse 50% 70% at 50% -20%, rgba(124,58,237,0.2), transparent 60%)',
          }}
        />
        <span className='pf-eyebrow mb-5'>{t('Ready to begin')}</span>
        <h2 className='pf-display mx-auto max-w-[14ch] text-balance'>
          {t('Light the')} <span className='pf-fire-text'>{t('fire.')}</span>
        </h2>
        <p
          className='pf-lead mx-auto mt-6 max-w-[40ch]'
          style={{ color: 'var(--pf-ink-2)' }}
        >
          {t(
            'Nine streams, one source. Spin up your gateway and route the first request in minutes.'
          )}
        </p>
        <div className='mt-9 flex flex-wrap items-center justify-center gap-3'>
          {isAuthenticated ? (
            <Link to='/dashboard' className='pf-btn pf-btn-fire'>
              {t('Go to Dashboard')}
              <ArrowRight className='size-4' />
            </Link>
          ) : (
            <Link to='/sign-up' className='pf-btn pf-btn-fire'>
              {t('Start free')}
              <ArrowRight className='size-4' />
            </Link>
          )}
          <Link to='/pricing' className='pf-btn pf-btn-ghost'>
            {t('View pricing')}
          </Link>
        </div>
      </div>
    </section>
  )
}
