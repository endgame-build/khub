import { expect, test } from 'claude-code/testing'
import type { On } from 'claude-code'

import { F } from './fixtures'
import { as, fakeHost, ROOT, settled, slash, START } from './helpers'

const ADR = 'adr/ad-2026-01-15-use-re2-patterns'
const REQ = 'requirement/req-search-answers-within-300-ms'
const ADD_ADR = 'khub add adr --title "Use RE2 patterns" --status accepted'
const DANGLING = `${REQ} › dangling: predicate realized_in, target cmp-serach`
const UNRESOLVED = "realized_in: no component 'cmp-serach' to satisfy relation 'realized_in'"
const AFTER_ADD = [`validate ${ADR} --format json`, 'check --format json', 'status --format json']

// What the baseline, then a relation typed by hand to a missing slug, answer.
const BROKEN = [F.check_base, F.check_dangling, F.status_base, F.status_dangling]
const ADDED = { result: { stdout: F.add_adr.stdout, stderr: '', interrupted: false } }

// The engine's own drawing, for a row the mod leaves alone or wraps.
function engineRows(on: On) {
  on('ui.render', ($, e) => {
    const { Text } = $.ui.resolve(e)

    return <Text>engine row</Text>
  })
}

const useRow = (id: string, command: string) => ({
  plugin: 'khub',
  surface: 'terminal' as const,
  component: 'ToolUse' as const,
  requestId: id,
  props: { tool_use_id: id, tool: 'Bash', input: { command }, isRunning: false, isErrored: false, isInterrupted: false },
})

// A slash command as the user types it.
// Stands above the mod and, at a turn's end, toasts what the mod's session state holds.
const peek = {
  name: 'peek',
  tier: 'prepend' as const,
  register(on: On) {
    on('turn.complete', async ($, e, next) => {
      const stats = await $.state.get({ plugin: 'khub', key: 'stats' } as const)
      const ledger = await $.state.get({ plugin: 'khub', key: 'ledger' } as const)

      $.ui.toast(`bypass ${stats.value?.bypass ?? 0}`)
      $.ui.toast(`ledger ${(ledger.value ?? []).map(entry => `${entry.op} ${entry.id} by ${entry.by}`).join(', ')}`)

      return next(e)
    })
  },
}

const TURN_END = { answer: '', durationMs: 1, isAborted: false, turnId: 't1', reason: 'answer' as const }

const resultRow = (id: string, tool: string) => ({
  plugin: 'khub',
  surface: 'terminal' as const,
  component: 'ToolResult' as const,
  requestId: id,
  props: { tool_use_id: id, tool, output: {}, isErrored: false },
})

// Stands above the mod and reports what it answered to `classic.PreToolUse`.
const witness = {
  name: 'witness',
  tier: 'prepend' as const,
  register(on: On) {
    on('classic.PreToolUse', async ($, e, next) => {
      const answered = await next(e)

      $.ui.toast(`pre-tool-use ask: ${answered.ask ?? 'none'}`)

      return answered
    })
  },
}

test('a session in a workspace resolves khub, loads the schema and takes the baseline', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  await $.session.start(START)

  expect(host.ran).toEqual(['--version', 'schema --format json', 'check --format json', 'status --format json'])
  expect(host.statuses.length).toBe(1)
})

test('outside a workspace the mod runs nothing', async ($, on) => {
  const host = fakeHost(on)

  await $.session.start({ ...START, cwd: '/elsewhere' })

  expect(host.ran).toEqual([])
  expect(host.statuses).toEqual([])
})

test('a khub write through Bash is validated and checked', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, F.validate_adr])

  on('tool.call', () => ({ result: { stdout: F.add_adr.stdout, stderr: '', interrupted: false } }))
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({ tool: 'Bash', command: 'khub add adr --title "Use RE2 patterns" --status accepted' })

  expect(host.ran).toEqual([
    'validate adr/ad-2026-01-15-use-re2-patterns --format json',
    'check --format json',
    'status --format json',
  ])
})

