import { expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'
import type { On } from 'claude-code'

import { parseJson } from '../hooks/cli'
import type { KhubStats, KhubType } from '../types'
import { F } from './fixtures'
import type { Fixture } from './fixtures'
import { as, fakeHost, ROOT, settled, slash, START } from './helpers'

const SEARCH = 'component/cmp-search'
const STORAGE = 'component/cmp-storage'
const ADR = 'adr/ad-2026-01-15-use-re2-patterns'
const TYPES = (parseJson(F.schema.stdout) as { types: KhubType[] }).types.map(type => type.name)

const json = (args: string[]) => [...args, '--format', 'json']
const line = (args: string[]) => json(args).join(' ')

// What Browse reads for the Search component, and for the list it sits in.
const GET = as(F.get_search, json(['get', SEARCH]))
const NEIGHBORS = as(F.neighbors_search, json(['neighbors', SEARCH]))
const READ = [line(['get', SEARCH]), line(['neighbors', SEARCH])]
const LIST = line(['query', '--type', 'component'])
const AFTER = (id: string) => [line(['validate', id]), 'check --format json', 'status --format json']

// What a form asks before it opens: the entities of every type a relation may point at.
// Components answer from the recording and every other type holds none.
const REPOS = { ...F.query_draft, stdout: JSON.stringify([{ id: 'repo/rp-api', title: 'API' }]) }
const held = (type: string) => (type === 'component' ? F.query_components : type === 'repo' ? REPOS : F.query_draft)
const TARGETS = TYPES.map(type => as(held(type), json(['query', '--type', type])))
const ASKED = TYPES.map(type => line(['query', '--type', type]))

const PANE = { title: 'khub', isFocused: true, bodyColumns: 80, placement: 'inline' as const, scroll: { offset: 0, bodyRows: 40 }, view: {} }
const BAND = { hasSurvey: false, isWorking: false, maxRows: 4, bodyColumns: 80, scroll: { offset: 0, bodyRows: 1 }, view: {} }

const pane = ($: Engine, requestId = 'khub') =>
  $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'Pane', requestId, props: PANE })

const band = ($: Engine) => $.ui.mount({ plugin: 'khub', surface: 'terminal', component: 'AbovePrompt', props: BAND })

type Drawn = Pick<Awaited<ReturnType<typeof pane>>, 'find' | 'findAll'>

const keys = async (ui: Drawn) => (await ui.findAll({ type: 'Button' })).map(button => button.key)
const shows = async (ui: Drawn, text: RegExp) => (await ui.find({ type: 'Text', text })) !== undefined

// The color of the innermost Text that shows `text`. A band part sits inside the line's own Text.
const colorOf = async (ui: Drawn, text: RegExp) => (await ui.findAll({ type: 'Text', text })).at(-1)?.props.color

type Host = ReturnType<typeof fakeHost>

// Starts the session and waits for the doctor's read, which runs on after the start returned.
// The status line is drawn once by the first refresh and once when the doctor is done.
async function started($: Engine, host: Host) {
  await $.session.start(START)
  await settled(() => host.statuses.length >= 2)
}

// The engine's own drawing, for a site the mod leaves alone.
function engineDraws(on: On) {
  on('ui.render', ($, e) => {
    const { Text } = $.ui.resolve(e)

    return <Text>engine drawing</Text>
  })
}

// A session in the workspace with its pane mounted, and the calls of the start put aside.
async function session($: Engine, on: On, replies: Fixture[] = [], options: Parameters<typeof fakeHost>[2] = {}) {
  const host = fakeHost(on, [F.check_base, F.status_base, ...replies], options)

  engineDraws(on)
  await started($, host)
  host.ran.length = 0
  host.upkeep.length = 0

  return { host, ui: await pane($) }
}

// The same, with Browse open on the Search component.
async function atSearch($: Engine, on: On, replies: Fixture[] = []) {
  const { host, ui } = await session($, on, [GET, NEIGHBORS, F.query_components, ...replies])

  await ui.press({ key: 'tab-browse' })
  await ui.press({ key: 'type-component' })
  await ui.press({ key: `entity-${SEARCH}` })
  host.ran.length = 0

  return { host, ui }
}

// Stands above the mod and, at a turn's end, toasts the tally the mod holds for every session.
const tally = {
  name: 'tally',
  tier: 'prepend' as const,
  register(on: On) {
    on('turn.complete', async ($, e, next) => {
      const answered = await next(e)
      const all = await $.state.get({ plugin: 'khub', key: 'allStats' } as const)

      $.ui.toast(`all calls ${all.value?.calls ?? 'none'}`)

      return answered
    })
  },
}

const DAY = 86_400_000

const TURN_END = { answer: '', durationMs: 1, isAborted: false, turnId: 't1', reason: 'answer' as const }

// 1. Browse

