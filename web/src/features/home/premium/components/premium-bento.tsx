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
import {
  Activity,
  CreditCard,
  Layers,
  ShieldCheck,
  Workflow,
  Zap,
} from 'lucide-react'
import { useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { useReveal } from '../lib'

export function PremiumBento() {
  const { t } = useTranslation()
  const root = useRef<HTMLElement>(null)
  useReveal(root)

  const cards = [
    {
      icon: Layers,
      title: t('One unified API'),
      desc: t(
        'OpenAI, Claude, Gemini, and 40+ providers behind a single, drop-in compatible endpoint. Switch models by changing one string.'
      ),
      span: 'md:col-span-2',
      fire: true,
    },
    {
      icon: Workflow,
      title: t('Smart routing'),
      desc: t('Weighted channels, automatic failover, and per-model limits.'),
      span: '',
    },
    {
      icon: Zap,
      title: t('Blazing speed'),
      desc: t(
        'Streaming-first, low-overhead proxy tuned for time-to-first-token.'
      ),
      span: '',
    },
    {
      icon: CreditCard,
      title: t('Transparent billing'),
      desc: t(
        'Token-accurate metering, tiered pricing, and per-key quotas — settled in real time.'
      ),
      span: 'md:col-span-2',
    },
    {
      icon: ShieldCheck,
      title: t('Enterprise-grade'),
      desc: t('Keys, groups, and granular permissions with full audit trails.'),
      span: '',
    },
    {
      icon: Activity,
      title: t('Full observability'),
      desc: t('Every request logged, charted, and traceable to the node.'),
      span: 'md:col-span-2',
    },
  ]

  return (
    <section ref={root} className='relative z-10 px-6 py-16 md:py-24'>
      <div className='mx-auto max-w-6xl'>
        <div className='mb-12 max-w-2xl'>
          <span data-reveal className='pf-eyebrow mb-4'>
            {t('The gateway')}
          </span>
          <h2 data-reveal className='pf-h2'>
            <span className='block'>{t('Everything between you and')}</span>
            <span className='pf-fire-text block'>{t('every model.')}</span>
          </h2>
        </div>

        <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
          {cards.map((c) => {
            const Icon = c.icon
            return (
              <div
                key={c.title}
                data-reveal
                className={`pf-card ${c.fire ? 'pf-card-fire' : ''} flex flex-col gap-3 p-7 ${c.span}`}
              >
                <span
                  className='flex size-11 items-center justify-center rounded-xl'
                  style={{
                    background: c.fire
                      ? 'var(--pf-grad)'
                      : 'rgba(124,58,237,0.08)',
                    color: c.fire ? '#fff' : 'var(--pf-violet)',
                  }}
                >
                  <Icon className='size-5' strokeWidth={1.75} />
                </span>
                <h3 className='text-lg font-bold tracking-tight'>{c.title}</h3>
                <p
                  className='text-sm leading-relaxed'
                  style={{ color: 'var(--pf-ink-2)' }}
                >
                  {c.desc}
                </p>
              </div>
            )
          })}
        </div>
      </div>
    </section>
  )
}
