import { expect, test } from 'claude-code/testing'
import type { EngineInterface, On, RenderElement } from 'claude-code'

import type { El } from '../hooks/el'
import { toolResultRow, toolUseRow, withValidation } from '../hooks/rows'
import type { KhubCall } from '../types'
import { F } from './fixtures'
import { fakeHost, START } from './helpers'

const ADD = 'khub add adr --title "Use RE2 patterns" --status accepted'
const SURFACES = ['terminal', 'desktop'] as const

const CALL: KhubCall = {
  verb: 'add',
  kind: 'simple',
  head: 'khub add adr',
  line: '+ adr/ad-2026-01-15-use-re2-patterns ✓',
  tone: 'ok',
  isRunning: false,
  ms: 38,
  outcome: 'ok',
  code: null,
}

const USE = { tool: 'Bash', input: { command: ADD }, isRunning: false, isErrored: false, isInterrupted: false }
const RESULT = { tool: 'Bash', output: { stdout: '', stderr: '', interrupted: false }, isErrored: false }

// Draws one view as the bottom of the chain. The shell passes every row on while no
// workspace is detected, so the mounted drawing is the view's own tree.
function draw(on: On, view: (el: El) => RenderElement) {
  on('ui.render', ($: EngineInterface, e) => view($.ui.resolve(e)))
}

test('the head row is a bullet, the bold head, the duration and a json button', async ($, on) => {
  draw(on, el => toolUseRow(el, CALL, () => {}))

  for (const surface of SURFACES) {
    const ui = await $.ui.mount({ plugin: 'khub', surface, component: 'ToolUse', requestId: 'r1', props: { ...USE, tool_use_id: 'r1' } })
    const bullet = await ui.find({ type: 'Text', text: /^● $/ })
    const head = await ui.find({ type: 'Text', text: /^khub add adr$/ })
    const button = await ui.find({ type: 'Button', key: 'json' })

    expect(bullet?.props.color).toBe('green')
    expect(bullet?.props.dimColor).toBe(false)
    expect(head?.props).toMatchObject({ bold: true, wrap: 'truncate-end' })
    expect((await ui.find({ type: 'Text', text: /^ 38 ms$/ }))?.props.dimColor).toBe(true)
    expect(button?.props).toMatchObject({ label: 'json', plain: true, dimColor: true })
    expect((await ui.findAll({ type: 'Text' })).filter(text => text.props.wrap !== undefined).length).toBe(1)
    await ui.unmount()
  }
})

test('a running row dims its bullet and shows no duration', async ($, on) => {
  draw(on, el => toolUseRow(el, { ...CALL, line: '', tone: 'plain', ms: 0, isRunning: true }, () => {}))

  const ui = await $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'ToolUse', requestId: 'r2', props: { ...USE, tool_use_id: 'r2' } })
  const bullet = await ui.find({ type: 'Text', text: /^● $/ })

  expect(bullet?.props.dimColor).toBe(true)
  expect(bullet?.props.color).toBe(undefined)
  expect(await ui.find({ type: 'Text', text: /ms$/ })).toBe(undefined)
})

test('the bullet takes the tone, and a long call is shown in seconds', async ($, on) => {
  const colors = { warn: 'yellow', bad: 'red', plain: undefined } as const

  // One instance per tone: the bottom hook reads the tone from the instance's id.
  on('ui.render', ($: EngineInterface, e) =>
    toolUseRow($.ui.resolve(e), { ...CALL, tone: e.requestId as keyof typeof colors, ms: 1234 }, () => {}),
  )

  for (const [tone, color] of Object.entries(colors)) {
    const ui = await $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'ToolUse', requestId: tone, props: { ...USE, tool_use_id: tone } })

    expect((await ui.find({ type: 'Text', text: /^● $/ }))?.props.color).toBe(color)
    expect(await ui.find({ type: 'Text', text: /^ 1\.2 s$/ })).toBeDefined()
    await ui.unmount()
  }
})

test('an ok result line colors its sign and its check, and truncates in the middle part', async ($, on) => {
  draw(on, el => toolResultRow(el, CALL))

  for (const surface of SURFACES) {
    const ui = await $.ui.mount({ plugin: 'khub', surface, component: 'ToolResult', requestId: 'r3', props: { ...RESULT, tool_use_id: 'r3' } })
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
    warn: { color: 'yellow', line: '+ adr/ad-2026-01-15-use-re2-patterns · 1 gap' },
    bad: { color: 'red', line: "✗ lookup_error: No entity 'nope' found" },
    plain: { color: undefined, line: '2 entities' },
  } as const

  on('ui.render', ($: EngineInterface, e) => {
    const tone = e.requestId as keyof typeof lines

    return toolResultRow($.ui.resolve(e), { ...CALL, tone, line: lines[tone].line })
  })

  for (const [tone, { color, line }] of Object.entries(lines)) {
    const ui = await $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'ToolResult', requestId: tone, props: { ...RESULT, tool_use_id: tone } })
    const body = await ui.find({ type: 'Text', text: line })

    expect(body?.props.color).toBe(color)
    expect(body?.props.wrap).toBe('truncate-end')
    await ui.unmount()
  }
})

