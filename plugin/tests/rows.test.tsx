import { expect, test } from 'claude-code/testing'
import type { EngineInterface, On, RenderElement } from 'claude-code'

import type { El } from '../hooks/el'
import { callRow } from '../hooks/rows'
import type { KhubCall } from '../types'

const ADD = 'khub add adr --title "Use RE2 patterns" --status accepted'
const SURFACES = ['terminal', 'desktop'] as const

const CALL: KhubCall = {
  head: 'khub add adr',
  line: '+ adr/ad-2026-01-15-use-re2-patterns ✓',
  tone: 'ok',
  isRunning: false,
  ms: 38,
  rows: [],
  more: 0,
}

const LIST: KhubCall = {
  head: 'khub query adr',
  line: '12 entities',
  tone: 'plain',
  isRunning: false,
  ms: 38,
  rows: [
    { text: 'adr/ad-2026-01-15-use-postgres', note: 'Use Postgres', flags: ['draft'] },
    { text: 'adr/ad-2026-02-03-event-bus', note: 'Event bus', flags: [] },
    { text: 'adr/ad-2026-03-11-auth', note: '', flags: ['orphan', 'stale'] },
  ],
  more: 9,
}

const USE = { tool: 'Bash', input: { command: ADD }, isRunning: false, isErrored: false, isInterrupted: false }

const row = (id: string, surface: (typeof SURFACES)[number] = 'terminal') => ({
  plugin: 'khub',
  surface,
  component: 'ToolUse' as const,
  requestId: id,
  props: { ...USE, tool_use_id: id },
})

// Draws one view as the bottom of the chain. The shell passes every row on while no
// workspace is detected, so the mounted drawing is the view's own tree.
function draw(on: On, view: (el: El) => RenderElement) {
  on('ui.render', ($: EngineInterface, e) => view($.ui.resolve(e)))
}

test('the head line is a bullet, the bold head, the duration and a json button', async ($, on) => {
  draw(on, el => callRow(el, CALL, () => {}))

  for (const surface of SURFACES) {
    const ui = await $.ui.mount(row('r1', surface))
    const bullet = await ui.find({ type: 'Text', text: /^● $/ })
    const head = await ui.find({ type: 'Text', text: /^khub add adr$/ })
    const button = await ui.find({ type: 'Button', key: 'json' })

    expect(await ui.drawn()).toMatchObject({ type: 'Box', props: { flexDirection: 'column' } })
    expect(bullet?.props.color).toBe('green')
    expect(bullet?.props.dimColor).toBe(false)
    expect(head?.props).toMatchObject({ bold: true, wrap: 'truncate-end' })
    expect((await ui.find({ type: 'Text', text: /^ 38 ms$/ }))?.props.dimColor).toBe(true)
    expect(button?.props).toMatchObject({ label: 'json', plain: true, dimColor: true })
    await ui.unmount()
  }
})

test('a running row dims its bullet, shows no duration and waits with an ellipsis', async ($, on) => {
  draw(on, el => callRow(el, { ...CALL, line: '', tone: 'plain', ms: 0, isRunning: true }, () => {}))

  const ui = await $.ui.mount(row('r3'))
  const bullet = await ui.find({ type: 'Text', text: /^● $/ })

  expect(bullet?.props.dimColor).toBe(true)
  expect(bullet?.props.color).toBe(undefined)
  expect(await ui.find({ type: 'Text', text: /ms$/ })).toBe(undefined)
  expect((await ui.find({ type: 'Text', text: /^ {2}⎿ …$/ }))?.props.dimColor).toBe(true)
})

test('a finished call that printed nothing says so', async ($, on) => {
  draw(on, el => callRow(el, { ...CALL, line: '', tone: 'plain' }, () => {}))

  const ui = await $.ui.mount(row('r4'))

  expect((await ui.find({ type: 'Text', text: /^● $/ }))?.props.dimColor).toBe(false)
  expect((await ui.find({ type: 'Text', text: /^ {2}⎿ \(no output\)$/ }))?.props.dimColor).toBe(true)
})

