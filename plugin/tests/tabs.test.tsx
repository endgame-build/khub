import { expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'
import type { On, RenderElement } from 'claude-code'

import type { KhubEntity, KhubHealth, KhubStats } from '../types'
import type { El } from '../hooks/el'
import type { PaneActions, PaneModel } from '../hooks/pane'
import { EMPTY_DOCTOR, EMPTY_HEALTH, EMPTY_STATS, EMPTY_UI, EMPTY_VIEW } from '../hooks/state'
import { browseTab } from '../hooks/tabs/browse'
import { healthTab } from '../hooks/tabs/health'
import { searchTab } from '../hooks/tabs/search'
import { statsRows, statsTab } from '../hooks/tabs/stats'
import { fakeHost } from './helpers'

type Surface = 'terminal' | 'desktop' | 'mobile'
type View = (el: El, actions: PaneActions) => RenderElement

const PANE = { title: 'view', isFocused: false, bodyColumns: 50, placement: 'dock' as const, scroll: { offset: 0, bodyRows: 30 }, view: {} }

const MODEL: PaneModel = {
  columns: 50,
  rows: 30,
  workspace: null,
  types: [],
  ui: EMPTY_UI,
  ledger: [],
  health: EMPTY_HEALTH,
  stats: EMPTY_STATS,
  allStats: null,
  view: EMPTY_VIEW,
  doctor: EMPTY_DOCTOR,
}

const type = (name: string) => ({ name, layout: 'file', format: 'md', path: name, id_shape: null, when: null, fields: [], relations: [] })

const SEARCH: KhubEntity = {
  id: 'component/cmp-search',
  type: 'component',
  slug: 'cmp-search',
  path: 'knowledge/components/cmp-search.md',
  isDraft: false,
  fields: [
    { name: 'title', value: 'Search' },
    { name: 'kind', value: 'service' },
    { name: 'owner', value: 'team-platform' },
    { name: 'lifecycle', value: 'production' },
    { name: 'tier', value: 'tier-1' },
  ],
  edges: [
    { predicate: 'depends_on', id: 'component/cmp-storage', direction: 'out' },
    { predicate: 'served_by', id: 'use-case/uc-find-a-document', direction: 'in' },
    { predicate: 'served_by', id: 'use-case/uc-other', direction: 'in' },
    { predicate: 'affects', id: 'adr/ad-1', direction: 'in' },
  ],
  body: '## Responsibilities\n\nFull-text search.',
}

// Draws a view from the test's own hook, beneath the mod, so the surface validates the
// tree and a press reaches the actions. Returns what the acts ran.
function stage(on: On, view: View) {
  const ran: unknown[][] = []
  const actions = new Proxy({} as PaneActions, {
    get:
      (_, name) =>
      (...args: unknown[]) =>
        void ran.push([name, ...args]),
  })

  fakeHost(on)
  on('ui.render', ($, e) => view($.ui.resolve(e), actions))

  return ran
}

const mount = <P extends Surface>($: Engine, surface: P) =>
  $.ui.mount({ plugin: 'test', surface, component: 'Pane', requestId: 'view', props: PANE })

const browsing = (browse: PaneModel['ui']['browse'], view: Partial<PaneModel['view']> = {}): PaneModel => ({
  ...MODEL,
  types: [type('adr'), type('component')],
  health: { ...EMPTY_HEALTH, counts: { counts: { adr: 1, component: 2 }, total: 3, draft: 0, active: 3, orphan: 0, stale: 0, stray: 0, malformed: 0 } },
  ui: { ...EMPTY_UI, tab: 'browse', browse },
  view: { ...EMPTY_VIEW, ...view },
})

// --- Browse

test('Browse opens on the types, each with its count', async ($, on) => {
  const ran = stage(on, (el, actions) => browseTab(el, browsing({ type: null, id: null }), actions))

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await mount($, surface)

    expect(await ui.find({ type: 'Text', text: /^TYPES$/ })).toBeDefined()
    expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual(['type-adr', 'type-component'])
    expect((await ui.findAll({ type: 'Text', text: /^\d+$/ })).map(text => text.text)).toEqual(['1', '2'])

    await ui.press({ key: 'type-component' })
    await ui.unmount()
  }

  expect(ran).toEqual([
    ['browseType', 'component'],
    ['browseType', 'component'],
  ])
})

