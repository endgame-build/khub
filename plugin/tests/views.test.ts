import { expect, test } from 'claude-code/testing'

import { parseJson } from '../hooks/cli'
import { entityOf, recordsOf } from '../hooks/views'
import { F } from './fixtures'
import type { Fixture } from './fixtures'

const json = (fixture: Fixture) => parseJson(fixture.stdout)

test('query rows become records with their title and true flags', () => {
  expect(recordsOf(json(F.query_components))).toEqual([
    { id: 'component/cmp-search', title: 'Search', flags: [], note: '' },
    { id: 'component/cmp-storage', title: 'Storage', flags: [], note: '' },
  ])
  expect(recordsOf([{ id: 'a/x', title: 'X', draft: true, orphan: true, stale: false }])).toEqual([
    { id: 'a/x', title: 'X', flags: ['draft', 'orphan'], note: '' },
  ])
})

test('a search hit carries its match share to three decimals', () => {
  expect(recordsOf(json(F.search_hits))).toEqual([{ id: 'component/cmp-search', title: 'Search', flags: [], note: 'match 1.000' }])
  expect(recordsOf([{ id: 'a/x', title: 'X', match: 0.4123 }])[0]?.note).toBe('match 0.412')
})

test('a document that is no list gives no records', () => {
  expect(recordsOf(json(F.get_missing))).toEqual([])
  expect(recordsOf(undefined)).toEqual([])
})

test('an entity takes its fields from the record and its edges from the neighbors list', () => {
  const entity = entityOf(json(F.get_search), json(F.neighbors_search))

  expect(entity?.id).toBe('component/cmp-search')
  expect(entity?.type).toBe('component')
  expect(entity?.slug).toBe('cmp-search')
  expect(entity?.path).toBe('knowledge/components/cmp-search.md')
  expect(entity?.isDraft).toBe(false)
  expect(entity?.fields.map(field => field.name)).toEqual([
    'created', 'updated', 'draft', 'title', 'kind', 'owner', 'lifecycle', 'tier', 'depends_on', 'repo',
  ])
  expect(entity?.fields.find(field => field.name === 'depends_on')?.value).toBe('cmp-storage')
  expect(entity?.edges).toEqual([
    { predicate: 'depends_on', id: 'component/cmp-storage', direction: 'out' },
    { predicate: 'repo', id: 'repo/rp-api', direction: 'out' },
    { predicate: 'served_by', id: 'use-case/uc-find-a-document', direction: 'in' },
  ])
})

test('the template hints are stripped from a body, and the headings stay', () => {
  const entity = entityOf(json(F.get_search), [])

  expect(entity?.body.includes('<!--')).toBe(false)
  expect(entity?.body.startsWith('## Responsibilities')).toBe(true)
  expect(entityOf({ id: 'a/x', type: 'a', slug: 'x', frontmatter: { draft: true, tags: ['p', 'q'] }, body: ' text ' }, undefined)).toEqual({
    id: 'a/x',
    type: 'a',
    slug: 'x',
    path: '',
    isDraft: true,
    fields: [
      { name: 'draft', value: 'true' },
      { name: 'tags', value: 'p, q' },
    ],
    edges: [],
    body: 'text',
  })
})

test('a refusal or a list is no entity', () => {
  expect(entityOf(json(F.get_missing), [])).toBe(null)
  expect(entityOf(json(F.query_components), [])).toBe(null)
  expect(entityOf(undefined, undefined)).toBe(null)
})