test('a write that leaves a new finding tells the model in a hidden note', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.check_dangling, F.status_base, F.status_dangling, F.validate_adr])

  on('tool.call', () => ({ result: { stdout: F.add_adr.stdout, stderr: '', interrupted: false } }))
  await $.session.start(START)

  // A passing workspace sets no status line, since the engine draws one as a warning.
  expect(host.statuses.at(-1)).toBe(undefined)

  const ran = await $.tool.call({ tool: 'Bash', command: ADD_ADR })

  expect(ran.context).toEqual([
    [
      'khub check: new findings:',
      `${REQ} › dangling: predicate realized_in, target cmp-serach`,
      `${REQ} › orphans`,
    ].join('\n'),
  ])
  expect(host.statuses.at(-1)).toBe('✗ 1 error · 10 entities')
})

test('a finding the model was told about is not repeated on the next write', async ($, on) => {
  fakeHost(on, [F.check_base, F.check_dangling, F.status_base, F.status_dangling, F.validate_adr])

  on('tool.call', () => ({ result: { stdout: F.add_adr.stdout, stderr: '', interrupted: false } }))
  await $.session.start(START)
  await $.tool.call({ tool: 'Bash', command: ADD_ADR })

  const again = await $.tool.call({ tool: 'Bash', command: ADD_ADR })

  expect(again.context).toBe(undefined)
})

test('a refused khub write runs no validate and adds no note', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  on('tool.call', () => ({ isError: true as const, result: F.add_refused.stdout, text: F.add_refused.stdout }))
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({
    tool: 'Bash',
    command: 'khub add requirement --title Other --kind non-functional --realized_in cmp-serach',
  })

  expect(host.ran).toEqual([])
  expect(ran.context).toBe(undefined)
})

test('a Write that would create an entity file is denied', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({
    tool: 'Write',
    file_path: `${ROOT}/knowledge/decisions/ad-2026-01-15-by-hand.md`,
    content: '---\ntype: adr\n---\n',
  })

  expect(ran.deny?.startsWith('khub: an entity is created with `khub add adr --title')).toBe(true)
  expect(host.ran).toEqual([])
})

test('khub remove --force asks the user first', { plugins: [witness] }, async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  on('tool.call', () => ({ result: { stdout: F.remove_actor.stdout, stderr: '', interrupted: false } }))
  await $.session.start(START)

  await $.tool.call({ tool: 'Bash', command: 'khub remove act-temp --force' })
  await $.tool.call({ tool: 'Bash', command: 'khub remove act-temp' })

  expect(host.toasts).toEqual([
    'pre-tool-use ask: khub remove --force deletes act-temp while other entities still point at it.',
    'pre-tool-use ask: none',
  ])
})

test('a khub call whose arguments hold a substitution is validated like any other', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, F.validate_adr])

  on('tool.call', () => ADDED)
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({ tool: 'Bash', command: `${ADD_ADR} --body "$(cat b.md)"` })

  expect(host.ran).toEqual(AFTER_ADD)
})

test('a write inside a chain refreshes health without naming an entity', async ($, on) => {
  const host = fakeHost(on, [...BROKEN])

  on('tool.call', () => ADDED)
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({ tool: 'Bash', command: `${ADD_ADR} && echo done` })

  expect(host.ran).toEqual(['check --format json', 'status --format json'])
  expect(ran.context).toEqual([['khub check: new findings:', DANGLING, `${REQ} › orphans`].join('\n')])
})

test('a khub call aimed at another workspace is left alone', async ($, on) => {
  let id = ''
  const host = fakeHost(on, [F.check_base, F.status_base])

  on('tool.call', (_$, e) => {
    id = e.tool_use_id

    return ADDED
  })
  engineRows(on)
  await $.session.start(START)
  host.ran.length = 0

  const command = 'khub -C /other add adr --title "Use RE2 patterns"'
  const ran = await $.tool.call({ tool: 'Bash', command })
  const row = await $.ui.mount(useRow(id, command))

  expect(host.ran).toEqual([])
  expect(ran.context).toBe(undefined)
  expect(await row.find({ type: 'Text', text: /^engine row$/ })).toBeDefined()
})

test('a call the chain denies keeps the engine row', async ($, on) => {
  let id = ''
  const host = fakeHost(on, [F.check_base, F.status_base])

  on('tool.call', (_$, e) => {
    id = e.tool_use_id

    return { deny: 'no' }
  })
  engineRows(on)
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({ tool: 'Bash', command: ADD_ADR })
  const row = await $.ui.mount(useRow(id, ADD_ADR))

  expect(ran.deny).toBe('no')
  expect(host.ran).toEqual([])
  expect(await row.find({ type: 'Text', text: /^engine row$/ })).toBeDefined()
  expect(await row.find({ type: 'Text', text: /^khub add adr$/ })).toBe(undefined)
})

