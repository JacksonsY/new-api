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
import { type CSSProperties, useMemo, useRef } from 'react'
import DottedMap from 'dotted-map'
import { useTranslation } from 'react-i18next'
import { useReveal } from '../lib'

// Glowing nodes — major regions a global AI gateway routes through.
const CITIES = [
  { name: 'San Francisco', lat: 37.77, lng: -122.42 },
  { name: 'New York', lat: 40.71, lng: -74.0 },
  { name: 'São Paulo', lat: -23.55, lng: -46.63 },
  { name: 'London', lat: 51.5, lng: -0.12 },
  { name: 'Frankfurt', lat: 50.11, lng: 8.68 },
  { name: 'Dubai', lat: 25.2, lng: 55.27 },
  { name: 'Mumbai', lat: 19.08, lng: 72.88 },
  { name: 'Singapore', lat: 1.35, lng: 103.82 },
  { name: 'Tokyo', lat: 35.68, lng: 139.69 },
  { name: 'Sydney', lat: -33.87, lng: 151.21 },
]

export function PremiumWorldMap() {
  const { t } = useTranslation()
  const root = useRef<HTMLElement>(null)
  useReveal(root)

  const { bg, nodes, w, h } = useMemo(() => {
    const map = new DottedMap({ height: 60, grid: 'diagonal' })
    const svg = map.getSVG({
      radius: 0.28,
      color: '#d4cdea',
      shape: 'circle',
      backgroundColor: 'transparent',
    })
    const w = map.image.width
    const h = map.image.height
    const bg = `url("data:image/svg+xml;utf8,${encodeURIComponent(svg)}")`
    const nodes = CITIES.flatMap((c) => {
      const pin = map.getPin({ lat: c.lat, lng: c.lng })
      if (!pin) return []
      return [{ name: c.name, left: (pin.x / w) * 100, top: (pin.y / h) * 100 }]
    })
    return { bg, nodes, w, h }
  }, [])

  return (
    <section ref={root} className='relative z-10 px-6 py-20'>
      <div className='mx-auto max-w-5xl'>
        <div className='mb-12 text-center'>
          <span data-reveal className='pf-eyebrow mb-4'>
            {t('Global reach')}
          </span>
          <h2 data-reveal className='pf-h2'>
            <span className='block'>{t('Low latency')}</span>
            <span className='pf-fire-text block'>{t('worldwide')}</span>
          </h2>
          <p data-reveal className='pf-lead mx-auto mt-5 max-w-md'>
            {t('Requests route to the nearest region, automatically')}
          </p>
        </div>

        <div
          data-reveal
          className='relative mx-auto w-full'
          style={{ aspectRatio: `${w} / ${h}` }}
        >
          {/* faint dotted world map */}
          <div
            aria-hidden
            className='absolute inset-0'
            style={{
              backgroundImage: bg,
              backgroundSize: 'contain',
              backgroundPosition: 'center',
              backgroundRepeat: 'no-repeat',
            }}
          />
          {/* glowing 离火 nodes */}
          {nodes.map((n, i) => (
            <span
              key={n.name}
              className='pf-node'
              title={n.name}
              style={
                {
                  left: `${n.left}%`,
                  top: `${n.top}%`,
                  '--d': `${(i % 5) * 0.5}s`,
                } as CSSProperties
              }
            />
          ))}
        </div>
      </div>
    </section>
  )
}
