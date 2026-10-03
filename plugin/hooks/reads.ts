import { rowsOf } from './cli'

const CAP = 10

// The inbound edges of an entity, as a note. Its file does not hold them.
// Reads the list `khub neighbors <id> --in --format json` returns. Undefined when there are none.
export function inboundNote(id: string, neighbors: unknown): string | undefined {
  const edges = rowsOf(neighbors)
    .filter(row => row.direction === 'in')
    .map(row => `${row.predicate} ← ${row.id}`)

  if (edges.length === 0) return undefined

  const more = edges.length - CAP
  const listed = `${edges.slice(0, CAP).join(', ')}${more > 0 ? `, and ${more} more` : ''}`

  return `khub: ${id} has inbound edges its file does not show: ${listed}`
}