test('a call that replaces the schema reloads it', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  on('tool.call', () => ({ result: { stdout: 'Upgraded build-hub 0.6.0', stderr: '', interrupted: false } }))
  await $.session.start(START)
  host.ran.length = 0

  await $.tool.call({ tool: 'Bash', command: 'khub upgrade' })

  expect(host.ran).toEqual(['schema --format json', 'check --format json', 'status --format json'])
})

test('an Edit of an entity file is validated, noted and marked under its row', async ($, on) => {
  let id = ''
  const host = fakeHost(on, [...BROKEN, F.validate_requirement_dangling])

  on('tool.call', (_$, e) => {
    id = e.tool_use_id

    return { result: {} }
  })
  engineRows(on)
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({
    tool: 'Edit',
    file_path: `${ROOT}/knowledge/requirements/req-search-answers-within-300-ms.md`,
    old_string: 'kind: non-functional',
    new_string: 'kind: non-functional\nrealized_in: [cmp-serach]',
  })
  const row = await $.ui.mount(resultRow(id, 'Edit'))

  expect(host.ran).toEqual([`validate ${REQ} --format json`, 'check --format json', 'status --format json'])
  expect(ran.context).toEqual([
    [`${REQ} › validate: ${UNRESOLVED}`, 'khub check: new findings:', DANGLING, `${REQ} › orphans`].join('\n'),
  ])
  expect(await row.find({ type: 'Text', text: /^engine row$/ })).toBeDefined()
  expect((await row.find({ type: 'Text', text: /^validate · 1 error · realized_in/ }))?.props.color).toBe('red')
  expect(host.statuses.at(-1)).toBe('✗ 1 error · 10 entities')
  expect(host.logs).toEqual([])
})

test('an Edit of a file khub refuses to validate draws no validate line', { plugins: [peek] }, async ($, on) => {
  let id = ''
  const refused = as(F.get_missing, ['validate', 'adr/README', '--format', 'json'])
  const host = fakeHost(on, [F.check_base, F.status_base, refused])

  on('tool.call', (_$, e) => {
    id = e.tool_use_id

    return { result: {} }
  })
  on('turn.complete', () => ({ text: '' }))
  engineRows(on)
  await $.session.start(START)

  const ran = await $.tool.call({
    tool: 'Edit',
    file_path: `${ROOT}/knowledge/decisions/README.md`,
    old_string: 'a',
    new_string: 'b',
  })
  const row = await $.ui.mount(resultRow(id, 'Edit'))

  expect(ran.context).toBe(undefined)
  expect(await row.find({ type: 'Text', text: /^engine row$/ })).toBeDefined()
  expect(await row.find({ type: 'Text', text: /validate/ })).toBe(undefined)

  await $.turn.complete(TURN_END)

  // khub named no entity, so the ledger holds none.
  expect(host.toasts[1]).toBe('ledger ')
})

test('a Write over an existing entity file passes and is validated', async ($, on) => {
  const path = `${ROOT}/knowledge/decisions/ad-2026-01-15-use-re2-patterns.md`
  const host = fakeHost(on, [F.check_base, F.status_base, F.validate_adr], { files: [path] })

  on('tool.call', () => ({ result: {} }))
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({ tool: 'Write', file_path: path, content: '---\ntype: adr\n---\n' })

  expect(ran.deny).toBe(undefined)
  expect(host.ran).toEqual(AFTER_ADD)
})

test('a Read of an entity file returns its inbound edges and counts as going around khub', { plugins: [peek] }, async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, F.neighbors_search_in])

  on('tool.call', () => ({ result: {} }))
  on('turn.complete', () => ({ text: '' }))
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({ tool: 'Read', file_path: `${ROOT}/knowledge/components/cmp-search.md` })

  expect(host.ran).toEqual(['neighbors component/cmp-search --in --format json'])
  expect(ran.context).toEqual([
    'khub: component/cmp-search has inbound edges its file does not show: served_by ← use-case/uc-find-a-document',
  ])

  await $.turn.complete(TURN_END)

  expect(host.toasts[0]).toBe('bypass 1')
})

