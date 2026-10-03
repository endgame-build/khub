import { expect, test } from 'claude-code/testing'

import { parseJson } from '../hooks/cli'
import { schemaNotes, schemaNotice, validateNotes } from '../hooks/schemaloop'
import { F } from './fixtures'
import type { Fixture } from './fixtures'

const json = (fixture: Fixture) => parseJson(fixture.stdout)

test('a schema that no longer resolves is said first and alone', () => {
  expect(schemaNotes('unknown type in policy', json(F.schema_diff_pending))).toEqual([
    'khub: the schema no longer resolves. unknown type in policy',
  ])
  expect(schemaNotice('unknown type in policy', json(F.schema_diff_pending))).toEqual({
    kind: 'schema',
    parts: [{ text: 'unknown type in policy', tone: 'bad' }],
    buttons: [],
  })
})

test('pending changes are listed one per line', () => {
  expect(schemaNotes(null, json(F.schema_diff_pending))).toEqual([
    'khub schema: 1 change since the last snapshot:',
    'added types.prd.attributes.recorded_note',
  ])
  expect(schemaNotice(null, json(F.schema_diff_pending))).toEqual({
    kind: 'schema',
    parts: [{ text: 'schema · 1 change pending', tone: 'warn' }],
    buttons: ['rewire', 'snapshot'],
  })
})

test('a change with both sides shows them, and a long list is cut at ten', () => {
  const change = (i: number) => ({ op: 'changed', path: `types.a.attributes.f${i}.type`, from: 'text', to: { enum: ['x'] } })
  const lines = schemaNotes(null, { pending: true, changes: Array.from({ length: 12 }, (_, i) => change(i)) })

  expect(lines[0]).toBe('khub schema: 12 changes since the last snapshot:')
  expect(lines[1]).toBe('changed types.a.attributes.f0.type (text → {"enum":["x"]})')
  expect(lines.length).toBe(12)
  expect(lines[11]).toBe('… and 2 more')
})

test('no snapshot is said once and offers one, and nothing pending is nothing', () => {
  expect(schemaNotes(null, json(F.schema_diff_none))).toEqual([
    'khub schema: no snapshot exists to compare against. `khub schema snapshot` records one.',
  ])
  expect(schemaNotice(null, json(F.schema_diff_none))).toEqual({
    kind: 'schema',
    parts: [{ text: 'schema · no snapshot yet', tone: 'warn' }],
    buttons: ['snapshot'],
  })
  expect(schemaNotes(null, json(F.schema_diff_clean))).toEqual([])
  expect(schemaNotice(null, json(F.schema_diff_clean))).toBe(null)
  expect(schemaNotes(null, undefined)).toEqual([])
})

test('entities the edited schema no longer accepts are listed by id, field and reason', () => {
  expect(validateNotes(json(F.validate_adr))).toEqual([])
  expect(validateNotes(undefined)).toEqual([])

  const lines = validateNotes(json(F.validate_requirement_dangling))

  expect(lines[0]).toMatch(/^khub validate: \d+ errors? under the edited schema:$/)
  expect(lines[1]).toMatch(/^requirement\/req-search-answers-within-300-ms › \w+: /)
})
