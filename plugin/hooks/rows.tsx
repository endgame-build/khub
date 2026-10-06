// This file draws the transcript row of a command's khub calls. Each call gets a head line,
// a result line, and the first rows of a list.

import type { RenderElement } from 'claude-code'

import type { KhubCall, KhubRow, KhubTone } from '../types'
import { toneColor as tint } from './el'
import type { El } from './el'

// Caps the padding of a list's first column.
const TEXT_MAX = 48

const duration = (ms: number) => (ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`)

// Splits a result line into `[mark, text, check]`. An ok line is colored on its leading sign and
// its trailing check alone; a warning or an error is colored whole.
function partsOf(line: string, tone: KhubTone): [string, string, string] {
  if (tone !== 'ok') return ['', line, '']

  const sign = /^[+~→⇢−] /.exec(line)?.[0] ?? ''
  const check = line.endsWith(' ✓') ? ' ✓' : ''

  return [sign, line.slice(sign.length, line.length - check.length), check]
}

// Draws the head line. `onRaw` flips the row to the engine's own drawing, and only the
// first call of a command carries its button.
function headLine({ Box, Text, Button }: El, call: KhubCall, onRaw: (() => void) | null): RenderElement {
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
        {onRaw !== null && <Text> </Text>}
        {onRaw !== null && <Button key="json" label="json" plain dimColor onPress={onRaw} />}
      </Box>
    </Box>
  )
}

// Draws `  ⎿ ` and what the call answered. The text is the one part that truncates.
function resultLine({ Box, Text }: El, call: KhubCall): RenderElement {
  if (call.line === '') {
    return (
      <Box>
        <Text dimColor>{call.isRunning ? '  ⎿ …' : '  ⎿ (no output)'}</Text>
      </Box>
    )
  }

  const [sign, text, check] = partsOf(call.line, call.tone)
  const whole = call.tone === 'ok' ? {} : tint(call.tone)

  return (
    <Box>
      <Box flexShrink={0}>
        <Text dimColor>{'  ⎿ '}</Text>
        {sign !== '' && <Text {...tint(call.tone)}>{sign}</Text>}
      </Box>
      <Text {...whole} wrap="truncate-end">
        {text}
      </Text>
      {check !== '' && (
        <Box flexShrink={0}>
          <Text {...tint(call.tone)}>{check}</Text>
        </Box>
      )}
    </Box>
  )
}

// Draws one list row under the result line: the padded first column, the note, the dim flags.
function listRow({ Box, Text }: El, row: KhubRow, width: number): RenderElement {
  return (
    <Box>
      <Box flexShrink={0}>
        <Text>{`    ${row.text.padEnd(width)}`}</Text>
      </Box>
      <Text wrap="truncate-end">{row.note === '' ? '' : `   ${row.note}`}</Text>
      {row.flags.length > 0 && (
        <Box flexShrink={0}>
          <Text dimColor>{`   ${row.flags.join(' ')}`}</Text>
        </Box>
      )}
    </Box>
  )
}

// Draws the lines of one khub call. The list's first column is padded to its widest id.
function callLines(el: El, call: KhubCall, onRaw: (() => void) | null): RenderElement[] {
  const { Box, Text } = el
  const width = Math.min(TEXT_MAX, Math.max(0, ...call.rows.map(row => row.text.length)))

  return [
    headLine(el, call, onRaw),
    resultLine(el, call),
    ...call.rows.map(row => listRow(el, row, width)),
    ...(call.more > 0
      ? [
          <Box>
            <Text dimColor>{`    … ${call.more} more`}</Text>
          </Box>,
        ]
      : []),
  ]
}

// Draws the whole row of a command's khub calls, one after another. The engine may draw a
// call's result inside its ToolUse row, so this one tree holds the heads, the results and
// the lists.
export function callRow(el: El, calls: KhubCall[], onRaw: () => void): RenderElement {
  const { Box } = el

  return <Box flexDirection="column">{calls.flatMap((call, i) => callLines(el, call, i === 0 ? onRaw : null))}</Box>
}

// Draws a result block with nothing in it, for a call whose row already shows the result.
export function emptyBlock({ Box }: El): RenderElement {
  return <Box />
}
