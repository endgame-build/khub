import { expect, test } from 'claude-code/testing'

import { parseJson } from '../hooks/cli'
import { inboundNote } from '../hooks/reads'
import { F } from './fixtures'

const ID = 'component/cmp-search'

test('the inbound rows of a neighbors list become a note', () => {
  expect(inboundNote(ID, parseJson(F.neighbors_search_in.stdout))).toBe(
    'khub: component/cmp-search has inbound edges its file does not show: served_by ← use-case/uc-find-a-document',
  )
})

test('no inbound edge, or no list, is no note', () => {
  expect(inboundNote(ID, [{ id: 'a/x', predicate: 'depends_on', direction: 'out' }])).toBe(undefined)
  expect(inboundNote(ID, parseJson(F.get_missing.stdout))).toBe(undefined)
  expect(inboundNote(ID, [])).toBe(undefined)
  expect(inboundNote(ID, undefined)).toBe(undefined)
})

test('more than ten inbound edges are cut', () => {
  const rows = Array.from({ length: 13 }, (_, i) => ({ id: `u/uc-${i}`, predicate: 'served_by', direction: 'in' }))
  const note = inboundNote(ID, rows) as string

  expect(note.endsWith('served_by ← u/uc-9, and 3 more')).toBe(true)
  expect(note.includes('u/uc-10')).toBe(false)
})