test('a type lists its entities, with a way back and two ways to add', async ($, on) => {
  const entities = [
    { id: 'component/cmp-search', title: 'Search', flags: [], note: '' },
    { id: 'component/cmp-stripe', title: 'Stripe', flags: ['draft', 'orphan'], note: '' },
  ]
  const ran = stage(on, (el, actions) => browseTab(el, browsing({ type: 'component', id: null }, { entities }), actions))
  const ui = await mount($, 'terminal')

  expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual([
    'back',
    'add',
    'add-claude',
    'entity-component/cmp-search',
    'entity-component/cmp-stripe',
  ])
  expect((await ui.find({ type: 'Text', text: /^draft orphan$/ }))?.props.color).toBe('yellow')

  await ui.press({ key: 'back' })
  await ui.press({ key: 'add' })
  await ui.press({ key: 'add-claude' })
  await ui.press({ key: 'entity-component/cmp-stripe' })

  expect(ran).toEqual([
    ['browseType', null],
    ['add', 'component'],
    ['withClaude', 'Add a component: '],
    ['browseEntity', 'component/cmp-stripe'],
  ])
})

test('a type with no entities says so', async ($, on) => {
  stage(on, (el, actions) => browseTab(el, browsing({ type: 'adr', id: null }), actions))

  const ui = await mount($, 'terminal')

  expect((await ui.find({ type: 'Text', text: /^No entities\.$/ }))?.props.dimColor).toBe(true)
})

test('an entity shows four fields, its edges both ways, its actions and its body', async ($, on) => {
  const ran = stage(on, (el, actions) =>
    browseTab(el, browsing({ type: 'component', id: SEARCH.id }, { entity: SEARCH }), actions),
  )

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await mount($, surface)

    expect(await ui.find({ type: 'Text', text: /^title Search · kind service · owner team-platform · lifecycle production$/ })).toBeDefined()
    expect((await ui.findAll({ type: 'Text', text: /^EDGES (OUT|IN)$/ })).map(text => text.text)).toEqual(['EDGES OUT', 'EDGES IN'])
    expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual([
      'back',
      'edge-0',
      'unlink-0',
      'edge-1',
      'impact-served_by',
      'edge-2',
      'edge-3',
      'impact-affects',
      'edit',
      'link',
      'draft',
      'remove',
      'edit-claude',
      'body-claude',
      'insert',
      'copy',
    ])
    expect((await ui.find({ type: 'Button', key: 'draft' }))?.props.label).toBe('Mark as draft')
    expect((await ui.find({ type: 'Markdown' }))?.props.text).toBe(SEARCH.body)
    await ui.unmount()
  }

  ran.length = 0

  const ui = await mount($, 'terminal')

  for (const key of ['back', 'edge-0', 'unlink-0', 'impact-served_by', 'edit', 'link', 'draft', 'remove', 'edit-claude', 'body-claude', 'insert', 'copy']) {
    await ui.press({ key })
  }

  expect(ran).toEqual([
    ['browseType', 'component'],
    ['browseEntity', 'component/cmp-storage'],
    ['unlink', SEARCH.id, 'depends_on', 'component/cmp-storage'],
    ['impact', SEARCH.id, 'served_by'],
    ['edit', SEARCH.id],
    ['link', SEARCH.id],
    ['setDraft', SEARCH.id, true],
    ['remove', SEARCH.id],
    ['withClaude', `Edit ${SEARCH.id}: `],
    ['withClaude', `Write the body of ${SEARCH.id}: `],
    ['insertId', SEARCH.id],
    ['copyId', SEARCH.id],
  ])
})

test('a draft is marked, offers to publish, and an impact tree draws under the edges', async ($, on) => {
  const draft = { ...SEARCH, isDraft: true, body: '' }
  const tree = 'component/cmp-search\n  use-case/uc-find-a-document'

  stage(on, (el, actions) => browseTab(el, browsing({ type: 'component', id: draft.id }, { entity: draft, tree }), actions))

  const ui = await mount($, 'terminal')

  expect((await ui.find({ type: 'Text', text: /^draft$/ }))?.props.color).toBe('yellow')
  expect((await ui.find({ type: 'Button', key: 'draft' }))?.props.label).toBe('Publish')
  expect((await ui.find({ type: 'Text', text: /uc-find-a-document$/ }))?.props.dimColor).toBe(true)
  expect(await ui.find({ type: 'Markdown' })).toBe(undefined)
})