test('Browse lists the types with their counts, then a type\'s entities, then one entity', async ($, on) => {
  const { host, ui } = await session($, on, [GET, NEIGHBORS, F.query_components])

  await ui.press({ key: 'tab-browse' })

  expect((await keys(ui)).filter(key => key?.startsWith('type-'))).toEqual(TYPES.map(type => `type-${type}`))
  expect(host.ran).toEqual([])

  await ui.press({ key: 'type-component' })

  expect(host.ran).toEqual([LIST])
  expect((await ui.find({ key: `entity-${SEARCH}` }))?.props.label).toBe(SEARCH)
  expect((await ui.find({ key: `entity-${STORAGE}` }))?.props.label).toBe(STORAGE)
  expect(await shows(ui, /^Storage$/)).toBe(true)

  await ui.press({ key: `entity-${SEARCH}` })

  expect(host.ran).toEqual([LIST, ...READ])
  expect(await shows(ui, new RegExp(`^${SEARCH}$`))).toBe(true)
  expect(await shows(ui, /title Search$/)).toBe(true)
  expect(await shows(ui, /^EDGES OUT$/)).toBe(true)
  expect(await shows(ui, /^EDGES IN$/)).toBe(true)
  expect(await ui.find({ type: 'Markdown', text: 'Responsibilities' })).toBeDefined()

  // The template's comments are khub's prompts to the author and stay out of the pane.
  expect(await ui.find({ type: 'Markdown', text: '<!--' })).toBe(undefined)
  expect(host.logs).toEqual([])
})

test('Browse steps back from an entity to its type, and from there to the types', async ($, on) => {
  const { host, ui } = await atSearch($, on)

  await ui.press({ key: 'back' })

  expect(host.ran).toEqual([LIST])
  expect(await ui.find({ key: `entity-${SEARCH}` })).toBeDefined()

  await ui.press({ key: 'back' })

  expect(await ui.find({ key: 'type-component' })).toBeDefined()
  expect(host.ran).toEqual([LIST])
})

test('an entity khub does not know is said in Browse', async ($, on) => {
  const { ui } = await session($, on, [
    as(F.search_hits, ['search', '--plain', '--limit', '20', '--format', 'json', '--', 'search']),
    as(F.get_missing, json(['get', SEARCH])),
    as(F.query_draft, json(['neighbors', SEARCH])),
  ])

  await ui.press({ key: 'tab-search' })
  await ui.input({ key: 'search', text: 'search' })
  await ui.press({ key: 'hit-0' })

  expect((await ui.find({ type: 'Text', text: /^No entity 'nope' found$/ }))?.props.color).toBe('red')
  expect(await shows(ui, /^loading…$/)).toBe(true)
})

// 2. Edges and impact

test('an entity shows its edges both ways, and an edge opens what it points at', async ($, on) => {
  const { host, ui } = await atSearch($, on)

  expect((await ui.find({ key: 'edge-0' }))?.props.label).toBe(STORAGE)
  expect((await ui.find({ key: 'edge-1' }))?.props.label).toBe('repo/rp-api')
  expect((await ui.find({ key: 'edge-2' }))?.props.label).toBe('use-case/uc-find-a-document')

  // An outbound edge can be unlinked. An inbound one is another entity's, and offers its impact.
  expect((await keys(ui)).filter(key => /^(unlink|impact)-/.test(key ?? ''))).toEqual(['unlink-0', 'unlink-1', 'impact-served_by'])

  await ui.press({ key: 'edge-1' })

  expect(host.ran).toEqual([line(['get', 'repo/rp-api']), line(['neighbors', 'repo/rp-api'])])
})

test('impact on an inbound edge walks that predicate backwards and shows the tree', async ($, on) => {
  const walk = ['impact', SEARCH, '--reverse', '--predicate', 'served_by', '--format', 'tree']
  const { host, ui } = await atSearch($, on, [as(F.impact_storage_tree, walk)])

  await ui.press({ key: 'impact-served_by' })

  expect(host.ran).toEqual([walk.join(' ')])
  expect(await shows(ui, /^component\/cmp-storage\n {2}component\/cmp-search$/)).toBe(true)

  // The tree belongs to the entity it was walked from.
  await ui.press({ key: 'back' })
  await ui.press({ key: `entity-${SEARCH}` })

  expect(await shows(ui, /cmp-storage\n/)).toBe(false)
})

// 3. The id, into the prompt and onto the clipboard

test('Insert id, Copy id and Edit with Claude hand the id over without running khub', async ($, on) => {
  const { host, ui } = await atSearch($, on)

  await ui.press({ key: 'insert' })
  await ui.press({ key: 'copy' })
  await ui.press({ key: 'edit-claude' })

  expect(host.inserted).toEqual([SEARCH])
  expect(host.copied).toEqual([SEARCH])
  expect(host.fills).toEqual([SEARCH, `Edit ${SEARCH}: `])
  expect(host.ran).toEqual([])
})

test('Add with Claude starts the prompt for the type in view', async ($, on) => {
  const { host, ui } = await session($, on, [F.query_components])

  await ui.press({ key: 'tab-browse' })
  await ui.press({ key: 'type-component' })
  await ui.press({ key: 'add-claude' })

  expect(host.fills).toEqual(['Add a component: '])
  expect(host.inserted).toEqual([])
})

// 4. Unlink

test('unlinking an edge runs khub, validates the entity and reads it again', async ($, on) => {
  const unlink = ['unlink', SEARCH, 'depends_on', STORAGE]
  const done = { ...F.unlink_adr, args: json(unlink), stdout: F.unlink_adr.stdout.replaceAll('ad-2026-01-15-use-re2-patterns', 'cmp-search').replaceAll('adr', 'component') }
  const { host, ui } = await atSearch($, on, [done, as(F.validate_adr, json(['validate', SEARCH]))])

  await ui.press({ key: 'unlink-0' })

  expect(host.ran).toEqual([line(unlink), ...AFTER(SEARCH), ...READ])
  expect(await shows(ui, /Refusing/)).toBe(false)
  expect(host.logs).toEqual([])
})

