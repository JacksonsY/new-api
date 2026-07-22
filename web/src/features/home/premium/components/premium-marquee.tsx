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
  Anthropic,
  Cohere,
  DeepSeek,
  Gemini,
  Groq,
  Meta,
  Midjourney,
  Mistral,
  OpenAI,
  Qwen,
  Suno,
  XAI,
} from '@lobehub/icons'
import { type ComponentType } from 'react'
import { useTranslation } from 'react-i18next'

type LogoMark = ComponentType<{ size: number; className?: string }>

// Brand logo marks from @lobehub/icons. `.Avatar` is the one lockup every
// brand ships (Anthropic has no `.Combine`), so it keeps the row consistent.
const PROVIDERS: { name: string; Mark: LogoMark }[] = [
  { name: 'OpenAI', Mark: OpenAI.Avatar },
  { name: 'Anthropic', Mark: Anthropic.Avatar },
  { name: 'Gemini', Mark: Gemini.Avatar },
  { name: 'DeepSeek', Mark: DeepSeek.Avatar },
  { name: 'Qwen', Mark: Qwen.Avatar },
  { name: 'Llama', Mark: Meta.Avatar },
  { name: 'Mistral', Mark: Mistral.Avatar },
  { name: 'Groq', Mark: Groq.Avatar },
  { name: 'xAI', Mark: XAI.Avatar },
  { name: 'Cohere', Mark: Cohere.Avatar },
  { name: 'Midjourney', Mark: Midjourney.Avatar },
  { name: 'Suno', Mark: Suno.Avatar },
]

export function PremiumMarquee() {
  const { t } = useTranslation()
  const row = [...PROVIDERS, ...PROVIDERS]

  return (
    <section className='relative z-10 py-14'>
      <p
        className='mb-8 text-center text-[0.72rem] font-semibold tracking-[0.2em] uppercase'
        style={{ color: 'var(--pf-muted)' }}
      >
        {t('One key, every major model')}
      </p>
      <div className='pf-fade-x pf-marquee-track overflow-hidden'>
        <div className='pf-marquee gap-9 px-4'>
          {row.map((p, i) => {
            const Mark = p.Mark
            return (
              <span
                key={`${p.name}-${i}`}
                className='inline-flex shrink-0 items-center gap-2.5'
                // 第二份拷贝只为无缝循环存在，读屏不重复播报
                aria-hidden={i >= PROVIDERS.length || undefined}
              >
                <Mark size={26} className='rounded-[7px]' />
                <span
                  className='text-[0.95rem] font-semibold whitespace-nowrap'
                  style={{ color: 'var(--pf-ink-2)' }}
                >
                  {p.name}
                </span>
              </span>
            )
          })}
        </div>
      </div>
    </section>
  )
}
