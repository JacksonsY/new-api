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
 * Lightweight placeholder for a provider icon: shown while the lazy LobeHub
 * chunk loads and when an icon name cannot be resolved. Deliberately dependency
 * -free so it never drags `@lobehub/icons` into the caller's chunk.
 */
export function LobeIconFallback(props: { size: number; label: string }) {
  return (
    <div
      className='bg-muted text-muted-foreground flex items-center justify-center rounded-full text-xs font-medium'
      style={{ width: props.size, height: props.size }}
    >
      {props.label}
    </div>
  )
}