test('the bullet takes the tone, and a long call is shown in seconds', async ($, on) => {
  const colors = { warn: 'yellow', bad: 'red', plain: undefined } as const

  // One instance per tone: the bottom hook reads the tone from the instance's id.
  on('ui.render', ($: EngineInterface, e) =>
    callRow($.ui.resolve(e), { ...CALL, tone: e.requestId as keyof typeof colors, ms: 1234 }, () => {}),
  )

  for (const [tone, color] of Object.entries(colors)) {
    const ui = await $.ui.mount(row(tone))

    expect((await ui.find({ type: 'Text', text: /^● $/ }))?.props.color).toBe(color)
    expect(await ui.find({ type: 'Text', text: /^ 1\.2 s$/ })).toBeDefined()
    await ui.unmount()
  }
})

test('an ok result line colors its sign and its check, and truncates in the middle part', async ($, on) => {
  draw(on, el => callRow(el, CALL, () => {}))

  for (const surface of SURFACES) {
    const ui = await $.ui.mount(row('r5', surface))
    const body = await ui.find({ type: 'Text', text: /^adr\/ad-2026-01-15-use-re2-patterns$/ })

    expect((await ui.find({ type: 'Text', text: /^ {2}⎿ $/ }))?.props.dimColor).toBe(true)
    expect((await ui.find({ type: 'Text', text: /^\+ $/ }))?.props.color).toBe('green')
    expect(body?.props.wrap).toBe('truncate-end')
    expect(body?.props.color).toBe(undefined)
    expect((await ui.find({ type: 'Text', text: /^ ✓$/ }))?.props.color).toBe('green')
    await ui.unmount()
  }
})

test('a warning or an error colors the whole result line', async ($, on) => {
  const lines = {
    warn: { color: 'yellow', line: 'no hits in 8 entities searched' },
    bad: { color: 'red', line: "✗ lookup_error: No entity 'nope' found" },
    plain: { color: undefined, line: '2 entities' },
  } as const

  on('ui.render', ($: EngineInterface, e) => {
    const tone = e.requestId as keyof typeof lines

    return callRow($.ui.resolve(e), { ...CALL, tone, line: lines[tone].line }, () => {})
  })

  for (const [tone, { color, line }] of Object.entries(lines)) {
    const ui = await $.ui.mount(row(tone))
    const body = await ui.find({ type: 'Text', text: line })

    expect(body?.props.color).toBe(color)
    expect(body?.props.wrap).toBe('truncate-end')
    await ui.unmount()
  }
})

test('list rows sit under the result line, padded to one column, with dim flags and the count left over', async ($, on) => {
  draw(on, el => callRow(el, LIST, () => {}))

  for (const surface of SURFACES) {
    const ui = await $.ui.mount(row('r6', surface))
    const longest = await ui.find({ type: 'Text', text: /^ {4}adr\/ad-2026-01-15-use-postgres$/ })
    const padded = await ui.find({ type: 'Text', text: /^ {4}adr\/ad-2026-02-03-event-bus {3}$/ })
    const note = await ui.find({ type: 'Text', text: /^ {3}Use Postgres$/ })

    expect(longest).toBeDefined()
    expect(padded).toBeDefined()
    expect(note?.props.wrap).toBe('truncate-end')
    expect((await ui.find({ type: 'Text', text: /^ {3}draft$/ }))?.props.dimColor).toBe(true)
    expect((await ui.find({ type: 'Text', text: /^ {3}orphan stale$/ }))?.props.dimColor).toBe(true)
    expect((await ui.find({ type: 'Text', text: /^ {4}… 9 more$/ }))?.props.dimColor).toBe(true)
    await ui.unmount()
  }
})

test('a call with no list draws no list rows', async ($, on) => {
  draw(on, el => callRow(el, CALL, () => {}))

  const ui = await $.ui.mount(row('r7'))

  expect(await ui.find({ type: 'Text', text: /more$/ })).toBe(undefined)
  expect(await ui.find({ type: 'Text', text: /^ {4}\S/ })).toBe(undefined)
})

test('one very long first column does not push every note off the row', async ($, on) => {
  const long = 'requirement/'.padEnd(80, 'x')

  draw(on, el =>
    callRow(el, { ...LIST, rows: [{ text: long, note: 'A', flags: [] }, { text: 'adr/a', note: 'B', flags: [] }], more: 0 }, () => {}),
  )

  const ui = await $.ui.mount(row('r8'))

  // The short id is padded to the cap of 48, not to the long id's 80.
  expect(await ui.find({ type: 'Text', text: new RegExp(`^ {4}adr/a {43}$`) })).toBeDefined()
})