test('an unlink khub refuses is said in red and nothing else runs', async ($, on) => {
  const unlink = ['unlink', SEARCH, 'depends_on', STORAGE]
  const { host, ui } = await atSearch($, on, [as(F.remove_refused, json(unlink))])

  await ui.press({ key: 'unlink-0' })

  expect(host.ran).toEqual([line(unlink)])
  expect((await ui.find({ type: 'Text', text: /^Refusing to remove component 'cmp-search'/ }))?.props.color).toBe('red')

  // The entity stays in view under the refusal.
  expect(await ui.find({ key: 'unlink-0' })).toBeDefined()
})

// 5. Publish and draft

test('Mark as draft edits the flag, validates and reads the entity again', async ($, on) => {
  const edit = ['edit', SEARCH, 'draft', 'true']
  const drafted = { ...GET, stdout: GET.stdout.replace('"draft": false', '"draft": true') }
  const { host, ui } = await atSearch($, on, [
    as(F.edit_component, json(edit)),
    as(F.edit_component, json(['edit', SEARCH, 'draft', 'false'])),
    as(F.validate_adr, json(['validate', SEARCH])),
    drafted,
  ])

  expect((await ui.find({ key: 'draft' }))?.props.label).toBe('Mark as draft')

  await ui.press({ key: 'draft' })

  expect(host.ran).toEqual([line(edit), ...AFTER(SEARCH), ...READ])
  expect((await ui.find({ key: 'draft' }))?.props.label).toBe('Publish')
  expect((await ui.find({ type: 'Text', text: /^draft$/ }))?.props.color).toBe('yellow')

  await ui.press({ key: 'draft' })

  expect(host.ran[6]).toBe(line(['edit', SEARCH, 'draft', 'false']))
})

// 6. The add form

// Opens the add form over adr from Browse, and mounts it.
async function addForm($: Engine, on: On, replies: Fixture[], options: Parameters<typeof fakeHost>[2] = {}) {
  const { host, ui } = await session($, on, [...TARGETS, ...replies], options)

  await ui.press({ key: 'tab-browse' })
  await ui.press({ key: 'type-adr' })
  host.ran.length = 0
  await ui.press({ key: 'add' })

  return { host, ui, form: await pane($, 'khub-form') }
}

const ADD = ['add', 'adr', '--title', 'Use RE2 patterns', '--status', 'accepted', '--author', 'Sam Rivera']

test('the add form asks for the relation targets, opens focused, and adds under the user\'s name', async ($, on) => {
  const { host, ui, form } = await addForm($, on, [as(F.add_adr, json(ADD)), F.validate_adr])

  expect(host.ran).toEqual(ASKED)
  expect(host.opened).toEqual(['khub-form'])
  expect(await shows(form, /^Add adr$/)).toBe(true)

  // A relation to any type offers the entities the form fetched.
  expect((await form.find({ key: 'field-supersedes' }))?.props.options).toEqual([{ value: '', label: '(unset)' }])

  host.ran.length = 0
  await form.input({ key: 'field-title', text: 'Use RE2 patterns', kind: 'change' })
  await form.select({ key: 'field-status', value: 'accepted' })

  expect((await form.find({ key: 'field-title' }))?.props.value).toBe('Use RE2 patterns')

  await form.press({ key: 'submit' })

  // Each value is one argument, so a title with spaces needs no quoting.
  expect(host.ran).toEqual([line(ADD), ...AFTER(ADR), line(['query', '--type', 'adr'])])
  expect(host.closed).toEqual(['khub-form'])
  expect(await shows(form, /^engine drawing$/)).toBe(true)
  expect(await shows(ui, /^adr$/)).toBe(true)
  expect(host.logs).toEqual([])
})

test('Add as draft passes --draft', async ($, on) => {
  const { host, form } = await addForm($, on, [as(F.add_adr, json([...ADD, '--draft'])), F.validate_adr])

  host.ran.length = 0
  await form.input({ key: 'field-title', text: 'Use RE2 patterns', kind: 'change' })
  await form.select({ key: 'field-status', value: 'accepted' })
  await form.press({ key: 'submit-draft' })

  expect(host.ran[0]).toBe(line([...ADD, '--draft']))
  expect(host.closed).toEqual(['khub-form'])
})

test('an add khub refuses keeps the form open with the refusal and what was typed', async ($, on) => {
  const { host, form } = await addForm($, on, [as(F.add_refused, json(ADD))])

  host.ran.length = 0
  await form.input({ key: 'field-title', text: 'Use RE2 patterns', kind: 'change' })
  await form.select({ key: 'field-status', value: 'accepted' })
  await form.press({ key: 'submit' })

  expect(host.ran).toEqual([line(ADD)])
  expect(host.closed).toEqual([])
  expect((await form.find({ type: 'Text', text: /^No component 'cmp-serach' to satisfy relation 'realized_in'$/ }))?.props.color).toBe('red')
  expect((await form.find({ key: 'field-title' }))?.props.value).toBe('Use RE2 patterns')
  expect((await form.find({ key: 'field-status' }))?.props.value).toBe('accepted')
})

