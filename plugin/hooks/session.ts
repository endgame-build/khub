// This file tracks what the session changed and builds the band's line. The functions are pure.

import type { KhubSession, KhubSummary, KhubTone } from '../types'
import { count, isRow, refusal, rowsOf, text } from './cli'
import type { Simple } from './parse'
import { failingOf } from './summarize'

export const EMPTY_SESSION: KhubSession = { baseline: null, current: [], edited: [] }

// These verbs change an entity in place. An add or a remove moves the id set instead.
const EDITS = new Set(['edit', 'link', 'unlink'])

// Returns what a refresh read from `khub check` and `khub query`, or null when either
// document is not the one khub prints.
export function summaryOf(check: unknown, query: unknown): { summary: KhubSummary; ids: string[] } | null {
  if (!isRow(check) || typeof check.passed !== 'boolean' || !Array.isArray(query)) return null

  const rows = rowsOf(query)
  const errors = failingOf(check).reduce((sum, found) => sum + found.items.length, 0)

  return {
    summary: { total: rows.length, draft: rows.filter(row => row.draft === true).length, passed: check.passed, errors },
    ids: rows.map(row => text(row.id)).filter(id => id !== ''),
  }
}

// Returns the entity a khub call changed in place, by qualified id. A refusal and an edge
// call that changed nothing name none.
export function editedBy(call: Simple, json: unknown): string[] {
  if (!EDITS.has(call.verb) || !isRow(json) || refusal(json) || json.changed === false) return []

  const id = text(json.id)

  return id === '' ? [] : [id]
}

// Returns the session after a refresh read `ids`. The first read fixes the baseline.
export const withIds = (session: KhubSession, ids: string[]): KhubSession => ({
  ...session,
  baseline: session.baseline ?? ids,
  current: ids,
})

// Returns the session after its tools changed the entities `ids` name.
export function withEdited(session: KhubSession, ids: string[]): KhubSession {
  const fresh = ids.filter(id => !session.edited.includes(id))

  return fresh.length === 0 ? session : { ...session, edited: [...session.edited, ...fresh] }
}

export type Diff = { added: number; removed: number; edited: number }

// Counts against the baseline. An entity added and then edited counts as added alone.
export function diffOf(session: KhubSession): Diff {
  const baseline = new Set(session.baseline ?? session.current)
  const current = new Set(session.current)

  return {
    added: session.current.filter(id => !baseline.has(id)).length,
    removed: [...baseline].filter(id => !current.has(id)).length,
    edited: session.edited.filter(id => baseline.has(id) && current.has(id)).length,
  }
}

export type Part = { text: string; tone: KhubTone }

// Returns the band's line after its `khub` label, such as
// `build-hub 0.6.0 · 87 entities [+2 −1 ~3] · 4 draft · check ✓`. A plain part draws dim.
export function bandParts(preset: string, version: string, summary: KhubSummary, diff: Diff): Part[] {
  const plain = (text: string): Part => ({ text, tone: 'plain' })
  const changes: Part[] = [
    ...(diff.added > 0 ? [{ text: `+${diff.added}`, tone: 'ok' as const }] : []),
    ...(diff.removed > 0 ? [{ text: `−${diff.removed}`, tone: 'bad' as const }] : []),
    ...(diff.edited > 0 ? [plain(`~${diff.edited}`)] : []),
  ]
  const name = [preset, version].filter(word => word !== '').join(' ')
  const check = summary.passed ? 'check ✓' : `check ✗ ${count(summary.errors, 'error')}`

  return [
    plain(`${name === '' ? '' : `${name} · `}${count(summary.total, 'entity', 'entities')}`),
    ...(changes.length > 0 ? [plain(' ['), ...changes.flatMap((part, i) => (i > 0 ? [plain(' '), part] : [part])), plain(']')] : []),
    plain(`${summary.draft > 0 ? ` · ${summary.draft} draft` : ''} · ${check}`),
  ]
}