test('an entity that has not arrived says loading, and a problem shows in red at any level', async ($, on) => {
  let model = browsing({ type: 'component', id: 'component/cmp-other' }, { entity: SEARCH })

  stage(on, (el, actions) => browseTab(el, model, actions))

  const waiting = await mount($, 'terminal')

  expect((await waiting.find({ type: 'Text', text: /^loading…$/ }))?.props.dimColor).toBe(true)
  expect(await waiting.find({ type: 'Button', key: 'edit' })).toBe(undefined)
  await waiting.unmount()

  model = browsing({ type: null, id: null }, { problem: "No entity 'nope' found" })

  const refused = await mount($, 'terminal')

  expect((await refused.find({ type: 'Text', text: /^No entity 'nope' found$/ }))?.props.color).toBe('red')
  expect(await refused.find({ type: 'Text', text: /^TYPES$/ })).toBeDefined()
})

// --- Search

const HITS = [
  { id: 'adr/ad-1', title: 'Use RE2 patterns', flags: [], note: 'match 1.000' },
  { id: 'component/cmp-search', title: 'Search', flags: [], note: 'match 0.412' },
]

test('Search takes a query and lists its hits, each opening in Browse', async ($, on) => {
  const model: PaneModel = { ...MODEL, ui: { ...EMPTY_UI, tab: 'search', query: 'regex' }, view: { ...EMPTY_VIEW, hits: HITS } }
  const ran = stage(on, (el, actions) => searchTab(el, model, actions))

  for (const surface of ['terminal', 'desktop'] as const) {
    const ui = await mount($, surface)

    expect((await ui.find({ type: 'Input', key: 'search' }))?.props).toEqual({ key: 'search', placeholder: 'search', value: 'regex' })
    expect((await ui.findAll({ type: 'Button' })).map(button => button.props.label)).toEqual(['adr/ad-1', 'component/cmp-search'])
    expect((await ui.findAll({ type: 'Text', text: /^match / })).map(text => text.text)).toEqual(['match 1.000', 'match 0.412'])
    expect(await ui.find({ type: 'Text', text: /^ {2}Use RE2 patterns$/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /^2 hits$/ })).toBeDefined()

    await ui.input({ key: 'search', text: 'patterns' })
    await ui.press({ key: 'hit-1' })
    await ui.unmount()
  }

  expect(ran).toEqual([
    ['search', 'patterns'],
    ['browseEntity', 'component/cmp-search'],
    ['search', 'patterns'],
    ['browseEntity', 'component/cmp-search'],
  ])
})

test('before a query Search is only the field, and a note from khub shows above the hits', async ($, on) => {
  let model: PaneModel = { ...MODEL, ui: { ...EMPTY_UI, tab: 'search' } }

  stage(on, (el, actions) => searchTab(el, model, actions))

  const empty = await mount($, 'terminal')

  expect(await empty.find({ type: 'Text', text: /hits?$/ })).toBe(undefined)
  await empty.unmount()

  model = { ...model, ui: { ...model.ui, query: 'zzzz' }, view: { ...EMPTY_VIEW, note: 'note: no hits in 8 entities searched' } }

  const noted = await mount($, 'terminal')

  expect((await noted.find({ type: 'Text', text: /^note: no hits/ }))?.props.dimColor).toBe(true)
  expect(await noted.find({ type: 'Text', text: /^0 hits$/ })).toBeDefined()
})

test('a surface with no text field says search needs one', async ($, on) => {
  stage(on, (el, actions) => searchTab(el, { ...MODEL, ui: { ...EMPTY_UI, tab: 'search' } }, actions))

  const ui = await mount($, 'mobile')

  expect(await ui.find({ type: 'Input' })).toBe(undefined)
  expect(await ui.find({ type: 'Text', text: /needs a text field/ })).toBeDefined()
})

// --- Stats

const STATS: KhubStats = {
  calls: 41,
  reads: 33,
  writes: 8,
  byVerb: { query: 14, get: 11, search: 8, add: 5, link: 2, check: 1, status: 1 },
  refused: { unknown_type: 2, alias_taken: 1 },
  searches: 8,
  searchEmpty: 2,
  searchTruncated: 1,
  bytes: 37_600,
  bypass: 6,
  durations: [20, 38, 38, 40, 120],
  slowest: { verb: 'check', ms: 210 },
}

const CHECKED: KhubHealth = {
  ...EMPTY_HEALTH,
  checkMs: 41,
  counts: { counts: {}, total: 146, draft: 0, active: 146, orphan: 0, stale: 0, stray: 0, malformed: 0 },
}