test('Cancel closes the form and runs nothing', async ($, on) => {
  const { host, form } = await addForm($, on, [])

  host.ran.length = 0
  await form.input({ key: 'field-title', text: 'Use RE2 patterns', kind: 'change' })
  await form.press({ key: 'cancel' })

  expect(host.ran).toEqual([])
  expect(host.closed).toEqual(['khub-form'])
  expect(await shows(form, /^engine drawing$/)).toBe(true)
})

test('where git names no user, an add from the form carries no author', async ($, on) => {
  const bare = ADD.slice(0, -2)
  const { host, form } = await addForm($, on, [as(F.add_adr, json(bare)), F.validate_adr], { user: '' })

  host.ran.length = 0
  await form.input({ key: 'field-title', text: 'Use RE2 patterns', kind: 'change' })
  await form.select({ key: 'field-status', value: 'accepted' })
  await form.press({ key: 'submit' })

  expect(host.ran[0]).toBe(line(bare))
})

// The edit and link forms

test('the edit form starts from the entity and sends only what changed', async ($, on) => {
  const edit = ['edit', SEARCH, '--lifecycle', 'deprecated']
  const { host, ui } = await atSearch($, on, [...TARGETS, as(F.edit_component, json(edit)), as(F.validate_adr, json(['validate', SEARCH]))])

  await ui.press({ key: 'edit' })

  const form = await pane($, 'khub-form')

  expect(await shows(form, new RegExp(`^Edit ${SEARCH}$`))).toBe(true)
  expect((await form.find({ key: 'field-title' }))?.props.value).toBe('Search')
  expect((await form.find({ key: 'field-lifecycle' }))?.props.value).toBe('production')
  expect((await form.find({ key: 'field-repo' }))?.props.value).toBe('rp-api')

  host.ran.length = 0
  await form.select({ key: 'field-lifecycle', value: 'deprecated' })
  await form.press({ key: 'submit' })

  expect(host.ran).toEqual([line(edit), ...AFTER(SEARCH), ...READ])
  expect(host.closed).toEqual(['khub-form'])
})

test('an edit form saved unchanged closes without a khub call', async ($, on) => {
  const { host, ui } = await atSearch($, on, TARGETS)

  await ui.press({ key: 'edit' })

  const form = await pane($, 'khub-form')

  host.ran.length = 0
  await form.press({ key: 'submit' })

  expect(host.ran).toEqual([])
  expect(host.closed).toEqual(['khub-form'])
})

// khub refuses an empty relation value and asks for an unlink, so the form unlinks.
test('clearing a relation in the edit form unlinks it', async ($, on) => {
  const unlink = ['unlink', SEARCH, 'repo', 'rp-api']
  const done = { ...F.unlink_adr, args: json(unlink), stdout: F.unlink_adr.stdout.replaceAll('ad-2026-01-15-use-re2-patterns', 'cmp-search').replaceAll('adr', 'component') }
  const { host, ui } = await atSearch($, on, [...TARGETS, done, as(F.validate_adr, json(['validate', SEARCH]))])

  await ui.press({ key: 'edit' })

  const form = await pane($, 'khub-form')

  host.ran.length = 0
  await form.select({ key: 'field-repo', value: '' })
  await form.press({ key: 'submit' })

  expect(host.ran).toEqual([line(unlink), ...AFTER(SEARCH), ...READ])
  expect(host.closed).toEqual(['khub-form'])
})

test('the link form offers the type\'s relations and links to the entity picked', async ($, on) => {
  const link = ['link', SEARCH, 'depends_on', 'cmp-storage']
  const linked = { ...F.link_adr, args: json(link), stdout: F.link_adr.stdout.replaceAll('ad-2026-01-15-use-re2-patterns', 'cmp-search').replaceAll('adr', 'component') }
  const { host, ui } = await atSearch($, on, [...TARGETS, linked, as(F.validate_adr, json(['validate', SEARCH]))])

  await ui.press({ key: 'link' })

  const form = await pane($, 'khub-form')
  const offered = (await form.find({ key: 'field-predicate' }))?.props.options as Array<{ value: string }>

  // A derived relation is the other entity's to write, so the form leaves it out.
  expect(offered.map(option => option.value)).toContain('depends_on')
  expect(offered.map(option => option.value)).not.toContain('serves')

  host.ran.length = 0
  await form.select({ key: 'field-predicate', value: 'depends_on' })
  await form.select({ key: 'field-target', value: 'cmp-storage' })
  await form.press({ key: 'submit' })

  expect(host.ran).toEqual([line(link), ...AFTER(SEARCH), ...READ])
  expect(host.closed).toEqual(['khub-form'])
})

// 7. The remove form

