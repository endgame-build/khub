import { expect, test } from 'claude-code/testing'

import { parseJson } from '../hooks/cli'
import { classify } from '../hooks/parse'
import type { Simple } from '../hooks/parse'
import { summarize } from '../hooks/summarize'
import { F } from './fixtures'
import type { Fixture } from './fixtures'

const ADR = 'ad-2026-01-15-use-re2-patterns'
const REQ = 'req-search-answers-within-300-ms'

// The summary of a command, answered by a recorded khub reply.
const say = (command: string, fixture: Fixture) =>
  summarize(classify(command) as Simple, parseJson(fixture.stdout), fixture.stderr)

// The summary of a command, answered by a document written here.
const sayOf = (command: string, json: unknown, note = '') => summarize(classify(command) as Simple, json, note)

test('the head names the verb and what the call is about', () => {
  const head = (command: string) => sayOf(command, undefined).head

  expect(head('khub add adr --title "Use RE2 patterns" --status accepted')).toBe('khub add adr')
  expect(head('khub get cmp-search --edges --format json')).toBe('khub get cmp-search')
  expect(head('khub query --type component --format json')).toBe('khub query component')
  expect(head('khub query --draft')).toBe('khub query')
  expect(head('khub search --plain "regex patterns"')).toBe('khub search "regex patterns"')
  expect(head('khub schema show adr --format json')).toBe('khub schema show adr')
  expect(head('khub schema')).toBe('khub schema')
  expect(head(`khub link ${ADR} affects cmp-search`)).toBe(`khub link ${ADR}`)
  expect(head('khub check --strict')).toBe('khub check')
  expect(head('cd ws && npx @endgame-build/khub impact cmp-storage --reverse')).toBe('khub impact cmp-storage')
})

test('a call with no document yet has no line', () => {
  expect(sayOf('khub add adr --title x', undefined)).toEqual({ head: 'khub add adr', line: '', tone: 'plain' })
})

test('a refusal shows its code and message', () => {
  expect(say('khub get nope --format json', F.get_missing)).toEqual({
    head: 'khub get nope',
    line: "✗ lookup_error: No entity 'nope' found",
    tone: 'bad',
  })
  expect(say('khub add requirement --title Other --realized_in cmp-serach', F.add_refused).line).toBe(
    "✗ referential_integrity: No component 'cmp-serach' to satisfy relation 'realized_in'",
  )
  expect(say('khub remove cmp-search', F.remove_refused).tone).toBe('bad')
})

test('add names the new id and marks a draft', () => {
  expect(say('khub add adr --title "Use RE2 patterns" --status accepted', F.add_adr)).toEqual({
    head: 'khub add adr',
    line: `+ adr/${ADR}`,
    tone: 'ok',
  })
  expect(sayOf('khub add actor --title Operator --draft', { id: 'actor/act-operator', draft: true }).line).toBe(
    '+ actor/act-operator · draft',
  )
})

test('edit names the field it set, from positionals or from a flag', () => {
  expect(say(`khub edit ${REQ} realized_in cmp-search --format json`, F.edit_fix).line).toBe(
    `~ requirement/${REQ} · realized_in cmp-search`,
  )
  expect(say('khub edit cmp-search --lifecycle deprecated --format json', F.edit_component)).toEqual({
    head: 'khub edit cmp-search',
    line: '~ component/cmp-search · lifecycle deprecated',
    tone: 'ok',
  })
  expect(say('khub edit cmp-search --body "New text" --format json', F.edit_component).line).toBe(
    '~ component/cmp-search · body',
  )
})

test('link and unlink show the edge, or say nothing changed', () => {
  expect(say(`khub link ${ADR} affects cmp-search`, F.link_adr)).toEqual({
    head: `khub link ${ADR}`,
    line: `→ ${ADR} affects cmp-search`,
    tone: 'ok',
  })
  expect(say(`khub link ${ADR} affects cmp-search`, F.link_adr_again)).toEqual({
    head: `khub link ${ADR}`,
    line: 'edge already present',
    tone: 'plain',
  })
  expect(say(`khub unlink ${ADR} affects cmp-search`, F.unlink_adr).line).toBe(`⇢ ${ADR} affects cmp-search`)
  expect(sayOf(`khub unlink ${ADR} affects cmp-search`, { changed: false }).line).toBe('no such edge')
})

test('remove names what went', () => {
  expect(say('khub remove act-temp --format json', F.remove_actor)).toEqual({
    head: 'khub remove act-temp',
    line: '− actor/act-temp',
    tone: 'ok',
  })
})

test('get shows the id, the title and the edge count', () => {
  expect(say('khub get cmp-search --edges --format json', F.get_search).line).toBe('component/cmp-search · Search · 3 edges')
  expect(sayOf('khub get cmp-search', { id: 'component/cmp-search', frontmatter: { name: 'Search' } }).line).toBe(
    'component/cmp-search · Search',
  )
  expect(sayOf('khub get a b', [{ id: 'x/a' }, { id: 'x/b' }]).line).toBe('2 entities')
})

