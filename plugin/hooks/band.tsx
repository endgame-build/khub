import type { RenderElement } from 'claude-code'

import { toneColor } from './el'
import type { El } from './el'
import type { Part } from './session'

// Draws the one line above the prompt, cut to `columns`. A failing check draws the line red.
// Otherwise a plain part is dim and a toned part takes its color.
export function summaryLine({ Box, Text }: El, parts: Part[], isFailing: boolean, columns: number): RenderElement {
  const style = (part: Part) => (isFailing ? { color: 'red' } : part.tone === 'plain' ? { dimColor: true } : toneColor(part.tone))

  return (
    <Box width={columns}>
      <Box flexShrink={0} marginRight={2}>
        <Text {...(isFailing ? { color: 'red' } : { dimColor: true })}>khub</Text>
      </Box>
      <Box flexShrink={1}>
        <Text wrap="truncate-end">
          {parts.map(part => (
            <Text {...style(part)}>{part.text}</Text>
          ))}
        </Text>
      </Box>
    </Box>
  )
}
