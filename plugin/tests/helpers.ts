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
    statuses: [] as Array<string | undefined>,

    // Set by a test to cut the stdout of every later khub call.
    isCut: false,
  }

  on('fs.exists', (_$, e) => ({ value: e.path === `${ROOT}/.khub` }))
  on('ui.status', (_$, e) => {
    host.statuses.push(e.text)

    return { value: undefined }
  })
  on('ui.log', () => ({ value: undefined }))
  on('session.start', (_$, e) => ({ cwd: e.cwd }))
  on('turn.start', (_$, e) => ({ turnId: e.turnId }))
  on('process.run', (_$, e) => {
    if (options.hasKhub === false) throw new Error('spawn khub ENOENT')

    const at = e.argv.indexOf('-C')
    const args = at < 0 ? e.argv.slice(1) : e.argv.slice(at + 2)

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
