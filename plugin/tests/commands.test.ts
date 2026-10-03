import { expect, test } from 'claude-code/testing'

import { parseJson } from '../hooks/cli'
import { passthrough, route } from '../hooks/commands'
import { fromArgs } from '../hooks/parse'
import { F } from './fixtures'
import type { Fixture } from './fixtures'

const ADR = 'ad-2026-01-15-use-re2-patterns'
const REQ = 'req-search-answers-within-300-ms'

// What `/khub <args>` prints for a recorded khub reply.
const printed = (args: string[], fixture: Fixture) =>
  passthrough(fromArgs(args), parseJson(fixture.stdout), fixture.stdout, fixture.stderr).split('\n')

test('no arguments open the pane, plain arguments run khub', () => {
  expect(route('')).toEqual({ kind: 'open' })
  expect(route('   ')).toEqual({ kind: 'open' })
  expect(route('get cmp-search --edges')).toEqual({ kind: 'run', argv: ['get', 'cmp-search', '--edges'] })
  expect(route('add adr --title "Use RE2 patterns" --status accepted')).toEqual({
    kind: 'run',
    argv: ['add', 'adr', '--title', 'Use RE2 patterns', '--status', 'accepted'],
  })
})

test('shell syntax and serve are turned away with a reason', () => {
  const plain = { kind: 'error', message: 'khub: plain arguments only, no pipes or redirections' }

  expect(route('query --type adr | jq .')).toEqual(plain)
  expect(route('get x > out.json')).toEqual(plain)
  expect(route('check 2>&1')).toEqual(plain)
  expect(route('status && khub check')).toEqual(plain)
  expect(route('get $(khub query --format ids)')).toEqual(plain)
  expect(route('add adr --title "n $(date)"')).toEqual(plain)
  expect(route('get `khub query --format ids`')).toEqual(plain)
  expect(route('add adr --body-file - < body.md')).toEqual(plain)
  expect(route('add adr --title "open')).toEqual(plain)

  const serve = route('serve --port 7777')

  expect(serve.kind).toBe('error')
  expect(serve.kind === 'error' && serve.message).toMatch(/serve blocks the session/)
})

test('a get prints the frontmatter and the edges, never the body', () => {
  const lines = printed(['get', 'cmp-search', '--edges'], F.get_search)

  expect(lines).toEqual([
    'khub get cmp-search  component/cmp-search · Search · 3 edges',
    'created: 2026-01-15',
    'updated: 2026-01-15',
    'draft: false',
    'title: Search',
    'kind: service',
    'owner: team-platform',
    'lifecycle: production',
    'tier: tier-1',
    'depends_on: cmp-storage',
    'repo: rp-api',
    'depends_on → cmp-storage',
    'repo → rp-api',
    'serves ← use-case/uc-find-a-document',
  ])
  expect(lines.join('\n')).not.toMatch(/Responsibilities/)
})

test('a list prints one line per record with its flags', () => {
  expect(printed(['query', '--type', 'component'], F.query_components)).toEqual([
    'khub query component  2 entities',
    'component/cmp-search · Search',
    'component/cmp-storage · Storage',
  ])
  expect(passthrough(fromArgs(['query', '--orphan']), [{ id: 'x/a', title: 'A', draft: true, orphan: true, stale: false }], '', '')).toBe(
    'khub query  1 entity\nx/a · A · draft · orphan',
  )
  expect(printed(['neighbors', 'cmp-search'], F.neighbors_search)).toEqual([
    'khub neighbors cmp-search  3 neighbors · in 1 · out 2',
    'component/cmp-storage · depends_on · out',
    'repo/rp-api · repo · out',
    'use-case/uc-find-a-document · served_by · in',
  ])
  expect(printed(['impact', 'cmp-storage', '--reverse'], F.impact_storage)).toEqual([
    'khub impact cmp-storage  1 affected · depth 1',
    'component/cmp-storage · depth 0',
    'component/cmp-search · depth 1',
  ])
})

test('a long list is cut at twenty records', () => {
  const rows = Array.from({ length: 23 }, (_, i) => ({ id: `x/e${i}`, title: `E${i}` }))
  const lines = passthrough(fromArgs(['query']), rows, '', '').split('\n')

  expect(lines.length).toBe(22)
  expect(lines[0]).toBe('khub query  23 entities')
  expect(lines[20]).toBe('x/e19 · E19')
  expect(lines[21]).toBe('… 3 more')
})

