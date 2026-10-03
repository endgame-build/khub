import { mock } from 'claude-code/testing'
import type { On } from 'claude-code'

import { F } from './fixtures'
import type { Fixture } from './fixtures'

export const ROOT = '/ws'

type HostOptions = {
  // Paths that exist beside the workspace's own `.khub`.
  files?: string[]

  // Slash command names someone else already owns.
  taken?: string[]

  // What `khub schema` answers, when not the recorded schema.
  schema?: Fixture

  // What `khub --version` prints, when not the recorded version.
  version?: string

  // What git answers for `user.name`. Empty stands for a name that is not set.
  user?: string

  // Where the clock starts, in milliseconds. It moves only when a test moves it.
  now?: number

  // What the plugin store holds when the session starts.
  store?: Record<string, unknown>
}

const UPKEEP = [
  'query --draft --format json',
  'upgrade --dry-run --format json',
]

const answer = (fixture: Fixture) => ({
  exitCode: fixture.exit,
  stdout: fixture.stdout,
  stderr: fixture.stderr,
  isStdoutTruncated: false,
  isStderrTruncated: false,
})

const same = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((word, i) => word === b[i])

// A recorded reply under other arguments, for a call the recorder did not make.
export const as = (fixture: Fixture, args: string[]): Fixture => ({ ...fixture, args })

// Stands in for the host: a workspace at ROOT, and khub answering from recorded output.
// Fixtures that share their arguments answer in the order given; the last one repeats.
export function fakeHost(on: On, replies: Fixture[] = [], options: HostOptions = {}) {
  // The upkeep calls every session makes answer from the recordings unless a test gives its own.
  const queue: Fixture[] = [
    options.schema ?? F.schema,
    ...replies,
    F.query_draft,
    F.upgrade_dry,
    as(F.validate_adr, ['validate', '--format', 'json']),
  ]
  const host = {
    clock: mock.clock(on, { now: options.now ?? 0 }),
    ran: [] as string[],

    // The calls every refresh and every session start make, kept apart from what a test is about.
    upkeep: [] as string[],
    closed: [] as string[],
    copied: [] as string[],
    statuses: [] as Array<string | undefined>,
    toasts: [] as string[],
    commands: [] as string[],
    opened: [] as string[],
    fills: [] as string[],

    // The fills that went in at the cursor and left the rest of the prompt alone.
    inserted: [] as string[],
    logs: [] as string[],
  }

  mock.store(on, options.store)
  on('fs.exists', (_$, e) => ({ value: e.path === `${ROOT}/.khub` || (options.files ?? []).includes(e.path) }))
  on('ui.status', (_$, e) => {
    host.statuses.push(e.text)

    return { value: undefined }
  })
  on('ui.toast', (_$, e) => {
    host.toasts.push(e.text)

    return { value: undefined }
  })
  on('ui.log', (_$, e) => {
    host.logs.push(e.text)

    return { value: undefined }
  })
  on('ui.open', (_$, e) => {
    host.opened.push(e.id)

    return { value: { isPlaced: true as const } }
  })
  on('ui.close', (_$, e) => {
    host.closed.push(e.id)

    return { value: undefined }
  })
  on('ui.copy', (_$, e) => {
    host.copied.push(e.text)

    return { value: { isCopied: true as const } }
  })
  on('prompt.fill', (_$, e) => {
    host.fills.push(e.text)
    if (e.mode === 'insert') host.inserted.push(e.text)

    return { isFilled: true }
  })
  on('command.register', (_$, e) => {
    if ((options.taken ?? []).includes(e.name)) throw new Error(`"/${e.name}" refused: it is the user's /${e.name}`)

    host.commands.push(e.name)

    return { value: { command: e.name } }
  })
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('turn.start', (_$, e) => ({ turnId: e.turnId }))
  on('process.run', (_$, e) => {
    if (e.argv[0] === 'git') {
      const name = options.user ?? 'Sam Rivera'

      return { value: answer({ args: [], exit: name === '' ? 1 : 0, stdout: name, stderr: '' }) }
    }

    const at = e.argv.indexOf('-C')
    const args = at < 0 ? e.argv.slice(1) : e.argv.slice(at + 2)
    const line = args.join(' ')

    if (UPKEEP.includes(line)) host.upkeep.push(line)
    else host.ran.push(line)

    if (args[0] === '--version') return { value: answer({ ...F.version, stdout: options.version ?? F.version.stdout }) }

    const index = queue.findIndex(fixture => same(fixture.args, args))
    const fixture = queue[index]

    if (fixture === undefined) throw new Error(`no fixture for: khub ${args.join(' ')}`)
    if (queue.some((later, i) => i > index && same(later.args, args))) queue.splice(index, 1)

    return { value: answer(fixture) }
  })

  return host
}

export const START = { cwd: ROOT, surface: 'terminal' as const, isInteractive: true }

// The test environment has a timer, and the type roots declare none.
declare const setTimeout: (run: () => void, ms: number) => unknown

// Waits for work a hook left running after it returned, in steps of five milliseconds.
export async function settled(isDone: () => boolean, steps = 200) {
  for (let i = 0; i < steps && !isDone(); i += 1) await new Promise<void>(resolve => setTimeout(resolve, 5))
}

// A slash command as the user types it.
export const slash = (command: string, args = '') => ({
  command,
  args,
  origin: { kind: 'composer' as const },
  presentation: { isFullscreen: true, columns: 120 },
})
