// khub's output, read.

export type Row = Record<string, unknown>
export type Refusal = { code: string; message: string }

// The first JSON document in khub's output. A `2>&1` call may carry `note:` lines around it.
export function parseJson(text: string): unknown {
  const trimmed = text.trim()

  for (const candidate of [trimmed, ...trimmed.split('\n').filter(line => /^[[{]/.test(line))]) {
    try {
      return JSON.parse(candidate)
    } catch {
      continue
    }
  }

  return undefined
}

// khub's error envelope is `{"error": {"code", "message"}}`, printed on stdout with exit 2.
export function refusal(json: unknown): Refusal | undefined {
  const error = (json as { error?: Partial<Refusal> } | undefined)?.error

  return typeof error?.code === 'string' ? { code: error.code, message: String(error.message ?? '') } : undefined
}

// `khub check` buckets that never fail the gate. Orphans fail it under `--strict`.
export const INFORMATIONAL = new Set(['draft_singletons', 'orphans', 'thin'])

export const isRow = (value: unknown): value is Row =>
  typeof value === 'object' && value !== null && !Array.isArray(value)

export const rowsOf = (json: unknown): Row[] => (Array.isArray(json) ? json.filter(isRow) : [])

export const text = (value: unknown) => (value === undefined || value === null ? '' : String(value))

export const count = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`

// The first line that says something.
export const firstLine = (lines: string) =>
  lines
    .split('\n')
    .map(line => line.trim())
    .find(line => line !== '') ?? ''
