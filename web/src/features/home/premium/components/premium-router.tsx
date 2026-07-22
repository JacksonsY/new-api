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
  DeepSeek,
  Gemini,
  Meta,
  Mistral,
  OpenAI,
} from '@lobehub/icons'
import { ChevronDown, GitBranch, KeyRound, Receipt, Scale } from 'lucide-react'
import { type ComponentType, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { useReveal } from '../lib'

type LogoMark = ComponentType<{ size: number; className?: string }>
type RouteNode = { name: string; Mark?: LogoMark }

// Six representative spokes + a "+34" overflow node so the fan reads as the
// full 40+ provider catalog without crowding the diagram.
const NODES: RouteNode[] = [
  { name: 'OpenAI', Mark: OpenAI.Avatar },
  { name: 'Anthropic', Mark: Anthropic.Avatar },
  { name: 'Gemini', Mark: Gemini.Avatar },
  { name: 'DeepSeek', Mark: DeepSeek.Avatar },
  { name: 'Llama', Mark: Meta.Avatar },
  { name: 'Mistral', Mark: Mistral.Avatar },
  { name: '+34' },
]

// Geometry in the 1000×520 viewBox. The hub disc (HTML, z-10) sits over the
// convergence point so every spoke appears to emerge from under it.
const HUB = { x: 500, y: 260 }
const APP = { x: 160, y: 260 }
const PROV_X = 840
const NODE_Y = [50, 120, 190, 260, 330, 400, 470]

const pctX = (x: number) => `${(x / 1000) * 100}%`
const pctY = (y: number) => `${(y / 520) * 100}%`

/** 离卦 ☲ — solid · broken · solid, rendered in white on the gradient hub. */
function Trigram() {
  return (
    <span className='flex flex-col items-center gap-[5px]'>
      <i className='block h-[3.5px] w-7 rounded-full bg-white/95' />
      <span className='flex gap-[6px]'>
        <i className='block h-[3.5px] w-[11px] rounded-full bg-white/95' />
        <i className='block h-[3.5px] w-[11px] rounded-full bg-white/95' />
      </span>
      <i className='block h-[3.5px] w-7 rounded-full bg-white/95' />
    </span>
  )
}

function RouteChip({ node }: { node: RouteNode }) {
  if (!node.Mark) {
    return <span className='pf-chip pf-fire-text font-bold'>{node.name}</span>
  }
  const Mark = node.Mark
  return (
    <span className='pf-chip'>
      <Mark size={20} className='rounded-[6px]' />
      {node.name}
    </span>
  )
}

export function PremiumRouter() {
  const { t } = useTranslation()
  const root = useRef<HTMLElement>(null)
  useReveal(root)

  return (
    <section ref={root} className='relative z-10 px-6 py-20 md:py-28'>
      <div className='mx-auto max-w-6xl'>
        <div className='mx-auto mb-14 max-w-2xl text-center'>
          <span data-reveal className='pf-eyebrow mb-4'>
            {t('How it works')}
          </span>
          <h2 data-reveal className='pf-h2'>
            <span className='block'>{t('One key routes to')}</span>
            <span className='pf-fire-text block'>{t('every provider')}</span>
          </h2>
          <p data-reveal className='pf-lead mx-auto mt-5 max-w-xl'>
            {t(
              'Send an OpenAI-style request. We route it to the right provider, fail over when one blinks, and meter it by the token — all behind a single key.'
            )}
          </p>
        </div>

        {/* ── Desktop fan diagram ─────────────────────────────────────── */}
        <div
          data-reveal
          className='relative mx-auto hidden w-full max-w-4xl md:block'
          style={{ aspectRatio: '1000 / 520' }}
        >
          <svg
            viewBox='0 0 1000 520'
            preserveAspectRatio='xMidYMid meet'
            className='absolute inset-0 h-full w-full'
            aria-hidden
          >
            <defs>
              <linearGradient
                id='pfFlow'
                gradientUnits='userSpaceOnUse'
                x1='140'
                y1='260'
                x2='860'
                y2='260'
              >
                <stop offset='0' stopColor='#7c3aed' />
                <stop offset='0.34' stopColor='#c81e9e' />
                <stop offset='0.66' stopColor='#f0452e' />
                <stop offset='1' stopColor='#f6b43e' />
              </linearGradient>
            </defs>

            {/* app → hub */}
            <path
              className='pf-spoke'
              d={`M${APP.x} ${APP.y} L${HUB.x} ${HUB.y}`}
            />
            <path
              className='pf-flow'
              d={`M${APP.x} ${APP.y} L${HUB.x} ${HUB.y}`}
            />

            {/* hub → providers */}
            {NODE_Y.map((y, i) => {
              const d = `M${HUB.x} ${HUB.y} C 680 ${HUB.y}, 700 ${y}, ${PROV_X} ${y}`
              return (
                <g key={y}>
                  <path className='pf-spoke' d={d} />
                  <path
                    className='pf-flow'
                    d={d}
                    style={{ animationDelay: `${(i % 4) * 0.22}s` }}
                  />
                </g>
              )
            })}
          </svg>

          {/* app node */}
          <div
            className='absolute z-10 -translate-x-1/2 -translate-y-1/2'
            style={{ left: pctX(APP.x), top: pctY(APP.y) }}
          >
            <div className='pf-glass flex flex-col items-start gap-1 rounded-2xl px-4 py-3'>
              <span
                className='inline-flex items-center gap-1 text-[0.66rem] font-semibold tracking-wider uppercase'
                style={{ color: 'var(--pf-muted)' }}
              >
                <KeyRound className='size-3' strokeWidth={2} />
                {t('Your app')}
              </span>
              <span className='font-mono text-[0.82rem] font-medium'>
                sk-jzlh-••••a39f
              </span>
            </div>
          </div>

          {/* request path floating on the app → hub wire */}
          <div
            className='absolute z-10 -translate-x-1/2 -translate-y-1/2'
            style={{ left: pctX(338), top: pctY(210) }}
          >
            <span className='pf-pill font-mono !text-[0.72rem]'>
              POST /v1/chat/completions
            </span>
          </div>

          {/* hub node */}
          <div
            className='absolute z-10 flex -translate-x-1/2 -translate-y-1/2 flex-col items-center gap-2.5'
            style={{ left: '50%', top: '50%' }}
          >
            <span className='pf-hub flex size-24 items-center justify-center rounded-full'>
              <Trigram />
            </span>
            <span
              className='text-[0.7rem] font-bold tracking-[0.18em] uppercase'
              style={{ color: 'var(--pf-ink-2)' }}
            >
              {t('Gateway')}
            </span>
          </div>

          {/* provider nodes */}
          {NODES.map((node, i) => (
            <div
              key={node.name}
              className='absolute z-10 -translate-x-1/2 -translate-y-1/2'
              style={{ left: pctX(PROV_X), top: pctY(NODE_Y[i]) }}
            >
              <RouteChip node={node} />
            </div>
          ))}
        </div>

        {/* ── Mobile vertical flow ────────────────────────────────────── */}
        <div className='mx-auto flex max-w-sm flex-col items-center gap-4 md:hidden'>
          <div className='pf-glass flex flex-col items-start gap-1 rounded-2xl px-4 py-3'>
            <span
              className='inline-flex items-center gap-1 text-[0.66rem] font-semibold tracking-wider uppercase'
              style={{ color: 'var(--pf-muted)' }}
            >
              <KeyRound className='size-3' strokeWidth={2} />
              {t('Your app')}
            </span>
            <span className='font-mono text-[0.82rem] font-medium'>
              sk-jzlh-••••a39f
            </span>
          </div>
          <ChevronDown
            className='size-5'
            style={{ color: 'var(--pf-magenta)' }}
          />
          <div className='flex flex-col items-center gap-2'>
            <span className='pf-hub flex size-16 items-center justify-center rounded-full'>
              <Trigram />
            </span>
            <span
              className='text-[0.66rem] font-bold tracking-[0.18em] uppercase'
              style={{ color: 'var(--pf-ink-2)' }}
            >
              {t('Gateway')}
            </span>
          </div>
          <ChevronDown
            className='size-5'
            style={{ color: 'var(--pf-ember)' }}
          />
          <div className='flex flex-wrap justify-center gap-2'>
            {NODES.map((node) => (
              <RouteChip key={node.name} node={node} />
            ))}
          </div>
        </div>

        {/* ── Capability pills ────────────────────────────────────────── */}
        <div
          data-reveal
          className='mt-14 flex flex-wrap items-center justify-center gap-3'
        >
          <span className='pf-pill'>
            <GitBranch
              className='size-3.5'
              style={{ color: 'var(--pf-violet)' }}
            />
            {t('Automatic failover')}
          </span>
          <span className='pf-pill'>
            <Scale
              className='size-3.5'
              style={{ color: 'var(--pf-magenta)' }}
            />
            {t('Load balancing')}
          </span>
          <span className='pf-pill'>
            <Receipt
              className='size-3.5'
              style={{ color: 'var(--pf-ember)' }}
            />
            {t('Unified billing')}
          </span>
        </div>
      </div>
    </section>
  )
}
