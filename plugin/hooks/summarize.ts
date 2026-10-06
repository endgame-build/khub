import type { KhubCall, KhubRow, KhubTone } from '../types'
import { clean, count, firstLine, INFORMATIONAL, isRow, refusal, rowsOf, text } from './cli'
import type { Row } from './cli'
import { edited } from './parse'
import type { Simple } from './parse'

export type Summary = { head: string; line: string; tone: KhubTone }

type Result = { line: string; tone: KhubTone }

// The first positional of these verbs names what the call is about, a type for add and an
// id for the rest.
const SUBJECT_VERBS = new Set([
  'add', 'edit', 'get', 'history', 'impact', 'link', 'neighbors', 'remove', 'unlink', 'validate',
])

const NOTHING: Result = { line: '', tone: 'plain' }

const sizeOf = (value: unknown) => (Array.isArray(value) ? value.length : 0)

// Returns the title a record carries, at its top or in its frontmatter. An entity is
// titled by `title` or by `name`, so both are read.
function titleOf(row: Row): string {
  const meta = isRow(row.frontmatter) ? row.frontmatter : row

  return text(meta.title ?? meta.name)
}

// Returns the buckets of a `khub check` payload that fail the gate, each with its findings.
export function failingOf(check: Row): Array<{ bucket: string; items: unknown[] }> {
  return Object.entries(check)
    .filter((entry): entry is [string, unknown[]] => Array.isArray(entry[1]) && entry[1].length > 0)
    .filter(([bucket]) => !INFORMATIONAL.has(bucket) || (bucket === 'orphans' && check.strict === true))
    .map(([bucket, items]) => ({ bucket, items }))
}

function subjectOf(call: Simple): string {
  if (call.verb === 'query') return typeof call.flags.type === 'string' ? call.flags.type : ''
  if (call.verb === 'search') return call.positional[0] === undefined ? '' : `"${call.positional[0]}"`
  if (call.verb === 'schema') return call.sub === 'show' ? (call.positional[0] ?? '') : ''

  return SUBJECT_VERBS.has(call.verb) ? (call.positional[0] ?? '') : ''
}

// The subject is text the agent typed, so it is cleaned like entity text.
const headOf = (call: Simple) =>
  clean(['khub', call.verb, call.sub ?? '', subjectOf(call)].filter(word => word !== '').join(' '))

function edgeResult(call: Simple, row: Row): Result {
  if (row.changed === false) {
    return { line: call.verb === 'link' ? 'edge already present' : 'no such edge', tone: 'plain' }
  }

  // An edge added draws `→`, an edge taken away `⇢`.
  const sign = call.verb === 'link' ? '→' : '⇢'

  return { line: `${sign} ${text(row.slug)} ${text(row.predicate)} ${text(row.target)}`, tone: 'ok' }
}

function searchResult(rows: Row[], note: string): Result {
  const hits = rows.length === 0 ? 'no hits' : `${count(rows.length, 'hit')} · top ${text(rows[0]?.id)}`
  const noted = firstLine(note).replace(/^note: /, '').slice(0, 80)

  if (noted === '') return { line: hits, tone: 'plain' }

  // khub's own note for an empty result already says there were no hits.
  return { line: rows.length === 0 && /^no hits/i.test(noted) ? noted : `${hits} · ${noted}`, tone: 'warn' }
}

function validateResult(row: Row): Result {
  const errors = sizeOf(row.errors)
  const gaps = sizeOf(row.gaps)
  const line = `${text(row.count)} checked · ${count(errors, 'error')} · ${count(gaps, 'gap')}`

  if (errors > 0) return { line, tone: 'bad' }

  return gaps > 0 ? { line, tone: 'warn' } : { line: `${line} ✓`, tone: 'ok' }
}

function checkResult(row: Row): Result {
  if (row.passed === true) return { line: 'passed ✓', tone: 'ok' }

  const failing = failingOf(row)

  return { line: ['failed', ...failing.map(found => `${found.bucket} ${found.items.length}`)].join(' · '), tone: 'bad' }
}

function schemaResult(call: Simple, json: unknown): Result {
  const row = isRow(json) ? json : {}

  if (call.sub === null || call.sub === 'types') {
    return { line: count(sizeOf(Array.isArray(json) ? json : row.types), 'type'), tone: 'plain' }
  }

  if (call.sub === 'show') {
    const fields = count(sizeOf(row.fields), 'field')

    return { line: `${text(row.name)} · ${fields} · ${count(sizeOf(row.relations), 'relation')}`, tone: 'plain' }
  }

  if (call.sub === 'edges') return { line: count(sizeOf(json), 'predicate'), tone: 'plain' }

  if (call.sub === 'diff') {
    const line = row.pending === true ? `pending · ${count(sizeOf(row.changes), 'change')}` : 'no pending changes'

    return { line, tone: 'plain' }
  }

  return call.sub === 'snapshot' ? { line: `snapshot · ${count(Number(row.types), 'type')}`, tone: 'ok' } : NOTHING
}