test('a refused remove offers force, and a forced remove leaves Browse at the type\'s list', async ($, on) => {
  const remove = ['remove', SEARCH]
  const forced = ['remove', SEARCH, '--force']
  const gone = { ...F.remove_actor, args: json(forced), stdout: F.remove_actor.stdout.replaceAll('act-temp', 'cmp-search').replaceAll('actor', 'component') }
  const { host, ui } = await atSearch($, on, [as(F.remove_refused, json(remove)), gone])

  await ui.press({ key: 'remove' })

  const form = await pane($, 'khub-form')

  // A confirmation has no fields, so it asks khub for no targets.
  expect(host.ran).toEqual([])
  expect(await shows(form, new RegExp(`^Remove ${SEARCH}$`))).toBe(true)
  expect(await keys(form)).toEqual(['submit', 'cancel'])

  await form.press({ key: 'submit' })

  expect(host.ran).toEqual([line(remove)])
  expect(host.closed).toEqual([])
  expect((await form.find({ type: 'Text', text: /^Refusing to remove component 'cmp-search'/ }))?.props.color).toBe('red')
  expect(await keys(form)).toEqual(['submit', 'show-edges', 'force', 'cancel'])

  await form.press({ key: 'force' })

  expect((await form.find({ key: 'submit' }))?.props.label).toBe('Remove anyway')
  expect(await form.find({ key: 'force' })).toBe(undefined)

  await form.press({ key: 'submit' })

  // A removed entity has nothing to validate, and Browse falls back to its type.
  expect(host.ran).toEqual([line(remove), line(forced), 'check --format json', 'status --format json', LIST])
  expect(host.closed).toEqual(['khub-form'])
  expect(await ui.find({ key: `entity-${STORAGE}` })).toBeDefined()
  expect(host.logs).toEqual([])
})

// 8. Search

test('a search lists its hits with their match share, and a hit opens in Browse', async ($, on) => {
  const find = ['search', '--plain', '--limit', '20', '--format', 'json', '--', 'search']
  const { host, ui } = await session($, on, [as(F.search_hits, find), GET, NEIGHBORS])

  await ui.press({ key: 'tab-search' })

  expect(await ui.find({ type: 'Input', key: 'search' })).toBeDefined()

  await ui.input({ key: 'search', text: 'search' })

  expect(host.ran).toEqual([find.join(' ')])
  expect((await ui.find({ key: 'hit-0' }))?.props.label).toBe(SEARCH)
  expect(await shows(ui, /^match 1\.000$/)).toBe(true)
  expect(await shows(ui, /^1 hit$/)).toBe(true)

  await ui.press({ key: 'hit-0' })

  expect(host.ran).toEqual([find.join(' '), ...READ])
  expect(await ui.find({ key: 'tab-search' })).toBeDefined()
  expect(await ui.find({ key: 'tab-browse' })).toBe(undefined)
  expect(await ui.find({ key: 'edge-0' })).toBeDefined()
})

test('a search with no hits shows khub\'s note', async ($, on) => {
  const find = ['search', '--plain', '--limit', '20', '--format', 'json', '--', 'zzzz']
  const { host, ui } = await session($, on, [as(F.search_empty, find)])

  await ui.press({ key: 'tab-search' })
  await ui.input({ key: 'search', text: 'zzzz' })

  expect(host.ran).toEqual([find.join(' ')])
  expect(await shows(ui, /^note: no hits in 8 entities searched/)).toBe(true)
  expect(await shows(ui, /^0 hits$/)).toBe(true)
})

test('an empty search runs nothing', async ($, on) => {
  const { host, ui } = await session($, on)

  await ui.press({ key: 'tab-search' })
  await ui.input({ key: 'search', text: '  ' })

  expect(host.ran).toEqual([])
})

test('a search text that starts with a dash reaches khub as the query, not as a flag', async ($, on) => {
  const { host, ui } = await session($, on)

  await ui.press({ key: 'tab-search' })
  await ui.input({ key: 'search', text: '--type' })

  expect(host.ran[0]?.endsWith(' -- --type')).toBe(true)
})

test('a search khub refuses shows the refusal', async ($, on) => {
  const find = ['search', '--plain', '--limit', '20', '--format', 'json', '--', 'a OR']
  const { ui } = await session($, on, [as(F.get_missing, find)])

  await ui.press({ key: 'tab-search' })
  await ui.input({ key: 'search', text: 'a OR' })

  expect(await shows(ui, /^No entity 'nope' found$/)).toBe(true)
})

// 9. Stats

const STATUS = { result: { stdout: F.status_base.stdout, stderr: '', interrupted: false } }

const EARLIER: KhubStats = {
  calls: 40,
  reads: 30,
  writes: 10,
  byVerb: { query: 30, add: 10 },
  refused: {},
  searches: 0,
  searchEmpty: 0,
  searchTruncated: 0,
  bytes: 8000,
  bypass: 0,
  durations: [5, 6],
  slowest: { verb: 'query', ms: 6 },
}

