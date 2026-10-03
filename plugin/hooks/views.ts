// khub's documents, reduced to what the pane shows.

import type { KhubEdge, KhubEntity, KhubRecord } from '../types'
import { isRow, rowsOf, text } from './cli'
import { titleOf } from './summarize'

const FLAGS = ['draft', 'orphan', 'stale']

const shown = (value: unknown): string => (Array.isArray(value) ? value.map(shown).join(', ') : text(value))

// The rows of a `khub query` or `khub search` document. `note` is the match share of a search hit.
export function recordsOf(json: unknown): KhubRecord[] {
  return rowsOf(json).map(row => ({
    id: text(row.id),
    title: titleOf(row),
    flags: FLAGS.filter(flag => row[flag] === true),
    note: typeof row.match === 'number' ? `match ${row.match.toFixed(3)}` : '',
  }))
}

// One entity from `khub get <id> --format json` and `khub neighbors <id> --format json`.
// The edges come from the neighbors list, which holds every inbound edge.
export function entityOf(got: unknown, neighbors: unknown): KhubEntity | null {
  if (!isRow(got) || typeof got.id !== 'string' || typeof got.type !== 'string' || typeof got.slug !== 'string') {
    return null
  }

  const meta = isRow(got.frontmatter) ? got.frontmatter : {}
  const edges = rowsOf(neighbors).map(
    (row): KhubEdge => ({ predicate: text(row.predicate), id: text(row.id), direction: row.direction === 'in' ? 'in' : 'out' }),
  )

  return {
    id: got.id,
    type: got.type,
    slug: got.slug,
    path: text(got.path),
    isDraft: meta.draft === true,
    fields: Object.entries(meta)
      .filter(([name]) => name !== 'type')
      .map(([name, value]) => ({ name, value: shown(value) })),
    edges,
    body: text(got.body)
      .replace(/<!--[\s\S]*?-->/g, '')
      .trim(),
  }
}
