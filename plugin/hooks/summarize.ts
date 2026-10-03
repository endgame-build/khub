import type { KhubTone } from '../types'
import { count, firstLine, INFORMATIONAL, isRow, refusal, rowsOf, text } from './cli'
import type { Row } from './cli'
import { edited } from './parse'
import type { Simple } from './parse'

export type Summary = { head: string; line: string; tone: KhubTone }

type Result = { line: string; tone: KhubTone }

// Verbs whose first positional names what the call is about, a type for add and an id for the rest.
const SUBJECT_VERBS = new Set([
  'add', 'edit', 'get', 'history', 'impact', 'link', 'neighbors', 'remove', 'unlink', 'validate',
])

const NOTHING: Result = { line: '', tone: 'plain' }

const sizeOf = (value: unknown) => (Array.isArray(value) ? value.length : 0)

// The title a record carries, at its top or in its frontmatter. khub itself names an
// entity by `name`, then `title`, so both are read.
export function titleOf(row: Row): string {
  const meta = isRow(row.frontmatter) ? row.frontmatter : row

  return text(meta.title ?? meta.name)
}

// The buckets of a `khub check` payload that hold findings, informational ones last.
export function bucketsOf(check: Row): Array<{ bucket: string; items: unknown[]; isInformational: boolean }> {
  const buckets = Object.entries(check)
    .filter((entry): entry is [string, unknown[]] => Array.isArray(entry[1]) && entry[1].length > 0)
    .map(([bucket, items]) => ({
      bucket,
      items,
      isInformational: INFORMATIONAL.has(bucket) && !(bucket === 'orphans' && check.strict === true),
    }))

  return [...buckets.filter(found => !found.isInformational), ...buckets.filter(found => found.isInformational)]
}

function subjectOf(call: Simple): string {
  if (call.verb === 'query') return typeof call.flags.type === 'string' ? call.flags.type : ''
  if (call.verb === 'search') return call.positional[0] === undefined ? '' : `"${call.positional[0]}"`
  if (call.verb === 'schema') return call.sub === 'show' ? (call.positional[0] ?? '') : ''

  return SUBJECT_VERBS.has(call.verb) ? (call.positional[0] ?? '') : ''
}

const headOf = (call: Simple) =>
  ['khub', call.verb, call.sub ?? '', subjectOf(call)].filter(word => word !== '').join(' ')

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

  const failing = bucketsOf(row).filter(found => !found.isInformational)

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

// What a call's JSON document says, by verb and by the document's shape.
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

// One line for a khub call. `head` names the call and `line` says what came back.
// `json` is khub's stdout document (undefined when it printed none), `note` its stderr.
export function summarize(call: Simple, json: unknown, note: string): Summary {
  const head = headOf(call)
  const refused = refusal(json)

  if (refused) return { head, line: `✗ ${refused.code}: ${refused.message}`, tone: 'bad' }

  // With no document the call is still running, or it printed prose.
  if (json === undefined) return { head, line: firstLine(note), tone: 'plain' }

  return { head, ...resultOf(call, json, note) }
}
