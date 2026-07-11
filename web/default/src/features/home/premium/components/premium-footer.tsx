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

import { SupportContact } from '@/components/support-contact'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'

import { PREMIUM_FOOTER_PLAYGROUND_ROUTE } from './premium-footer-links'

type FootLink = { label: string; to?: string; href?: string }

export function PremiumFooter() {
  const { t } = useTranslation()
  const { systemName, logo } = useSystemConfig()
  const { status } = useStatus()
  const year = new Date().getFullYear()
  const brand = systemName || '九紫离火'
  const docs =
    (status?.docs_link as string | undefined) || 'https://docs.newapi.pro'

  const columns: { title: string; links: FootLink[] }[] = [
    {
      title: t('Product'),
      links: [
        { label: t('Pricing'), to: '/pricing' },
        { label: t('Rankings'), to: '/rankings' },
        { label: t('Get Started'), to: '/sign-up' },
      ],
    },
    {
      title: t('Developers'),
      links: [
        { label: t('Docs'), href: docs },
        { label: t('Playground'), to: PREMIUM_FOOTER_PLAYGROUND_ROUTE },
      ],
    },
    {
      title: t('Company'),
      links: [
        { label: t('About'), to: '/about' },
        { label: t('Sign In'), to: '/sign-in' },
      ],
    },
    {
      title: t('Legal'),
      links: [
        { label: t('Privacy Policy'), to: '/privacy-policy' },
        { label: t('User Agreement'), to: '/user-agreement' },
      ],
    },
  ]

  return (
    <footer
      className='relative z-10 border-t'
      style={{ borderColor: 'var(--pf-line)' }}
    >
      <div className='mx-auto max-w-6xl px-6 py-16'>
        <div className='grid gap-10 md:grid-cols-[1.5fr_repeat(4,1fr)]'>
          {/* Brand block */}
          <div>
            <Link to='/' className='flex items-center gap-2'>
              <img
                src={logo}
                alt={brand}
                className='size-7 rounded-lg object-cover'
              />
              <span className='pf-fire-text text-lg font-bold tracking-tight'>
                {brand}
              </span>
            </Link>
            <p
              className='mt-4 max-w-[26ch] text-sm leading-relaxed'
              style={{ color: 'var(--pf-muted)' }}
            >
              {t('One key to every model.')}
            </p>
            <SupportContact
              className='mt-5'
              linkClassName='pf-footlink'
              style={{ color: 'var(--pf-muted)' }}
            />
          </div>

          {/* Link columns */}
          {columns.map((col) => (
            <div key={col.title}>
              <h4
                className='mb-3.5 text-[0.72rem] font-semibold tracking-[0.14em] uppercase'
                style={{ color: 'var(--pf-ink)' }}
              >
                {col.title}
              </h4>
              <ul className='flex flex-col gap-2.5'>
                {col.links.map((l) => (
                  <li key={l.label}>
                    {l.to ? (
                      <Link to={l.to} className='pf-footlink'>
                        {l.label}
                      </Link>
                    ) : (
                      <a
                        href={l.href}
                        target='_blank'
                        rel='noopener noreferrer'
                        className='pf-footlink'
                      >
                        {l.label}
                      </a>
                    )}
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        {/* Bottom bar: deployment brand © + preserved open-source attribution */}
        <div
          className='mt-14 flex flex-col gap-2 border-t pt-7 text-xs sm:flex-row sm:items-center sm:justify-between'
          style={{ borderColor: 'var(--pf-line)', color: 'var(--pf-muted)' }}
        >
          <span>
            &copy; {year} {brand}
          </span>
          <span style={{ opacity: 0.85 }}>
            {t('Powered by')}{' '}
            <a
              href='https://github.com/QuantumNous/new-api'
              target='_blank'
              rel='noopener noreferrer'
              className='font-medium transition-colors'
              style={{ color: 'var(--pf-ink-2)' }}
            >
              {t('New API')}
            </a>
          </span>
        </div>
      </div>
    </footer>
  )
}
