import { expect, test } from 'claude-code/testing'

import { parseJson } from '../hooks/cli'
import { EMPTY_STATS } from '../hooks/state'
import { merged, percentile, since, withBypass, withCall } from '../hooks/stats'
import type { CallRecord } from '../hooks/stats'
import { F } from './fixtures'

const record = (over: Partial<CallRecord>): CallRecord => ({
  verb: 'get',
  isWrite: false,
  ms: 40,
  outcome: 'ok',
  code: null,
  bytes: 100,
  json: undefined,
  note: '',
  ...over,
})

test('calls are counted by verb, as reads or writes, with their bytes and durations', () => {
  let stats = withCall(EMPTY_STATS, record({ verb: 'get', ms: 40, bytes: 1106 }))

  stats = withCall(stats, record({ verb: 'add', isWrite: true, ms: 55, bytes: 182 }))
  stats = withCall(stats, record({ verb: 'get', ms: 30, bytes: 10 }))

  expect(stats.calls).toBe(3)
  expect(stats.reads).toBe(2)
  expect(stats.writes).toBe(1)
  expect(stats.byVerb).toEqual({ get: 2, add: 1 })
  expect(stats.bytes).toBe(1298)
  expect(stats.durations).toEqual([40, 55, 30])
  expect(stats.slowest).toEqual({ verb: 'add', ms: 55 })
  expect(EMPTY_STATS.calls).toBe(0)
})

test('a refusal is counted under its code', () => {
  let stats = withCall(EMPTY_STATS, record({ outcome: 'refused', code: 'lookup_error' }))

  stats = withCall(stats, record({ verb: 'add', isWrite: true, outcome: 'refused', code: 'referential_integrity' }))
  stats = withCall(stats, record({ outcome: 'refused', code: 'lookup_error' }))
  stats = withCall(stats, record({ verb: 'check', outcome: 'gate' }))

  expect(stats.refused).toEqual({ lookup_error: 2, referential_integrity: 1 })
})

test('a search is counted as empty or truncated from its hits and its note', () => {
  const hits = parseJson(F.search_hits.stdout)
  let stats = withCall(EMPTY_STATS, record({ verb: 'search', json: hits }))

  const none = parseJson(F.search_empty.stdout)

  stats = withCall(stats, record({ verb: 'search', json: none, note: F.search_empty.stderr }))
  stats = withCall(stats, record({ verb: 'search', json: hits, note: 'note: 3 of 40 hits shown' }))
  stats = withCall(stats, record({ verb: 'query', json: [] }))

  expect(stats.searches).toBe(3)
  expect(stats.searchEmpty).toBe(1)
  expect(stats.searchTruncated).toBe(1)
})

test('a compound command is counted and left out of the timing sample', () => {
  const stats = withCall(withCall(EMPTY_STATS, record({ ms: 20 })), record({ verb: 'compound', ms: 5000, bytes: 0 }))

  expect(stats.calls).toBe(2)
  expect(stats.byVerb).toEqual({ get: 1, compound: 1 })
  expect(stats.durations).toEqual([20])
  expect(stats.slowest).toEqual({ verb: 'get', ms: 20 })
})

test('the timing sample keeps the last five hundred calls', () => {
  let stats = EMPTY_STATS

  for (let ms = 1; ms <= 510; ms++) stats = withCall(stats, record({ ms }))

  expect(stats.durations.length).toBe(500)
  expect(stats.durations[0]).toBe(11)
  expect(stats.slowest).toEqual({ verb: 'get', ms: 510 })
})

test('a bypass is counted', () => {
  expect(withBypass(withBypass(EMPTY_STATS)).bypass).toBe(2)
})

test('a percentile is the nearest rank, in whole milliseconds', () => {
  expect(percentile([], 50)).toBe(undefined)
  expect(percentile([38.4], 50)).toBe(38)
  expect(percentile([50, 30, 38], 50)).toBe(38)
  expect(percentile([10, 20, 30, 40], 50)).toBe(20)
  expect(percentile([10, 20, 30, 40], 95)).toBe(40)
  expect(percentile([10, 20, 30, 40], 0)).toBe(10)
})

test('two tallies merge into one, and no earlier tally is the session alone', () => {
  const earlier = withCall(withCall(EMPTY_STATS, record({ verb: 'get', ms: 90, bytes: 10 })), record({ outcome: 'refused', code: 'lookup_error' }))
  const session = withBypass(withCall(EMPTY_STATS, record({ verb: 'get', ms: 40, bytes: 5 })))
  const all = merged(earlier, session)

  expect(merged(null, session)).toEqual(session)
  expect(all.calls).toBe(3)
  expect(all.byVerb.get).toBe(earlier.byVerb.get! + 1)
  expect(all.refused).toEqual({ lookup_error: 1 })
  expect(all.bytes).toBe(earlier.bytes + 5)
  expect(all.bypass).toBe(1)
  expect(all.durations).toEqual([...earlier.durations, 40])
  expect(all.slowest).toEqual(earlier.slowest)
  expect(merged(session, earlier).slowest).toEqual(earlier.slowest)
})

test('the merged sample keeps the newest five hundred durations', () => {
  const long = { ...EMPTY_STATS, durations: Array.from({ length: 500 }, (_, i) => i) }
  const all = merged(long, { ...EMPTY_STATS, durations: [1000, 1001] })

  expect(all.durations.length).toBe(500)
  expect(all.durations.slice(-2)).toEqual([1000, 1001])
  expect(all.durations[0]).toBe(2)
})

test('what a turn did is the tally now less the tally last written', () => {
  const before = withCall(EMPTY_STATS, record({ verb: 'get', ms: 40, bytes: 100 }))
  const now = withCall(withCall(before, record({ verb: 'get', ms: 10, bytes: 50 })), record({ verb: 'add', isWrite: true, ms: 90 }))
  const turn = since(now, before)

  expect(turn.calls).toBe(2)
  expect(turn.reads).toBe(1)
  expect(turn.writes).toBe(1)
  expect(turn.byVerb).toEqual({ get: 1, add: 1 })
  expect(turn.bytes).toBe(now.bytes - before.bytes)
  expect(turn.durations).toEqual([10, 90])

  // Two sessions that each add their own turn lose nothing of the other's.
  expect(merged(merged(before, turn), turn).calls).toBe(5)
  expect(since(now, now).calls).toBe(0)
})

test('a word that is no khub verb counts under one name', () => {
  const stats = withCall(withCall(EMPTY_STATS, record({ verb: 'qurey' })), record({ verb: 'serach' }))

  expect(stats.byVerb).toEqual({ other: 2 })
  expect(withCall(EMPTY_STATS, record({ verb: 'schema show' })).byVerb).toEqual({ 'schema show': 1 })
})
