// This file reads khub's output.

export type Row = Record<string, unknown>
export type Refusal = { code: string; message: string }

// Returns the lines that may hold a JSON document. khub prints each document on one line.
const documentLines = (text: string) => text.split('\n').filter(line => /^[[{]/.test(line))

// Returns the first JSON document in khub's output. A `2>&1` call may carry `note:` lines
// around it.
export function parseJson(text: string): unknown {
  const trimmed = text.trim()

  for (const candidate of [trimmed, ...documentLines(trimmed)]) {
    try {
      return JSON.parse(candidate)
    } catch {
      continue
    }
  }

  return undefined
}

// Returns one JSON document per call of a chain, or null when the output does not hold
// exactly `count` of them.
export function documentsOf(text: string, count: number): unknown[] | null {
  const lines = documentLines(text)

  if (lines.length !== count) return null

  try {
    return lines.map(line => JSON.parse(line) as unknown)
  } catch {
    return null
  }
}

// khub's error envelope is `{"error": {"code", "message"}}`, printed on stdout with exit 2.
export function refusal(json: unknown): Refusal | undefined {
  const error = (json as { error?: Partial<Refusal> } | undefined)?.error

  return typeof error?.code === 'string' ? { code: error.code, message: String(error.message ?? '') } : undefined
}

// These `khub check` buckets never fail the gate. Orphans fail it under `--strict`.
export const INFORMATIONAL = new Set(['draft_singletons', 'orphans', 'thin'])

export const isRow = (value: unknown): value is Row =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

export const rowsOf = (json: unknown): Row[] => (Array.isArray(json) ? json.filter(isRow) : [])

export const text = (value: unknown) => (value === undefined || value === null ? '' : String(value))

export const count = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

// Returns the first line that says something.
export const firstLine = (lines: string) =>
  lines
    .split('\n')
    .map(line => line.trim())
    .find(line => line !== '') ?? ''

// Entity text is untrusted, so control characters never reach the terminal.
export const clean = (value: string) => value.replace(/[\u0000-\u001f\u007f-\u009f]/g, ' ')
