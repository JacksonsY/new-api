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
import { lazy, Suspense, useRef } from 'react'
import { Link } from '@tanstack/react-router'
import { ArrowRight } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useGSAP } from '@gsap/react'
import { gsap, prefersReducedMotion, registerGsap } from '../lib'

// three.js is heavy (~600kB) — lazy-load it into its own async chunk so the
// hero paints instantly. The CSS orb stands in until WebGL is ready.
const EnergyCore = lazy(() =>
  import('../energy-core').then((m) => ({ default: m.EnergyCore }))
)

export function PremiumHero({ isAuthenticated }: { isAuthenticated: boolean }) {
  const { t } = useTranslation()
  const root = useRef<HTMLElement>(null)
  registerGsap()

  useGSAP(
    () => {
      if (prefersReducedMotion()) return
      const tl = gsap.timeline({ defaults: { ease: 'power3.out' } })
      tl.from('[data-an="core"]', {
        scale: 0.55,
        opacity: 0,
        duration: 1.3,
        ease: 'expo.out',
      })
        .from(
          '[data-an="eyebrow"]',
          { y: 18, opacity: 0, duration: 0.7 },
          0.25
        )
        .from(
          '[data-an="line"]',
          { yPercent: 118, opacity: 0, duration: 0.95, stagger: 0.12 },
          '-=0.45'
        )
        .from('[data-an="lead"]', { y: 20, opacity: 0, duration: 0.7 }, '-=0.5')
        .from(
          '[data-an="cta"] > *',
          { y: 16, opacity: 0, duration: 0.6, stagger: 0.1 },
          '-=0.45'
        )

      // gentle parallax: the core drifts up as you scroll past the hero
      gsap.to('[data-an="core-wrap"]', {
        yPercent: -16,
        ease: 'none',
        scrollTrigger: {
          trigger: root.current,
          start: 'top top',
          end: 'bottom top',
          scrub: true,
        },
      })
    },
    { scope: root }
  )

  return (
    <section
      ref={root}
      className='relative flex min-h-[100svh] flex-col items-center justify-center px-6 pt-28 pb-20 text-center sm:pt-32'
    >
      {/* 离火核 — live 3D centerpiece */}
      <div
        data-an='core-wrap'
        className='pointer-events-none relative mb-2 flex items-center justify-center'
      >
        <div
          aria-hidden
          className='absolute -z-10'
          style={{
            width: 'clamp(280px, 34vw, 440px)',
            height: 'clamp(280px, 34vw, 440px)',
            background:
              'radial-gradient(circle, rgba(240,69,46,0.34), rgba(124,58,237,0.22) 45%, transparent 66%)',
            filter: 'blur(46px)',
          }}
        />
        <div
          data-an='core'
          style={{
            width: 'clamp(216px, 25vw, 348px)',
            height: 'clamp(216px, 25vw, 348px)',
          }}
        >
          <Suspense
            fallback={
              <div className='pf-core-fallback h-full w-full' aria-hidden />
            }
          >
            <EnergyCore className='h-full w-full' />
          </Suspense>
        </div>
      </div>

      <div className='relative z-10 flex flex-col items-center'>
        <span data-an='eyebrow' className='pf-pill mb-5'>
          <span
            className='size-1.5 rounded-full'
            style={{ background: 'var(--pf-grad)' }}
          />
          {t('Unified AI Gateway')}
        </span>

        <h1 className='pf-display max-w-[16ch]'>
          <span className='block overflow-hidden pb-[0.08em]'>
            <span data-an='line' className='block'>
              {t('One API.')}
            </span>
          </span>
          <span className='block overflow-hidden pb-[0.12em]'>
            <span data-an='line' className='pf-fire-text block'>
              {t('Every model.')}
            </span>
          </span>
        </h1>

        <p data-an='lead' className='pf-lead mt-7 font-medium tracking-wide'>
          {t('100+ models · 40+ providers · one key')}
        </p>

        <div
          data-an='cta'
          className='mt-9 flex flex-wrap items-center justify-center gap-3'
        >
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
            {t('Explore models')}
          </Link>
        </div>
      </div>

    </section>
  )
}
