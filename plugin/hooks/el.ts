import type { Elements } from 'claude-code'

import type { KhubTone } from '../types'

// Holds the elements every surface draws. A view takes this table from the shell's
// `$.ui.resolve(e)`.
export type El = Pick<Elements['terminal'], 'Box' | 'Text' | 'Button'>

// Returns a tone's text color, as props to spread. A plain tone keeps the surface's own color.
export function toneColor(tone: KhubTone): { color?: string } {
  if (tone === 'plain') return {}

  return { color: tone === 'ok' ? 'green' : tone === 'warn' ? 'yellow' : 'red' }
}
