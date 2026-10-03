import { expect, test } from 'claude-code/testing'

import type { KhubHealth, KhubLedgerEntry } from '../types'
import { parseJson } from '../hooks/cli'
import {
  blockReason,
  changeOf,
  findingsOf,
  ledgerWith,
  nextHealth,
  notesFor,
  noticeOf,
  validationLine,
  validationMark,
  validationOf,
} from '../hooks/integrity'
import type { Change, Validation } from '../hooks/integrity'
import { classify } from '../hooks/parse'
import type { Simple } from '../hooks/parse'
import { EMPTY_HEALTH } from '../hooks/state'
import { F } from './fixtures'
import type { Fixture } from './fixtures'

const ADR = 'adr/ad-2026-01-15-use-re2-patterns'
const REQ = 'requirement/req-search-answers-within-300-ms'
const DANGLING = `${REQ} › dangling: predicate realized_in, target cmp-serach`
const ORPHAN = `${REQ} › orphans`
const UNRESOLVED = "realized_in: no component 'cmp-serach' to satisfy relation 'realized_in'"

const json = (fixture: Fixture) => parseJson(fixture.stdout)
const validated = (fixture: Fixture) => validationOf(json(fixture)) as Validation
const call = (command: string) => classify(command) as Simple
const callOf = (fixture: Fixture) =>
  call(['khub', ...fixture.args.map(arg => (/\s/.test(arg) ? `"${arg}"` : arg))].join(' '))
const change = (op: Change['op'], id: string, detail = ''): Change => {
  const [type, slug] = id.split('/') as [string, string]

  return { op, entity: { type, slug }, detail, by: 'agent' }
}

// The session's healths: a clean baseline, then a relation typed by hand to a missing slug.
const clean = nextHealth(EMPTY_HEALTH, json(F.check_base), json(F.status_base), 12)
const broken = nextHealth(clean, json(F.check_dangling), json(F.status_dangling), 15)

test('a write record becomes a change, a no-op or a refusal becomes none', () => {
  expect(changeOf(callOf(F.add_adr), json(F.add_adr))).toEqual(change('add', ADR))
  expect(
    changeOf(call('khub add actor --title Temp --draft'), { type: 'actor', slug: 'act-temp', draft: true }),
  ).toEqual(change('add', 'actor/act-temp', 'draft'))
  expect(changeOf(callOf(F.edit_fix), json(F.edit_fix))).toEqual(change('edit', REQ, 'realized_in cmp-search'))
  expect(changeOf(call('khub edit cmp-search --lifecycle deprecated --format json'), json(F.edit_component))).toEqual(
    change('edit', 'component/cmp-search', 'lifecycle deprecated'),
  )
  expect(changeOf(callOf(F.link_adr), json(F.link_adr))).toEqual(change('link', ADR, 'affects cmp-search'))
  expect(changeOf(callOf(F.unlink_adr), json(F.unlink_adr))).toEqual(change('unlink', ADR, 'affects cmp-search'))
  expect(changeOf(callOf(F.remove_actor), json(F.remove_actor))).toEqual(change('remove', 'actor/act-temp'))

  expect(changeOf(callOf(F.link_adr_again), json(F.link_adr_again))).toBe(null)
  expect(changeOf(callOf(F.add_refused), json(F.add_refused))).toBe(null)
  expect(changeOf(callOf(F.get_search), json(F.get_search))).toBe(null)
  expect(changeOf(call('khub reindex'), undefined)).toBe(null)
})

test('an edited body is named in the ledger, never quoted', () => {
  for (const command of [
    'khub edit cmp-search --body "the secret text" --strict',
    'khub edit cmp-search --body-file notes.md',
    'khub edit cmp-search body "the secret text"',
  ]) {
    expect(changeOf(call(command), json(F.edit_component))?.detail).toBe('body')
  }

  expect(changeOf(call('khub edit cmp-search --tier tier-2 --body "the secret text"'), json(F.edit_component))?.detail).toBe(
    'tier tier-2, body',
  )
})

test('a validate document reduces to errors, gaps and lenses', () => {
  const passing = validated(F.validate_adr)
  const failing = validated(F.validate_requirement_dangling)

  expect(passing.errors).toEqual([])
  expect(passing.gaps).toEqual([])
  expect(passing.lenses[0]).toEqual({ code: 'alternatives', name: 'A real alternative, not a strawman' })
  expect(failing.errors).toEqual([UNRESOLVED])
  expect(failing.lenses.map(lens => lens.code)).toEqual(['single', 'testable', 'measurable', 'realized'])
  expect(validationOf({ errors: [], gaps: [{ field: 'body', reason: "'## Decision' 4 words of prose" }] })?.gaps).toEqual([
    "'## Decision' 4 words of prose",
  ])
})