test('a result with no line yet draws a dim ellipsis', async ($, on) => {
  draw(on, el => toolResultRow(el, { ...CALL, line: '', tone: 'plain', isRunning: true }))

  const ui = await $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'ToolResult', requestId: 'r4', props: { ...RESULT, tool_use_id: 'r4' } })

  expect((await ui.find({ type: 'Text', text: /^ {2}⎿ …$/ }))?.props.dimColor).toBe(true)
})

test('a finished call that printed nothing says so, and its bullet is not dim', async ($, on) => {
  on('ui.render', ($: EngineInterface, e) => {
    const el = $.ui.resolve(e)
    const quiet = { ...CALL, line: '', tone: 'plain' as const }

    return e.component === 'ToolUse' ? toolUseRow(el, quiet, () => {}) : toolResultRow(el, quiet)
  })

  const head = await $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'ToolUse', requestId: 'r6', props: { ...USE, tool_use_id: 'r6' } })
  const result = await $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'ToolResult', requestId: 'r6', props: { ...RESULT, tool_use_id: 'r6' } })

  expect((await head.find({ type: 'Text', text: /^● $/ }))?.props.dimColor).toBe(false)
  expect((await result.find({ type: 'Text', text: /^ {2}⎿ \(no output\)$/ }))?.props.dimColor).toBe(true)
  expect(await result.find({ type: 'Text', text: /…/ })).toBe(undefined)
})

test('the validate line sits under the engine drawing', async ($, on) => {
  on('ui.render', ($: EngineInterface, e) => {
    const el = $.ui.resolve(e)

    return withValidation(el, <el.Text>engine diff</el.Text>, {
      id: 'adr/ad-2026-01-15-use-re2-patterns',
      line: "validate · 1 gap · '## Decision' 9 words of prose, at least 15 asked for",
      tone: 'warn',
    })
  })

  for (const surface of SURFACES) {
    const ui = await $.ui.mount({ plugin: 'khub', surface, component: 'ToolResult', requestId: 'r5', props: { ...RESULT, tool: 'Edit', output: {}, tool_use_id: 'r5' } })
    const drawn = await ui.drawn()
    const line = await ui.find({ type: 'Text', text: /^validate · 1 gap/ })

    expect(drawn).toMatchObject({ type: 'Box', props: { flexDirection: 'column' } })
    expect(await ui.find({ type: 'Text', text: /^engine diff$/ })).toBeDefined()
    expect(line?.props).toMatchObject({ color: 'yellow', wrap: 'truncate-end' })
    await ui.unmount()
  }
})

test('through the shell, a khub call draws compact rows and its json button shows the engine row', async ($, on) => {
  let id = ''

  fakeHost(on, [F.check_base, F.status_base, F.validate_adr])
  on('tool.call', (_$, e) => {
    id = e.tool_use_id

    return { result: { stdout: F.add_adr.stdout, stderr: '', interrupted: false } }
  })
  on('ui.render', ($: EngineInterface, e) => {
    const { Text } = $.ui.resolve(e)

    return <Text>engine row</Text>
  })

  await $.session.start(START)

  for (const surface of SURFACES) {
    await $.tool.call({ tool: 'Bash', command: ADD })

    const head = await $.ui.mount({ plugin: 'khub', surface, component: 'ToolUse', requestId: id, props: { ...USE, tool_use_id: id } })
    const result = await $.ui.mount({ plugin: 'khub', surface, component: 'ToolResult', requestId: id, props: { ...RESULT, tool_use_id: id } })

    expect(await head.find({ type: 'Text', text: /^khub add adr$/ })).toBeDefined()
    expect(await result.find({ type: 'Text', text: /adr\/ad-2026-01-15-use-re2-patterns/ })).toBeDefined()
    expect(await result.find({ type: 'Text', text: /engine row/ })).toBe(undefined)

    await head.press({ key: 'json' })
    await result.redraw()

    expect(await head.find({ type: 'Text', text: /engine row/ })).toBeDefined()
    expect(await result.find({ type: 'Text', text: /engine row/ })).toBeDefined()
    await head.unmount()
    await result.unmount()
  }
})

test('through the shell, a row that is not a recorded khub call stays the engine row', async ($, on) => {
  fakeHost(on, [F.check_base, F.status_base])
  on('ui.render', ($: EngineInterface, e) => {
    const { Text } = $.ui.resolve(e)

    return <Text>engine row</Text>
  })

  await $.session.start(START)

  const ui = await $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'ToolUse', requestId: 'unknown', props: { ...USE, tool_use_id: 'unknown' } })

  expect(await ui.find({ type: 'Text', text: /engine row/ })).toBeDefined()
})
