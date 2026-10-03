// What a schema edit did, read from `khub schema diff` and the check that follows it.

import type { KhubNotice } from '../types'
import { count, firstLine, isRow, refusal, rowsOf, text } from './cli'
import type { Row } from './cli'

const CAP = 10

// The changes of a `khub schema diff` document that reports some pending.
const pending = (diff: unknown): Row[] => (isRow(diff) && diff.pending === true ? rowsOf(diff.changes) : [])

const shown = (value: unknown) => (typeof value === 'string' ? value : JSON.stringify(value))

function changeLine(change: Row): string {
  const hasBoth = change.from !== null && change.from !== undefined && change.to !== null && change.to !== undefined

  return `${text(change.op)} ${text(change.path)}${hasBoth ? ` (${shown(change.from)} → ${shown(change.to)})` : ''}`
}

// The lines the model reads after a schema file changed. `problem` is khub's complaint
// when the schema no longer resolves, `diff` is `khub schema diff --format json`.
export function schemaNotes(problem: string | null, diff: unknown): string[] {
  if (problem !== null) return [`khub: the schema no longer resolves. ${problem}`]

  if (refusal(diff)?.code === 'no_schema_snapshot') {
    return ['khub schema: no snapshot exists to compare against. `khub schema snapshot` records one.']
  }

  const changes = pending(diff)

  if (changes.length === 0) return []

  const more = changes.length - CAP

  return [
    `khub schema: ${count(changes.length, 'change')} since the last snapshot:`,
    ...changes.slice(0, CAP).map(changeLine),
    ...(more > 0 ? [`… and ${more} more`] : []),
  ]
}

// The band line for it, or null when nothing changed. A schema that does not resolve shows
// khub's complaint and offers nothing, since neither action runs on it.
export function schemaNotice(problem: string | null, diff: unknown): KhubNotice | null {
  if (problem !== null) return { kind: 'schema', parts: [{ text: firstLine(problem), tone: 'bad' }], buttons: [] }

  if (refusal(diff)?.code === 'no_schema_snapshot') {
    return { kind: 'schema', parts: [{ text: 'schema · no snapshot yet', tone: 'warn' }], buttons: ['snapshot'] }
  }

  const changes = pending(diff)

  return changes.length === 0
    ? null
    : {
        kind: 'schema',
        parts: [{ text: `schema · ${count(changes.length, 'change')} pending`, tone: 'warn' }],
        buttons: ['rewire', 'snapshot'],
      }
}

// The lines the model reads about entities the edited schema no longer accepts.
// `validation` is `khub validate --format json` over the whole workspace.
export function validateNotes(validation: unknown): string[] {
  const errors = isRow(validation) ? rowsOf(validation.errors) : []

  if (errors.length === 0) return []

  const more = errors.length - CAP

  return [
    `khub validate: ${count(errors.length, 'error')} under the edited schema:`,
    ...errors.slice(0, CAP).map(error => `${text(error.id)} › ${text(error.field)}: ${text(error.reason)}`),
    ...(more > 0 ? [`… and ${more} more`] : []),
  ]
}
