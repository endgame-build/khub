// The integrity loop's arithmetic over findings, deltas, notes, the ledger and the band line.

import type {
  KhubCounts,
  KhubFinding,
  KhubHealth,
  KhubLedgerEntry,
  KhubLens,
  KhubNotice,
  KhubPart,
  KhubValidation,
} from '../types'
import { count, INFORMATIONAL, isRow as isDoc, rowsOf as rows } from './cli'
import { edited } from './parse'
import type { Simple } from './parse'
import type { Entity } from './workspace'

export type Change = { op: KhubLedgerEntry['op']; entity: Entity; detail: string; by: 'agent' | 'user' }

// `khub validate <type>/<slug> --format json`, reduced to what the mod shows.
export type Validation = { errors: string[]; gaps: string[]; lenses: KhubLens[] }

const NOTE_CAP = 10
const LEDGER_CAP = 200

const idOf = (entity: Entity) => `${entity.type}/${entity.slug}`
// A row that is only an id has its bucket as its message, which the line says once.
export const lineOf = (finding: KhubFinding) =>
  `${finding.id} › ${finding.bucket}${finding.message === finding.bucket ? '' : `: ${finding.message}`}`

// The change a khub write record describes, or null when it changed no entity.
export function changeOf(call: Simple, json: unknown): Change | null {
  if (!isDoc(json) || typeof json.type !== 'string' || typeof json.slug !== 'string') return null

  const entity = { type: json.type, slug: json.slug }
  const by = 'agent' as const

  switch (call.verb) {
    case 'add':
      return { op: 'add', entity, detail: json.draft === true ? 'draft' : '', by }
    case 'edit':
      return { op: 'edit', entity, detail: edited(call), by }
    case 'remove':
      return json.removed === true ? { op: 'remove', entity, detail: '', by } : null
    case 'link':
    case 'unlink':
      return json.changed === false ? null : { op: call.verb, entity, detail: `${json.predicate} ${json.target}`, by }
    default:
      return null
  }
}

// Null when the document is not a validate report, as when khub refused the id.
export function validationOf(json: unknown): Validation | null {
  if (!isDoc(json) || !Array.isArray(json.errors)) return null

  const doc = json

  return {
    errors: rows(doc.errors).map(row => `${row.field}: ${row.reason}`),
    gaps: rows(doc.gaps).map(row => String(row.reason)),
    lenses: rows(doc.lenses).map(row => ({ code: String(row.code), name: String(row.name ?? row.code) })),
  }
}

const shown = (value: unknown): string =>
  Array.isArray(value) ? value.map(shown).join(' → ') : isDoc(value) ? JSON.stringify(value) : String(value)

const isEmpty = (value: unknown) => value === null || value === '' || (Array.isArray(value) && value.length === 0)

// One row of a bucket. A string is an id, a list is a cycle, and a record names itself by id, path or alias.
function findingOf(bucket: string, row: unknown): KhubFinding {
  let id = String(row)
  let message = bucket

  if (Array.isArray(row)) {
    id = String(row[0])
    message = shown(row)
  } else if (isDoc(row)) {
    const named = ['id', 'path', 'alias'].find(name => typeof row[name] === 'string') ?? 'id'
    const fields = Object.entries(row).filter(
      ([name, value]) => name !== named && name !== 'id' && name !== 'type' && name !== 'slug' && !isEmpty(value),
    )

    id = String(row[named] ?? '?')
    message = fields.map(([name, value]) => `${name} ${shown(value)}`).join(', ') || bucket
  }

  return { key: `${bucket}|${id}|${message}`, bucket, id, message, isError: !INFORMATIONAL.has(bucket) }
}

// The rows of every `khub check` bucket, flattened.
export function findingsOf(check: unknown): KhubFinding[] {
  if (!isDoc(check)) return []

  return Object.entries(check).flatMap(([bucket, value]) =>
    Array.isArray(value) ? value.map(row => findingOf(bucket, row)) : [],
  )
}

