import { expect, test } from 'claude-code/testing'

import {
  callsOf,
  changesSchema,
  classify,
  edited,
  isDestructive,
  isWrite,
  mutates,
  takesFormat,
  tokenize,
} from '../hooks/parse'
import { stamped } from '../hooks/parse'
import type { Simple } from '../hooks/parse'

const simple = (command: string) => classify(command) as Simple
const verbs = (command: string) => callsOf(classify(command)).map(call => call.verb)

test('a plain khub call is simple, with verb, positionals and flags', () => {
  const call = simple('khub add adr --title "Use RE2 patterns" --status accepted --draft --format json')

  expect(call.kind).toBe('simple')
  expect(call.verb).toBe('add')
  expect(call.workspace).toBe(null)
  expect(call.positional).toEqual(['adr'])
  expect(call.flags).toEqual({ title: 'Use RE2 patterns', status: 'accepted', draft: true, format: 'json' })
  expect(isWrite(call)).toBe(true)
})

test('every spelling of the binary is recognized', () => {
  for (const binary of ['khub', './khub', '/opt/bin/khub', 'npx @endgame-build/khub', 'npx -y @endgame-build/khub@0.27.0']) {
    expect(simple(`${binary} status --format json`).verb).toBe('status')
  }

  expect(classify('npx khub status').kind).toBe('none')
  expect(classify('echo khub').kind).toBe('none')
  expect(classify('ls -la').kind).toBe('none')
})

test('env assignments, -C and a leading cd do not hide the call', () => {
  expect(simple('KHUB_PARITY_NOW=2026-01-15 khub -C ./ws get cmp-search --edges').positional).toEqual(['cmp-search'])
  expect(simple('khub --workspace=/tmp/ws query --type adr').flags).toEqual({ type: 'adr' })
  expect(simple('cd docs/ws && khub link ad-1 affects cmp-search').verb).toBe('link')
})

test('the workspace a call names is kept', () => {
  expect(simple('khub -C /other/ws add adr --title x').workspace).toBe('/other/ws')
  expect(simple('khub --workspace ./ws status').workspace).toBe('./ws')
  expect(simple('khub --workspace=/tmp/ws status').workspace).toBe('/tmp/ws')
  expect(simple('khub status').workspace).toBe(null)
})

test('a two-word schema verb keeps its view', () => {
  const call = simple('khub schema show adr --format json')

  expect(call.sub).toBe('show')
  expect(call.positional).toEqual(['adr'])
  expect(simple('khub schema').sub).toBe(null)
})

test('stderr and stdin redirections stay simple, a stdout redirection or a pipe does not', () => {
  expect(classify('khub check --format json 2>&1').kind).toBe('simple')
  expect(simple('khub check 2>/dev/null --strict').flags).toEqual({ strict: true })
  expect(simple('khub add adr --title x --body-file - < body.md').flags).toEqual({ title: 'x', 'body-file': '-' })
  expect(simple('khub add adr --title x --body-file - <<< "one line"').flags).toEqual({ title: 'x', 'body-file': '-' })

  expect(classify('khub query --type adr > out.json').kind).toBe('compound')
  expect(classify('khub query --type adr | jq .').kind).toBe('compound')
  expect(classify('khub add adr --title x && khub check').kind).toBe('compound')
  expect(verbs('khub add adr --title x && khub check')).toEqual(['add', 'check'])
  expect(classify('ls && khub status').kind).toBe('compound')
  expect(verbs('ls && khub status')).toEqual(['status'])
})

test('a compound command lists the khub calls it holds, in full', () => {
  expect(classify('khub remove cmp-search --force 2>&1 | head')).toEqual({
    kind: 'compound',
    calls: [
      { kind: 'simple', verb: 'remove', sub: null, workspace: null, positional: ['cmp-search'], flags: { force: true } },
    ],
  })
})

test('a substitution stays inside its word and the call stays simple', () => {
  const quoted = simple('khub add adr --title "Use RE2 patterns" --body "$(cat b.md)"')

  expect(quoted.kind).toBe('simple')
  expect(quoted.flags).toEqual({ title: 'Use RE2 patterns', body: '$(cat b.md)' })

  const bare = simple('khub get $(khub query --format ids | head -1)')

  expect(bare.kind).toBe('simple')
  expect(bare.verb).toBe('get')
  expect(bare.positional).toEqual(['$(khub query --format ids | head -1)'])

  expect(simple('khub get `khub query --format ids`').positional).toEqual(['`khub query --format ids`'])
  expect(simple('khub add adr --title "n: $(echo "a (b)")"').flags).toEqual({ title: 'n: $(echo "a (b)")' })
})

test('a heredoc body is cut out and never read as commands', () => {
  const command = [
    "khub add adr --title x --body-file - <<'EOF'",
    "It's a (draft) body",
    'khub remove x --force',
    'EOF',
  ].join('\n')
  const call = simple(command)

  expect(call.kind).toBe('simple')
  expect(call.verb).toBe('add')
  expect(call.flags).toEqual({ title: 'x', 'body-file': '-' })
  expect(isDestructive(call)).toBe(false)

  // What follows the closing line is read again.
  expect(verbs(`${command}\nkhub check`)).toEqual(['add', 'check'])

  const tabbed = ['khub add adr --title x --body-file - <<-EOF', '\tkhub upgrade', '\tEOF'].join('\n')

  expect(verbs(tabbed)).toEqual(['add'])
})