// Reads what a call's JSON document says, by verb and by the document's shape.
function resultOf(call: Simple, json: unknown, note: string): Result {
  const rows = rowsOf(json)
  const row = isRow(json) ? json : {}
  const entities = { line: rows.length === 0 ? 'no entities' : count(rows.length, 'entity', 'entities'), tone: 'plain' } as const

  switch (call.verb) {
    case 'add':
      return { line: `+ ${text(row.id)}${row.draft === true ? ' · draft' : ''}`, tone: 'ok' }

    case 'edit': {
      const fields = edited(call)

      return { line: `~ ${text(row.id ?? call.positional[0])}${fields === '' ? '' : ` · ${fields}`}`, tone: 'ok' }
    }

    case 'link':
    case 'unlink':
      return edgeResult(call, row)

    case 'remove':
      return { line: `− ${text(row.id)}`, tone: 'ok' }

    case 'get': {
      if (Array.isArray(json)) return entities

      const title = titleOf(row)
      const edges = Array.isArray(row.edges) ? count(row.edges.length, 'edge') : ''

      return { line: [text(row.id), title, edges].filter(part => part !== '').join(' · '), tone: 'plain' }
    }

    case 'query':
    case 'stale':
      return entities

    case 'search':
      return searchResult(rows, note)

    case 'neighbors': {
      const inbound = rows.filter(found => found.direction === 'in').length

      return { line: `${count(rows.length, 'neighbor')} · in ${inbound} · out ${rows.length - inbound}`, tone: 'plain' }
    }

    case 'impact': {
      // The depth-0 row is the entity itself.
      const depths = rows.map(found => Number(found.depth)).filter(depth => depth > 0)

      return { line: `${depths.length} affected · depth ${Math.max(0, ...depths)}`, tone: 'plain' }
    }

    case 'history':
      return { line: `${rows.length} in chain`, tone: 'plain' }

    case 'status': {
      const total = count(Number(row.total), 'entity', 'entities')

      return { line: `${total} · ${text(row.draft)} draft · ${text(row.orphan)} orphan · ${text(row.stale)} stale`, tone: 'plain' }
    }

    case 'validate':
      return validateResult(row)

    case 'check':
      return checkResult(row)

    case 'schema':
      return schemaResult(call, json)

    default:
      return { line: firstLine(note), tone: 'plain' }
  }
}

// Returns one line for a khub call. `head` names the call and `line` says what came back.
// `json` is khub's stdout document (undefined when it printed none), `note` its stderr.
export function summarize(call: Simple, json: unknown, note: string): Summary {
  const head = headOf(call)
  const refused = refusal(json)

  if (refused) return { head, line: clean(`✗ ${refused.code}: ${refused.message}`), tone: 'bad' }

  // With no document the call is still running, or it printed prose.
  if (json === undefined) return { head, line: clean(firstLine(note)), tone: 'plain' }

  const result = resultOf(call, json, note)

  return { head, line: clean(result.line), tone: result.tone }
}

// Sets how many list rows a call's row shows before `… n more`.
const PREVIEW = 5

const FLAGS = ['draft', 'orphan', 'stale']

const entityRow = (row: Row): KhubRow => ({
  text: text(row.id),
  note: titleOf(row),
  flags: FLAGS.filter(flag => row[flag] === true),
})

// Returns one row per finding of a bucket that fails the gate, named by its id, path or alias.
function findingRows(check: Row): KhubRow[] {
  return failingOf(check).flatMap(found =>
    found.items.map(item => ({
      text: found.bucket,
      note: isRow(item) ? text(item.id ?? item.path ?? item.alias) : text(item),
      flags: [],
    })),
  )
}

// Returns every row a call's document lists, by verb.
function listOf(call: Simple, json: unknown): KhubRow[] {
  const rows = rowsOf(json)
  const row = isRow(json) ? json : {}

  switch (call.verb) {
    case 'get':
    case 'query':
    case 'search':
    case 'stale':
      return rows.map(entityRow)

    case 'neighbors':
      return rows.map(found => ({
        text: text(found.id),
        note: `${text(found.direction)} ${text(found.predicate)}`.trim(),
        flags: [],
      }))

    case 'check':
      return row.passed === false ? findingRows(row) : []

    case 'validate':
      return rowsOf(row.errors).map(found => ({
        text: text(found.id),
        note: `${text(found.field)}: ${text(found.reason)}`,
        flags: [],
      }))

    default:
      return []
  }
}

// Returns the first rows of a list result, and how many more it holds.
export function previewOf(call: Simple, json: unknown): { rows: KhubRow[]; more: number } {
  const all = refusal(json) ? [] : listOf(call, json)

  return {
    rows: all.slice(0, PREVIEW).map(row => ({ ...row, text: clean(row.text), note: clean(row.note) })),
    more: Math.max(0, all.length - PREVIEW),
  }
}

// Returns the rows of a command's calls while the command runs.
export const waiting = (calls: Simple[]): KhubCall[] =>
  calls.map(call => ({ ...summarize(call, undefined, ''), isRunning: true, ms: 0, rows: [], more: 0 }))

// Returns the rows of a command's calls once it has answered, each read from its own
// document. The first row carries the command's duration.
export const finished = (calls: Simple[], docs: unknown[], ms: number, note: string): KhubCall[] =>
  calls.map((call, i) => ({
    ...summarize(call, docs[i], note),
    isRunning: false,
    ms: i === 0 ? ms : 0,
    ...previewOf(call, docs[i]),
  }))