test('a Read outside the type folders runs nothing', { plugins: [peek] }, async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  on('tool.call', () => ({ result: {} }))
  on('turn.complete', () => ({ text: '' }))
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({ tool: 'Read', file_path: `${ROOT}/README.md` })

  expect(host.ran).toEqual([])
  expect(ran.context).toBe(undefined)

  await $.turn.complete(TURN_END)

  expect(host.toasts[0]).toBe('bypass 0')
})

test('the slash command runs khub and leaves the model a note without the body', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, as(F.get_search, ['get', 'cmp-search', '--format', 'json'])])

  on('command.run', () => ({ text: 'not the mod' }))
  await $.session.start(START)

  expect(host.commands).toEqual(['khub'])

  const ran = await $.command.run(slash('khub', 'get cmp-search'))

  expect(ran.text?.split('\n')[0]).toBe('get cmp-search  component/cmp-search · Search · 3 edges')
  expect(ran.text?.includes('kind: service')).toBe(true)
  expect(ran.text?.includes('Responsibilities')).toBe(false)
  expect(ran.context).toEqual([
    'khub: the user ran `khub get cmp-search`, which answered: component/cmp-search · Search · 3 edges',
  ])
})

test('the slash command with no arguments opens the pane', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  on('command.run', () => ({ text: 'not the mod' }))
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.command.run(slash('khub'))

  expect(ran.text).toBe(undefined)
  expect(host.opened).toEqual(['khub'])
  expect(host.ran).toEqual([])
})

test('a write through the slash command is the user\'s, validated and noted', { plugins: [peek] }, async ($, on) => {
  const add = ['add', 'adr', '--title', 'Use RE2 patterns', '--status', 'accepted', '--author=Sam Rivera', '--format', 'json']
  const host = fakeHost(on, [...BROKEN, as(F.add_adr, add), F.validate_adr])

  on('command.run', () => ({ text: 'not the mod' }))
  on('turn.complete', () => ({ text: '' }))
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.command.run(slash('khub', 'add adr --title "Use RE2 patterns" --status accepted'))


  // The user's own add carries their name.
  expect(host.ran).toEqual([add.join(' '), ...AFTER_ADD])
  expect(ran.text?.split('\n')[0]).toBe(`add adr  + ${ADR}`)
  expect(ran.context).toEqual([
    `khub: the user ran \`khub add adr\`, which answered: + ${ADR}`,
    'khub check: new findings:',
    DANGLING,
    `${REQ} › orphans`,
  ])

  await $.turn.complete(TURN_END)

  expect(host.toasts[1]).toBe(`ledger add ${ADR} by user`)
})

test('where the khub skill owns /khub the mod shares the name with it', async ($, on) => {
  const got = as(F.get_search, ['get', 'cmp-search', '--format', 'json'])
  const host = fakeHost(on, [F.check_base, F.status_base, got], { taken: ['khub'] })

  on('command.run', (_$, e) => ({ text: `the skill ran with: ${e.args}` }))
  await $.session.start(START)
  host.ran.length = 0

  // A khub command is the mod's, and so is the bare name. Anything else is the skill's.
  expect(host.commands).toEqual([])
  expect((await $.command.run(slash('khub', 'get cmp-search'))).text?.startsWith('get cmp-search')).toBe(true)
  expect(host.ran).toEqual(['get cmp-search --format json'])
  expect((await $.command.run(slash('khub', 'how do I record a decision'))).text).toBe(
    'the skill ran with: how do I record a decision',
  )

  await $.command.run(slash('khub'))

  expect(host.opened).toEqual(['khub'])
})

test('the plugin\'s own skill takes /khub under its full name, and the mod shares that too', async ($, on) => {
  const got = as(F.get_search, ['get', 'cmp-search', '--format', 'json'])
  const host = fakeHost(on, [F.check_base, F.status_base, got], { taken: ['khub'] })

  on('command.run', (_$, e) => ({ text: `the skill ran with: ${e.args}` }))
  await $.session.start(START)
  host.ran.length = 0

  expect((await $.command.run(slash('khub:khub', 'get cmp-search'))).text?.startsWith('get cmp-search')).toBe(true)
  expect(host.ran).toEqual(['get cmp-search --format json'])
  expect((await $.command.run(slash('khub:khub', 'how do I record a decision'))).text).toBe(
    'the skill ran with: how do I record a decision',
  )
})

test('the skill\'s own name is shared even where the mod holds /khub', async ($, on) => {
  fakeHost(on, [F.check_base, F.status_base])

  on('command.run', (_$, e) => ({ text: `the skill ran with: ${e.args}` }))
  await $.session.start(START)

  expect((await $.command.run(slash('khub:khub', 'how do I record a decision'))).text).toBe(
    'the skill ran with: how do I record a decision',
  )
})

