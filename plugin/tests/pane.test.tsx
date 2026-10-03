import { expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'
import type { On, RenderElement } from 'claude-code'

import type { KhubFinding, KhubHealth, KhubLedgerEntry, KhubTab } from '../types'
import type { El } from '../hooks/el'
import { paneBody, TABS } from '../hooks/pane'
import type { PaneActions, PaneModel } from '../hooks/pane'
import { EMPTY_DOCTOR, EMPTY_HEALTH, EMPTY_STATS, EMPTY_UI, EMPTY_VIEW } from '../hooks/state'
import { buckets, countsLine, cutAtLine, healthTab } from '../hooks/tabs/health'
import { ledgerCounts, sessionTab } from '../hooks/tabs/session'
import { F } from './fixtures'
import { fakeHost, START } from './helpers'

type Surface = 'terminal' | 'desktop'
type View = (el: El, actions: PaneActions) => RenderElement

const SURFACES = ['terminal', 'desktop'] as const
const ADR = 'adr/ad-2026-01-15-use-re2-patterns'
const REQ = 'requirement/req-search-answers-within-300-ms'

const pane = (title: string) => ({
  title,
  isFocused: false,
  bodyColumns: 50,
  placement: 'dock' as const,
  scroll: { offset: 0, bodyRows: 30 },
  view: {},
})

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

const entry = (op: KhubLedgerEntry['op'], id: string, rest: Partial<KhubLedgerEntry> = {}): KhubLedgerEntry => ({
  op,
  id,
  detail: '',
  by: 'agent',
  since: 't1',
  turn: 't1',
  errors: 0,
  gaps: 0,
  lenses: [],
  ...rest,
})

const finding = (bucket: string, id: string, message: string, isError = true): KhubFinding => ({
  key: `${bucket}|${id}|${message}`,
  bucket,
  id,
  message,
  isError,
})

// Draws a view from the test's own hook, beneath the mod, so the surface validates the
// tree and a press reaches the actions. Returns what the presses ran.
function stage(on: On, view: View) {
  const ran: unknown[][] = []

  // Every action records its name and what it was called with.
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

const mount = ($: Engine, surface: Surface) =>
  $.ui.mount({ plugin: 'test', surface, component: 'Pane', requestId: 'view', props: pane('view') })

// Every string a tree shows, in document order: one entry per Text or Button.
function lines(node: RenderElement | string): string[] {
  if (typeof node === 'string') return [node]
  if (node.type === 'Button') return [`[${node.props.label}]`]
  if (node.type === 'Code') return [node.props.source]

  const children = ((node as { children?: Array<RenderElement | string> }).children ?? []).flatMap(lines)

  return node.type === 'Text' ? [children.join('')] : children
}

// --- the pane and its tab row

test('the tab row marks the active tab and offers the other four', async ($, on) => {
  const ran = stage(on, (el, actions) => paneBody(el, MODEL, actions))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)

    expect((await ui.find({ type: 'Text', text: /^ Session $/ }))?.props).toEqual({ bold: true, inverse: true })
    expect((await ui.findAll({ type: 'Button' })).map(button => button.props)).toEqual([
      { key: 'tab-health', label: 'Health', hotkey: '2', plain: true },
      { key: 'tab-browse', label: 'Browse', hotkey: '3', plain: true },
      { key: 'tab-search', label: 'Search', hotkey: '4', plain: true },
      { key: 'tab-stats', label: 'Stats', hotkey: '5', plain: true },
    ])
    expect(await ui.find({ type: 'Text', text: /^No khub writes this session yet\.$/ })).toBeDefined()

    await ui.press({ key: 'tab-browse' })
    await ui.unmount()
  }

  expect(ran).toEqual([
    ['tab', 'browse'],
    ['tab', 'browse'],
  ])
})

test('every tab has a place in the row and a body of its own', async ($, on) => {
  expect(TABS.map(({ tab, hotkey }) => `${hotkey} ${tab}`)).toEqual(['1 session', '2 health', '3 browse', '4 search', '5 stats'])

  const tabs: KhubTab[] = ['browse', 'search', 'stats']
  let tab: KhubTab = 'browse'

  stage(on, (el, actions) => paneBody(el, { ...MODEL, ui: { ...EMPTY_UI, tab } }, actions))

  for (tab of tabs) {
    const ui = await mount($, 'terminal')

    expect(await ui.find({ type: 'Text', text: /^Not built yet\.$/ })).toBe(undefined)
    expect(await ui.find({ type: 'Text', text: new RegExp(`^ ${tab[0]?.toUpperCase()}${tab.slice(1)} $`) })).toBeDefined()
    expect(await ui.find({ type: 'Button', key: `tab-${tab}` })).toBe(undefined)
    expect(await ui.find({ type: 'Button', key: 'tab-session' })).toBeDefined()
    await ui.unmount()
  }
})

