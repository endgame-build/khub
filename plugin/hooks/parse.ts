// This file recognizes khub invocations in a Bash command string.

export type Simple = {
  kind: 'simple'
  verb: string

  // Holds the second word of a two-word verb, such as `show` in `schema show adr`.
  sub: string | null

  // Holds the value of `-C` or `--workspace`, when the call names one.
  workspace: string | null
  positional: string[]
  flags: Record<string, string | true>
}

// `simple` is one khub call whose stdout is khub's alone. `compound` runs khub among
// other things, and lists the khub calls it could read.
export type Parsed = { kind: 'none' } | { kind: 'compound'; calls: Simple[] } | Simple

type Word = { text: string }
type Operator = { op: '&&' | '||' | ';' | '|' | '&' | '>out' | '>err' | '<' }
type Token = Word | Operator
type Segment = { words: string[]; ops: string[] }

const NONE: Parsed = { kind: 'none' }
const BOOLEAN_FLAGS = new Set([
  'active', 'draft', 'dry-run', 'edges', 'force', 'help', 'in', 'no-schema',
  'no-template', 'no-wire', 'open', 'orphan', 'out', 'plain', 'reverse', 'stale', 'strict',
])
const WRITES = new Set(['add', 'edit', 'link', 'unlink', 'remove'])
const OPERATORS = new Set(['backfill', 'init', 'reindex', 'upgrade', 'wire'])
const SCHEMA_VIEWS = new Set(['base', 'diff', 'edges', 'show', 'snapshot', 'types'])

// These flags of `khub edit` name no field.
const EDIT_OPTIONS = new Set(['format', 'strict'])

const isWord = (token: Token): token is Word => 'text' in token
const isOn = (flag: string | true | undefined) => flag !== undefined && flag !== 'false'
const call = (verb: string, flags: Simple['flags'] = {}): Simple => ({
  kind: 'simple',
  verb,
  sub: null,
  workspace: null,
  positional: [],
  flags,
})

// Returns the index of the `)` that closes the `$(` opening at `start`, or -1.
function closeOf(command: string, start: number): number {
  let depth = 0

  for (let i = start + 1; i < command.length; i += 1) {
    const c = command[i]

    if (c === '\\') {
      i += 1
    } else if (c === "'") {
      const end = command.indexOf("'", i + 1)

      if (end < 0) return -1
      i = end
    } else if (c === '(') {
      depth += 1
    } else if (c === ')') {
      depth -= 1
      if (depth === 0) return i
    }
  }

  return -1
}

