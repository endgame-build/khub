import type { KhubCall, KhubStats } from '../types'
import { isVerb } from './parse'

export type CallRecord = {
  verb: string
  isWrite: boolean
  ms: number
  outcome: KhubCall['outcome']
  code: string | null
  bytes: number
  json: unknown
  note: string
}

const SAMPLE = 500

const bump = (tally: Record<string, number>, name: string) => ({ ...tally, [name]: (tally[name] ?? 0) + 1 })

export function withCall(stats: KhubStats, call: CallRecord): KhubStats {
  // A mistyped command is not a verb, and counts under one name.
  const record = { ...call, verb: call.verb === 'compound' || isVerb(call.verb.split(' ')[0]) ? call.verb : 'other' }
  const isSearch = record.verb === 'search'
  const hits = Array.isArray(record.json) ? record.json.length : undefined

  // A compound command's duration covers more than khub, so it stays out of the sample.
  const isTimed = record.verb !== 'compound'
  const isSlowest = isTimed && (stats.slowest === null || record.ms > stats.slowest.ms)

  return {
    ...stats,
    calls: stats.calls + 1,
    reads: stats.reads + (record.isWrite ? 0 : 1),
    writes: stats.writes + (record.isWrite ? 1 : 0),
    byVerb: bump(stats.byVerb, record.verb),
    refused: record.outcome === 'refused' ? bump(stats.refused, record.code ?? 'unknown') : stats.refused,
    searches: stats.searches + (isSearch ? 1 : 0),
    searchEmpty: stats.searchEmpty + (isSearch && hits === 0 ? 1 : 0),
    searchTruncated: stats.searchTruncated + (isSearch && hits !== undefined && hits > 0 && record.note !== '' ? 1 : 0),
    bytes: stats.bytes + record.bytes,
    durations: isTimed ? [...stats.durations, record.ms].slice(-SAMPLE) : stats.durations,
    slowest: isSlowest ? { verb: record.verb, ms: record.ms } : stats.slowest,
  }
}

// A Read of an entity file that went around khub.
export function withBypass(stats: KhubStats): KhubStats {
  return { ...stats, bypass: stats.bypass + 1 }
}

// The nearest-rank percentile, in whole milliseconds.
export function percentile(durations: readonly number[], p: number): number | undefined {
  if (durations.length === 0) return undefined

  const sorted = [...durations].sort((a, b) => a - b)
  const rank = Math.min(sorted.length, Math.max(1, Math.ceil((p / 100) * sorted.length)))

  return Math.round(sorted[rank - 1] as number)
}

const summed = (a: Record<string, number>, b: Record<string, number>) =>
  Object.entries(b).reduce((tally, [name, n]) => ({ ...tally, [name]: (tally[name] ?? 0) + n }), a)

const less = (a: Record<string, number>, b: Record<string, number>) =>
  Object.fromEntries(Object.entries(a).flatMap(([name, n]) => (n - (b[name] ?? 0) > 0 ? [[name, n - (b[name] ?? 0)]] : [])))

// What a session did since `before`, an earlier reading of the same tally.
export function since(now: KhubStats, before: KhubStats): KhubStats {
  const timed = now.durations.length - before.durations.length

  return {
    calls: now.calls - before.calls,
    reads: now.reads - before.reads,
    writes: now.writes - before.writes,
    byVerb: less(now.byVerb, before.byVerb),
    refused: less(now.refused, before.refused),
    searches: now.searches - before.searches,
    searchEmpty: now.searchEmpty - before.searchEmpty,
    searchTruncated: now.searchTruncated - before.searchTruncated,
    bytes: now.bytes - before.bytes,
    bypass: now.bypass - before.bypass,

    // The sample is capped, so past the cap the newest durations stand in.
    durations: timed > 0 ? now.durations.slice(-timed) : [],
    slowest: now.slowest,
  }
}

// Two tallies as one, for the all-sessions view. The sample keeps the newest durations.
export function merged(all: KhubStats | null, session: KhubStats): KhubStats {
  if (all === null) return session

  return {
    calls: all.calls + session.calls,
    reads: all.reads + session.reads,
    writes: all.writes + session.writes,
    byVerb: summed(all.byVerb, session.byVerb),
    refused: summed(all.refused, session.refused),
    searches: all.searches + session.searches,
    searchEmpty: all.searchEmpty + session.searchEmpty,
    searchTruncated: all.searchTruncated + session.searchTruncated,
    bytes: all.bytes + session.bytes,
    bypass: all.bypass + session.bypass,
    durations: [...all.durations, ...session.durations].slice(-SAMPLE),
    slowest: (session.slowest?.ms ?? -1) > (all.slowest?.ms ?? -1) ? session.slowest : all.slowest,
  }
}
