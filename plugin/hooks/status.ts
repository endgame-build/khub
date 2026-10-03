import type { KhubHealth, KhubStats } from '../types'
import { percentile } from './stats'

const P50_AFTER = 3

// The status line, such as `khub ✗ 1 error · 146 entities · 3 draft · p50 38 ms`.
export function statusText(health: KhubHealth, stats: KhubStats): string {
  if (health.passed === null) return 'khub …'

  const errors = health.findings.filter(finding => finding.isError).length
  const total = health.counts?.total ?? 0
  const draft = health.counts?.draft ?? 0
  const p50 = stats.durations.length >= P50_AFTER ? percentile(stats.durations, 50) : undefined

  return [
    health.passed ? 'khub ✓' : errors > 0 ? `khub ✗ ${errors} error${errors === 1 ? '' : 's'}` : 'khub ✗',
    ...(total > 0 ? [`${total} ${total === 1 ? 'entity' : 'entities'}`] : []),
    ...(draft > 0 ? [`${draft} draft`] : []),
    ...(p50 === undefined ? [] : [`p50 ${p50} ms`]),
  ].join(' · ')
}
