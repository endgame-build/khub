// Where the arguments of `/khub` go, and what a passthrough run prints in the transcript.

import { isRow, refusal, rowsOf, text } from './cli'
import type { Row } from './cli'
import { findingsOf, lineOf } from './integrity'
import { tokenize } from './parse'
import type { Simple } from './parse'
import { summarize, titleOf } from './summarize'

// `/khub` alone opens the pane; `/khub <args>` runs khub with those arguments.
export type Route =
  | { kind: 'open' }
  | { kind: 'doctor' }
  | { kind: 'run'; argv: string[] }
  | { kind: 'error'; message: string }

const MAX_ROWS = 20
const MAX_PROSE_LINES = 40
const FLAGS = ['draft', 'orphan', 'stale']

export function route(args: string): Route {
  if (args.trim() === '') return { kind: 'open' }

  const tokens = tokenize(args)

  if (tokens === null || tokens.some(token => 'op' in token)) {
    return { kind: 'error', message: 'khub: plain arguments only, no pipes or redirections' }
  }

  const argv = tokens.flatMap(token => ('text' in token ? [token.text] : []))

  // The arguments go to khub as they are, with no shell to expand a substitution.
  if (argv.some(word => /\$\(|`/.test(word))) {
    return { kind: 'error', message: 'khub: plain arguments only, no pipes or redirections' }
  }

  // The mod's own word, which khub has no command for.
  if (argv.length === 1 && argv[0] === 'doctor') return { kind: 'doctor' }

  if (argv[0] === 'serve') {
    return { kind: 'error', message: 'khub: serve blocks the session until it is stopped, so it is not available here' }
  }

  return { kind: 'run', argv }
}

const scalar = (value: unknown) => (Array.isArray(value) ? value.map(text).join(', ') : text(value))

// The first `limit` lines, then how many were left out.
function capped(lines: string[], limit: number, what: string): string[] {
  return lines.length > limit ? [...lines.slice(0, limit), `… ${lines.length - limit} more${what}`] : lines
}

// One record of a list, which is an entity, a neighbor, an impact step or a named row of the schema.
function recordLine(row: Row): string {
  const label = text(row.id ?? row.name ?? row.predicate)
  const title = titleOf(row)
  const parts = [label, title === label ? '' : title]

  if (row.direction !== undefined) parts.push(text(row.predicate), text(row.direction))
  if (row.depth !== undefined && row.direction === undefined) parts.push(`depth ${text(row.depth)}`)

  return [...parts, ...FLAGS.filter(flag => row[flag] === true)].filter(part => part !== '').join(' · ')
}

// One type of the schema, with its name, how it is stored and where.
const typeLine = (row: Row) => [row.name, row.layout, row.path].map(text).filter(part => part !== '').join(' · ')

// The frontmatter and the edges of one `khub get` record. Its body text stays out.
function entityLines(row: Row): string[] {
  const meta = isRow(row.frontmatter) ? row.frontmatter : {}
  const fields = Object.entries(meta)
    .filter(([key]) => key !== 'type')
    .map(([key, value]) => `${key}: ${scalar(value)}`)
  const edges = rowsOf(row.edges).map(
    edge => `${text(edge.predicate)} ${edge.derived === true ? '←' : '→'} ${scalar(edge.target)}`,
  )

  return [...fields, ...edges]
}

function validateLines(row: Row): string[] {
  const line = (item: Row, mark: string) => `${text(item.id)}: ${mark}${text(item.field)}: ${text(item.reason)}`

  return [...rowsOf(row.errors).map(item => line(item, '')), ...rowsOf(row.gaps).map(item => line(item, '- '))]
}

function schemaLines(row: Row): string[] {
  const fields = rowsOf(row.fields).map(field => {
    const values = Array.isArray(field.enum) ? ` (${field.enum.map(text).join(' | ')})` : ''

    return `${text(field.name)}: ${text(field.type)}${field.required === true ? ', required' : ''}${values}`
  })
  const relations = rowsOf(row.relations).map(relation => {
    const facets = [relation.many === true ? 'many' : '', relation.required === true ? 'required' : '']
      .filter(facet => facet !== '')
      .map(facet => `, ${facet}`)
      .join('')

    return `${text(relation.predicate)} → ${scalar(relation.to)}${facets}`
  })

  return [...fields, ...relations]
}

// The lines under the first one, by the shape of khub's document.
function bodyOf(json: unknown, out: string): string[] {
  if (json === undefined) return capped(out.trim() === '' ? [] : out.trimEnd().split('\n'), MAX_PROSE_LINES, ' lines')

  if (Array.isArray(json)) {
    return capped(json.map(item => (isRow(item) ? recordLine(item) : scalar(item))), MAX_ROWS, '')
  }

  if (!isRow(json)) return []
  if (isRow(json.frontmatter)) return entityLines(json)
  if (typeof json.passed === 'boolean') {
    const findings = findingsOf(json)

    // Errors come before informational findings, as in the notes.
    return [...findings.filter(finding => finding.isError), ...findings.filter(finding => !finding.isError)].map(lineOf)
  }
  if (Array.isArray(json.errors) && Array.isArray(json.gaps)) return validateLines(json)
  if (Array.isArray(json.fields) && Array.isArray(json.relations)) return schemaLines(json)
  if (isRow(json.counts)) return Object.entries(json.counts).map(([name, n]) => `${name} ${text(n)}`)
  if (Array.isArray(json.types)) return capped(rowsOf(json.types).map(typeLine), MAX_ROWS, '')

  return []
}

// What a passthrough call prints in the transcript. `json` is khub's stdout document
// (undefined for prose commands), `out` its raw stdout, `note` its stderr.
export function passthrough(call: Simple, json: unknown, out: string, note: string): string {
  // The note is printed whole at the end, so the first line is summarized without it.
  const { head, line } = summarize(call, json, '')
  const first = line === '' ? head : `${head}  ${line}`
  const body = refusal(json) ? [] : bodyOf(json, out)

  return [first, ...body, ...(note.trim() === '' ? [] : [note.trim()])].join('\n')
}