test('a document that is no validate report is no validation', () => {
  expect(validationOf(undefined)).toBe(null)
  expect(validationOf(json(F.get_missing))).toBe(null)
  expect(validationOf(json(F.add_adr))).toBe(null)
})

test('every check bucket flattens to findings', () => {
  expect(findingsOf(json(F.check_base))).toEqual([])
  expect(findingsOf(json(F.check_dangling))).toEqual([
    { key: `orphans|${REQ}|orphans`, bucket: 'orphans', id: REQ, message: 'orphans', isError: false },
    {
      key: `dangling|${REQ}|predicate realized_in, target cmp-serach`,
      bucket: 'dangling',
      id: REQ,
      message: 'predicate realized_in, target cmp-serach',
      isError: true,
    },
  ])

  const shapes = findingsOf({
    passed: false,
    incomplete: [{ id: 'a/x', type: 'a', slug: 'x', missing_fields: ['kind'], missing_relations: [] }],
    misplaced: [{ path: 'notes/x.md', type: 'a', expected: 'knowledge/a' }],
    cycles: [['a/x', 'a/y', 'a/x']],
    suppressed_dangling: 0,
    draft_singletons: ['prd'],
    alias_conflicts: [{ alias: 'Initech', claimants: ['a/x', 'a/y'] }],
    strict: false,
    thin: [{ id: 'a/x', type: 'a', slug: 'x', reason: "'## Decision' 4 words of prose" }],
  })

  expect(shapes.map(finding => `${finding.id} › ${finding.bucket}: ${finding.message}`)).toEqual([
    'a/x › incomplete: missing_fields kind',
    'notes/x.md › misplaced: expected knowledge/a',
    'a/x › cycles: a/x → a/y → a/x',
    'prd › draft_singletons: draft_singletons',
    'Initech › alias_conflicts: claimants a/x → a/y',
    "a/x › thin: reason '## Decision' 4 words of prose",
  ])
  expect(shapes.map(finding => finding.isError)).toEqual([true, true, true, false, true, false])
  expect(findingsOf(json(F.get_missing))).toEqual([])
})

test('the first check fixes the baseline, later ones leave it', () => {
  expect(clean.passed).toBe(true)
  expect(clean.baseline).toEqual([])
  expect(clean.counts?.total).toBe(8)
  expect(clean.checkMs).toBe(12)

  expect(broken.passed).toBe(false)
  expect(broken.baseline).toEqual([])
  expect(broken.findings.length).toBe(2)
  expect(broken.counts?.total).toBe(10)

  const standing = nextHealth(EMPTY_HEALTH, json(F.check_dangling), json(F.status_dangling), 1)

  expect(standing.baseline?.length).toBe(2)
})

test('a new finding is told once, then told again as cleared', () => {
  const told = notesFor(broken, change('edit', REQ, 'file edited'), validationOf(json(F.validate_requirement_dangling)))

  expect(told.lines).toEqual([`${REQ} › validate: ${UNRESOLVED}`, 'khub check: new findings:', DANGLING, ORPHAN])
  expect(told.health.noted.length).toBe(2)
  expect(notesFor(told.health, null, null).lines).toEqual([])

  const fixed = nextHealth(told.health, json(F.check_fixed), json(F.status_fixed), 9)

  expect(fixed.passed).toBe(true)
  expect(fixed.noted).toEqual([])
  expect(fixed.cleared).toEqual([ORPHAN, DANGLING])

  const after = notesFor(fixed, null, null)

  expect(after.lines).toEqual(['khub check: cleared:', ORPHAN, DANGLING])
  expect(after.health.cleared).toEqual([])
  expect(notesFor(after.health, null, null).lines).toEqual([])
})

test('findings that stood before the session are told once, under their own heading', () => {
  const standing = nextHealth(EMPTY_HEALTH, json(F.check_dangling), json(F.status_dangling), 1)
  const told = notesFor(standing, null, null)

  expect(told.lines).toEqual(['khub check: findings that stood before this session:', DANGLING, ORPHAN])
  expect(notesFor(told.health, null, null).lines).toEqual([])
})

test('a group of findings is cut at ten lines', () => {
  const dangling = Array.from({ length: 12 }, (_, i) => ({ id: `a/x${i}`, predicate: 'p', target: 't' }))
  const many = { passed: false, dangling }
  const told = notesFor(nextHealth(clean, many, undefined, 1), null, null)

  expect(told.lines.length).toBe(12)
  expect(told.lines[0]).toBe('khub check: new findings:')
  expect(told.lines[11]).toBe('… and 2 more; run khub check')
})

