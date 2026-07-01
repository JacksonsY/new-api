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
import { PublicHeader } from '@/components/layout/components/public-header'
import { PremiumBento } from './components/premium-bento'
import { PremiumFooter } from './components/premium-footer'
import { PremiumCTA } from './components/premium-cta'
import { PremiumGetstarted } from './components/premium-getstarted'
import { PremiumHero } from './components/premium-hero'
import { PremiumMarquee } from './components/premium-marquee'
import { PremiumRouter } from './components/premium-router'
import { PremiumWorldMap } from './components/premium-worldmap'
import { PremiumQuickstart } from './components/premium-quickstart'
import { PremiumStats } from './components/premium-stats'
import { useSmoothScroll } from './lib'
import './premium.css'

/**
 * 「离火・白」 — the bespoke premium marketing landing. Light-first, glass,
 * GSAP/Lenis choreographed, with the live 3D 离火核 centerpiece. Rendered in
 * place of the stock home sections for the default (non-custom) landing.
 */
export function PremiumLanding({
  isAuthenticated,
}: {
  isAuthenticated: boolean
}) {
  useSmoothScroll()

  return (
    <div className='pf'>
      <div className='pf-aurora' />
      <div className='pf-grid' />
      <div className='pf-grain' />

      <PublicHeader />

      <main className='relative z-10'>
        <PremiumHero isAuthenticated={isAuthenticated} />
        <PremiumMarquee />
        <PremiumStats />
        <PremiumRouter />
        <PremiumBento />
        <PremiumWorldMap />
        <PremiumQuickstart />
        <PremiumGetstarted />
        <PremiumCTA isAuthenticated={isAuthenticated} />
      </main>

      <PremiumFooter />
    </div>
  )
}