// Health after a check and a status run. The first run fixes the baseline.
// A document that is not a check (khub could not run it) leaves the findings as they stood.
export function nextHealth(prev: KhubHealth, check: unknown, status: unknown, checkMs: number): KhubHealth {
  const counts = isDoc(status) && isDoc(status.counts) ? (status as KhubCounts) : prev.counts

  if (!isDoc(check) || typeof check.passed !== 'boolean') return { ...prev, counts, passed: null, checkMs }

  const findings = findingsOf(check)
  const keys = findings.map(finding => finding.key)
  const present = new Set(keys)
  const gone = prev.findings.filter(finding => prev.noted.includes(finding.key) && !present.has(finding.key))

  return {
    counts,
    passed: check.passed,
    findings,
    baseline: prev.baseline ?? keys,
    noted: prev.noted.filter(key => present.has(key)),
    cleared: [...prev.cleared, ...gone.map(lineOf)],
    checkMs,
  }
}

// A headed group of finding lines, errors first, cut at ten.
function group(heading: string, findings: KhubFinding[]): string[] {
  if (findings.length === 0) return []

  const ordered = [...findings.filter(finding => finding.isError), ...findings.filter(finding => !finding.isError)]
  const more = ordered.length - NOTE_CAP

  return [
    heading,
    ...ordered.slice(0, NOTE_CAP).map(lineOf),
    ...(more > 0 ? [`… and ${more} more; run khub check`] : []),
  ]
}

// What to tell the model after a write, and the health with those findings marked as told.
export function notesFor(
  health: KhubHealth,
  change: Change | null,
  validation: Validation | null,
): { lines: string[]; health: KhubHealth } {
  const id = change ? idOf(change.entity) : ''
  const baseline = new Set(health.baseline ?? [])
  const reasons = change && validation ? [...validation.errors, ...validation.gaps] : []

  // What validate just said about the changed entity is not said again as a check finding.
  const isSaid = (finding: KhubFinding) => finding.id === id && reasons.some(reason => finding.message.includes(reason))
  const untold = health.findings.filter(finding => !health.noted.includes(finding.key) && !isSaid(finding))

  const lines = [
    ...(change && validation ? validation.errors.map(error => `${id} › validate: ${error}`) : []),
    ...(change && validation ? validation.gaps.map(gap => `${id} › gap: ${gap}`) : []),
    ...group(
      'khub check: findings that stood before this session:',
      untold.filter(finding => baseline.has(finding.key)),
    ),
    ...group(
      'khub check: new findings:',
      untold.filter(finding => !baseline.has(finding.key)),
    ),
    ...(health.cleared.length > 0 ? ['khub check: cleared:', ...health.cleared] : []),
  ]

  return { lines, health: { ...health, noted: health.findings.map(finding => finding.key), cleared: [] } }
}

// The ledger after a change. An entity added or edited keeps one entry, which the
// latest validation of that entity updates; an edge or a removal is an entry of its own.
// An add writes the body from its template, a file edit may have changed it, and a khub
// edit names it.
export const FILE_EDITED = 'file edited'

const touchesBody = (change: Change) =>
  change.op === 'add' || change.detail === FILE_EDITED || /\bbody\b/.test(change.detail)

export function ledgerWith(
  ledger: KhubLedgerEntry[],
  change: Change,
  validation: Validation | null,
  turn: string,
): KhubLedgerEntry[] {
  const id = idOf(change.entity)
  const isEntity = change.op === 'add' || change.op === 'edit'
  const at = ledger.findIndex(entry => entry.id === id && (entry.op === 'add' || entry.op === 'edit'))

  // Lenses are questions about the prose, so they show once the body was written or changed.
  const hasLenses = touchesBody(change) || (ledger[at]?.lenses.length ?? 0) > 0
  const judged = validation
    ? { errors: validation.errors.length, gaps: validation.gaps.length, lenses: hasLenses ? validation.lenses : [] }
    : {}

  // The entity's own entry carries what validate last said about it.
  const updated = ledger.map((entry, i) => (i === at ? { ...entry, ...judged, ...(isEntity ? { turn } : {}) } : entry))

  if (isEntity && at >= 0) return updated
  if (!isEntity && ledger.some(entry => entry.op === change.op && entry.id === id && entry.detail === change.detail)) {
    return updated
  }

  const entry: KhubLedgerEntry = {
    op: change.op,
    id,
    detail: change.detail,
    by: change.by,
    since: turn,
    turn,
    errors: 0,
    gaps: 0,
    lenses: [],
    ...(isEntity ? judged : {}),
  }

  return [...updated, entry].slice(-LEDGER_CAP)
}

