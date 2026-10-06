import { expect, test } from 'claude-code/testing'
import type { On } from 'claude-code'

import { F } from './fixtures'
import { as, fakeHost, ROOT, settled, START } from './helpers'

const ADD = 'khub add adr --title "Use RE2 patterns" --status accepted'
const SURFACES = ['terminal', 'desktop'] as const
const BAND = { hasSurvey: false, isWorking: false, maxRows: 4, bodyColumns: 80, scroll: { offset: 0, bodyRows: 1 }, view: {} }
const BASELINE = ['--version', 'schema --format json', 'check --format json', 'query --format json']
const REFRESH = ['check --format json', 'query --format json']

const bash = (stdout: string) => ({ result: { stdout, stderr: '', interrupted: false } })

const band = (surface: (typeof SURFACES)[number] = 'terminal') => ({
  plugin: 'khub',
  surface,
  component: 'AbovePrompt' as const,
  props: BAND,
})

const useRow = (id: string, command: string, surface: (typeof SURFACES)[number] = 'terminal') => ({
  plugin: 'khub',
  surface,
  component: 'ToolUse' as const,
  requestId: id,
  props: { tool_use_id: id, tool: 'Bash', input: { command }, isRunning: false, isErrored: false, isInterrupted: false },
})

// The engine's own drawing, for a row or a band the mod leaves alone.
function engine(on: On) {
  on('ui.render', ($, e) => {
    const { Text } = $.ui.resolve(e)

    return <Text>engine drawing</Text>
  })
}

// Answers every Bash call with `stdout`, and keeps the id and the command of the last one.
function answering(on: On, stdout: string) {
  const seen = { id: '', command: '' }

  on('tool.call', (_$, e) => {
    seen.id = e.tool_use_id
    seen.command = String((e as { command?: string }).command ?? '')

    return bash(stdout)
  })

  return seen
}

test('a session in a workspace resolves khub, loads the schema and takes the baseline', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base])

  await $.session.start(START)

  expect(host.ran).toEqual(BASELINE)
  expect(host.statuses).toEqual([undefined])
})

test('outside a workspace the mod runs nothing and draws nothing', async ($, on) => {
  const host = fakeHost(on)

  engine(on)
  on('tool.call', () => bash(F.add_adr.stdout))
  await $.session.start({ ...START, cwd: '/elsewhere' })
  await $.tool.call({ tool: 'Bash', command: ADD })
  await $.turn.start({ text: 'hi', turnId: 't1' })
  await settled(() => host.ran.length > 0, 10)

  expect(host.ran).toEqual([])
  expect(host.statuses).toEqual([])
  expect(await (await $.ui.mount(band())).find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
})

test('the band shows the preset, the count and the gate, dim, with no bracket', async ($, on) => {
  fakeHost(on, [F.check_base, F.query_base])
  engine(on)
  await $.session.start(START)

  for (const surface of SURFACES) {
    const ui = await $.ui.mount(band(surface))

    expect((await ui.find({ type: 'Text', text: /^khub$/ }))?.props.dimColor).toBe(true)
    expect((await ui.find({ type: 'Text', text: /^build-hub 0\.6\.0 · 8 entities$/ }))?.props.dimColor).toBe(true)
    expect((await ui.find({ type: 'Text', text: /^ · check ✓$/ }))?.props.dimColor).toBe(true)
    expect(await ui.find({ type: 'Text', text: /^ \[$/ })).toBe(undefined)
    expect(await ui.find({ type: 'Text', text: /^engine drawing$/ })).toBe(undefined)
    expect(await ui.find({ type: 'Button' })).toBe(undefined)
    await ui.unmount()
  }
})

test('while a survey holds the band, it is the engine\'s', async ($, on) => {
  fakeHost(on, [F.check_base, F.query_base])
  engine(on)
  await $.session.start(START)

  const ui = await $.ui.mount({ ...band(), props: { ...BAND, hasSurvey: true } })

  expect(await ui.find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
})

test('a khub add runs unchanged, draws a compact row and shows +1 on the band', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base, F.query_added])
  const seen = answering(on, F.add_adr.stdout)

  engine(on)
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({ tool: 'Bash', command: ADD })

  // The model reads what khub printed, and nothing beside it.
  expect(seen.command).toBe(ADD)
  expect(ran.result).toEqual({ stdout: F.add_adr.stdout, stderr: '', interrupted: false })
  expect(ran.context).toBe(undefined)

  // The refresh runs after the result went back.
  await settled(() => host.ran.length >= 2)
  expect(host.ran).toEqual(REFRESH)

  for (const surface of SURFACES) {
    const row = await $.ui.mount(useRow(seen.id, ADD, surface))
    const line = await $.ui.mount(band(surface))

    expect(await row.find({ type: 'Text', text: /^khub add adr$/ })).toBeDefined()
    expect(await row.find({ type: 'Text', text: /^adr\/ad-2026-01-15-use-re2-patterns$/ })).toBeDefined()
    expect(await row.find({ type: 'Text', text: /^engine drawing$/ })).toBe(undefined)
    expect((await line.find({ type: 'Text', text: /^\+1$/ }))?.props.color).toBe('green')
    expect(await line.find({ type: 'Text', text: /^build-hub 0\.6\.0 · 9 entities$/ })).toBeDefined()
    await row.unmount()
    await line.unmount()
  }
})

