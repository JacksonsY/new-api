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
import { Check } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useReveal } from '../lib'

const C = {
  key: '#c81e9e',
  str: '#f0452e',
  fn: '#7c3aed',
  num: '#b8860b',
  dim: '#8a8499',
}

export function PremiumQuickstart() {
  const { t } = useTranslation()
  const root = useRef<HTMLElement>(null)
  useReveal(root)

  const points = [
    t('Drop-in compatible with the OpenAI SDK'),
    t('Swap models without touching your code'),
    t('One key meters, bills, and rate-limits everything'),
  ]

  return (
    <section ref={root} className='relative z-10 px-6 py-16 md:py-24'>
      <div className='mx-auto grid max-w-6xl items-center gap-12 lg:grid-cols-2 lg:gap-16'>
        <div>
          <span data-reveal className='pf-eyebrow mb-4'>
            {t('Quickstart')}
          </span>
          <h2 data-reveal className='pf-h2 mb-5'>
            {t('Ship in')} <span className='pf-fire-text'>{t('one line.')}</span>
          </h2>
          <p
            data-reveal
            className='pf-lead mb-7 max-w-md'
            style={{ color: 'var(--pf-ink-2)' }}
          >
            {t(
              'Point your base URL at the gateway and keep the SDK you already use. That is the entire migration.'
            )}
          </p>
          <ul className='flex flex-col gap-3'>
            {points.map((p) => (
              <li
                key={p}
                data-reveal
                className='flex items-center gap-3 text-sm font-medium'
              >
                <span
                  className='flex size-5 items-center justify-center rounded-full'
                  style={{ background: 'var(--pf-grad)', color: '#fff' }}
                >
                  <Check className='size-3' strokeWidth={3} />
                </span>
                {p}
              </li>
            ))}
          </ul>
        </div>

        <div data-reveal className='pf-card pf-card-fire overflow-hidden p-0'>
          <div
            className='flex items-center gap-2 px-5 py-3.5'
            style={{ borderBottom: '1px solid var(--pf-line)' }}
          >
            <span
              className='size-3 rounded-full'
              style={{ background: '#ff5f57' }}
            />
            <span
              className='size-3 rounded-full'
              style={{ background: '#febc2e' }}
            />
            <span
              className='size-3 rounded-full'
              style={{ background: '#28c840' }}
            />
            <span
              className='ml-2 text-xs font-medium'
              style={{ color: 'var(--pf-muted)' }}
            >
              chat.completion.sh
            </span>
          </div>
          <pre
            className='overflow-x-auto px-5 py-5 text-[13px] leading-relaxed'
            style={{ fontFamily: 'var(--font-mono, ui-monospace, monospace)' }}
          >
            <code>
              <span style={{ color: C.dim }}># one endpoint, every model{'\n'}</span>
              <span style={{ color: C.fn }}>curl</span>{' '}
              {typeof window !== 'undefined'
                ? window.location.origin
                : 'https://api.jzlh99.com'}
              <span style={{ color: C.str }}>/v1/chat/completions</span> \{'\n'}
              {'  '}-H <span style={{ color: C.str }}>"Authorization: Bearer $JZLH_KEY"</span> \{'\n'}
              {'  '}-d <span style={{ color: C.str }}>{"'{"}</span>
              {'\n'}
              {'       '}<span style={{ color: C.key }}>"model"</span>:{' '}
              <span style={{ color: C.str }}>"gpt-4o"</span>,{'\n'}
              {'       '}<span style={{ color: C.key }}>"stream"</span>:{' '}
              <span style={{ color: C.num }}>true</span>,{'\n'}
              {'       '}<span style={{ color: C.key }}>"messages"</span>: [
              {'{'} <span style={{ color: C.key }}>"role"</span>:{' '}
              <span style={{ color: C.str }}>"user"</span>,{' '}
              <span style={{ color: C.key }}>"content"</span>:{' '}
              <span style={{ color: C.str }}>"离火当令"</span> {'}'}]{'\n'}
              {'     '}<span style={{ color: C.str }}>{"}'"}</span>
            </code>
          </pre>
        </div>
      </div>
    </section>
  )
}
