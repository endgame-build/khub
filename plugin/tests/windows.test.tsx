import { expect, test } from 'claude-code/testing'
import type { On } from 'claude-code'

import { F } from './fixtures'
import { fakeHost, settled } from './helpers'

// A session on Windows: Claude Code names its directory with a drive and backslashes, and
// Git Bash runs the agent's commands.
const ROOT = 'C:\\Work\\me\\ws'
const KHUB_DIR = `${ROOT}\\.khub`
const LAUNCHER = `${ROOT}\\node_modules\\@endgame-build\\khub\\bin\\khub.js`
const START = { cwd: `${ROOT}\\docs`, surface: 'terminal' as const, isInteractive: true }
const BASELINE = ['--version', 'schema --format json', 'check --format json', 'query --format json']
const REFRESH = ['check --format json', 'query --format json']
const BAND = { hasSurvey: false, isWorking: false, maxRows: 4, bodyColumns: 80, scroll: { offset: 0, bodyRows: 1 }, view: {} }

const bash = (stdout: string) => ({ result: { stdout, stderr: '', interrupted: false } })
const band = { plugin: 'khub', surface: 'terminal' as const, component: 'AbovePrompt' as const, props: BAND }

const useRow = (id: string, command: string) => ({
  plugin: 'khub',
  surface: 'terminal' as const,
  component: 'ToolUse' as const,
  requestId: id,
  props: { tool_use_id: id, tool: 'Bash', input: { command }, isRunning: false, isErrored: false, isInterrupted: false },
})

function engine(on: On) {
  on('ui.render', ($, e) => {
    const { Text } = $.ui.resolve(e)

    return <Text>engine drawing</Text>
  })
}

// Answers every Bash call with `stdout`, and keeps the id of the last one.
function answering(on: On, stdout: string) {
  const seen = { id: '' }

  on('tool.call', (_$, e) => {
    seen.id = e.tool_use_id

    return bash(stdout)
  })

  return seen
}

test('a Windows session finds the workspace above its directory and runs the npm launcher with node', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base], { files: [KHUB_DIR, LAUNCHER] })

  await $.session.start(START)

  expect(host.ran).toEqual(BASELINE)
  expect(host.programs).toEqual([['node', LAUNCHER], ['node', LAUNCHER], ['node', LAUNCHER], ['node', LAUNCHER]])
  expect(host.statuses).toEqual([undefined])
})

test('with no npm install a Windows session runs khub from the path', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base], { files: [KHUB_DIR] })

  await $.session.start(START)

  expect(host.ran).toEqual(BASELINE)
  expect(host.programs[0]).toEqual(['khub'])
})

test('when khub on the path will not start, a Windows session reaches npm\'s global khub.cmd through cmd', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base], { files: [KHUB_DIR], missing: ['khub'] })

  await $.session.start(START)

  expect(host.ran).toEqual(BASELINE)
  expect(host.programs[0]).toEqual(['cmd', '/c', 'khub'])
  expect(host.statuses).toEqual([undefined])
})

test('a Windows Edit of an entity file, in any spelling of the root, shows ~1 on the band', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base], { files: [KHUB_DIR] })

  on('tool.call', () => ({ result: {} }))
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({
    tool: 'Edit',
    file_path: 'c:\\work\\me\\ws\\knowledge\\components\\cmp-search.md',
    old_string: 'lifecycle: production',
    new_string: 'lifecycle: deprecated',
  })
  await settled(() => host.ran.length >= 2)

  expect(host.ran).toEqual(REFRESH)
  expect(await (await $.ui.mount(band)).find({ type: 'Text', text: /^~1$/ })).toBeDefined()
})

test('a Windows Write of a schema file reads the schema and the band again', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base], { files: [KHUB_DIR] })

  on('tool.call', () => ({ result: {} }))
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({ tool: 'Write', file_path: `${KHUB_DIR}\\ontology.yaml`, content: 'x' })
  await settled(() => host.ran.length >= 3)

  expect(host.ran).toEqual(['schema --format json', ...REFRESH])
})

test('khub.exe after a Git Bash cd draws a compact row and is followed by a refresh', async ($, on) => {
  const command = 'cd /c/Work/me/ws && khub.exe add adr --title "Use RE2 patterns" --status accepted'
  const host = fakeHost(on, [F.check_base, F.query_base, F.query_added], { files: [KHUB_DIR] })
  const seen = answering(on, F.add_adr.stdout)

  engine(on)
  await $.session.start(START)
  host.ran.length = 0
  await $.tool.call({ tool: 'Bash', command })
  await settled(() => host.ran.length >= 2)

  const row = await $.ui.mount(useRow(seen.id, command))

  expect(host.ran).toEqual(REFRESH)
  expect(await row.find({ type: 'Text', text: /^khub add adr$/ })).toBeDefined()
  expect(await row.find({ type: 'Text', text: /^engine drawing$/ })).toBe(undefined)
  expect(await (await $.ui.mount(band)).find({ type: 'Text', text: /^\+1$/ })).toBeDefined()
})

test('a Windows call aimed at this workspace by any spelling is read, and one aimed elsewhere passes untouched', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.query_base], { files: [KHUB_DIR] })
  const seen = answering(on, F.query_base.stdout)

  engine(on)
  await $.session.start(START)
  host.ran.length = 0

  const here = 'khub.exe -C C:/work/me/ws query --format json'

  await $.tool.call({ tool: 'Bash', command: here })
  expect(await (await $.ui.mount(useRow(seen.id, here))).find({ type: 'Text', text: /^8 entities$/ })).toBeDefined()

  const elsewhere = 'khub.exe -C D:\\\\other query --format json'

  await $.tool.call({ tool: 'Bash', command: elsewhere })
  expect(await (await $.ui.mount(useRow(seen.id, elsewhere))).find({ type: 'Text', text: /^engine drawing$/ })).toBeDefined()
})