test('Stats counts the session\'s khub calls and switches to the tally of every session', { plugins: [tally] }, async ($, on) => {
  on('tool.call', () => STATUS)
  on('turn.complete', () => ({ text: '' }))

  const { host, ui } = await session($, on, [], { store: { stats: { [ROOT]: EARLIER, '/other': EARLIER } } })

  await ui.press({ key: 'tab-stats' })

  expect(await shows(ui, /^THIS SESSION$/)).toBe(true)
  expect(await shows(ui, /^No khub calls yet\.$/)).toBe(true)

  await $.tool.call({ tool: 'Bash', command: 'khub status' })

  expect(await shows(ui, /^read 1 · write 0$/)).toBe(true)
  expect(await shows(ui, /^status 1$/)).toBe(true)
  expect((await ui.find({ key: 'scope' }))?.props.label).toBe('All sessions')

  // Before a turn ends, every session is what the store held at the start.
  await ui.press({ key: 'scope' })

  expect(await shows(ui, /^ALL SESSIONS$/)).toBe(true)
  expect(await shows(ui, /^read 30 · write 10$/)).toBe(true)

  await $.turn.complete(TURN_END)

  expect(host.toasts).toEqual(['all calls 41'])
  expect(await shows(ui, /^read 31 · write 10$/)).toBe(true)

  await ui.press({ key: 'scope' })

  expect(await shows(ui, /^read 1 · write 0$/)).toBe(true)
})

// Each turn's end writes the stored tally plus the whole session, never the last write plus the session.
test('a second turn does not count the session\'s calls twice', { plugins: [tally] }, async ($, on) => {
  on('tool.call', () => STATUS)
  on('turn.complete', () => ({ text: '' }))

  const { host } = await session($, on, [], { store: { stats: { [ROOT]: EARLIER } } })

  await $.tool.call({ tool: 'Bash', command: 'khub status' })
  await $.turn.complete(TURN_END)
  await $.tool.call({ tool: 'Bash', command: 'khub status' })
  await $.turn.complete({ ...TURN_END, turnId: 't2' })

  expect(host.toasts).toEqual(['all calls 41', 'all calls 42'])
})

test('a subagent\'s turn end writes no tally', { plugins: [tally] }, async ($, on) => {
  on('tool.call', () => STATUS)
  on('turn.complete', () => ({ text: '' }))

  const { host } = await session($, on)

  await $.tool.call({ tool: 'Bash', command: 'khub status' })
  await $.turn.complete({ ...TURN_END, agentId: 'a1' })

  expect(host.toasts).toEqual(['all calls none'])
})

// 10. The author stamp

// Runs one Bash call through the mod and returns the command that reached the engine.
async function reached($: Engine, on: On, command: string) {
  let seen = ''

  on('tool.call', (_$, e) => {
    seen = (e as { command: string }).command

    return { result: { stdout: F.add_adr.stdout, stderr: '', interrupted: false } }
  })
  fakeHost(on, [F.check_base, F.status_base, F.validate_adr])
  await $.session.start(START)
  await $.tool.call({ tool: 'Bash', command })

  return seen
}

test('an add by the agent that names no author is stamped with the agent\'s', async ($, on) => {
  expect(await reached($, on, 'khub add adr --title "Use RE2 patterns"')).toBe('khub add adr --author \'claude-code\' --title "Use RE2 patterns"')
})

test('the stamp is the configured author', { options: { agent_author: 'robot' } }, async ($, on) => {
  expect(await reached($, on, 'khub add adr --title X')).toBe('khub add adr --author \'robot\' --title X')
})

test('an add that names its author is left as written', async ($, on) => {
  expect(await reached($, on, 'khub add adr --title X --author "Sam Rivera"')).toBe('khub add adr --title X --author "Sam Rivera"')
})

test('an empty configured author stamps nothing', { options: { agent_author: '' } }, async ($, on) => {
  expect(await reached($, on, 'khub add adr --title X')).toBe('khub add adr --title X')
})

test('only an add is stamped', async ($, on) => {
  expect(await reached($, on, 'khub edit cmp-search lifecycle deprecated')).toBe('khub edit cmp-search lifecycle deprecated')
})

test('an add inside a chain is left as written', async ($, on) => {
  expect(await reached($, on, 'khub add adr --title X && echo done')).toBe('khub add adr --title X && echo done')
})

// The stamp follows the type, so what ends the line never matters.
test('an add piped onward is stamped on the add, not on the pipe\'s last command', async ($, on) => {
  expect(await reached($, on, 'khub add adr --title X | cat')).not.toBe('khub add adr --title X | cat --author claude-code')
})

test('an add with a trailing comment keeps its stamp outside the comment', async ($, on) => {
  expect(await reached($, on, 'khub add adr --title X # note')).toBe('khub add adr --author \'claude-code\' --title X # note')
})

test('an add that reads its body from a heredoc keeps the heredoc closed', async ($, on) => {
  const command = 'khub add adr --title X --body-file - <<EOF\nWhy RE2.\nEOF'

  expect((await reached($, on, command)).split('\n')).toContain('EOF')
})

// 11. The schema loop

const ONTOLOGY = { tool: 'Edit' as const, file_path: `${ROOT}/.khub/ontology.yaml`, old_string: 'a', new_string: 'b' }

test('a schema edit reloads the schema, tells the model what changed and offers rewire and snapshot', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, F.schema_diff_pending, as(F.schema_snapshot, ['schema', 'snapshot'])])

  on('tool.call', () => ({ result: {} }))
  engineDraws(on)
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call(ONTOLOGY)

  expect(host.ran).toEqual(['schema --format json', 'schema diff --format json', 'validate --format json', 'check --format json', 'status --format json'])
  expect(ran.context).toEqual([
    ['khub schema: 1 change since the last snapshot:', 'added types.prd.attributes.recorded_note'].join('\n'),
  ])

  const line = await band($)

  expect(await colorOf(line, /^schema · 1 change pending$/)).toBe('yellow')
  expect(await keys(line)).toEqual(['rewire', 'snapshot', 'hide'])

  host.ran.length = 0
  await line.press({ key: 'snapshot' })

  // The schema line leaves the band once the user acted on it, and the quiet summary returns.
  expect(host.ran).toEqual(['schema snapshot'])
  expect(await shows(line, /^✓ · 8 entities$/)).toBe(true)
})

