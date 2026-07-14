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
  ArrowRight,
  BadgeCheck,
  Check,
  ClipboardCheck,
  Gauge,
  HandCoins,
  Handshake,
  Layers,
  Receipt,
  Route,
  ShieldCheck,
  Share2,
  Store,
  Timer,
  Wallet,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { PremiumPublicLayout } from '@/components/layout'
import { cn } from '@/lib/utils'

const SUPPLIER_CTA = '/supplier/apply'
const AGENT_CTA = '/register'

function HeroCtas({ t }: { t: (k: string) => string }) {
  return (
    <div className='mt-7 flex flex-wrap items-center justify-center gap-3'>
      <a href={SUPPLIER_CTA} className='pf-btn pf-btn-fire'>
        {t('Become a supplier')}
        <ArrowRight className='size-4' />
      </a>
      <a href={AGENT_CTA} className='pf-btn pf-btn-ghost'>
        {t('Become an agent')}
      </a>
    </div>
  )
}

function SectionHead({
  eyebrow,
  title,
  accent,
}: {
  eyebrow: string
  title: string
  accent: string
}) {
  return (
    <div className='mx-auto mb-9 max-w-2xl text-center'>
      <span className='pf-eyebrow mb-3'>{eyebrow}</span>
      <h2
        className='text-2xl font-bold tracking-tight sm:text-3xl'
        style={{ color: 'var(--pf-ink)' }}
      >
        {title} <span className='pf-fire-text'>{accent}</span>
      </h2>
    </div>
  )
}

type ProgramFeature = { title: string; desc: string }
type Program = {
  icon: LucideIcon
  fire: boolean
  eyebrow: string
  title: string
  subtitle: string
  features: ProgramFeature[]
  cta: { label: string; href: string }
  footnote?: string
}

function ProgramCard({ program }: { program: Program }) {
  const { icon: Icon } = program
  return (
    <div
      className={cn(
        'pf-card flex flex-col overflow-hidden p-6 sm:p-7',
        program.fire && 'pf-card-fire'
      )}
    >
      <div className='flex items-center gap-3'>
        <span
          className='inline-flex size-10 shrink-0 items-center justify-center rounded-xl'
          style={{
            background: 'rgba(255,255,255,0.9)',
            border: '1px solid var(--pf-line-2)',
          }}
        >
          <Icon className='size-5' style={{ color: 'var(--pf-fire)' }} />
        </span>
        <div>
          <p className='pf-eyebrow'>{program.eyebrow}</p>
          <h3
            className='text-base font-bold sm:text-lg'
            style={{ color: 'var(--pf-ink)' }}
          >
            {program.title}
          </h3>
        </div>
      </div>

      <p
        className='mt-3 text-sm leading-relaxed'
        style={{ color: 'var(--pf-ink-2)' }}
      >
        {program.subtitle}
      </p>

      <ul className='mt-4 space-y-2.5'>
        {program.features.map((f) => (
          <li key={f.title} className='flex gap-2.5'>
            <span
              className='mt-0.5 inline-flex size-5 shrink-0 items-center justify-center rounded-full'
              style={{ background: 'var(--pf-grad)' }}
            >
              <Check className='size-3 text-white' />
            </span>
            <span className='text-sm' style={{ color: 'var(--pf-ink-2)' }}>
              <span className='font-semibold' style={{ color: 'var(--pf-ink)' }}>
                {f.title}
              </span>{' '}
              — {f.desc}
            </span>
          </li>
        ))}
      </ul>

      <div className='mt-auto pt-6'>
        <a href={program.cta.href} className='pf-btn pf-btn-fire'>
          {program.cta.label}
          <ArrowRight className='size-4' />
        </a>
        {program.footnote && (
          <p className='mt-2.5 text-xs' style={{ color: 'var(--pf-muted)' }}>
            {program.footnote}
          </p>
        )}
      </div>
    </div>
  )
}

