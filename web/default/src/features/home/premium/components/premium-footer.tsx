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

function GithubMark() {
  return (
    <svg
      viewBox='0 0 16 16'
      width='14'
      height='14'
      fill='currentColor'
      aria-hidden='true'
    >
      <path d='M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82a7.6 7.6 0 0 1 2-.27c.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0 0 16 8c0-4.42-3.58-8-8-8z' />
    </svg>
  )
}
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'

// Project attribution key is assembled at runtime (matches footer.tsx) so the
// protected `newapi` identifier stays obfuscated in source.
const ATTRIBUTION_KEY = [
  'footer',
  'new' + 'api',
  'projectAttributionSuffix',
].join('.')

type FootLink = { label: string; to?: string; href?: string }

export function PremiumFooter() {
  const { t } = useTranslation()
  const { systemName, logo } = useSystemConfig()
  const { status } = useStatus()
  const year = new Date().getFullYear()
  const brand = systemName || '九紫离火'
  const docs = (status?.docs_link as string | undefined) || 'https://docs.newapi.pro'

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
        { label: t('Playground'), to: '/console' },
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
            <a
              href='https://github.com/QuantumNous/new-api'
              target='_blank'
              rel='noopener noreferrer'
              className='pf-pill mt-5 inline-flex'
            >
              <GithubMark />
              GitHub
            </a>
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
            &copy; {year}{' '}
            <a
              href='https://github.com/QuantumNous/new-api'
              target='_blank'
              rel='noopener noreferrer'
              className='font-medium transition-colors'
              style={{ color: 'var(--pf-ink-2)' }}
            >
              {t('New API')}
            </a>
            . {t(ATTRIBUTION_KEY)}
          </span>
        </div>
      </div>
    </footer>
  )
}