test('Rewire runs khub wire and clears the schema line', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, F.schema_diff_pending, as(F.schema_snapshot, ['wire'])])

  on('tool.call', () => ({ result: {} }))
  engineDraws(on)
  await $.session.start(START)
  await $.tool.call(ONTOLOGY)

  const line = await band($)

  host.ran.length = 0
  await line.press({ key: 'rewire' })

  expect(host.ran).toEqual(['wire'])
  expect(await shows(line, /^✓ · 8 entities$/)).toBe(true)
})

test('a schema edit with no snapshot to compare against says so and offers a snapshot', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, F.schema_diff_none])

  on('tool.call', () => ({ result: {} }))
  engineDraws(on)
  await $.session.start(START)

  const ran = await $.tool.call(ONTOLOGY)

  expect(ran.context).toEqual(['khub schema: no snapshot exists to compare against. `khub schema snapshot` records one.'])
  const line = await band($)

  expect(await colorOf(line, /^schema · no snapshot yet$/)).toBe('yellow')
  expect(await keys(line)).toEqual(['snapshot', 'hide'])
  expect(host.logs).toEqual([])
})

test('a schema edit that breaks the schema says so on the band, the status line and to the model', async ($, on) => {
  const broken = as(F.get_missing, ['schema', '--format', 'json'])

  // The session starts on the recorded schema, and the edit leaves one khub refuses.
  const host = fakeHost(on, [broken, F.check_base, F.status_base])

  on('tool.call', () => ({ result: {} }))
  engineDraws(on)
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call(ONTOLOGY)

  // No diff is asked of a schema that does not resolve.
  expect(host.ran).toEqual(['schema --format json', 'check --format json', 'status --format json'])
  expect(ran.context).toEqual(["khub: the schema no longer resolves. schema: No entity 'nope' found"])
  expect(host.statuses.at(-1)).toBe("schema: No entity 'nope' found")
  const line = await band($)

  // khub's complaint is the line, and neither schema action runs on a broken schema.
  expect(await colorOf(line, /^schema: No entity 'nope' found$/)).toBe('red')
  expect(await keys(line)).toEqual(['hide'])
})

test('a Write of a new schema layer file is not refused as an entity file', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, F.schema_diff_clean])

  on('tool.call', () => ({ result: {} }))
  await $.session.start(START)
  host.ran.length = 0

  const ran = await $.tool.call({ tool: 'Write', file_path: `${ROOT}/.khub/policy.yaml`, content: 'gates: {}\n' })

  expect(ran.deny).toBe(undefined)
  expect(ran.context).toBe(undefined)
  expect(host.ran).toEqual(['schema --format json', 'schema diff --format json', 'validate --format json', 'check --format json', 'status --format json'])
})

// 12. Doctor

test('a workspace that needs nothing shows no doctor line', async ($, on) => {
  const { ui } = await session($, on)

  await ui.press({ key: 'tab-health' })

  expect(await keys(await band($))).toEqual([])
  expect(await shows(ui, /^WORKSPACE$/)).toBe(false)
})

test('the upgrade preview is asked at most once a day', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base], { now: 2 * DAY })
  const previews = () => host.upkeep.filter(call => call.startsWith('upgrade')).length

  await started($, host)

  expect(previews()).toBe(1)

  await $.command.run(slash('khub', 'doctor'))

  expect(previews()).toBe(1)

  await host.clock.advance(DAY + 1)
  await $.command.run(slash('khub', 'doctor'))

  expect(previews()).toBe(2)
})

test('a workspace with no stored preview is asked for one, whatever the clock reads', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  await started($, host)

  expect(host.upkeep).toEqual(['query --draft --format json', 'upgrade --dry-run --format json'])
})

test('a pending preset upgrade and its drift show in Health', async ($, on) => {
  const plan = parseJson(F.upgrade_dry.stdout) as Record<string, unknown>
  const ahead = { ...F.upgrade_dry, stdout: JSON.stringify({ ...plan, version_to: '0.7.0', schema_drift: ['types.adr'] }) }
  const host = fakeHost(on, [F.check_base, F.status_base, ahead], { now: 2 * DAY })

  engineDraws(on)
  await started($, host)

  const ui = await pane($)

  await ui.press({ key: 'tab-health' })

  expect(await shows(ui, /^preset build-hub 0\.6\.0 → 0\.7\.0$/)).toBe(true)
  expect(await shows(ui, /^types\.adr$/)).toBe(true)

  const line = await band($)

  // A real upgrade stays a khub command, so the line offers nothing to press.
  expect(await shows(line, /^preset build-hub 0\.6\.0 → 0\.7\.0$/)).toBe(true)
  expect(await keys(line)).toEqual(['hide'])
  expect(host.logs).toEqual([])
})

// 13. Drafts

const DRAFTS = as(F.query_components, json(['query', '--draft']))

