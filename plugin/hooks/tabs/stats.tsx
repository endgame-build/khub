import type { RenderElement } from 'claude-code'

import type { KhubHealth, KhubStats } from '../../types'
import type { El } from '../el'
import type { PaneActions, PaneModel } from '../pane'
import { percentile } from '../stats'

// Verbs the `verbs` row names.
const VERBS_SHOWN = 6

const tally = (counts: Record<string, number>, limit = Infinity) =>
  Object.entries(counts)
    .sort((a, b) => b[1] - a[1])
    .slice(0, limit)
    .map(([name, n]) => `${name} ${n}`)
    .join(' · ')

// The rows of the tab as label, count and detail. A row with nothing to say is left out.
export function statsRows(stats: KhubStats, health: KhubHealth): Array<[string, string, string]> {
  const p50 = percentile(stats.durations, 50)
  const p95 = percentile(stats.durations, 95)
  const refused = Object.values(stats.refused).reduce((sum, n) => sum + n, 0)
  const slowest = stats.slowest ? ` · slowest ${stats.slowest.verb} ${Math.round(stats.slowest.ms)} ms` : ''

  const rows: Array<[string, string, string] | null> = [
    ['calls', String(stats.calls), `read ${stats.reads} · write ${stats.writes}`],
    ['verbs', '', tally(stats.byVerb, VERBS_SHOWN)],
    refused > 0 ? ['refused', String(refused), tally(stats.refused)] : null,
    stats.searches > 0
      ? ['search', String(stats.searches), `${stats.searchEmpty} empty · ${stats.searchTruncated} truncated`]
      : null,
    stats.bytes > 0 ? ['context', '', `est. ${(stats.bytes / 4000).toFixed(1)}k tokens of khub output`] : null,
    stats.bypass > 0 ? ['bypass', String(stats.bypass), 'direct reads of entity files'] : null,
    p50 !== undefined && p95 !== undefined ? ['time', '', `p50 ${p50} ms · p95 ${p95} ms${slowest}`] : null,
    health.checkMs > 0 ? ['check', '', `${Math.round(health.checkMs)} ms at ${health.counts?.total ?? 0} entities`] : null,
  ]

  return rows.filter((row): row is [string, string, string] => row !== null)
}

// How the agent used khub, and how long khub took.
export function statsTab({ Box, Text, Button }: El, model: PaneModel, actions: PaneActions): RenderElement {
  const isAll = model.ui.statsScope === 'all'
  const stats = isAll ? (model.allStats ?? model.stats) : model.stats

  return (
    <Box flexDirection="column">
      <Box justifyContent="space-between">
        <Text dimColor>{isAll ? 'ALL SESSIONS' : 'THIS SESSION'}</Text>
        <Button
          key="scope"
          label={isAll ? 'This session' : 'All sessions'}
          onPress={() => actions.statsScope(isAll ? 'session' : 'all')}
        />
      </Box>
      {stats.calls === 0 ? (
        <Text dimColor>No khub calls yet.</Text>
      ) : (
        statsRows(stats, model.health).map(([label, n, detail]) => (
          <Box>
            <Box width={9} flexShrink={0}>
              <Text>{label}</Text>
            </Box>
            <Box width={5} flexShrink={0}>
              <Text>{n}</Text>
            </Box>
            <Text wrap="truncate-end">{detail}</Text>
          </Box>
        ))
      )}
    </Box>
  )
}
