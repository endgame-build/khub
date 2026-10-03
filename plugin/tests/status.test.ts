import { expect, test } from 'claude-code/testing'

import type { KhubHealth } from '../types'
import { parseJson } from '../hooks/cli'
import { nextHealth } from '../hooks/integrity'
import { EMPTY_HEALTH, EMPTY_STATS } from '../hooks/state'
import { statusText } from '../hooks/status'
import { F } from './fixtures'

const json = (text: string) => parseJson(text)
const clean = nextHealth(EMPTY_HEALTH, json(F.check_base.stdout), json(F.status_base.stdout), 1)
const broken = nextHealth(clean, json(F.check_dangling.stdout), json(F.status_dangling.stdout), 1)

test('before the first check the line only names khub', () => {
  expect(statusText(EMPTY_HEALTH, EMPTY_STATS)).toBe('khub …')
})

test('a passing workspace shows a check mark and its counts', () => {
  expect(statusText(clean, EMPTY_STATS)).toBe('khub ✓ · 8 entities')
})

test('a failing workspace shows its error count, informational findings left out', () => {
  expect(statusText(broken, EMPTY_STATS)).toBe('khub ✗ 1 error · 10 entities')
})

test('p50 appears once three calls were timed', () => {
  expect(statusText(clean, { ...EMPTY_STATS, durations: [30, 50] })).toBe('khub ✓ · 8 entities')
  expect(statusText(clean, { ...EMPTY_STATS, durations: [30, 50, 38] })).toBe('khub ✓ · 8 entities · p50 38 ms')
})

test('errors, entities, drafts and p50 read as one line', () => {
  const health: KhubHealth = {
    ...broken,
    counts: { counts: {}, total: 146, draft: 3, active: 143, orphan: 2, stale: 6, stray: 0, malformed: 0 },
  }

  expect(statusText(health, { ...EMPTY_STATS, durations: [30, 50, 38] })).toBe(
    'khub ✗ 1 error · 146 entities · 3 draft · p50 38 ms',
  )
})

test('counts are singular where one', () => {
  const health: KhubHealth = {
    ...clean,
    counts: { counts: {}, total: 1, draft: 0, active: 1, orphan: 0, stale: 0, stray: 0, malformed: 0 },
  }

  expect(statusText(health, EMPTY_STATS)).toBe('khub ✓ · 1 entity')
})
