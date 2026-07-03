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
import { ContactFloat } from '@/components/contact-float'
import { PremiumFooter } from '@/features/home/premium/components/premium-footer'
import '@/features/home/premium/premium.css'
import type { TopNavLink } from '../types'
import { PublicHeader, type PublicHeaderProps } from './public-header'

type PremiumPublicLayoutProps = {
  children: React.ReactNode
  /** Wrap children in a centered, header-clearing container. Off by default so
   *  data pages (pricing, rankings) can own their full-bleed layout. */
  showMainContainer?: boolean
  /** Render the shared premium footer below the content. */
  showFooter?: boolean
  navContent?: React.ReactNode
  headerProps?: Omit<PublicHeaderProps, 'navContent'>
  navLinks?: TopNavLink[]
  showThemeSwitch?: boolean
  showAuthButtons?: boolean
  showNotifications?: boolean
  logo?: React.ReactNode
  siteName?: string
}

/**
 * The public shell for the `离火・白` premium surface — the same fire canvas as
 * the landing page (`.pf` design system + aurora / grid / grain atmosphere +
 * shared header/footer), reused across the public pages (Model Square,
 * Rankings, About, legal) so they read as one brand family. Follows the app's
 * active light/dark theme — `.pf` renders the light 离火·白 surface, and
 * `html.dark .pf` renders the 玄夜 dark variant.
 */
export function PremiumPublicLayout(props: PremiumPublicLayoutProps) {
  const { showMainContainer = false, showFooter = true } = props

  return (
    <div className='pf min-h-svh'>
      <div className='pf-aurora' aria-hidden />
      <div className='pf-grid' aria-hidden />
      <div className='pf-grain' aria-hidden />

      <PublicHeader
        navContent={props.navContent}
        navLinks={props.navLinks}
        showThemeSwitch={props.showThemeSwitch}
        showAuthButtons={props.showAuthButtons}
        showNotifications={props.showNotifications}
        logo={props.logo}
        siteName={props.siteName}
        {...props.headerProps}
      />

      {showMainContainer ? (
        <main className='relative z-10 mx-auto w-full max-w-5xl px-4 pt-24 pb-12 sm:px-6'>
          {props.children}
        </main>
      ) : (
        <main className='relative z-10'>{props.children}</main>
      )}

      {showFooter && <PremiumFooter />}
      <ContactFloat />
    </div>
  )
}
