import type { Elements } from 'claude-code'

import type { KhubTone } from '../types'

// The elements every surface draws. A view takes this table from the shell's `$.ui.resolve(e)`.
// `Input` and `Select` may be missing, and a surface with no text entry hands over ones that
// draw an empty box, so a view probes what they draw.
export type El = Pick<Elements['terminal'], 'Box' | 'Text' | 'Button' | 'Code' | 'Markdown' | 'Link'> &
  Partial<Pick<Elements['terminal'], 'Input' | 'Select'>>

// A tone's text color, as props to spread. A plain tone keeps the surface's own color.
export function toneColor(tone: KhubTone): { color?: string } {
  if (tone === 'plain') return {}

  return { color: tone === 'ok' ? 'green' : tone === 'warn' ? 'yellow' : 'red' }
}