test('query and stale count entities', () => {
  expect(say('khub query --type component --format json', F.query_components).line).toBe('2 entities')
  expect(say('khub query --draft --format json', F.query_draft).line).toBe('no entities')
  expect(sayOf('khub stale --days 30', [{ id: 'x/a' }])).toEqual({ head: 'khub stale', line: '1 entity', tone: 'plain' })
})

test('search shows the top hit, and warns with khub\'s note', () => {
  expect(say('khub search --plain search --format json', F.search_hits)).toEqual({
    head: 'khub search "search"',
    line: '1 hit · top component/cmp-search',
    tone: 'plain',
  })

  const empty = say('khub search --plain zzzz --format json', F.search_empty)

  expect(empty.tone).toBe('warn')
  expect(empty.line).toBe('no hits in 8 entities searched; for structure try `khub query --type <type>`, `k')
  expect(empty.line.length).toBe(80)
  expect(sayOf('khub search --plain api', [{ id: 'x/a' }, { id: 'x/b' }], 'note: 2 of 40 hits shown; raise --limit')).toEqual({
    head: 'khub search "api"',
    line: '2 hits · top x/a · 2 of 40 hits shown; raise --limit',
    tone: 'warn',
  })
  expect(sayOf('khub search zzzz', []).line).toBe('no hits')
})

test('the walks count what they reached', () => {
  expect(say('khub neighbors cmp-search --format json', F.neighbors_search).line).toBe('3 neighbors · in 1 · out 2')
  expect(say('khub impact cmp-storage --reverse --format json', F.impact_storage).line).toBe('1 affected · depth 1')
  expect(sayOf('khub impact cmp-x', [{ id: 'x/x', depth: 0 }]).line).toBe('0 affected · depth 0')
  expect(sayOf(`khub history ${ADR}`, [{ id: `adr/${ADR}`, superseded_by: null }]).line).toBe('1 in chain')
})

test('status, validate and check report their counts', () => {
  expect(say('khub status --format json', F.status_base).line).toBe('8 entities · 0 draft · 0 orphan · 0 stale')
  expect(say('khub status --format json', F.status_dangling).line).toBe('10 entities · 0 draft · 1 orphan · 0 stale')

  expect(say(`khub validate adr/${ADR} --format json`, F.validate_adr)).toEqual({
    head: `khub validate adr/${ADR}`,
    line: '1 checked · 0 errors · 0 gaps ✓',
    tone: 'ok',
  })
  expect(say(`khub validate requirement/${REQ}`, F.validate_requirement_dangling)).toEqual({
    head: `khub validate requirement/${REQ}`,
    line: '1 checked · 1 error · 0 gaps',
    tone: 'bad',
  })
  expect(sayOf('khub validate', { count: 4, errors: [], gaps: [{}, {}] })).toEqual({
    head: 'khub validate',
    line: '4 checked · 0 errors · 2 gaps',
    tone: 'warn',
  })

  expect(say('khub check --format json', F.check_base)).toEqual({ head: 'khub check', line: 'passed ✓', tone: 'ok' })
  expect(say('khub check --format json', F.check_dangling)).toEqual({
    head: 'khub check',
    line: 'failed · dangling 1',
    tone: 'bad',
  })
  expect(sayOf('khub check --strict', { passed: false, strict: true, orphans: ['x/a', 'x/b'], thin: [{}] }).line).toBe(
    'failed · orphans 2',
  )
})

test('schema views say how much they hold', () => {
  expect(say('khub schema --format json', F.schema).line).toBe('11 types')
  expect(sayOf('khub schema types', ['prd', 'adr']).line).toBe('2 types')
  expect(sayOf('khub schema show adr', { name: 'adr', fields: [{}, {}, {}], relations: [{}] }).line).toBe(
    'adr · 3 fields · 1 relation',
  )
  expect(sayOf('khub schema edges', [{}, {}]).line).toBe('2 predicates')
  expect(sayOf('khub schema diff', { pending: true, changes: [{}, {}] }).line).toBe('pending · 2 changes')
  expect(sayOf('khub schema diff', { pending: false, changes: [] }).line).toBe('no pending changes')
  expect(sayOf('khub schema snapshot', { path: '.khub/schema.applied.yaml', types: 11 })).toEqual({
    head: 'khub schema snapshot',
    line: 'snapshot · 11 types',
    tone: 'ok',
  })
})

test('any other verb shows the first line of its note, or nothing', () => {
  expect(say('khub reindex --dry-run', F.reindex_dry)).toEqual({ head: 'khub reindex', line: '', tone: 'plain' })
  expect(sayOf('khub wire', undefined, '\nindex skipped: 2 malformed files\nmore').line).toBe(
    'index skipped: 2 malformed files',
  )
})
