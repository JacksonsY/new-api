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
import { KeyRound, Rocket, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useReveal } from '../lib'

export function PremiumGetstarted() {
  const { t } = useTranslation()
  const root = useRef<HTMLElement>(null)
  useReveal(root)

  const steps = [
    {
      icon: KeyRound,
      title: t('Create your key'),
      desc: t(
        'Sign up and generate an API key in the console — free and instant.'
      ),
    },
    {
      icon: Wallet,
      title: t('Add credits'),
      desc: t(
        'Top up a pay-as-you-go balance. Token-accurate metering, no subscription.'
      ),
    },
    {
      icon: Rocket,
      title: t('Point & ship'),
      desc: t(
        'Set your base URL to the gateway. Your existing OpenAI, Claude, and Gemini code runs unchanged.'
      ),
    },
  ]

  return (
    <section ref={root} className='relative z-10 px-6 py-16 md:py-24'>
      <div className='mx-auto max-w-6xl'>
        <div className='mx-auto mb-14 max-w-2xl text-center'>
          <span data-reveal className='pf-eyebrow mb-4'>
            {t('Get started')}
          </span>
          <h2 data-reveal className='pf-h2'>
            {t('From zero to your')}{' '}
            <span className='pf-fire-text'>{t('first token.')}</span>
          </h2>
        </div>

        <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
          {steps.map((s, i) => {
            const Icon = s.icon
            return (
              <div
                key={s.title}
                data-reveal
                className='pf-card flex flex-col gap-4 p-7'
              >
                <span
                  className='inline-flex size-9 items-center justify-center rounded-full text-sm font-bold text-white'
                  style={{ background: 'var(--pf-grad)' }}
                >
                  {i + 1}
                </span>
                <div className='flex items-center gap-2.5'>
                  <Icon
                    className='size-5'
                    strokeWidth={1.75}
                    style={{ color: 'var(--pf-violet)' }}
                  />
                  <h3 className='text-lg font-bold tracking-tight'>{s.title}</h3>
                </div>
                <p
                  className='text-sm leading-relaxed'
                  style={{ color: 'var(--pf-ink-2)' }}
                >
                  {s.desc}
                </p>
              </div>
            )
          })}
        </div>
      </div>
    </section>
  )
}