// Per entity touched this turn, the larger of what validate counted and what check
// newly reports for it, so a problem both gates name is counted once. Findings on
// anything else count in full.
function countOnce(
  entries: KhubLedgerEntry[],
  findings: KhubFinding[],
  counted: (entry: KhubLedgerEntry) => number,
): number {
  const ids = [...new Set(entries.map(entry => entry.id))]
  const forId = (id: string) => findings.filter(finding => finding.id === id).length
  const validated = (id: string) =>
    Math.max(0, ...entries.filter(entry => entry.id === id && (entry.op === 'add' || entry.op === 'edit')).map(counted))

  return (
    ids.reduce((sum, id) => sum + Math.max(validated(id), forId(id)), 0) +
    findings.filter(finding => !ids.includes(finding.id)).length
  )
}

// The band's one line, or null when nothing needs attention.
export function noticeOf(ledger: KhubLedgerEntry[], health: KhubHealth, turn: string): KhubNotice | null {
  const mine = ledger.filter(entry => entry.turn === turn)
  const baseline = new Set(health.baseline ?? [])
  const fresh = health.findings.filter(finding => !baseline.has(finding.key))
  const added = new Map<string, number>()

  // An entity added in an earlier turn and touched in this one counts as edited.
  const isAdded = (entry: KhubLedgerEntry) => entry.op === 'add' && entry.since === turn

  for (const entry of mine.filter(isAdded)) {
    const type = entry.id.split('/')[0] as string

    added.set(type, (added.get(type) ?? 0) + 1)
  }

  const edits = mine.filter(entry => entry.op === 'edit' || (entry.op === 'add' && !isAdded(entry))).length
  const edges = mine.filter(entry => entry.op === 'link' || entry.op === 'unlink').length
  const removed = mine.filter(entry => entry.op === 'remove').length
  const errors = countOnce(mine, fresh.filter(finding => finding.isError), entry => entry.errors)
  const gaps = countOnce(mine, fresh.filter(finding => !finding.isError), entry => entry.gaps)

  const parts: KhubPart[] = [
    ...[...added].map(([type, n]): KhubPart => ({ text: `+${n} ${type}`, tone: 'ok' })),
    ...(edits > 0 ? [{ text: `~${edits} edited`, tone: 'plain' } as const] : []),
    ...(edges > 0 ? [{ text: count(edges, 'edge'), tone: 'plain' } as const] : []),
    ...(removed > 0 ? [{ text: `−${removed} removed`, tone: 'plain' } as const] : []),
    ...(errors > 0 ? [{ text: count(errors, 'new error'), tone: 'bad' } as const] : []),
    ...(gaps > 0 ? [{ text: count(gaps, 'gap'), tone: 'warn' } as const] : []),
  ]

  return parts.length > 0 ? { kind: 'findings', parts, buttons: ['review', 'check'] } : null
}

// The line under an Edit or Write row on an entity file.
export function validationLine(id: string, validation: Validation): KhubValidation {
  const { errors, gaps } = validation
  const said = (found: string[], noun: string) => `validate · ${count(found.length, noun)} · ${found[0]}`

  if (errors.length > 0) return { id, line: said(errors, 'error'), tone: 'bad' }
  if (gaps.length > 0) return { id, line: said(gaps, 'gap'), tone: 'warn' }

  return { id, line: 'validate ✓', tone: 'ok' }
}

// What a khub add or edit row says about the entity it wrote, such as ` ✓`, ` · 1 gap` or ` · 2 errors`.
export function validationMark(validation: Validation): KhubPart {
  if (validation.errors.length > 0) return { text: ` · ${count(validation.errors.length, 'error')}`, tone: 'bad' }
  if (validation.gaps.length > 0) return { text: ` · ${count(validation.gaps.length, 'gap')}`, tone: 'warn' }

  return { text: ' ✓', tone: 'ok' }
}

// Why a turn may not finish under `gate: block`, or undefined when it may.
export function blockReason(health: KhubHealth): string | undefined {
  const baseline = new Set(health.baseline ?? [])
  const fresh = health.findings.filter(finding => finding.isError && !baseline.has(finding.key))

  if (fresh.length === 0) return undefined

  const more = fresh.length - NOTE_CAP

  return [
    `khub: this session introduced ${count(fresh.length, 'error')} that \`khub check\` reports.`,
    `Fix ${fresh.length === 1 ? 'it' : 'them'} before finishing:`,
    ...fresh.slice(0, NOTE_CAP).map(lineOf),
    ...(more > 0 ? [`… and ${more} more; run khub check`] : []),
  ].join('\n')
}