test('Health lists the drafts, and publish clears the flag', async ($, on) => {
  const publish = ['edit', SEARCH, 'draft', 'false']
  const { host, ui } = await session($, on, [DRAFTS, as(F.edit_component, json(publish)), as(F.validate_adr, json(['validate', SEARCH]))])

  await ui.press({ key: 'tab-health' })

  expect(await shows(ui, /^DRAFTS$/)).toBe(true)
  expect(await shows(ui, new RegExp(`^${SEARCH}$`))).toBe(true)
  expect(await keys(ui)).toContain('publish-1')

  await ui.press({ key: 'publish-0' })

  expect(host.ran.slice(0, 4)).toEqual([line(publish), ...AFTER(SEARCH)])

  // The refresh after the write reads the drafts again, and none are left.
  expect(host.upkeep).toEqual(['query --draft --format json'])
  expect(await ui.find({ key: 'publish-0' })).toBe(undefined)
})

test('publishing a draft from Health leaves the pane on Health', async ($, on) => {
  const publish = ['edit', SEARCH, 'draft', 'false']
  const { ui } = await session($, on, [DRAFTS, as(F.edit_component, json(publish)), as(F.validate_adr, json(['validate', SEARCH]))])

  await ui.press({ key: 'tab-health' })
  await ui.press({ key: 'publish-0' })

  expect(await ui.find({ key: 'tab-health' })).toBe(undefined)
  expect(await ui.find({ key: 'tab-browse' })).toBeDefined()
})

test('a publish khub refuses is said in Health', async ($, on) => {
  const publish = ['edit', SEARCH, 'draft', 'false']
  const { ui } = await session($, on, [DRAFTS, as(F.remove_refused, json(publish))])

  await ui.press({ key: 'tab-health' })
  await ui.press({ key: 'publish-0' })

  expect(await shows(ui, /^Refusing to remove component 'cmp-search'/)).toBe(true)
})

test('remove on a draft in Health asks to remove that draft', async ($, on) => {
  const { host, ui } = await session($, on, [DRAFTS, as(F.remove_actor, json(['remove', STORAGE]))])

  await ui.press({ key: 'tab-health' })
  await ui.press({ key: 'remove-1' })

  const form = await pane($, 'khub-form')

  expect(await shows(form, new RegExp(`^Remove ${STORAGE}$`))).toBe(true)

  await form.press({ key: 'submit' })

  expect(host.ran[0]).toBe(line(['remove', STORAGE]))
})

test('remove on a draft never removes the entity Browse has open', async ($, on) => {
  const { host, ui } = await session($, on, [GET, NEIGHBORS, F.query_components, DRAFTS, as(F.remove_actor, json(['remove', STORAGE]))])

  await ui.press({ key: 'tab-browse' })
  await ui.press({ key: 'type-component' })
  await ui.press({ key: `entity-${SEARCH}` })
  await ui.press({ key: 'tab-health' })
  host.ran.length = 0
  await ui.press({ key: 'remove-1' })

  const form = await pane($, 'khub-form')

  await form.press({ key: 'submit' })

  expect(host.ran).not.toContain(line(['remove', SEARCH]))
})

// A write khub could not run

test('a write khub could not run is said in Browse', async ($, on) => {
  const { host, ui } = await atSearch($, on)

  await ui.press({ key: 'unlink-0' })

  expect(host.ran).toEqual([line(['unlink', SEARCH, 'depends_on', STORAGE])])
  expect(await colorOf(ui, /^khub did not run: /)).toBe('red')
})

// The sidebar

test('under sidebar wide the pane is asked for at the start', { options: { sidebar: 'wide' } }, async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  await started($, host)

  expect(host.opened).toEqual(['khub'])
})

test('under the default sidebar the pane waits to be asked for', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base])

  await started($, host)

  expect(host.opened).toEqual([])
})

// Review fixes

test('a form whose edit khub refuses removes no edge', async ($, on) => {
  const { host, ui } = await atSearch($, on, [as(F.get_missing, json(['edit', SEARCH, '--description=--x']))])

  await ui.press({ key: 'edit' })

  const form = await pane($, 'khub-form')

  await form.input({ key: 'field-description', text: '--x' })
  await form.input({ key: 'field-depends_on', text: '' })
  host.ran.length = 0
  await form.press({ key: 'submit' })

  // The dash-led value rides in the flag's own argument, and the unlink waits for the edit.
  expect(host.ran).toEqual([line(['edit', SEARCH, '--description=--x'])])
  expect(host.closed).toEqual([])
})

test('/khub doctor reads the workspace again and prints what it needs', async ($, on) => {
  const plan = parseJson(F.upgrade_dry.stdout) as Record<string, unknown>
  const ahead = { ...F.upgrade_dry, stdout: JSON.stringify({ ...plan, version_to: '0.7.0', schema_drift: ['types.adr'] }) }
  const host = fakeHost(on, [F.check_base, F.status_base, ahead], { now: 2 * DAY })

  await started($, host)

  const ran = await $.command.run(slash('khub', 'doctor'))

  expect(ran.text).toBe(
    ['doctor', 'preset build-hub 0.6.0 → 0.7.0, applied by `khub upgrade`', 'schema drift types.adr'].join('\n'),
  )
})