// --- the Session tab

const LEDGER: KhubLedgerEntry[] = [
  entry('add', ADR, { gaps: 1, lenses: [{ code: 'alternatives', name: 'A real alternative, not a strawman' }] }),
  entry('link', ADR, { detail: 'affects cmp-search' }),
  entry('add', REQ, { errors: 2, gaps: 1 }),
  entry('edit', 'component/cmp-search', { detail: 'lifecycle deprecated' }),
  entry('unlink', ADR, { detail: 'affects cmp-search' }),
  entry('remove', 'actor/act-temp'),
]

test('the ledger counts leave out what did not happen', () => {
  expect(ledgerCounts([])).toBe('')
  expect(ledgerCounts(LEDGER)).toBe(' · 2 added · 1 edited · 2 edges · 1 removed')
  expect(ledgerCounts([entry('link', ADR)])).toBe(' · 1 edge')
})

test('the Session tab lists what the session wrote, each with its result', async ($, on) => {
  stage(on, (el, actions) => sessionTab(el, { ...MODEL, ledger: LEDGER }, actions))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)

    expect(lines(await ui.drawn())).toEqual([
      'THIS SESSION · 2 added · 1 edited · 2 edges · 1 removed',
      '+ ',
      ADR,
      '1 gap',
      `→ ${ADR} affects cmp-search`,
      '+ ',
      REQ,
      '2 errors',
      '~ ',
      'component/cmp-search',
      '✓',
      `⇢ ${ADR} affects cmp-search`,
      '− actor/act-temp',
      `LENSES ${ADR}`,
      'alternatives',
      'A real alternative, not a strawman',
      '[Ask Claude to answer]',
    ])

    expect((await ui.find({ type: 'Text', text: /^1 gap$/ }))?.props).toEqual({ color: 'yellow' })
    expect((await ui.find({ type: 'Text', text: /^2 errors$/ }))?.props).toEqual({ color: 'red' })
    expect((await ui.find({ type: 'Text', text: /^✓$/ }))?.props).toEqual({ color: 'green' })
    expect((await ui.find({ type: 'Text', text: /^\+ $/ }))?.props).toEqual({ color: 'green' })
    expect((await ui.find({ type: 'Text', text: /^⇢/ }))?.props).toEqual({ wrap: 'truncate-end', dimColor: true })
    expect((await ui.find({ type: 'Text', text: /^THIS SESSION/ }))?.props).toEqual({ dimColor: true })
    await ui.unmount()
  }
})

test('an id is the part of its row that truncates', async ($, on) => {
  stage(on, (el, actions) => sessionTab(el, { ...MODEL, ledger: [entry('add', ADR, { gaps: 1 })] }, actions))

  const ui = await mount($, 'terminal')
  const row = (await ui.findAll({ type: 'Box' })).find(box => box.props.justifyContent === 'space-between')

  expect(row).toBeDefined()
  expect((await ui.find({ type: 'Text', text: new RegExp(`^${ADR}$`) }))?.props).toEqual({ wrap: 'truncate-end' })
  expect((await ui.findAll({ type: 'Box' })).map(box => box.props.flexShrink).filter(shrink => shrink !== undefined)).toEqual([1, 0])
})

test('the lenses button asks about its own entity', async ($, on) => {
  const ran = stage(on, (el, actions) => sessionTab(el, { ...MODEL, ledger: LEDGER }, actions))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)

    expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual([`lenses-${ADR}`])
    await ui.press({ key: `lenses-${ADR}` })
    await ui.unmount()
  }

  expect(ran).toEqual([
    ['answerLenses', ADR],
    ['answerLenses', ADR],
  ])
})

test('an empty ledger says so', async ($, on) => {
  stage(on, (el, actions) => sessionTab(el, MODEL, actions))

  expect(lines(await (await mount($, 'terminal')).drawn())).toEqual(['No khub writes this session yet.'])
})

// --- the Health tab

const DANGLING = finding('dangling', REQ, 'predicate: realized_in, target: cmp-serach')
const INCOMPLETE = finding('incomplete', 'component/cmp-stripe', 'missing_fields: kind')
const ORPHAN = finding('orphans', REQ, 'orphans', false)

const FAILING: KhubHealth = {
  ...EMPTY_HEALTH,
  passed: false,
  counts: { counts: {}, total: 146, draft: 3, active: 143, orphan: 2, stale: 0, stray: 0, malformed: 0 },
  findings: [ORPHAN, DANGLING, INCOMPLETE],
  baseline: [INCOMPLETE.key],
}

