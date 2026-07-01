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
import { createContext, useContext } from 'react'

/**
 * Marks the subtree as the `离火・白` premium surface — a light-first marketing
 * canvas (scoped under `.pf`) that stays white regardless of the app's active
 * light/dark theme. Theme-reactive descendants (charts, etc.) read this to pin
 * themselves to their light variant so they don't render dark on the white
 * surface when the OS/app theme is dark.
 */
const PremiumSurfaceContext = createContext(false)

export function PremiumSurfaceProvider({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <PremiumSurfaceContext.Provider value={true}>
      {children}
    </PremiumSurfaceContext.Provider>
  )
}

export function useIsPremiumSurface() {
  return useContext(PremiumSurfaceContext)
}
