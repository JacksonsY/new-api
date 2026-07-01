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
import { useTranslation } from 'react-i18next'
import { useGSAP } from '@gsap/react'
import { gsap, prefersReducedMotion, registerGsap, useReveal } from '../lib'

function format(v: number, decimals: number) {
  return decimals > 0 ? v.toFixed(decimals) : Math.round(v).toLocaleString()
}

function StatNumber(props: {
  end: number
  suffix?: string
  decimals?: number
}) {
  const { end, suffix = '', decimals = 0 } = props
  const ref = useRef<HTMLSpanElement>(null)

  useGSAP(
    () => {
      const el = ref.current
      if (!el) return
      if (prefersReducedMotion()) {
        el.textContent = `${format(end, decimals)}${suffix}`
        return
      }
      registerGsap()
      const obj = { v: 0 }
      gsap.to(obj, {
        v: end,
        duration: 1.7,
        ease: 'power2.out',
        scrollTrigger: { trigger: el, start: 'top 90%', once: true },
        onUpdate: () => {
          el.textContent = `${format(obj.v, decimals)}${suffix}`
        },
      })
    },
    { scope: ref }
  )

  return (
    <span ref={ref} className='tabular-nums'>
      0{suffix}
    </span>
  )
}

export function PremiumStats() {
  const { t } = useTranslation()
  const root = useRef<HTMLElement>(null)
  useReveal(root)

  const stats = [
    { end: 40, suffix: '+', label: t('upstream providers') },
    { end: 100, suffix: '+', label: t('models, one endpoint') },
    { end: 50, suffix: '+', label: t('compatible API routes') },
    { end: 99.9, suffix: '%', label: t('routing uptime'), decimals: 1 },
  ]

  return (
    <section ref={root} className='relative z-10 px-6 py-14'>
      <div
        data-reveal
        className='pf-glass mx-auto grid max-w-5xl grid-cols-2 gap-y-10 rounded-[28px] px-6 py-10 md:grid-cols-4 md:py-12'
      >
        {stats.map((s) => (
          <div key={s.label} className='flex flex-col items-center text-center'>
            <span
              className='pf-fire-text text-4xl font-bold tracking-tight md:text-5xl'
              style={{ letterSpacing: '-0.03em' }}
            >
              <StatNumber end={s.end} suffix={s.suffix} decimals={s.decimals} />
            </span>
            <span
              className='mt-2 text-xs font-medium md:text-sm'
              style={{ color: 'var(--pf-muted)' }}
            >
              {s.label}
            </span>
          </div>
        ))}
      </div>
    </section>
  )
}