test('a khub run that returns no check leaves the findings as they stood', () => {
  const told = notesFor(broken, null, null).health
  const failed = nextHealth(told, json(F.get_missing), undefined, 3)

  expect(failed.passed).toBe(null)
  expect(failed.findings).toEqual(told.findings)
  expect(failed.noted).toEqual(told.noted)
  expect(failed.cleared).toEqual([])
  expect(failed.counts).toEqual(told.counts)
})

test('the ledger keeps one entry per entity and one per edge', () => {
  const lensed = validated(F.validate_adr)
  const gapped = { errors: [], gaps: ['thin'], lenses: lensed.lenses }
  let ledger: KhubLedgerEntry[] = []

  ledger = ledgerWith(ledger, change('add', ADR), lensed, 't1')
  ledger = ledgerWith(ledger, change('link', ADR, 'affects cmp-search'), lensed, 't1')
  ledger = ledgerWith(ledger, change('link', ADR, 'affects cmp-search'), lensed, 't1')

  expect(ledger.map(entry => `${entry.op} ${entry.id} ${entry.detail}`.trim())).toEqual([
    `add ${ADR}`,
    `link ${ADR} affects cmp-search`,
  ])
  expect(ledger[0]?.lenses.length).toBe(4)
  expect(ledger[1]?.lenses).toEqual([])

  ledger = ledgerWith(ledger, change('edit', ADR, 'file edited'), gapped, 't2')

  expect(ledger.length).toBe(2)
  expect(ledger[0]).toEqual({
    op: 'add',
    id: ADR,
    detail: '',
    by: 'agent',
    since: 't1',
    turn: 't2',
    errors: 0,
    gaps: 1,
    lenses: lensed.lenses,
  })

  ledger = ledgerWith(ledger, change('remove', 'actor/act-temp'), null, 't2')

  expect(ledger.at(-1)?.op).toBe('remove')

  for (let i = 0; i < 205; i++) ledger = ledgerWith(ledger, change('remove', `actor/act-${i}`), null, 't3')

  expect(ledger.length).toBe(200)
})

test('the band line counts what the turn wrote and what check newly reports', () => {
  const invalid = validationOf(json(F.validate_requirement_dangling))
  let ledger: KhubLedgerEntry[] = []

  ledger = ledgerWith(ledger, change('add', ADR), validationOf(json(F.validate_adr)), 't1')
  ledger = ledgerWith(ledger, change('link', ADR, 'affects cmp-search'), null, 't1')
  ledger = ledgerWith(ledger, change('add', REQ), validationOf(undefined), 't1')
  ledger = ledgerWith(ledger, change('edit', REQ, 'file edited'), invalid, 't1')

  expect(noticeOf(ledger, broken, 't1')).toEqual({
    kind: 'findings',
    parts: [
      { text: '+1 adr', tone: 'ok' },
      { text: '+1 requirement', tone: 'ok' },
      { text: '1 edge', tone: 'plain' },
      { text: '1 new error', tone: 'bad' },
      { text: '1 gap', tone: 'warn' },
    ],
    buttons: ['review', 'check'],
  })

  // Another turn wrote nothing, and the findings still stand.
  expect(noticeOf(ledger, broken, 't2')?.parts.map(part => part.text)).toEqual(['1 new error', '1 gap'])
  expect(noticeOf(ledger, clean, 't2')).toBe(null)
  expect(noticeOf([], clean, 't1')).toBe(null)
})

test('the band line counts edits, removals and errors only validate reports', () => {
  let ledger: KhubLedgerEntry[] = []

  const invalid = { errors: ['kind: not valid'], gaps: [], lenses: [] }
  const thin = { errors: [], gaps: ['thin', 'thin'], lenses: [] }

  ledger = ledgerWith(ledger, change('edit', 'component/cmp-search', 'kind bogus'), invalid, 't1')
  ledger = ledgerWith(ledger, change('edit', 'component/cmp-storage', 'file edited'), thin, 't1')
  ledger = ledgerWith(ledger, change('remove', 'actor/act-temp'), null, 't1')
  ledger = ledgerWith(ledger, change('unlink', ADR, 'affects cmp-search'), null, 't1')
  ledger = ledgerWith(ledger, change('link', ADR, 'affects cmp-storage'), null, 't1')

  expect(noticeOf(ledger, clean, 't1')?.parts.map(part => part.text)).toEqual([
    '~2 edited',
    '2 edges',
    '−1 removed',
    '1 new error',
    '2 gaps',
  ])
})