const health = (value: KhubHealth, diff: string | null = null): View => (el, actions) =>
  healthTab(el, { ...MODEL, health: value, ui: { ...EMPTY_UI, tab: 'health', diff } }, actions)

test('the counts line always names the entities and leaves out the zeros', () => {
  const counts = { counts: {}, total: 8, draft: 0, active: 8, orphan: 0, stale: 0, stray: 0, malformed: 0 }

  expect(countsLine(counts)).toBe('8 entities')
  expect(countsLine({ ...counts, total: 146, draft: 3, orphan: 2, stale: 6 })).toBe('146 entities · 3 draft · 2 orphan · 6 stale')
  expect(countsLine({ ...counts, total: 0, stale: 1 })).toBe('0 entities · 1 stale')
})

test('buckets keep first-seen order, with the error buckets first', () => {
  expect(buckets([ORPHAN, DANGLING, INCOMPLETE, finding('dangling', ADR, 'x')]).map(group => [group.bucket, group.rows.length])).toEqual([
    ['dangling', 2],
    ['incomplete', 1],
    ['orphans', 1],
  ])
  expect(buckets([])).toEqual([])
})

test('a diff is cut at a line end to what the Code element takes', () => {
  const diff = Array.from({ length: 3000 }, (_, i) => `+line ${i}`).join('\n')
  const cut = cutAtLine(diff)

  expect(cutAtLine('--- a\n+++ b')).toBe('--- a\n+++ b')
  expect(cut.length <= 10_000).toBe(true)
  expect(diff.startsWith(`${cut}\n`)).toBe(true)
  expect(cutAtLine('x'.repeat(30), 10)).toBe('x'.repeat(10))
})

test('before the first check the Health tab says it is checking', async ($, on) => {
  stage(on, health(EMPTY_HEALTH))

  const ui = await mount($, 'terminal')

  expect(lines(await ui.drawn())).toEqual(['CHECK ', 'checking…', '[Run check]', '[Reindex]'])
  expect((await ui.find({ type: 'Text', text: /^checking…$/ }))?.props).toEqual({ dimColor: true })
})

test('a passing check shows the verdict and the counts', async ($, on) => {
  stage(on, health({ ...FAILING, passed: true, findings: [], baseline: [] }))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)

    expect(lines(await ui.drawn())).toEqual([
      'CHECK ',
      '✓ passing',
      '146 entities · 3 draft · 2 orphan',
      '[Run check]',
      '[Reindex]',
    ])
    expect((await ui.find({ type: 'Text', text: /^✓ passing$/ }))?.props).toEqual({ color: 'green' })
    await ui.unmount()
  }
})

test('a failing check lists its findings by bucket and marks the new ones', async ($, on) => {
  stage(on, health(FAILING))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)

    expect(lines(await ui.drawn())).toEqual([
      'CHECK ',
      '✗ failing · 2 new this session',
      '146 entities · 3 draft · 2 orphan',
      'dangling  1',
      `  ${REQ}`,
      'predicate: realized_in, target: cmp-serach',
      'new',
      '[fix]',
      'incomplete  1',
      '  component/cmp-stripe',
      'missing_fields: kind',
      '[fix]',
      'orphans  1  informational',
      `  ${REQ}`,
      'orphans',
      'new',
      '[Run check]',
      '[Reindex]',
    ])

    expect((await ui.find({ type: 'Text', text: /^✗ failing/ }))?.props).toEqual({ color: 'red' })
    expect((await ui.find({ type: 'Text', text: /^dangling {2}1$/ }))?.props).toEqual({ color: 'red' })
    expect((await ui.find({ type: 'Text', text: /informational$/ }))?.props).toEqual({ dimColor: true })
    expect((await ui.find({ type: 'Text', text: /^predicate: realized_in/ }))?.props).toEqual({ dimColor: true, wrap: 'truncate-end' })
    expect((await ui.find({ type: 'Text', text: /^new$/ }))?.props).toEqual({ color: 'yellow' })
    await ui.unmount()
  }
})

test('a failing check with nothing new says only that it fails', async ($, on) => {
  stage(on, health({ ...FAILING, findings: [INCOMPLETE] }))

  const ui = await mount($, 'terminal')

  expect(await ui.find({ type: 'Text', text: /^✗ failing$/ })).toBeDefined()
  expect(await ui.find({ type: 'Text', text: /^new$/ })).toBe(undefined)
})

test('an error finding has a fix button keyed by its place in health, an informational one has none', async ($, on) => {
  const ran = stage(on, health(FAILING))
  const ui = await mount($, 'terminal')
  const fixes = await ui.findAll({ type: 'Button', text: /^fix$/ })

  expect(fixes.map(button => button.props)).toEqual([
    { key: 'fix-1', label: 'fix', plain: true, dimColor: true },
    { key: 'fix-2', label: 'fix', plain: true, dimColor: true },
  ])

  await ui.press({ key: 'fix-1' })
  await ui.press({ key: 'fix-2' })
  expect(ran).toEqual([
    ['fix', DANGLING],
    ['fix', INCOMPLETE],
  ])
})