test('validate prints its errors and gaps', () => {
  expect(printed(['validate', `requirement/${REQ}`], F.validate_requirement_dangling)).toEqual([
    `khub validate requirement/${REQ}  1 checked · 1 error · 0 gaps`,
    `requirement/${REQ}: realized_in: no component 'cmp-serach' to satisfy relation 'realized_in'`,
  ])

  const gap = { id: `adr/${ADR}`, field: 'body', reason: "'## Decision' 4 words of prose, at least 15 asked for" }

  expect(passthrough(fromArgs(['validate']), { count: 3, errors: [], gaps: [gap] }, '', '').split('\n')[1]).toBe(
    `adr/${ADR}: - body: '## Decision' 4 words of prose, at least 15 asked for`,
  )
  expect(printed(['validate', `adr/${ADR}`], F.validate_adr)).toEqual([
    `khub validate adr/${ADR}  1 checked · 0 errors · 0 gaps ✓`,
  ])
})

test('check prints one line per finding, in the format the notes use', () => {
  expect(printed(['check'], F.check_dangling)).toEqual([
    'khub check  failed · dangling 1',
    `requirement/${REQ} › dangling: predicate realized_in, target cmp-serach`,
    `requirement/${REQ} › orphans`,
  ])
  expect(printed(['check'], F.check_base)).toEqual(['khub check  passed ✓'])

  const incomplete = { id: 'component/cmp-stripe', type: 'component', slug: 'cmp-stripe', missing_fields: ['kind'], missing_relations: [] }
  const misplaced = { path: 'notes/adr.md', type: 'adr', expected: 'knowledge/decisions' }

  expect(passthrough(fromArgs(['check']), { passed: false, incomplete: [incomplete], misplaced: [misplaced] }, '', '').split('\n')).toEqual([
    'khub check  failed · incomplete 1 · misplaced 1',
    'component/cmp-stripe › incomplete: missing_fields kind',
    'notes/adr.md › misplaced: expected knowledge/decisions',
  ])
})

test('schema show prints fields and relations, status its counts', () => {
  const type = {
    name: 'adr',
    fields: [
      { name: 'title', type: 'text', required: true, enum: null },
      { name: 'status', type: 'text', required: true, enum: ['proposed', 'accepted', 'rejected'] },
      { name: 'tags', type: 'list', required: false, enum: null },
    ],
    relations: [
      { predicate: 'supersedes', to: ['adr'], many: false, required: false },
      { predicate: 'affects', to: ['any'], many: true, required: false },
      { predicate: 'provider', to: ['component'], many: false, required: true },
    ],
  }

  expect(passthrough(fromArgs(['schema', 'show', 'adr']), type, '', '').split('\n')).toEqual([
    'khub schema show adr  adr · 3 fields · 3 relations',
    'title: text, required',
    'status: text, required (proposed | accepted | rejected)',
    'tags: list',
    'supersedes → adr',
    'affects → any, many',
    'provider → component, required',
  ])

  const schema = printed(['schema'], F.schema)

  expect(schema[0]).toBe('khub schema  11 types')
  expect(schema.slice(1, 4)).toEqual([
    'prd · singleton · knowledge/prd.md',
    'arc42 · singleton · knowledge/arc42.md',
    'capability · file · knowledge/capabilities',
  ])
  expect(schema.length).toBe(12)

  const status = printed(['status'], F.status_base)

  expect(status[0]).toBe('khub status  8 entities · 0 draft · 0 orphan · 0 stale')
  expect(status).toContain('component 2')
})

test('a prose command prints its output, cut at forty lines', () => {
  const lines = printed(['reindex', '--dry-run'], F.reindex_dry)

  expect(lines[0]).toBe('khub reindex')
  expect(lines[1]).toBe('--- index.md')
  expect(lines.length).toBe(F.reindex_dry.stdout.split('\n').length + 1)

  const long = Array.from({ length: 45 }, (_, i) => `line ${i}`).join('\n')
  const cut = passthrough(fromArgs(['wire']), undefined, long, '').split('\n')

  expect(cut.length).toBe(42)
  expect(cut[41]).toBe('… 5 more lines')
  expect(passthrough(fromArgs(['reindex']), undefined, '', '')).toBe('khub reindex')
})

test('a refusal prints one line, and a note is appended once', () => {
  expect(printed(['get', 'nope'], F.get_missing)).toEqual(["khub get nope  ✗ lookup_error: No entity 'nope' found"])
  expect(printed(['remove', 'cmp-search'], F.remove_refused).length).toBe(1)
  expect(printed(['search', '--plain', 'zzzz'], F.search_empty)).toEqual([
    'khub search "zzzz"  no hits',
    'note: no hits in 8 entities searched; for structure try `khub query --type <type>`, `khub neighbors <id>` or `khub get <id>`',
  ])
  expect(printed(['add', 'adr', '--title', 'Use RE2 patterns', '--status', 'accepted'], F.add_adr)).toEqual([
    `khub add adr  + adr/${ADR}`,
  ])
})

test('doctor alone is the mod\'s own word, and with more it goes to khub', () => {
  expect(route('doctor')).toEqual({ kind: 'doctor' })
  expect(route('doctor now')).toEqual({ kind: 'run', argv: ['doctor', 'now'] })
})