test('an entity added in an earlier turn and touched in this one counts as edited', () => {
  let ledger: KhubLedgerEntry[] = []

  ledger = ledgerWith(ledger, change('add', ADR), validated(F.validate_adr), 't1')

  expect(noticeOf(ledger, clean, 't1')?.parts).toEqual([{ text: '+1 adr', tone: 'ok' }])
  expect(noticeOf(ledger, clean, 't2')).toBe(null)

  ledger = ledgerWith(ledger, change('edit', ADR, 'file edited'), validated(F.validate_adr), 't2')

  expect(ledger.map(entry => [entry.op, entry.since, entry.turn])).toEqual([['add', 't1', 't2']])
  expect(noticeOf(ledger, clean, 't2')?.parts).toEqual([{ text: '~1 edited', tone: 'plain' }])
  expect(noticeOf(ledger, clean, 't1')).toBe(null)

  // A second entity added in the later turn is counted as added beside it.
  ledger = ledgerWith(ledger, change('add', REQ), null, 't2')

  expect(noticeOf(ledger, clean, 't2')?.parts.map(part => part.text)).toEqual(['+1 requirement', '~1 edited'])
})

test('a finding in the baseline is not counted as new', () => {
  const standing = nextHealth(EMPTY_HEALTH, json(F.check_dangling), json(F.status_dangling), 1)

  expect(noticeOf([], standing, 't1')).toBe(null)
  expect(blockReason(standing)).toBe(undefined)
})

test('the validate line and mark say pass, gaps or errors', () => {
  const passing = validated(F.validate_adr)
  const failing = validated(F.validate_requirement_dangling)
  const gapped = { errors: [], gaps: ["'## Decision' 9 words of prose, at least 15 asked for", 'x'], lenses: [] }

  expect(validationLine(ADR, passing)).toEqual({ id: ADR, line: 'validate ✓', tone: 'ok' })
  expect(validationLine(REQ, failing)).toEqual({ id: REQ, line: `validate · 1 error · ${UNRESOLVED}`, tone: 'bad' })
  expect(validationLine(ADR, gapped)).toEqual({
    id: ADR,
    line: "validate · 2 gaps · '## Decision' 9 words of prose, at least 15 asked for",
    tone: 'warn',
  })

  expect(validationMark(passing)).toEqual({ text: ' ✓', tone: 'ok' })
  expect(validationMark(failing)).toEqual({ text: ' · 1 error', tone: 'bad' })
  expect(validationMark(gapped)).toEqual({ text: ' · 2 gaps', tone: 'warn' })
})

test('a blocking gate names the errors the session introduced', () => {
  const reason = blockReason(broken)

  expect(reason?.split('\n')).toEqual([
    'khub: this session introduced 1 error that `khub check` reports.',
    'Fix it before finishing:',
    DANGLING,
  ])
  expect(blockReason(clean)).toBe(undefined)
  expect(blockReason(EMPTY_HEALTH as KhubHealth)).toBe(undefined)
})

test('a gap validate reported is not repeated as a check finding', () => {
  const gap = "'## Context' 7 words of prose, at least 25 asked for"
  const thin = { key: `thin|${ADR}|reason ${gap}`, bucket: 'thin', id: ADR, message: `reason ${gap}`, isError: false }
  const other = { key: 'thin|adr/other|reason x', bucket: 'thin', id: 'adr/other', message: 'reason x', isError: false }
  const health: KhubHealth = { ...clean, findings: [thin, other] }
  const told = notesFor(health, change('add', ADR), { errors: [], gaps: [gap], lenses: [] })

  expect(told.lines).toEqual([`${ADR} › gap: ${gap}`, 'khub check: new findings:', 'adr/other › thin: reason x'])
  expect(told.health.noted).toEqual([thin.key, other.key])
})

test('lenses show once the body was written or changed, and stay after that', () => {
  const lensed = validated(F.validate_adr)

  // A field edit alone asks no question about the prose.
  expect(ledgerWith([], change('edit', ADR, 'status accepted'), lensed, 't1')[0]?.lenses).toEqual([])
  expect(ledgerWith([], change('edit', ADR, 'body'), lensed, 't1')[0]?.lenses.length).toBe(4)
  expect(ledgerWith([], change('edit', ADR, 'file edited'), lensed, 't1')[0]?.lenses.length).toBe(4)

  const added = ledgerWith([], change('add', ADR), lensed, 't1')

  expect(ledgerWith(added, change('edit', ADR, 'status accepted'), lensed, 't2')[0]?.lenses.length).toBe(4)
})