export function PartnersPage() {
  const { t } = useTranslation()

  const stats = [
    { value: '40+', label: t('upstream providers') },
    { value: '100+', label: t('models, one endpoint') },
    { value: '50+', label: t('compatible API routes') },
    { value: '99.9%', label: t('routing uptime') },
  ]

  const programs: Program[] = [
    {
      icon: Store,
      fire: true,
      eyebrow: t('Supplier Program'),
      title: t('Turn your API capacity into revenue'),
      subtitle: t(
        'List your upstream channels on the platform. We route real traffic to them and settle your earnings at cost — you keep the margin you set.'
      ),
      features: [
        {
          title: t('List your channels'),
          desc: t('Submit, get reviewed, and join the routing pool.'),
        },
        {
          title: t('Cost-based settlement'),
          desc: t('Earn by your quote rate on every routed request.'),
        },
        {
          title: t('Quality protected'),
          desc: t('Authenticity detection keeps the marketplace clean.'),
        },
      ],
      cta: { label: t('Apply to onboard'), href: SUPPLIER_CTA },
      footnote: t('Payouts are settled by the platform after a maturity period.'),
    },
    {
      icon: Share2,
      fire: false,
      eyebrow: t('Agent Program'),
      title: t('Grow your business by bringing users'),
      subtitle: t(
        'Invite users and build your downstream. You earn a share of what they consume, with anti-fraud safeguards and manual payouts.'
      ),
      features: [
        {
          title: t('Bring your users'),
          desc: t('Build your downstream through your referral link.'),
        },
        {
          title: t('Commission on usage'),
          desc: t('Earn a share of downstream consumption.'),
        },
        {
          title: t('Withdraw your earnings'),
          desc: t('Matured commission can be withdrawn or converted.'),
        },
      ],
      cta: { label: t('Sign up to start'), href: AGENT_CTA },
      footnote: t('Agent status is activated by the platform — sign up first, then contact us.'),
    },
  ]

  const steps = [
    {
      icon: ClipboardCheck,
      title: t('Onboard'),
      desc: t(
        'Apply as a supplier or sign up as an agent. Review is quick and the requirements are light.'
      ),
    },
    {
      icon: Route,
      title: t('Serve real traffic'),
      desc: t(
        'Approved supplier channels join the routing pool; agents bring users. Real usage starts flowing to you.'
      ),
    },
    {
      icon: Wallet,
      title: t('Get settled'),
      desc: t(
        'Earnings accrue per request, mature over a short hold, then the platform pays you out.'
      ),
    },
  ]

  const benefits = [
    {
      icon: Receipt,
      title: t('Per-request accounting'),
      desc: t('Every routed request is priced and logged transparently.'),
    },
    {
      icon: Timer,
      title: t('Maturity protection'),
      desc: t('A short hold guards against refunds and fraud before payout.'),
    },
    {
      icon: ShieldCheck,
      title: t('Quality safeguards'),
      desc: t('Authenticity detection and health checks keep the pool clean.'),
    },
    {
      icon: HandCoins,
      title: t('Controlled payouts'),
      desc: t('Manual, auditable payouts — no surprise clawbacks.'),
    },
    {
      icon: Layers,
      title: t('Shared routing pool'),
      desc: t('Compete in the same pool by price and priority.'),
    },
    {
      icon: BadgeCheck,
      title: t('Anti-fraud built in'),
      desc: t('IP checks and risk controls protect honest partners.'),
    },
  ]

  const faqs = [
    {
      q: t('How am I paid?'),
      a: t(
        'Suppliers earn the cost of traffic routed to their channels by their quote rate; agents earn a share of downstream consumption. Amounts accrue per request.'
      ),
    },
    {
      q: t('When do payouts happen?'),
      a: t(
        'Earnings mature after a short hold to cover refunds and risk checks, then the platform settles payouts manually.'
      ),
    },
    {
      q: t('What do I need to become a supplier?'),
      a: t(
        'An upstream endpoint (base URL, key) serving priced models. Submit a channel and it goes through a quick review before joining the pool.'
      ),
    },
    {
      q: t('How do I become an agent?'),
      a: t(
        'Sign up for an account first, then contact us to activate agent status. You then invite users through your referral link.'
      ),
    },
    {
      q: t('Is user data safe?'),
      a: t(
        'Suppliers only see aggregate metrics for their own channels in the portal, and every partner is bound by a data-handling agreement.'
      ),
    },
  ]

  return (
    <PremiumPublicLayout>
      {/* Hero */}
      <section className='mx-auto w-full max-w-5xl px-6 pt-20 pb-4 text-center sm:pt-24'>
        <span className='pf-pill mb-5'>
          <Handshake className='size-3.5' style={{ color: 'var(--pf-fire)' }} />
          {t('Partner Program')}
        </span>
        <h1
          className='mx-auto text-3xl font-bold tracking-tight sm:text-4xl md:text-5xl'
          style={{ color: 'var(--pf-ink)' }}
        >
          {t('Earn with the platform,')}{' '}
          <span className='pf-fire-text whitespace-nowrap'>{t('two ways.')}</span>
        </h1>
        <p className='pf-lead mx-auto mt-5 max-w-2xl'>
          {t(
            'Supply upstream capacity as a supplier, or bring users as an agent. Real traffic, transparent per-request accounting, and platform-settled payouts.'
          )}
        </p>
        <HeroCtas t={t} />
      </section>

      {/* Stats — social proof */}
      <section className='relative z-10 px-6 py-8'>
        <div className='pf-glass mx-auto grid max-w-4xl grid-cols-2 gap-y-6 rounded-[24px] px-6 py-7 md:grid-cols-4 md:py-8'>
          {stats.map((s) => (
            <div key={s.label} className='flex flex-col items-center text-center'>
              <span className='pf-fire-text text-3xl font-bold tracking-tight md:text-4xl'>
                {s.value}
              </span>
              <span
                className='mt-1.5 text-xs font-medium'
                style={{ color: 'var(--pf-muted)' }}
              >
                {s.label}
              </span>
            </div>
          ))}
        </div>
      </section>

      {/* Programs */}
      <section className='relative z-10 px-6 py-10'>
        <div className='mx-auto max-w-5xl'>
          <SectionHead
            eyebrow={t('Two programs')}
            title={t('Pick the side that')}
            accent={t('fits you.')}
          />
          <div className='grid items-stretch gap-5 lg:grid-cols-2'>
            {programs.map((p) => (
              <ProgramCard key={p.eyebrow} program={p} />
            ))}
          </div>
        </div>
      </section>

      {/* How it works */}
      <section className='relative z-10 px-6 py-10 md:py-12'>
        <div className='mx-auto max-w-5xl'>
          <SectionHead
            eyebrow={t('How it works')}
            title={t('From onboarding to')}
            accent={t('payout.')}
          />
          <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
            {steps.map((s, i) => {
              const Icon = s.icon
              return (
                <div key={s.title} className='pf-card flex flex-col gap-3 p-6'>
                  <div className='flex items-center gap-2.5'>
                    <span
                      className='inline-flex size-8 items-center justify-center rounded-full text-sm font-bold text-white'
                      style={{ background: 'var(--pf-grad)' }}
                    >
                      {i + 1}
                    </span>
                    <Icon
                      className='size-5'
                      style={{ color: 'var(--pf-fire)' }}
                    />
                    <h3 className='text-base font-bold tracking-tight'>
                      {s.title}
                    </h3>
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

      {/* Benefits */}
      <section className='relative z-10 px-6 py-10 md:py-12'>
        <div className='mx-auto max-w-5xl'>
          <SectionHead
            eyebrow={t('Why partner with us')}
            title={t('Built to pay partners')}
            accent={t('fairly.')}
          />
          <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3'>
            {benefits.map((b) => {
              const Icon = b.icon
              return (
                <div key={b.title} className='pf-card flex flex-col gap-2.5 p-5'>
                  <span
                    className='inline-flex size-9 items-center justify-center rounded-xl'
                    style={{
                      background: 'rgba(255,255,255,0.9)',
                      border: '1px solid var(--pf-line-2)',
                    }}
                  >
                    <Icon className='size-4' style={{ color: 'var(--pf-fire)' }} />
                  </span>
                  <h3
                    className='text-sm font-bold'
                    style={{ color: 'var(--pf-ink)' }}
                  >
                    {b.title}
                  </h3>
                  <p
                    className='text-xs leading-relaxed'
                    style={{ color: 'var(--pf-ink-2)' }}
                  >
                    {b.desc}
                  </p>
                </div>
              )
            })}
          </div>
        </div>
      </section>

      {/* FAQ */}
      <section className='relative z-10 px-6 py-10 md:py-12'>
        <div className='mx-auto max-w-3xl'>
          <SectionHead
            eyebrow={t('FAQ')}
            title={t('Questions,')}
            accent={t('answered.')}
          />
          <div className='space-y-2.5'>
            {faqs.map((f) => (
              <div key={f.q} className='pf-card p-5'>
                <div className='flex items-start gap-2.5'>
                  <Gauge
                    className='mt-0.5 size-4 shrink-0'
                    style={{ color: 'var(--pf-fire)' }}
                  />
                  <div>
                    <h3
                      className='text-sm font-bold'
                      style={{ color: 'var(--pf-ink)' }}
                    >
                      {f.q}
                    </h3>
                    <p
                      className='mt-1 text-sm leading-relaxed'
                      style={{ color: 'var(--pf-ink-2)' }}
                    >
                      {f.a}
                    </p>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      {/* Final CTA */}
      <section className='relative z-10 px-6 py-12 md:py-16'>
        <div className='pf-card pf-card-fire mx-auto max-w-3xl overflow-hidden px-8 py-12 text-center'>
          <span className='pf-eyebrow mb-4'>{t('Get started')}</span>
          <h2
            className='mx-auto max-w-[16ch] text-2xl font-bold tracking-tight sm:text-3xl'
            style={{ color: 'var(--pf-ink)' }}
          >
            {t('Ready to')} <span className='pf-fire-text'>{t('earn?')}</span>
          </h2>
          <p className='pf-lead mx-auto mt-4 max-w-xl'>
            {t('Pick your program and start today.')}
          </p>
          <HeroCtas t={t} />
        </div>
      </section>
    </PremiumPublicLayout>
  )
}
