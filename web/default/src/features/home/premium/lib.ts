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

/**
 * Motion engine for the premium landing.
 *
 * Pairs Lenis momentum smooth-scroll with GSAP's ScrollTrigger so scrubbed
 * animations stay perfectly in sync with the eased scroll position. Both are
 * scoped to the landing route (mounted/destroyed with the Home component) and
 * fully bypassed under `prefers-reduced-motion`.
 */
import { type RefObject, useEffect } from 'react'
import { useGSAP } from '@gsap/react'
import { gsap } from 'gsap'
import { ScrollTrigger } from 'gsap/ScrollTrigger'
import Lenis from 'lenis'

let registered = false

/** Register GSAP plugins exactly once. */
export function registerGsap() {
  if (registered) return
  gsap.registerPlugin(ScrollTrigger)
  registered = true
}

export function prefersReducedMotion(): boolean {
  return (
    typeof window !== 'undefined' &&
    window.matchMedia('(prefers-reduced-motion: reduce)').matches
  )
}

export { gsap, ScrollTrigger }

/**
 * Mount Lenis smooth-scroll for the lifetime of the calling component and
 * drive ScrollTrigger from its eased position. No-ops under reduced motion.
 */
export function useSmoothScroll() {
  useEffect(() => {
    if (prefersReducedMotion()) return
    registerGsap()

    const lenis = new Lenis({
      duration: 1.15,
      easing: (t: number) => Math.min(1, 1.001 - Math.pow(2, -10 * t)),
      smoothWheel: true,
    })

    lenis.on('scroll', ScrollTrigger.update)

    const onTick = (time: number) => lenis.raf(time * 1000)
    gsap.ticker.add(onTick)
    gsap.ticker.lagSmoothing(0)

    return () => {
      gsap.ticker.remove(onTick)
      lenis.destroy()
      ScrollTrigger.getAll().forEach((st) => st.kill())
    }
  }, [])
}

/** Split a string into word-wrapped spans for staggered headline reveals. */
export function splitWords(text: string): string[] {
  return text.split(/(\s+)/).filter((chunk) => chunk.length > 0)
}

/**
 * Reveal every `[data-reveal]` descendant of `scope` as it scrolls into view.
 * Scoped + auto-cleaned via useGSAP; a no-op under reduced motion (elements
 * just stay visible).
 */
export function useReveal(scope: RefObject<HTMLElement | null>) {
  useGSAP(
    () => {
      if (prefersReducedMotion()) return
      registerGsap()
      const root = scope.current
      if (!root) return
      const els = Array.from(
        root.querySelectorAll<HTMLElement>('[data-reveal]')
      )
      els.forEach((el) => {
        gsap.from(el, {
          y: 32,
          opacity: 0,
          duration: 0.85,
          ease: 'power3.out',
          scrollTrigger: { trigger: el, start: 'top 86%', once: true },
        })
      })
    },
    { scope }
  )
}
