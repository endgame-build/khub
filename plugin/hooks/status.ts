import type { KhubHealth, KhubStats } from '../types'
import { percentile } from './stats'

const P50_AFTER = 3

// The status line, such as `✗ 1 error · 146 entities · 3 draft · p50 38 ms`. The engine
// draws the plugin's name in front of it.
export function statusText(health: KhubHealth, stats: KhubStats): string {
  if (health.passed === null) return '…'

  const errors = health.findings.filter(finding => finding.isError).length
  const total = health.counts?.total ?? 0
  const draft = health.counts?.draft ?? 0
  const p50 = stats.durations.length >= P50_AFTER ? percentile(stats.durations, 50) : undefined

  return [
    health.passed ? '✓' : errors > 0 ? `✗ ${errors} error${errors === 1 ? '' : 's'}` : '✗',
    ...(total > 0 ? [`${total} ${total === 1 ? 'entity' : 'entities'}`] : []),
    ...(draft > 0 ? [`${draft} draft`] : []),
    ...(p50 === undefined ? [] : [`p50 ${p50} ms`]),
  ].join(' · ')
}