test('a khub query draws its first five rows and the count left over', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base])
  const seen = answering(on, F.query_base.stdout)

  await $.session.start(START)
  host.ran.length = 0
  await $.tool.call({ tool: 'Bash', command: 'khub query --format json' })

  for (const surface of SURFACES) {
    const row = await $.ui.mount(useRow(seen.id, 'khub query --format json', surface))

    expect(await row.find({ type: 'Text', text: /^khub query$/ })).toBeDefined()
    expect(await row.find({ type: 'Text', text: /^8 entities$/ })).toBeDefined()
    expect((await row.findAll({ type: 'Text', text: /^ {4}[a-z0-9-]+\/[a-z0-9-]+ *$/ })).length).toBe(5)
    expect(await row.find({ type: 'Text', text: /^ {4}… 3 more$/ })).toBeDefined()
    await row.unmount()
  }

  // A read is followed by no refresh.
  await settled(() => host.ran.length > 0, 10)
  expect(host.ran).toEqual([])
})

test('the json button hands a row back to the engine', async ($, on) => {
  fakeHost(on, [F.check_base, F.query_base, F.query_added])
  const seen = answering(on, F.add_adr.stdout)

  engine(on)
  await $.session.start(START)

  // Each surface presses the button on a call of its own.
  for (const surface of SURFACES) {
    await $.tool.call({ tool: 'Bash', command: ADD })

    const row = await $.ui.mount(useRow(seen.id, ADD, surface))

    expect(await row.find({ type: 'Text', text: /^khub add adr$/ })).toBeDefined()
    await row.press({ key: 'json' })

    expect(await row.find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
    expect(await row.find({ type: 'Text', text: /^khub add adr$/ })).toBe(undefined)
    await row.unmount()
  }
})

test('a row that is no recorded khub call stays the engine\'s', async ($, on) => {
  fakeHost(on, [F.check_base, F.query_base])
  engine(on)
  await $.session.start(START)

  const row = await $.ui.mount(useRow('unknown', 'ls -la'))

  expect(await row.find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
})

test('an Edit of an entity file shows ~1 on the band', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base])

  on('tool.call', () => ({ result: {} }))
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({
    tool: 'Edit',
    file_path: `${ROOT}/knowledge/components/cmp-search.md`,
    old_string: 'lifecycle: production',
    new_string: 'lifecycle: deprecated',
  })
  await settled(() => host.ran.length >= 2)

  expect(host.ran).toEqual(REFRESH)

  for (const surface of SURFACES) {
    const line = await $.ui.mount(band(surface))

    expect((await line.find({ type: 'Text', text: /^~1$/ }))?.props.dimColor).toBe(true)
    expect(await line.find({ type: 'Text', text: /^\+/ })).toBe(undefined)
    await line.unmount()
  }
})