test('a bucket shows eight findings and counts the rest', async ($, on) => {
  const many = Array.from({ length: 11 }, (_, i) => finding('dangling', `adr/ad-${i}`, `target: missing-${i}`))

  stage(on, health({ ...FAILING, findings: many, baseline: many.map(row => row.key) }))

  const ui = await mount($, 'terminal')
  const shown = lines(await ui.drawn())

  expect(shown.filter(line => line.startsWith('  adr/')).length).toBe(8)
  expect(shown).toContain('dangling  11')
  expect(shown).toContain('  … 3 more')
  expect((await ui.findAll({ type: 'Button', text: /^fix$/ })).map(button => button.key)).toEqual(
    Array.from({ length: 8 }, (_, i) => `fix-${i}`),
  )
})

test('the two buttons run the check and preview the index', async ($, on) => {
  const ran = stage(on, health(FAILING))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)

    expect((await ui.find({ type: 'Button', key: 'check' }))?.props).toEqual({ key: 'check', label: 'Run check', variant: 'primary' })
    expect((await ui.find({ type: 'Button', key: 'reindex' }))?.props).toEqual({ key: 'reindex', label: 'Reindex' })
    await ui.press({ key: 'check' })
    await ui.press({ key: 'reindex' })
    await ui.unmount()
  }

  expect(ran).toEqual([['check'], ['previewReindex'], ['check'], ['previewReindex']])
})

test('an index preview draws as a diff, with buttons to apply or close it', async ($, on) => {
  const ran = stage(on, health(FAILING, F.reindex_dry.stdout))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)

    expect((await ui.find({ type: 'Code' }))?.props).toEqual({ source: F.reindex_dry.stdout, format: 'diff' })
    expect((await ui.find({ type: 'Button', key: 'reindex-apply' }))?.props.label).toBe('Rebuild index.md')
    expect((await ui.find({ type: 'Button', key: 'diff-close' }))?.props.label).toBe('Close')
    await ui.press({ key: 'reindex-apply' })
    await ui.press({ key: 'diff-close' })
    await ui.unmount()
  }

  expect(ran).toEqual([['reindex'], ['closeDiff'], ['reindex'], ['closeDiff']])
})

test('a preview that is not a diff draws as one dim line', async ($, on) => {
  stage(on, health(FAILING, 'index.md is current'))

  const ui = await mount($, 'terminal')

  expect(await ui.find({ type: 'Code' })).toBe(undefined)
  expect((await ui.find({ type: 'Text', text: /^index\.md is current$/ }))?.props).toEqual({ dimColor: true, wrap: 'truncate-end' })
  expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual([
    'fix-1',
    'fix-2',
    'check',
    'reindex',
    'reindex-apply',
    'diff-close',
  ])
})

test('without a preview there is no diff and no apply button', async ($, on) => {
  stage(on, health(FAILING))

  const ui = await mount($, 'terminal')

  expect(await ui.find({ type: 'Code' })).toBe(undefined)
  expect(await ui.find({ type: 'Button', key: 'reindex-apply' })).toBe(undefined)
})

// --- through the shell

test('the mod draws its pane: an empty Session tab, then Health on a press', async ($, on) => {
  const host = fakeHost(on, [F.check_base, F.status_base, F.query_draft, F.reindex_dry])

  await $.session.start(START)

  for (const surface of SURFACES) {
    const ui = await $.ui.mount({ plugin: 'khub', surface, component: 'Pane', requestId: 'khub', props: pane('khub') })

    expect(await ui.find({ type: 'Text', text: /^ Session $/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /^No khub writes this session yet\.$/ })).toBeDefined()

    await ui.press({ key: 'tab-health' })
    expect(await ui.find({ type: 'Text', text: /^ Health $/ })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /^CHECK/ })).toBeDefined()
    expect(await ui.find({ type: 'Button', key: 'tab-session' })).toBeDefined()

    host.ran.length = 0
    await ui.press({ key: 'check' })
    expect(host.ran).toEqual(['check --format json', 'status --format json'])

    await ui.press({ key: 'reindex' })
    expect(host.ran.at(-1)).toBe('reindex --dry-run')
    expect((await ui.find({ type: 'Code' }))?.props.format).toBe('diff')

    await ui.press({ key: 'diff-close' })
    expect(await ui.find({ type: 'Code' })).toBe(undefined)

    await ui.press({ key: 'tab-session' })
    await ui.unmount()
  }
})
