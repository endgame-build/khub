import { expect, test } from 'claude-code/testing'

import { parseJson } from '../hooks/cli'
import { classify } from '../hooks/parse'
import type { Simple } from '../hooks/parse'
import { bandParts, diffOf, editedBy, EMPTY_SESSION, summaryOf, withEdited, withIds } from '../hooks/session'
import type { KhubSummary } from '../types'
import { F } from './fixtures'

const doc = (fixture: { stdout: string }) => parseJson(fixture.stdout)
const call = (command: string) => classify(command) as Simple
const line = (...args: Parameters<typeof bandParts>) => bandParts(...args).map(part => part.text).join('')

const SUMMARY: KhubSummary = { total: 87, draft: 4, passed: true, errors: 0 }
const NONE = { added: 0, removed: 0, edited: 0 }

test('the first read fixes the baseline, and later reads leave it', () => {
  const first = withIds(EMPTY_SESSION, ['a/1', 'a/2'])
  const second = withIds(first, ['a/2', 'a/3'])

  expect(first).toEqual({ baseline: ['a/1', 'a/2'], current: ['a/1', 'a/2'], edited: [] })
  expect(second.baseline).toEqual(['a/1', 'a/2'])
  expect(second.current).toEqual(['a/2', 'a/3'])
})

test('added and removed are counted against the baseline', () => {
  const session = withIds(withIds(EMPTY_SESSION, ['a/1', 'a/2']), ['a/2', 'a/3', 'a/4'])

  expect(diffOf(session)).toEqual({ added: 2, removed: 1, edited: 0 })
  expect(diffOf(EMPTY_SESSION)).toEqual(NONE)
})

test('an edit counts once, and only for an entity that was there and still is', () => {
  const base = withIds(EMPTY_SESSION, ['a/1', 'a/2'])
  const edited = withEdited(withEdited(base, ['a/1']), ['a/1'])

  expect(edited.edited).toEqual(['a/1'])
  expect(withEdited(edited, ['a/1'])).toBe(edited)
  expect(diffOf(edited)).toEqual({ added: 0, removed: 0, edited: 1 })

  // Added in the session and then edited: it counts as added alone.
  expect(diffOf(withEdited(withIds(base, ['a/1', 'a/2', 'a/3']), ['a/3']))).toEqual({ added: 1, removed: 0, edited: 0 })

  // Edited and then removed: it counts as removed alone.
  expect(diffOf(withIds(edited, ['a/2']))).toEqual({ added: 0, removed: 1, edited: 0 })
})

test('a refresh reads the total, the drafts, the gate and the qualified ids', () => {
  const base = summaryOf(doc(F.check_base), doc(F.query_base))
  const broken = summaryOf(doc(F.check_dangling), doc(F.query_dangling))

  expect(base?.summary).toEqual({ total: 8, draft: 0, passed: true, errors: 0 })
  expect(base?.ids).toContain('component/cmp-search')
  expect(base?.ids.length).toBe(8)

  // The orphan is informational, so the one error is the dangling relation.
  expect(broken?.summary).toEqual({ total: 10, draft: 0, passed: false, errors: 1 })
  expect(summaryOf({ passed: true }, [{ id: 'a/1', draft: true }, { id: 'a/2' }])?.summary.draft).toBe(1)
})

test('a refresh that read something else is no refresh', () => {
  expect(summaryOf(doc(F.get_missing), doc(F.query_base))).toBe(null)
  expect(summaryOf(doc(F.check_base), doc(F.get_missing))).toBe(null)
  expect(summaryOf(undefined, undefined)).toBe(null)
})

test('an edit, a link and an unlink name the entity they changed', () => {
  expect(editedBy(call('khub edit cmp-search lifecycle deprecated'), doc(F.edit_component))).toEqual(['component/cmp-search'])
  expect(editedBy(call('khub link x affects cmp-search'), doc(F.link_adr))).toEqual(['adr/ad-2026-01-15-use-re2-patterns'])
  expect(editedBy(call('khub unlink x affects cmp-search'), doc(F.unlink_adr))).toEqual(['adr/ad-2026-01-15-use-re2-patterns'])
})

test('a call that changed no entity in place names none', () => {
  expect(editedBy(call('khub link x affects cmp-search'), doc(F.link_adr_again))).toEqual([])
  expect(editedBy(call('khub add adr --title x'), doc(F.add_adr))).toEqual([])
  expect(editedBy(call('khub remove act-temp'), doc(F.remove_actor))).toEqual([])
  expect(editedBy(call('khub edit nope kind x'), doc(F.get_missing))).toEqual([])
  expect(editedBy(call('khub edit cmp-search kind x'), undefined)).toEqual([])
})

test('the band names the preset, the count, the changes, the drafts and the gate', () => {
  expect(line('build-hub', '0.6.0', SUMMARY, { added: 2, removed: 1, edited: 3 })).toBe(
    'build-hub 0.6.0 · 87 entities [+2 −1 ~3] · 4 draft · check ✓',
  )
})

test('the band leaves out what is zero', () => {
  expect(line('build-hub', '0.6.0', { ...SUMMARY, draft: 0 }, NONE)).toBe('build-hub 0.6.0 · 87 entities · check ✓')
  expect(line('build-hub', '0.6.0', { ...SUMMARY, total: 1 }, { ...NONE, removed: 1 })).toBe(
    'build-hub 0.6.0 · 1 entity [−1] · 4 draft · check ✓',
  )
  expect(line('', '', SUMMARY, { ...NONE, edited: 2 })).toBe('87 entities [~2] · 4 draft · check ✓')
})

test('an added count is ok, a removed count is bad, and the rest is plain', () => {
  const tones = Object.fromEntries(
    bandParts('build-hub', '0.6.0', SUMMARY, { added: 2, removed: 1, edited: 3 }).map(part => [part.text, part.tone]),
  )

  expect(tones['+2']).toBe('ok')
  expect(tones['−1']).toBe('bad')
  expect(tones['~3']).toBe('plain')
  expect(tones[' · 4 draft · check ✓']).toBe('plain')
})

test('a failing gate counts its errors', () => {
  expect(line('build-hub', '0.6.0', { total: 10, draft: 0, passed: false, errors: 1 }, NONE)).toBe(
    'build-hub 0.6.0 · 10 entities · check ✗ 1 error',
  )
  expect(line('build-hub', '0.6.0', { total: 10, draft: 0, passed: false, errors: 2 }, NONE)).toMatch(/check ✗ 2 errors$/)
})
