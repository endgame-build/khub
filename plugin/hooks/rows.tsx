// The transcript rows of a khub call, a head row and a result row.

import type { RenderElement } from 'claude-code'

import type { KhubCall, KhubTone, KhubValidation } from '../types'
import { toneColor as tint } from './el'
import type { El } from './el'

const duration = (ms: number) => (ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`)

// A result line as `[mark, text, check]`. An ok line is colored on its leading sign and
// its trailing check alone; a warning or an error is colored whole.
function partsOf(line: string, tone: KhubTone): [string, string, string] {
  if (tone !== 'ok') return ['', line, '']

  const sign = /^[+~→⇢−] /.exec(line)?.[0] ?? ''
  const check = line.endsWith(' ✓') ? ' ✓' : ''

  return [sign, line.slice(sign.length, line.length - check.length), check]
}

// `  ⎿ ` and one line under a row. The line is the one part that truncates.
function under({ Box, Text }: El, line: string, tone: KhubTone): RenderElement {
  const [sign, text, check] = partsOf(line, tone)
  const whole = tone === 'ok' ? {} : tint(tone)

  return (
    <Box>
      <Box flexShrink={0}>
        <Text dimColor>{'  ⎿ '}</Text>
        {sign !== '' && <Text {...tint(tone)}>{sign}</Text>}
      </Box>
      <Text {...whole} wrap="truncate-end">
        {text}
      </Text>
      {check !== '' && (
        <Box flexShrink={0}>
          <Text {...tint(tone)}>{check}</Text>
        </Box>
      )}
    </Box>
  )
}

// The head of a khub call's row. `onRaw` flips the row to the engine's own drawing.
export function toolUseRow({ Box, Text, Button }: El, call: KhubCall, onRaw: () => void): RenderElement {
  return (
    <Box>
      <Box flexShrink={0}>
        <Text {...tint(call.tone)} dimColor={call.isRunning}>
          {'● '}
        </Text>
      </Box>
      <Text bold wrap="truncate-end">
        {call.head}
      </Text>
      <Box flexShrink={0}>
        {call.ms > 0 && <Text dimColor>{` ${duration(call.ms)}`}</Text>}
        <Text> </Text>
        <Button key="json" label="json" plain dimColor onPress={onRaw} />
      </Box>
    </Box>
  )
}

// The result line under it.
export function toolResultRow(el: El, call: KhubCall): RenderElement {
  const { Box, Text } = el

  if (call.line !== '') return under(el, call.line, call.tone)

  return (
    <Box>
      <Text dimColor>{call.isRunning ? '  ⎿ …' : '  ⎿ (no output)'}</Text>
    </Box>
  )
}

// The engine's Edit or Write result with the validate line beneath.
export function withValidation(el: El, base: RenderElement, validation: KhubValidation): RenderElement {
  const { Box } = el

  return (
    <Box flexDirection="column">
      {base}
      {under(el, validation.line, validation.tone)}
    </Box>
  )
}