test('a khub older than the mod reads is named on the status line, and the mod stands aside', async ($, on) => {
  const host = fakeHost(on, [], { version: 'khub 0.26.0' })

  on('tool.call', () => ({ result: { stdout: '', stderr: '', interrupted: false } }))
  await $.session.start(START)

  // Only the version was asked.
  expect(host.statuses).toEqual(['needs khub 0.27.0 or newer'])
  expect(host.ran).toEqual(['--version'])
  host.ran.length = 0

  const ran = await $.tool.call({ tool: 'Bash', command: 'khub add adr --title x' })

  expect(ran.context).toBe(undefined)
  expect(host.ran).toEqual([])
})

test('outside a workspace /khub is not the mod\'s', async ($, on) => {
  const host = fakeHost(on)

  on('command.run', (_$, e) => ({ text: `someone else ran: ${e.args}` }))
  await $.session.start({ ...START, cwd: '/elsewhere' })

  expect(host.commands).toEqual([])
  expect((await $.command.run(slash('khub', 'status'))).text).toBe('someone else ran: status')
  expect(host.ran).toEqual([])
})

test('under gate block a turn does not finish on an error the session introduced', { options: { gate: 'block' } }, async ($, on) => {
  const host = fakeHost(on, [...BROKEN, F.validate_adr])

  on('tool.call', () => ADDED)
  on('classic.Stop', () => ({}))
  await $.session.start(START)
  await $.tool.call({ tool: 'Bash', command: ADD_ADR })
  host.ran.length = 0

  const stopped = await $.classic.Stop({ stop_hook_active: false })

  // Health is read again before the gate decides.
  expect(host.ran).toEqual(['check --format json', 'status --format json'])
  expect(stopped.block?.split('\n')).toEqual([
    'khub: this session introduced 1 error that `khub check` reports.',
    'Fix it before finishing:',
    DANGLING,
  ])
  expect((await $.classic.Stop({ stop_hook_active: true })).block).toBe(undefined)
})

test('under gate block a clean session finishes', { options: { gate: 'block' } }, async ($, on) => {
  fakeHost(on, [F.check_base, F.status_base])
  on('classic.Stop', () => ({}))
  await $.session.start(START)

  expect((await $.classic.Stop({ stop_hook_active: false })).block).toBe(undefined)
})

test('under the default gate a turn finishes whatever the session introduced', async ($, on) => {
  const host = fakeHost(on, [...BROKEN, F.validate_adr])

  on('tool.call', () => ADDED)
  on('classic.Stop', () => ({}))
  await $.session.start(START)
  await $.tool.call({ tool: 'Bash', command: ADD_ADR })
  host.ran.length = 0

  expect((await $.classic.Stop({ stop_hook_active: false })).block).toBe(undefined)
  expect(host.ran).toEqual([])
})

test('a schema that does not resolve is said on the status line', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base], { schema: as(F.get_missing, ['schema', '--format', 'json']) })

  await $.session.start(START)

  expect(host.statuses.at(-1)).toBe("schema: No entity 'nope' found")
})

test('a check khub does not answer is said on the status line', async ($, on) => {
  const host = fakeHost(on, [as(F.get_missing, ['check', '--format', 'json']), F.status_base])

  await $.session.start(START)

  expect(host.statuses.at(-1)?.startsWith('check: ')).toBe(true)
})

test('a turn start reads the schema and health again in the background', async ($, on) => {
  const host = fakeHost(on, [...BROKEN])

  await $.session.start(START)
  host.ran.length = 0

  await $.turn.start({ text: 'what changed?', turnId: 't2' })
  await settled(() => host.ran.length >= 3)

  expect(host.ran).toEqual(['schema --format json', 'check --format json', 'status --format json'])
  expect(host.statuses.at(-1)).toBe('✗ 1 error · 10 entities')
  expect(host.logs).toEqual([])
})

test('outside a workspace a turn start runs nothing', async ($, on) => {
  const host = fakeHost(on)

  await $.session.start({ ...START, cwd: '/elsewhere' })
  await $.turn.start({ text: 'hi', turnId: 't2' })
  await settled(() => host.ran.length > 0, 10)

  expect(host.ran).toEqual([])
})