// Returns the command with the body of the heredoc that opens at `at` (just past `<<`) cut
// out, and the index where reading goes on. It is null when the heredoc never closes.
function withoutHeredoc(command: string, at: number): { command: string; next: number } | null {
  const opener = /^-?\s*(['"]?)([^\s'"<>|;&]+)\1/.exec(command.slice(at))
  const lineEnd = command.indexOf('\n', at)

  if (!opener || lineEnd < 0) return null

  const stripsTabs = command[at] === '-'
  const lines = command.slice(lineEnd + 1).split('\n')
  const closing = lines.findIndex(line => (stripsTabs ? line.replace(/^\t+/, '') : line) === opener[2])

  if (closing < 0) return null

  return {
    command: [command.slice(0, lineEnd), ...lines.slice(closing + 1)].join('\n'),
    next: at + opener[0].length,
  }
}

// Splits a command into words and operators. A substitution stays inside its word,
// unread. Returns null for a construct it does not follow, such as a subshell or an
// unterminated quote.
export function tokenize(input: string): Token[] | null {
  const tokens: Token[] = []
  let command = input
  let word: string | null = null
  let isTarget = false
  let i = 0

  // A redirection's target is read as a word and dropped.
  const flush = () => {
    if (word !== null && !isTarget) tokens.push({ text: word })
    if (word !== null) isTarget = false
    word = null
  }

  // Takes a `$(…)` or a backtick span whole into the current word.
  const substitution = (): boolean => {
    const end = command[i] === '`' ? command.indexOf('`', i + 1) : closeOf(command, i)

    if (end < 0) return false
    word = (word ?? '') + command.slice(i, end + 1)
    i = end + 1

    return true
  }

  while (i < command.length) {
    const c = command[i] as string
    const pair = command.slice(i, i + 2)

    if (c === "'") {
      const end = command.indexOf("'", i + 1)

      if (end < 0) return null
      word = (word ?? '') + command.slice(i + 1, end)
      i = end + 1
    } else if (c === '"') {
      word ??= ''
      i += 1

      while (command[i] !== '"') {
        if (i >= command.length) return null

        if (command[i] === '`' || command.slice(i, i + 2) === '$(') {
          if (!substitution()) return null
          continue
        }

        if (command[i] === '\\' && '"\\$`'.includes(command[i + 1] ?? '')) i += 1
        word += command[i]
        i += 1
      }

      i += 1
    } else if (c === '\\') {
      word = (word ?? '') + (command[i + 1] ?? '')
      i += 2
    } else if (c === '`' || pair === '$(') {
      if (!substitution()) return null
    } else if (c === '(' || c === ')') {
      return null
    } else if (c === ' ' || c === '\t') {
      flush()
      i += 1
    } else if (c === '\n' || c === ';') {
      flush()
      tokens.push({ op: ';' })
      i += 1
    } else if (pair === '&&' || pair === '||') {
      flush()
      tokens.push({ op: pair })
      i += 2
    } else if (pair === '<<' && command[i + 2] !== '<') {
      const cut = withoutHeredoc(command, i + 2)

      if (cut === null) return null
      flush()
      tokens.push({ op: '<' })
      command = cut.command
      i = cut.next
    } else if (c === '>' || c === '<' || pair === '&>') {
      // A digit word straight before `>` is the descriptor, such as `2` in `2>`.
      const fd = pair !== '&>' && word !== null && /^\d+$/.test(word) ? word : ''

      if (fd === '') flush()
      word = null
      i += pair === '&>' ? 2 : 1
      while (command[i] === '>' || command[i] === '<') i += 1
      tokens.push({ op: c === '<' ? '<' : fd === '2' ? '>err' : '>out' })

      if (command[i] === '&') {
        i += 1
        while (/\d/.test(command[i] ?? '')) i += 1
      } else {
        isTarget = true
      }
    } else if (c === '|' || c === '&') {
      flush()
      tokens.push({ op: c })
      i += 1
    } else {
      word = (word ?? '') + c
      i += 1
    }
  }

  flush()

  return tokens
}

// Reads one segment's words as a khub call, or null when it runs something else.
function khubCall(words: string[]): Simple | null {
  let i = 0

  while (/^[A-Za-z_][A-Za-z0-9_]*=/.test(words[i] ?? '')) i += 1

  const binary = words[i] ?? ''

  if (binary === 'npx') {
    i += 1
    while ((words[i] ?? '').startsWith('-')) i += 1
    if (!/^@endgame-build\/khub(@|$)/.test(words[i] ?? '')) return null
  } else if (binary !== 'khub' && !binary.endsWith('/khub')) {
    return null
  }

  i += 1

  let workspace: string | null = null

  // Global options come before the verb.
  for (;;) {
    const word = words[i] ?? ''

    if (word === '-C' || word === '--workspace') {
      workspace = words[i + 1] ?? null
      i += 2
    } else if (word.startsWith('--workspace=')) {
      workspace = word.slice('--workspace='.length)
      i += 1
    } else {
      break
    }
  }

  const verb = words[i] ?? ''

  if (verb === '' || verb.startsWith('-')) return { ...call(verb.replace(/^--/, '')), workspace }

  i += 1

  const found: Simple = { ...call(verb), workspace }

  if (verb === 'schema' && SCHEMA_VIEWS.has(words[i] ?? '')) {
    found.sub = words[i] as string
    i += 1
  }

  while (i < words.length) {
    const word = words[i] as string

    if (!word.startsWith('--')) {
      found.positional.push(word)
      i += 1
      continue
    }

    const equals = word.indexOf('=')
    const name = word.slice(2, equals < 0 ? undefined : equals)
    const next = words[i + 1]

    if (equals >= 0) {
      found.flags[name] = word.slice(equals + 1)
      i += 1
    } else if (BOOLEAN_FLAGS.has(name) || next === undefined || next.startsWith('--')) {
      found.flags[name] = true
      i += 1
    } else {
      found.flags[name] = next
      i += 2
    }
  }

  return found
}

// Reads the khub calls off the text of a command the tokenizer could not follow. It keeps
// each verb, and whether `--force` or `--dry-run` follows it.
function scan(command: string): Simple[] {
  const calls = command.matchAll(
    /(?:^|[\s;&|(`])(?:[^\s;&|]*\/)?khub\s+(?:(?:-C|--workspace)\s+\S+\s+)?([a-z][a-z-]*)([^;&|\n]*)/g,
  )

  return [...calls].map(match => {
    const rest = match[2] ?? ''

    return call(match[1] as string, {
      ...(rest.includes('--force') ? { force: true as const } : {}),
      ...(rest.includes('--dry-run') ? { 'dry-run': true as const } : {}),
    })
  })
}

export function classify(command: string): Parsed {
  if (!command.includes('khub')) return NONE

  const tokens = tokenize(command)

  if (tokens === null) {
    const calls = scan(command)

    return calls.length === 0 ? NONE : { kind: 'compound', calls }
  }

  // Collects segments between control operators, with the operators and redirections each saw.
  const segments: Segment[] = [{ words: [], ops: [] }]
  const separators: string[] = []

  for (const token of tokens) {
    const segment = segments[segments.length - 1] as Segment

    if (isWord(token)) {
      segment.words.push(token.text)
    } else if (token.op === '>out' || token.op === '>err' || token.op === '<') {
      segment.ops.push(token.op)
    } else {
      separators.push(token.op)
      segments.push({ words: [], ops: [] })
    }
  }

  const filled = segments.filter(segment => segment.words.length > 0)
  const read = filled.map(segment => khubCall(segment.words))
  const calls = read.filter((found): found is Simple => found !== null)

  if (calls.length === 0) return NONE

  const last = filled[filled.length - 1] as Segment
  const lastCall = read[read.length - 1]

  // A call is simple when it is the one khub call, last, after nothing but `cd <dir> &&`,
  // with stdout left alone.
  const isSimple =
    calls.length === 1 &&
    lastCall !== null &&
    lastCall !== undefined &&
    separators.every(op => op === '&&' || op === ';') &&
    filled.slice(0, -1).every(segment => segment.words[0] === 'cd' && segment.words.length === 2) &&
    !last.ops.includes('>out')

  return isSimple ? lastCall : { kind: 'compound', calls }
}

// Returns the khub calls of a command, however it was read.
export const callsOf = (parsed: Parsed): Simple[] =>
  parsed.kind === 'simple' ? [parsed] : parsed.kind === 'compound' ? parsed.calls : []

export const isWrite = (found: Simple) => WRITES.has(found.verb)

// Tells whether a call changes entities, the schema, the index or the agent files.
export const mutates = (found: Simple) =>
  isWrite(found) ||
  (OPERATORS.has(found.verb) && !isOn(found.flags['dry-run'])) ||
  (found.verb === 'schema' && found.sub === 'snapshot')

// Returns what a `khub edit` call changed, as `<field> <value>` pairs. A body is named,
// never quoted.
export function edited(found: Simple): string {
  const [, field, value] = found.positional
  const pairs = field === undefined ? [] : [field === 'body' ? 'body' : `${field} ${value ?? ''}`.trim()]

  for (const [name, flag] of Object.entries(found.flags)) {
    if (EDIT_OPTIONS.has(name)) continue
    pairs.push(name === 'body' || name === 'body-file' ? 'body' : flag === true ? name : `${name} ${flag}`)
  }

  return pairs.join(', ')
}
