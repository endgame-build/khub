// This file is the shell. Every call on `$` lives here, because the engine follows `$`
// into the hooks module only. The files beside it are pure and hold the logic.

import { atom, memberOf, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { KhubCall, KhubType } from '../types'
import { summaryLine } from './band'
import { clean, documentsOf, firstLine, parseJson, refusal } from './cli'
import { callsOf, classify, mutates } from './parse'
import { callRow, emptyBlock } from './rows'
import { bandParts, diffOf, editedBy, EMPTY_SESSION, summaryOf, withEdited, withIds } from './session'
import { finished, waiting } from './summarize'
import { ancestors, binCandidates, entityAt, isElsewhere, isOlder, MIN_KHUB, relative } from './workspace'
import type { Ws } from './workspace'

// The engine reads which state a module touches from atoms that are consts of this file.
const workspaceAtom = atom({ plugin: 'khub', key: 'workspace' } as const, null)
const summaryAtom = atom({ plugin: 'khub', key: 'summary' } as const, null)
const sessionAtom = atom({ plugin: 'khub', key: 'session' } as const, EMPTY_SESSION)

// These two are families with one member per tool_use_id.
const callRows = atom({ plugin: 'khub', key: 'calls' } as const, null)
const rawRows = atom({ plugin: 'khub', key: 'rawRows' } as const, false)

type Ran = { exit: number; json: unknown; out: string; note: string; isCut: boolean }
type SchemaView = { provenance?: { preset?: string; version?: string }; types?: KhubType[] }

// The session's workspace is kept beside its state copy, so hooks read it without a hop.
let ws: Ws | null = null

// Refreshes run one after another, so an older read never lands on a newer one.
let refreshing: Promise<void> = Promise.resolve()

// A refresh asked for while one waits to start joins the waiting one.
let isQueued = false

// Runs khub in the workspace. Exit 0 is success, 1 a failed gate, 2 a refusal, and -1
// means khub did not run, with the reason in `note`.
async function khub($: EngineInterface, here: Ws, args: readonly string[]): Promise<Ran> {
  try {
    const ran = await $.process.run([...here.bin, '-C', here.root, ...args], { timeoutMs: 20_000 })

    return {
      exit: ran.exitCode,
      json: parseJson(ran.stdout),
      out: ran.stdout,
      note: ran.stderr.trim(),
      isCut: ran.isStdoutTruncated,
    }
  } catch (error) {
    return { exit: -1, json: undefined, out: '', note: String(error), isCut: false }
  }
}

// Lets work run on after its hook returned. A failure goes to the debug log.
function background($: EngineInterface, work: Promise<unknown>) {
  void work.catch(error => $.ui.log(String(error), { to: 'debug' }))
}

// Reads the resolved schema. A schema that does not resolve is the status line's trouble
// until it does.
async function loadSchema($: EngineInterface) {
  const here = ws

  if (!here) return

  const ran = await khub($, here, ['schema', '--format', 'json'])
  const view = ran.json as SchemaView | undefined

  if (ran.exit !== 0 || !view?.types) {
    $.ui.status(clean(`schema: ${firstLine(refusal(ran.json)?.message ?? (ran.out || ran.note))}`))

    return
  }

  const types = view.types.map(({ name, layout, format, path }) => ({ name, layout, format, path }))

  // The preset's name and version come from workspace files, so they are cleaned like entity text.
  const workspace = {
    root: here.root,
    bin: here.bin,
    version: here.version,
    preset: clean(view.provenance?.preset ?? ''),
    presetVersion: clean(view.provenance?.version ?? ''),
  }

  ws = { ...workspace, types }
  $.ui.status(undefined)
  await update($, workspaceAtom, () => workspace)
}

// Finds the workspace above `cwd`, resolves khub and loads the schema.
async function detect($: EngineInterface, cwd: string, binary: string): Promise<'ok' | 'none' | 'no-binary' | 'old'> {
  ws = null

  let root: string | undefined

  for (const dir of ancestors(cwd)) {
    if (await $.fs.exists(`${dir}/.khub`)) {
      root = dir
      break
    }
  }

  if (root === undefined) return 'none'

  let found: Ws | null = null

  for (const bin of binCandidates(root, binary)) {
    const program = bin[0] as string

    if (program.startsWith('/') && !(await $.fs.exists(program))) continue

    try {
      const ran = await $.process.run([...bin, '--version'], { timeoutMs: 5_000 })

      if (ran.exitCode === 0) {
        found = { root, bin, version: ran.stdout.trim(), preset: '', presetVersion: '', types: [] }
        break
      }
    } catch {
      // This candidate is not installed, so the next one is tried.
    }
  }

  if (found === null) return 'no-binary'

  // An older khub prints documents the mod misreads, so every hook passes through.
  if (isOlder(found.version, MIN_KHUB)) return 'old'

  ws = found
  await loadSchema($)

  return 'ok'
}

// Reads the gate and the entities. A cut document, or a call that did not answer, keeps
// what the band shows.
async function runRefresh($: EngineInterface) {
  const here = ws

  if (!here) return

  const [check, query] = await Promise.all([
    khub($, here, ['check', '--format', 'json']),
    khub($, here, ['query', '--format', 'json']),
  ])
  const found = check.isCut || query.isCut || query.exit !== 0 ? null : summaryOf(check.json, query.json)

  if (found === null) return

  await update($, summaryAtom, () => found.summary)
  await update($, sessionAtom, session => withIds(session, found.ids))
}

// Queues a refresh behind the running one. The first refresh of a session fixes the baseline.
function refresh($: EngineInterface): Promise<void> {
  if (isQueued) return refreshing

  isQueued = true

  const run = () => {
    isQueued = false

    return runRefresh($)
  }

  refreshing = refreshing.then(run, run)

  return refreshing
}

// Looks for the workspace and khub. The status line says why khub cannot be asked, and
// the engine draws that line as a warning under the plugin's name.
async function start($: EngineInterface, cwd: string, binary: string) {
  const found = await detect($, cwd, binary)

  if (found === 'no-binary') $.ui.status('not installed')
  if (found === 'old') $.ui.status(`needs khub ${MIN_KHUB} or newer`)
  if (found === 'ok') await refresh($)
}

// Follows an Edit or a Write of a file khub reads. An entity file counts its entity as
// edited, and a schema file is read again before the band.
async function afterFile($: EngineInterface, path: string) {
  const here = ws
  const inside = here ? relative(here, path) : null

  if (!here || inside === null) return

  if (inside.startsWith('.khub/')) {
    background($, loadSchema($).then(() => refresh($)))

    return
  }

  const entity = entityAt(here, path)

  if (entity === null) return

  // A collection file holds many entities, so it names none.
  if (entity.slug !== '*') {
    const id = `${entity.type}/${entity.slug}`

    await update($, sessionAtom, session => withEdited(session, [id]))
  }

  background($, refresh($))
}

export const register: Register = (on, options) => {
  const binary = String(options.binary ?? '')

  // The session's directory is kept for a later look when khub could not be asked.
  let cwd = ''

  // The baseline is taken before the first prompt, so no write lands inside it.
  on('session.start', async ($, e, next) => {
    cwd = e.cwd
    await start($, cwd, binary)

    return next(e)
  })

  // A turn may follow edits made outside Claude, so the schema and the band are read again.
  // Without a workspace the mod looks again, so trouble clears once its cause is gone.
  on('turn.start', ($, e, next) => {
    if (ws) background($, loadSchema($).then(() => refresh($)))
    else if (cwd !== '') background($, start($, cwd, binary))

    return next(e)
  })

  // The call runs unchanged and the model reads what khub printed. A simple call and a
  // chain of calls get a compact row, and a call that changes the workspace is followed
  // by a refresh.
  on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
    if (!ws) return next(e)

    const here = ws
    const parsed = classify(e.command)
    const calls = callsOf(parsed).filter(call => !isElsewhere(here, call.workspace))

    if (calls.length === 0) return next(e)

    const simple = parsed.kind === 'simple' ? parsed : null

    // A chain keeps its row only while every call in it is aimed at this workspace.
    const chain = parsed.kind === 'chain' && calls.length === parsed.calls.length ? parsed.calls : null
    const shown = simple ? [simple] : (chain ?? [])
    const row = memberOf(callRows, { requestId: e.tool_use_id ?? '' })

    if (shown.length > 0) await update($, row, (): KhubCall[] => waiting(shown))

    const startedAt = await $.clock.now()
    const ran = await next(e)
    const ms = (await $.clock.now()) - startedAt

    // A call the user or a hook refused never ran, so its row is the engine's.
    if (ran.deny !== undefined) {
      if (shown.length > 0) await update($, row, () => null)

      return ran
    }

    if (shown.length > 0) {
      // A failed gate or a refusal arrives as an error whose text is what khub printed.
      const result = (ran.isError === true ? undefined : ran.result) as
        | { stdout?: string; stderr?: string; persistedOutputPath?: string }
        | undefined
      const stdout = ran.isError === true ? (ran.text ?? '') : (result?.stdout ?? '')
      const stderr = result?.stderr ?? ''

      // A simple call may print prose, or notes around its document. A chain must print
      // one document per call, and one that does not is the engine's row.
      const docs = simple ? [parseJson(stdout)] : documentsOf(stdout, shown.length)
      const isProse = simple !== null && docs?.[0] === undefined

      // Output the engine moved to disk may be cut here. A cut document is the engine's row.
      const isCut = isProse && result?.persistedOutputPath !== undefined

      // A prose command prints no document, so its first line is the result. stderr does
      // not say which call of a chain wrote it, so a chain's rows carry no note.
      const note = simple ? (isProse ? stdout || stderr : stderr) : ''
      const edited = docs === null ? [] : shown.flatMap((call, i) => editedBy(call, docs[i]))

      await update($, row, (): KhubCall[] | null => (docs === null || isCut ? null : finished(shown, docs, ms, note)))
      if (edited.length > 0) await update($, sessionAtom, session => withEdited(session, edited))
    }

    if (calls.some(mutates)) background($, refresh($))

    return ran
  }).catch(async ($, e, next) => {
    // A failure in the mod hands the row back to the engine and passes the call on.
    await update($, memberOf(callRows, { requestId: e.tool_use_id ?? '' }), () => null)

    return next(e)
  })

  on('tool.call', { tool: 'Edit' }, async ($, e, next) => {
    const ran = await next(e)

    if (ran.deny === undefined && ran.isError !== true) await afterFile($, e.file_path)

    return ran
  }).catch(($, e, next) => next(e))

  on('tool.call', { tool: 'Write' }, async ($, e, next) => {
    const ran = await next(e)

    if (ran.deny === undefined && ran.isError !== true) await afterFile($, e.file_path)

    return ran
  }).catch(($, e, next) => next(e))

  // The compact row holds a call's head, its result and its list, so it stands for the
  // whole row. The `json` button hands the row back to the engine.
  on('ui.render', { component: 'ToolUse' }, async ($, e, next) => {
    if (!ws || e.props.tool !== 'Bash') return next(e)

    const calls = await read($, memberOf(callRows, e))

    if (calls === null || (await read($, memberOf(rawRows, e)))) return next(e)

    return callRow($.ui.resolve(e), calls, () => background($, update($, memberOf(rawRows, e), () => true)))
  })

  // The detailed transcript draws a call's result as a block of its own. The compact row
  // already shows the result, so the block of a row the mod drew is drawn as nothing.
  on('ui.render', { component: 'ToolResult' }, async ($, e, next) => {
    if (!ws || e.props.tool !== 'Bash') return next(e)

    const calls = await read($, memberOf(callRows, e))

    if (calls === null || (await read($, memberOf(rawRows, e)))) return next(e)

    return emptyBlock($.ui.resolve(e))
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    if (!ws || e.props.hasSurvey) return next(e)

    const summary = await read($, summaryAtom)
    const workspace = await read($, workspaceAtom)

    // Before the first refresh there is nothing to say yet.
    if (summary === null || workspace === null) return next(e)

    const parts = bandParts(workspace.preset, workspace.presetVersion, summary, diffOf(await read($, sessionAtom)))

    return summaryLine($.ui.resolve(e), parts, !summary.passed, e.props.bodyColumns)
  })
}
