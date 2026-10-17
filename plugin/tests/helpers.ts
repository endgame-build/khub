import { mock } from 'claude-code/testing'
import type { On } from 'claude-code'

import { F } from './fixtures'
import type { Fixture } from './fixtures'

export const ROOT = '/ws'

type HostOptions = {
  // What `khub schema` answers, when not the recorded schema.
  schema?: Fixture

  // What `khub --version` prints, when not the recorded version.
  version?: string

  // False for a machine with no khub to run.
  hasKhub?: boolean

  // Programs that fail to start, as a spawn of a missing file does.
  missing?: string[]

  // The paths that exist, when not the workspace's `.khub` under ROOT.
  files?: string[]
}

const answer = (fixture: Fixture, isStdoutTruncated = false) => ({
  exitCode: fixture.exit,
  stdout: fixture.stdout,
  stderr: fixture.stderr,
  isStdoutTruncated,
  isStderrTruncated: false,
})

const same = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every((word, i) => word === b[i])

// A recorded reply under other arguments, for a call the recorder did not make.
export const as = (fixture: Fixture, args: string[]): Fixture => ({ ...fixture, args })

// Stands in for the host. It gives a workspace at ROOT and answers khub calls from recorded
// output. Fixtures that share their arguments answer in the order given, and the last one repeats.
export function fakeHost(on: On, replies: Fixture[] = [], options: HostOptions = {}) {
  const queue: Fixture[] = [options.schema ?? F.schema, ...replies]
  const host = {
    clock: mock.clock(on, { now: 0 }),
    ran: [] as string[],

    // The command each khub call ran, the words before its arguments.
    programs: [] as string[][],
    statuses: [] as Array<string | undefined>,

    // Set by a test to cut the stdout of every later khub call.
    isCut: false,
  }

  // The kit runs on this machine's platform, so it resolves a Windows path, which is not
  // absolute here, against its own directory. A path is matched with that prefix too.
  on('fs.exists', (_$, e) => ({
    value: (options.files ?? [`${ROOT}/.khub`]).some(file => e.path === file || e.path.endsWith(`/${file}`)),
  }))
  on('ui.status', (_$, e) => {
    host.statuses.push(e.text)

    return { value: undefined }
  })
  on('ui.log', () => ({ value: undefined }))
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('turn.start', (_$, e) => ({ turnId: e.turnId }))
  on('process.run', (_$, e) => {
    if (options.hasKhub === false || options.missing?.includes(e.argv[0] ?? '')) throw new Error('spawn khub ENOENT')

    // A call is `<program…> -C <root> <args>`, or `<program…> --version`.
    const at = e.argv.indexOf('-C')
    const args = at < 0 ? e.argv.slice(-1) : e.argv.slice(at + 2)

    host.programs.push(e.argv.slice(0, at < 0 ? -1 : at))
    host.ran.push(args.join(' '))

    if (args[0] === '--version') return { value: answer({ ...F.version, stdout: options.version ?? F.version.stdout }) }

    const index = queue.findIndex(fixture => same(fixture.args, args))
    const fixture = queue[index]

    if (fixture === undefined) throw new Error(`no fixture for: khub ${args.join(' ')}`)
    if (queue.some((later, i) => i > index && same(later.args, args))) queue.splice(index, 1)

    return { value: answer(fixture, host.isCut) }
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