test('a subshell or an open quote is read off the text', () => {
  expect(tokenize('(cd ws && khub remove x --force)')).toBe(null)
  expect(tokenize('khub add adr --title "open')).toBe(null)
  expect(tokenize("khub add adr --title 'open")).toBe(null)
  expect(tokenize('khub get "$(khub query')).toBe(null)

  expect(classify('(cd ws && khub remove x --force)')).toEqual({
    kind: 'compound',
    calls: [{ kind: 'simple', verb: 'remove', sub: null, workspace: null, positional: [], flags: { force: true } }],
  })
  expect(classify('(khub -C ./ws upgrade --dry-run)')).toEqual({
    kind: 'compound',
    calls: [{ kind: 'simple', verb: 'upgrade', sub: null, workspace: null, positional: [], flags: { 'dry-run': true } }],
  })
  expect(verbs('khub add adr --title "open')).toEqual(['add'])
  expect(verbs('(./bin/khub add adr --title x; khub check)')).toEqual(['add', 'check'])
  expect(classify('(echo khub)').kind).toBe('none')
})

test('the tokenizer splits words, quotes and operators', () => {
  expect(tokenize("echo 'a b' c\\ d")).toEqual([{ text: 'echo' }, { text: 'a b' }, { text: 'c d' }])
  expect(tokenize('khub check 2>&1')).toEqual([{ text: 'khub' }, { text: 'check' }, { op: '>err' }])
  expect(tokenize('a > out.txt; b | c && d')).toEqual([
    { text: 'a' },
    { op: '>out' },
    { op: ';' },
    { text: 'b' },
    { op: '|' },
    { text: 'c' },
    { op: '&&' },
    { text: 'd' },
  ])
})

test('the calls of a command come out however it was read', () => {
  expect(callsOf(classify('ls -la'))).toEqual([])
  expect(callsOf(classify('khub status')).map(call => call.verb)).toEqual(['status'])
  expect(verbs('khub add adr --title x | tee out.txt')).toEqual(['add'])
})

test('mutating and destructive calls are told apart', () => {
  expect(mutates(simple('khub reindex'))).toBe(true)
  expect(mutates(simple('khub reindex --dry-run'))).toBe(false)
  expect(mutates(simple('khub reindex --dry-run=true'))).toBe(false)
  expect(mutates(simple('khub schema snapshot'))).toBe(true)
  expect(mutates(simple('khub get x'))).toBe(false)
  expect(isDestructive(simple('khub remove cmp-search --force'))).toBe(true)
  expect(isDestructive(simple('khub remove cmp-search --force=true'))).toBe(true)
  expect(isDestructive(simple('khub remove cmp-search --force=false'))).toBe(false)
  expect(isDestructive(simple('khub remove cmp-search'))).toBe(false)
  expect(isDestructive(simple('khub init build-hub . --force'))).toBe(true)
  expect(isDestructive(simple('khub upgrade'))).toBe(true)
  expect(isDestructive(simple('khub upgrade --dry-run'))).toBe(false)
  expect(isDestructive(simple('khub upgrade --dry-run=true'))).toBe(false)
})

test('a call that may replace the schema is known', () => {
  expect(changesSchema(simple('khub upgrade'))).toBe(true)
  expect(changesSchema(simple('khub init build-hub . --force'))).toBe(true)
  expect(changesSchema(simple('khub upgrade --dry-run'))).toBe(false)
  expect(changesSchema(simple('khub schema snapshot'))).toBe(false)
  expect(changesSchema(simple('khub add adr --title x'))).toBe(false)
})

test('--format json is added only where khub takes it and the call names none', () => {
  expect(takesFormat(simple('khub get cmp-search'))).toBe(true)
  expect(takesFormat(simple('khub check'))).toBe(true)
  expect(takesFormat(simple('khub get cmp-search --format raw'))).toBe(false)

  for (const prose of ['reindex', 'wire', 'viz', 'backfill']) {
    expect(takesFormat(simple(`khub ${prose}`))).toBe(false)
  }
})

test('an edit names the fields it changed, and a body by name alone', () => {
  expect(edited(simple('khub edit cmp-search lifecycle deprecated'))).toBe('lifecycle deprecated')
  expect(edited(simple('khub edit cmp-search --lifecycle deprecated --format json'))).toBe('lifecycle deprecated')
  expect(edited(simple('khub edit cmp-search --lifecycle deprecated --tier tier-2 --strict'))).toBe(
    'lifecycle deprecated, tier tier-2',
  )
  expect(edited(simple('khub edit cmp-search kind service --owner team-platform'))).toBe(
    'kind service, owner team-platform',
  )
  expect(edited(simple('khub edit cmp-search --draft'))).toBe('draft')
  expect(edited(simple('khub edit cmp-search'))).toBe('')

  expect(edited(simple('khub edit cmp-search --body "the secret text"'))).toBe('body')
  expect(edited(simple('khub edit cmp-search --body-file notes.md'))).toBe('body')
  expect(edited(simple('khub edit cmp-search body "the secret text"'))).toBe('body')
})

test('a simple add gains the author, and nothing else does', () => {
  const add = 'khub add adr --title "Use RE2 patterns" --status accepted '

  expect(stamped(add, classify(add) as Simple, 'claude-code')).toBe(
    'khub add adr --author \'claude-code\' --title "Use RE2 patterns" --status accepted ',
  )
  expect(stamped('khub edit x kind service', classify('khub edit x kind service') as Simple, 'claude-code')).toBe(null)
  expect(stamped('khub add adr --author dana', classify('khub add adr --author dana') as Simple, 'claude-code')).toBe(null)
  expect(stamped(add, classify(add) as Simple, '')).toBe(null)
})