test('a Write of a schema file reads the schema and the band again', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base])

  on('tool.call', () => ({ result: {} }))
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({ tool: 'Write', file_path: `${ROOT}/.khub/ontology.yaml`, content: 'x' })
  await settled(() => host.ran.length >= 3)

  expect(host.ran).toEqual(['schema --format json', ...REFRESH])
  expect(await (await $.ui.mount(band())).find({ type: 'Text', text: /^~/ })).toBe(undefined)
})

test('a Write khub does not read, a Write outside the workspace and an Edit that failed change nothing', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base])
  let isFailing = false

  on('tool.call', () => (isFailing ? { isError: true as const, result: 'no match', text: 'no match' } : { result: {} }))
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({ tool: 'Write', file_path: `${ROOT}/cmd/main.go`, content: 'x' })
  await $.tool.call({ tool: 'Write', file_path: '/elsewhere/notes.md', content: 'x' })
  isFailing = true
  await $.tool.call({
    tool: 'Edit',
    file_path: `${ROOT}/knowledge/components/cmp-search.md`,
    old_string: 'a',
    new_string: 'b',
  })
  await settled(() => host.ran.length > 0, 10)

  expect(host.ran).toEqual([])
  expect(await (await $.ui.mount(band())).find({ type: 'Text', text: /^~/ })).toBe(undefined)
})

test('a khub edit and a link through Bash count as edits, once per entity', async ($, on) => {
  fakeHost(on, [F.check_base, F.query_base])
  let stdout = F.edit_component.stdout

  on('tool.call', () => bash(stdout))
  await $.session.start(START)

  await $.tool.call({ tool: 'Bash', command: 'khub edit cmp-search lifecycle deprecated' })
  await $.tool.call({ tool: 'Bash', command: 'khub edit cmp-search tier tier-2' })
  stdout = F.link_adr_again.stdout
  await $.tool.call({ tool: 'Bash', command: 'khub link ad-2026-01-15-use-re2-patterns affects cmp-search' })

  expect(await (await $.ui.mount(band())).find({ type: 'Text', text: /^~1$/ })).toBeDefined()
})

test('a compound command keeps the engine row and is followed by a refresh when it writes', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base, F.query_added])
  const seen = answering(on, F.add_adr.stdout)

  engine(on)
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({ tool: 'Bash', command: `${ADD} | tee out.json` })

  expect(await (await $.ui.mount(useRow(seen.id, `${ADD} | tee out.json`))).find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
  await settled(() => host.ran.length >= 2)
  expect(host.ran).toEqual(REFRESH)

  host.ran.length = 0
  await $.tool.call({ tool: 'Bash', command: 'khub query --format json | head -5' })
  await settled(() => host.ran.length > 0, 10)

  expect(host.ran).toEqual([])
})

test('a call aimed at another workspace passes untouched', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base])
  const seen = answering(on, F.add_adr.stdout)

  engine(on)
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({ tool: 'Bash', command: 'khub -C /elsewhere add adr --title x' })

  expect(host.ran).toEqual([])
  expect(await (await $.ui.mount(useRow(seen.id, ADD))).find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
})