test('the stats rows name what happened and leave out what did not', () => {
  expect(statsRows(STATS, CHECKED)).toEqual([
    ['calls', '41', 'read 33 · write 8'],
    ['verbs', '', 'query 14 · get 11 · search 8 · add 5 · link 2 · check 1'],
    ['refused', '3', 'unknown_type 2 · alias_taken 1'],
    ['search', '8', '2 empty · 1 truncated'],
    ['context', '', 'est. 9.4k tokens of khub output'],
    ['bypass', '6', 'direct reads of entity files'],
    ['time', '', 'p50 38 ms · p95 120 ms · slowest check 210 ms'],
    ['check', '', '41 ms at 146 entities'],
  ])
  expect(statsRows({ ...EMPTY_STATS, calls: 1, reads: 1, byVerb: { status: 1 } }, EMPTY_HEALTH)).toEqual([
    ['calls', '1', 'read 1 · write 0'],
    ['verbs', '', 'status 1'],
  ])
})

test('Stats shows this session, and its button switches to all sessions', async ($, on) => {
  let model: PaneModel = { ...MODEL, ui: { ...EMPTY_UI, tab: 'stats' }, stats: STATS, health: CHECKED }
  const ran = stage(on, (el, actions) => statsTab(el, model, actions))
  const session = await mount($, 'terminal')

  expect(await session.find({ type: 'Text', text: /^THIS SESSION$/ })).toBeDefined()
  expect((await session.find({ type: 'Button', key: 'scope' }))?.props.label).toBe('All sessions')
  expect(await session.find({ type: 'Text', text: /^read 33 · write 8$/ })).toBeDefined()

  await session.press({ key: 'scope' })
  await session.unmount()

  model = { ...model, ui: { ...model.ui, statsScope: 'all' }, allStats: { ...STATS, calls: 400, reads: 300, writes: 100 } }

  const all = await mount($, 'desktop')

  expect(await all.find({ type: 'Text', text: /^ALL SESSIONS$/ })).toBeDefined()
  expect(await all.find({ type: 'Text', text: /^read 300 · write 100$/ })).toBeDefined()

  await all.press({ key: 'scope' })
  expect(ran).toEqual([
    ['statsScope', 'all'],
    ['statsScope', 'session'],
  ])
})

test('all sessions falls back to this one until a tally exists, and no calls says so', async ($, on) => {
  let model: PaneModel = { ...MODEL, ui: { ...EMPTY_UI, tab: 'stats', statsScope: 'all' }, stats: STATS }

  stage(on, (el, actions) => statsTab(el, model, actions))

  const fallback = await mount($, 'terminal')

  expect(await fallback.find({ type: 'Text', text: /^read 33 · write 8$/ })).toBeDefined()
  await fallback.unmount()

  model = { ...MODEL, ui: { ...EMPTY_UI, tab: 'stats' } }

  const none = await mount($, 'terminal')

  expect((await none.find({ type: 'Text', text: /^No khub calls yet\.$/ }))?.props.dimColor).toBe(true)
})

// --- Health, the sections stage 2 adds

test('Health lists the drafts, each with publish and remove', async ($, on) => {
  const drafts = [{ id: 'actor/act-operator', title: 'Operator', flags: ['draft'], note: '' }]
  const ran = stage(on, (el, actions) => healthTab(el, { ...MODEL, view: { ...EMPTY_VIEW, drafts } }, actions))
  const ui = await mount($, 'terminal')

  expect(await ui.find({ type: 'Text', text: /^DRAFTS$/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /^actor\/act-operator$/ })).toBeDefined()

  await ui.press({ key: 'publish-0' })
  await ui.press({ key: 'remove-0' })
  expect(ran).toEqual([
    ['setDraft', 'actor/act-operator', false],
    ['remove', 'actor/act-operator'],
  ])
})

test('Health shows what the workspace needs, and nothing when it needs nothing', async ($, on) => {
  let model: PaneModel = {
    ...MODEL,
    doctor: { upgrade: 'preset build-hub 0.5.0 → 0.6.0', drift: ['types.adr'] },
  }
  const ran = stage(on, (el, actions) => healthTab(el, model, actions))
  const needy = await mount($, 'terminal')

  expect(await needy.find({ type: 'Text', text: /^WORKSPACE$/ })).toBeDefined()
  expect(await needy.find({ type: 'Text', text: /^preset build-hub 0\.5\.0 → 0\.6\.0$/ })).toBeDefined()
  expect((await needy.find({ type: 'Text', text: /^types\.adr$/ }))?.props.dimColor).toBe(true)
  await needy.unmount()

  model = MODEL

  const fine = await mount($, 'terminal')

  expect(await fine.find({ type: 'Text', text: /^(WORKSPACE|DRAFTS)$/ })).toBe(undefined)
})
