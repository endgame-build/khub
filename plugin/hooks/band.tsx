import type { RenderElement } from 'claude-code'

import type { KhubNotice } from '../types'
import { toneColor } from './el'
import type { El } from './el'

export type BandActions = {
  review: () => void
  check: () => void
  hide: () => void

  // For a `schema` notice.
  rewire: () => void
  snapshot: () => void
}

// The one line above the prompt, cut to `columns`. The parts are the one place that truncates.
export function bandLine(
  { Box, Text, Button }: El,
  notice: KhubNotice,
  columns: number,
  actions: BandActions,
): RenderElement {
  const parts = notice.parts.flatMap((part, i) => [
    ...(i > 0 ? [' · '] : []),
    <Text {...toneColor(part.tone)}>{part.text}</Text>,
  ])

  const has = (button: KhubNotice['buttons'][number]) => notice.buttons.includes(button)

  return (
    <Box width={columns}>
      <Box flexShrink={0} marginRight={2}>
        <Text bold>khub</Text>
      </Box>
      <Box flexShrink={1}>
        <Text wrap="truncate-end">{parts}</Text>
      </Box>
      <Box flexShrink={0} marginLeft={2} columnGap={1}>
        {has('review') && <Button key="review" label="Review" hotkey="r" variant="primary" onPress={() => actions.review()} />}
        {has('check') && <Button key="check" label="Check" hotkey="c" onPress={() => actions.check()} />}
        {has('rewire') && <Button key="rewire" label="Rewire" variant="primary" onPress={() => actions.rewire()} />}
        {has('snapshot') && <Button key="snapshot" label="Snapshot" onPress={() => actions.snapshot()} />}
        <Button key="hide" label="×" plain dimColor role="dismiss" onPress={() => actions.hide()} />
      </Box>
    </Box>
  )
}
