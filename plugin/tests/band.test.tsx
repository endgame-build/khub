import { expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'
import type { On } from 'claude-code'

import type { KhubNotice } from '../types'
import { bandLine } from '../hooks/band'
import { toneColor } from '../hooks/el'
import type { BandActions } from '../hooks/band'
import { F } from './fixtures'
import { fakeHost, START } from './helpers'

const BAND = { hasSurvey: false, isWorking: false, maxRows: 4, bodyColumns: 80, scroll: { offset: 0, bodyRows: 1 }, view: {} }

const FINDINGS: KhubNotice = {
  kind: 'findings',
  buttons: ['review', 'check'],
  parts: [
    { text: '+1 adr', tone: 'plain' },
    { text: '1 edge', tone: 'ok' },
    { text: '1 new error', tone: 'bad' },
    { text: '1 gap', tone: 'warn' },
  ],
}

// Draws the band from the test's own hook, beneath the mod, so the surface validates
// the tree and a press reaches the actions. Returns what the presses ran.
function stage(on: On, notice: KhubNotice, columns: number) {
  const ran: string[] = []
  const actions: BandActions = {
    review: () => void ran.push('review'),
    check: () => void ran.push('check'),
    hide: () => void ran.push('hide'),
    rewire: () => void ran.push('rewire'),
    snapshot: () => void ran.push('snapshot'),
  }

  fakeHost(on)
  on('ui.render', ($, e) => bandLine($.ui.resolve(e), notice, columns, actions))

  return ran
}

const mount = ($: Engine, surface: 'terminal' | 'desktop') =>
  $.ui.mount({ plugin: 'test', surface, component: 'AbovePrompt', props: BAND })

test('a tone is a color, and plain is none', () => {
  expect(toneColor('ok')).toEqual({ color: 'green' })
  expect(toneColor('warn')).toEqual({ color: 'yellow' })
  expect(toneColor('bad')).toEqual({ color: 'red' })
  expect(toneColor('plain')).toEqual({})
})

test('the band is one row: khub, the parts in their tones, then the buttons', async ($, on) => {
  stage(on, FINDINGS, 80)

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await mount($, surface)
    const row = (await ui.drawn()) as { type: string; props?: unknown }

    expect(row.type).toBe('Box')
    expect(row.props).toEqual({ width: 80 })
    expect((await ui.find({ type: 'Text', text: /^khub$/ }))?.props).toEqual({ bold: true })

    expect((await ui.find({ type: 'Text', text: /^\+1 adr$/ }))?.props).toEqual({})
    expect((await ui.find({ type: 'Text', text: /^1 edge$/ }))?.props).toEqual({ color: 'green' })
    expect((await ui.find({ type: 'Text', text: /^1 new error$/ }))?.props).toEqual({ color: 'red' })
    expect((await ui.find({ type: 'Text', text: /^1 gap$/ }))?.props).toEqual({ color: 'yellow' })

    expect((await ui.findAll({ type: 'Button' })).map(button => button.props)).toEqual([
      { key: 'review', label: 'Review', hotkey: 'r', variant: 'primary' },
      { key: 'check', label: 'Check', hotkey: 'c' },
      { key: 'hide', label: '×', plain: true, dimColor: true, role: 'dismiss' },
    ])
    await ui.unmount()
  }
})

test('the parts are what gives way when the row is narrow', async ($, on) => {
  stage(on, FINDINGS, 40)

  type Row = { props: Record<string, unknown>; children: Row[] }

  const row = (await (await mount($, 'terminal')).drawn()) as unknown as Row
  const [name, parts, buttons] = row.children

  expect(row.props).toEqual({ width: 40 })
  expect(name?.props.flexShrink).toBe(0)
  expect(parts?.props).toEqual({ flexShrink: 1 })
  expect(parts?.children[0]?.props).toEqual({ wrap: 'truncate-end' })
  expect(buttons?.props.flexShrink).toBe(0)
})

test('the parts are joined by a middle dot', async ($, on) => {
  stage(on, FINDINGS, 80)

  const ui = await mount($, 'terminal')
  const parts = await ui.find({ type: 'Text', text: / · / })

  expect(parts?.children.filter(child => typeof child === 'string')).toEqual([' · ', ' · ', ' · '])
})

test('each button runs its action', async ($, on) => {
  const ran = stage(on, FINDINGS, 80)

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await mount($, surface)

    await ui.press({ key: 'review' })
    await ui.press({ key: 'check' })
    await ui.press({ key: 'hide' })
    await ui.unmount()
  }

  expect(ran).toEqual(['review', 'check', 'hide', 'review', 'check', 'hide'])
})

test('a schema notice offers to rewire and to snapshot', async ($, on) => {
  const ran = stage(on, { kind: 'schema', parts: [{ text: 'schema · 2 changes', tone: 'warn' }], buttons: ['rewire', 'snapshot'] }, 80)
  const ui = await mount($, 'terminal')

  expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual(['rewire', 'snapshot', 'hide'])

  await ui.press({ key: 'rewire' })
  await ui.press({ key: 'snapshot' })
  expect(ran).toEqual(['rewire', 'snapshot'])
})

test('a doctor notice offers nothing but to hide it', async ($, on) => {
  const ran = stage(on, { kind: 'doctor', parts: [{ text: 'preset build-hub 0.6.0 → 0.7.0', tone: 'plain' }], buttons: [] }, 80)
  const ui = await mount($, 'desktop')

  expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual(['hide'])

  await ui.press({ key: 'hide' })
  expect(ran).toEqual(['hide'])
})

test('with nothing to say the band shows the workspace\'s state as one dim line', async ($, on) => {
  fakeHost(on, [F.check_base, F.status_base])
  on('ui.render', ($, e) => {
    const { Text } = $.ui.resolve(e)

    return <Text>engine band</Text>
  })
  await $.session.start(START)

  const ui = await $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'AbovePrompt', props: BAND })

  expect((await ui.find({ type: 'Text', text: /^✓ · 8 entities$/ }))?.props.dimColor).toBe(true)
  expect(await ui.find({ type: 'Text', text: /engine band/ })).toBe(undefined)
  expect(await ui.find({ type: 'Button' })).toBe(undefined)
})

test('before the first check, and while a survey shows, the band is the engine\'s', async ($, on) => {
  fakeHost(on, [F.check_base, F.status_base])
  on('ui.render', ($, e) => {
    const { Text } = $.ui.resolve(e)

    return <Text>engine band</Text>
  })
  await $.session.start(START)

  const ui = await $.ui.mount({
    plugin: 'khub',
    surface: 'terminal',
    component: 'AbovePrompt',
    props: { ...BAND, hasSurvey: true },
  })

  expect(await ui.find({ type: 'Text', text: /engine band/ })).toBeDefined()
})