test('output the engine moved to disk and cut inline keeps the engine row', async ($, on) => {
  let id = ''

  fakeHost(on, [F.check_base, F.query_base])
  engine(on)
  on('tool.call', (_$, e) => {
    id = e.tool_use_id

    return { result: { stdout: F.query_base.stdout.slice(0, 200), stderr: '', interrupted: false, persistedOutputPath: '/tmp/out.txt' } }
  })
  await $.session.start(START)
  await $.tool.call({ tool: 'Bash', command: 'khub query --format json' })

  expect(await (await $.ui.mount(useRow(id, 'khub query --format json'))).find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
})

test('a refused call shows khub\'s refusal on its row', async ($, on) => {
  let id = ''

  fakeHost(on, [F.check_base, F.query_base])
  on('tool.call', (_$, e) => {
    id = e.tool_use_id

    return { isError: true as const, result: F.add_refused.stdout, text: F.add_refused.stdout }
  })
  await $.session.start(START)

  const ran = await $.tool.call({ tool: 'Bash', command: 'khub add requirement --title Other --realized_in cmp-serach' })

  expect(ran.isError).toBe(true)

  for (const surface of SURFACES) {
    const row = await $.ui.mount(useRow(id, 'khub add requirement', surface))

    expect((await row.find({ type: 'Text', text: /^✗ / }))?.props.color).toBe('red')
    await row.unmount()
  }
})

test('a failing gate turns the band red and counts the errors', async ($, on) => {
  fakeHost(on, [F.check_dangling, F.query_dangling])
  await $.session.start(START)

  for (const surface of SURFACES) {
    const line = await $.ui.mount(band(surface))

    expect((await line.find({ type: 'Text', text: /^khub$/ }))?.props.color).toBe('red')
    expect((await line.find({ type: 'Text', text: /^build-hub 0\.6\.0 · 10 entities$/ }))?.props.color).toBe('red')
    expect((await line.find({ type: 'Text', text: /^ · check ✗ 1 error$/ }))?.props.color).toBe('red')
    await line.unmount()
  }
})

test('a cut document keeps what the band shows', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base, F.query_added])

  on('tool.call', () => bash(F.add_adr.stdout))
  await $.session.start(START)
  host.ran.length = 0
  host.isCut = true
  await $.tool.call({ tool: 'Bash', command: ADD })
  await settled(() => host.ran.length >= 2)

  const line = await $.ui.mount(band())

  expect(await line.find({ type: 'Text', text: /^build-hub 0\.6\.0 · 8 entities$/ })).toBeDefined()
  expect(await line.find({ type: 'Text', text: /^\+1$/ })).toBe(undefined)
})

test('a turn start reads the schema and the band again in the background', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.check_dangling, F.query_base, F.query_dangling])

  await $.session.start(START)
  host.ran.length = 0

  await $.turn.start({ text: 'what changed?', turnId: 't2' })
  await settled(() => host.ran.length >= 3)

  expect(host.ran).toEqual(['schema --format json', ...REFRESH])
  expect(await (await $.ui.mount(band())).find({ type: 'Text', text: /^\+2$/ })).toBeDefined()
})

test('an older khub is named on the status line and every hook passes', async ($, on) => {
  const host = fakeHost(on, [], { version: 'khub 0.26.0' })

  engine(on)
  on('tool.call', () => bash(F.add_adr.stdout))
  await $.session.start(START)
  await $.tool.call({ tool: 'Bash', command: ADD })

  expect(host.statuses).toEqual(['needs khub 0.27.0 or newer'])
  expect(host.ran).toEqual(['--version'])
  expect(await (await $.ui.mount(band())).find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
})

test('a khub updated during the session is picked up at the next turn start', async ($, on) => {
  const options: { version?: string } = { version: 'khub 0.26.0' }
  const host = fakeHost(on, [F.check_base, F.query_base], options)

  await $.session.start(START)
  delete options.version
  host.ran.length = 0

  await $.turn.start({ text: 'hi', turnId: 't1' })
  await settled(() => host.ran.length >= 4)

  expect(host.ran).toEqual(BASELINE)
  expect(host.statuses).toEqual(['needs khub 0.27.0 or newer', undefined])
  expect(await (await $.ui.mount(band())).find({ type: 'Text', text: /^build-hub 0\.6\.0 · 8 entities$/ })).toBeDefined()
})

test('a machine with no khub says so on the status line', async ($, on) => {
  const host = fakeHost(on, [], { hasKhub: false })

  await $.session.start(START)

  expect(host.statuses).toEqual(['not installed'])
})

test('a schema that does not resolve is said on the status line', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base], { schema: as(F.get_missing, ['schema', '--format', 'json']) })

  await $.session.start(START)

  expect(host.statuses.at(-1)).toBe("schema: No entity 'nope' found")
})
