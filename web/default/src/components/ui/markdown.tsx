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
import { lazy, Suspense } from 'react'

// The markdown renderer pulls in marked + katex (+ katex CSS) + dompurify — a large,
// first-paint-irrelevant stack. It is rendered inside the app shell (NotificationPopover
// in both headers), which otherwise hoists that stack into the initial chunk. Splitting it
// behind React.lazy keeps marked/katex/dompurify out of first paint; the chunk loads the
// first time any markdown is actually rendered and is cached thereafter.
const MarkdownImpl = lazy(() =>
  import('./markdown-impl').then((m) => ({ default: m.Markdown }))
)

interface MarkdownProps {
  breaks?: boolean
  children: string
  className?: string
}

export function Markdown(props: MarkdownProps) {
  return (
    <Suspense fallback={<div className={props.className} />}>
      <MarkdownImpl breaks={props.breaks} className={props.className}>
        {props.children}
      </MarkdownImpl>
    </Suspense>
  )
}
