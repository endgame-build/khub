// The shell. Every call on `$` lives in this file, because the engine follows `$` into
// the hooks module only. The files beside it are pure and hold the logic.

import { atom, memberOf, read, update } from 'claude-code'
import type { EngineInterface, Register, ToolCallResult } from 'claude-code'

import type { KhubCall, KhubForm, KhubNotice, KhubStats, KhubTab, KhubType } from '../types'
import { bandLine, summaryLine } from './band'
import { firstLine, isRow, parseJson, refusal } from './cli'
import { passthrough, route } from './commands'
import { doctorNotice, doctorOf, doctorText } from './doctor'
import { argvOf, openForm, targetTypes, unlinksOf } from './forms'
import { formView } from './formview'
import { askReason, denyBash, newFileDenial } from './guards'
import {
  blockReason,
  changeOf,
  FILE_EDITED,
  ledgerWith,
  lineOf,
  nextHealth,
  notesFor,
  noticeOf,
  validationLine,
  validationMark,
  validationOf,
} from './integrity'
import type { Change, Validation } from './integrity'
import { paneBody } from './pane'
import type { PaneActions } from './pane'
import { callsOf, changesSchema, classify, fromArgs, isVerb, isWrite, mutates, stamped, takesFormat } from './parse'
import type { Simple } from './parse'
import { inboundNote } from './reads'
import { toolResultRow, toolUseRow, withValidation } from './rows'
import { schemaNotes, schemaNotice, validateNotes } from './schemaloop'
import { EMPTY_DOCTOR, EMPTY_HEALTH, EMPTY_STATS, EMPTY_UI, EMPTY_VIEW } from './state'
import { merged, since, withBypass, withCall } from './stats'
import { statusText } from './status'
import { summarize } from './summarize'
import { entityOf, recordsOf } from './views'
import { ancestors, binCandidates, entityAt, isElsewhere, isOlder, isSchemaFile, MIN_KHUB } from './workspace'
import type { Ws } from './workspace'

// The engine reads which state a module touches from atoms that are consts of this file.
const workspaceAtom = atom({ plugin: 'khub', key: 'workspace' } as const, null)
const typesAtom = atom({ plugin: 'khub', key: 'types' } as const, [])
const healthAtom = atom({ plugin: 'khub', key: 'health' } as const, EMPTY_HEALTH)
const ledgerAtom = atom({ plugin: 'khub', key: 'ledger' } as const, [])
const uiAtom = atom({ plugin: 'khub', key: 'ui' } as const, EMPTY_UI)
const statsAtom = atom({ plugin: 'khub', key: 'stats' } as const, EMPTY_STATS)
const noticeAtom = atom({ plugin: 'khub', key: 'notice' } as const, null)
const allStatsAtom = atom({ plugin: 'khub', key: 'allStats' } as const, null)
const viewAtom = atom({ plugin: 'khub', key: 'view' } as const, EMPTY_VIEW)
const formAtom = atom({ plugin: 'khub', key: 'form' } as const, null)
const doctorAtom = atom({ plugin: 'khub', key: 'doctor' } as const, EMPTY_DOCTOR)

// Families, one member per tool_use_id.
const callRows = atom({ plugin: 'khub', key: 'calls' } as const, null)
const validationRows = atom({ plugin: 'khub', key: 'validations' } as const, null)
const rawRows = atom({ plugin: 'khub', key: 'rawRows' } as const, false)

const PANE = 'khub'
const FORM = 'khub-form'
const DAY = 86_400_000

type Ran = { exit: number; json: unknown; out: string; note: string; ms: number }
type SchemaView = { provenance?: { preset?: string; version?: string }; types?: KhubType[] }
type FileCall = { tool: 'Edit' | 'Write'; tool_use_id: string; file_path: string }

// The workspace of this session, kept beside its state copy so hooks read it without a hop.
let ws: Ws | null = null
let turn = ''

// Whether a khub skill owns `/khub`. The mod then shares the name with it.
let isShared = false

// Why khub could not be asked about the workspace, shown on the status line while it lasts.
let trouble = ''

// The band line the user dismissed. It stays away until it says something else.
let dismissed = ''

// The band line a schema edit left, until the user rewires or snapshots.
let schemaLine: KhubNotice | null = null

// Why a write did not happen. `code` is khub's refusal code, empty when khub printed none.
type Failure = { code: string; message: string }

// Who the user's own writes are stamped with, and the tally earlier sessions left in the store.
let userName = ''
let storedStats: KhubStats | null = null

// The session tally as last written to the store.
let writtenStats: KhubStats = EMPTY_STATS

// Counts the reads Browse and Search ask for, so an answer that arrives late is dropped.
let asking = 0

// Refreshes run one after another, so an older check never lands on a newer one.
let refreshing: Promise<void> = Promise.resolve()

// Runs khub in the workspace. Exit 0 is success, 1 a failed gate, 2 a refusal, and -1
// means khub did not run, with the reason in `note`.
async function khub($: EngineInterface, args: readonly string[]): Promise<Ran> {
  const { bin, root } = ws as Ws
  const startedAt = await $.clock.now()

  try {
    const ran = await $.process.run([...bin, '-C', root, ...args], { timeoutMs: 20_000 })
    const ms = (await $.clock.now()) - startedAt

    return { exit: ran.exitCode, json: parseJson(ran.stdout), out: ran.stdout, note: ran.stderr.trim(), ms }
  } catch (error) {
    return { exit: -1, json: undefined, out: '', note: String(error), ms: (await $.clock.now()) - startedAt }
  }
}

// Lets work run on after its hook returned. A failure goes to the debug log.
function background($: EngineInterface, work: Promise<unknown>) {
  void work.catch(error => $.ui.log(`khub: ${String(error)}`, { to: 'debug' }))
}

// Reads the resolved schema. A schema that does not resolve is trouble until it does.
async function loadSchema($: EngineInterface): Promise<string | null> {
  const ran = await khub($, ['schema', '--format', 'json'])
  const view = ran.json as SchemaView | undefined
  const types = view?.types

  if (ran.exit !== 0 || !types) {
    trouble = `schema: ${firstLine(refusal(ran.json)?.message ?? (ran.out || ran.note))}`

    return trouble
  }

  trouble = ''
  ws = { ...(ws as Ws), types, preset: view.provenance?.preset ?? '', presetVersion: view.provenance?.version ?? '' }
  await update($, typesAtom, () => types)

  return null
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

  for (const bin of binCandidates(root, binary)) {
    const program = bin[0] as string

    if (program.startsWith('/') && !(await $.fs.exists(program))) continue

    try {
      const ran = await $.process.run([...bin, '--version'])

      if (ran.exitCode === 0) {
        ws = { root, bin, version: ran.stdout.trim(), preset: '', presetVersion: '', types: [] }
        break
      }
    } catch {
      // This candidate is not installed, so the next one is tried.
    }
  }

  if (ws === null) return 'no-binary'

  // An older khub prints documents the mod misreads, so every hook passes through.
  if (isOlder(ws.version, MIN_KHUB)) {
    ws = null

    return 'old'
  }

  await loadSchema($)

  const { types: _types, ...workspace } = ws as Ws
  await update($, workspaceAtom, () => workspace)

  return 'ok'
}

// Redraws the status line and the band from health and the ledger. The engine draws a
// plugin's status line as a warning, so it carries problems only, and the band's quiet
// line carries the everyday summary.
async function show($: EngineInterface) {
  const health = await read($, healthAtom)
  const ledger = await read($, ledgerAtom)
  const notice = noticeOf(ledger, health, turn) ?? schemaLine ?? doctorNotice(await read($, doctorAtom))
  const said = notice?.parts.map(part => part.text).join(' · ') ?? ''

  $.ui.status(trouble !== '' ? trouble : health.passed === false ? statusText(health, await read($, statsAtom)) : undefined)
  await update($, noticeAtom, () => (said === dismissed ? null : notice))
}

async function runRefresh($: EngineInterface) {
  if (!ws) return

  const [check, status, drafts] = await Promise.all([
    khub($, ['check', '--format', 'json']),
    khub($, ['status', '--format', 'json']),
    khub($, ['query', '--draft', '--format', 'json']),
  ])

  await update($, viewAtom, view => ({ ...view, drafts: recordsOf(drafts.json) }))

  // Exit 1 is a failing workspace, which is an answer. Anything else is khub not answering.
  if (!trouble.startsWith('schema')) trouble = check.exit === 0 || check.exit === 1 ? '' : `check: ${firstLine(check.note || check.out)}`

  await update($, healthAtom, health => nextHealth(health, check.json, status.json, check.ms))
  await show($)
}

// Runs check and status. The first run of a session fixes the baseline.
function refresh($: EngineInterface): Promise<void> {
  refreshing = refreshing.then(
    () => runRefresh($),
    () => runRefresh($),
  )

  return refreshing
}

// After a change, validates the entity, records it, refreshes health, and returns the
// lines the model should read. `row` names the call whose row shows the result.
async function afterWrite(
  $: EngineInterface,
  change: Change | null,
  row?: { id: string; kind: 'call' | 'file' },
): Promise<string[]> {
  let validation: Validation | null = null

  if (change && change.op !== 'remove' && change.entity.slug !== '*') {
    const id = `${change.entity.type}/${change.entity.slug}`
    const validated = validationOf((await khub($, ['validate', id, '--format', 'json'])).json)

    validation = validated

    if (validated && row?.kind === 'file') {
      await update($, memberOf(validationRows, { requestId: row.id }), () => validationLine(id, validated))
    }

    if (validated && row?.kind === 'call') {
      const mark = validationMark(validated)

      await update($, memberOf(callRows, { requestId: row.id }), call =>
        call === null ? null : { ...call, line: call.line + mark.text, tone: mark.tone },
      )
    }
  }

  // A file khub does not know as an entity, such as a stray in a type's folder, is no ledger entry.
  const isEntity = !(row?.kind === 'file' && validation === null)

  if (change && isEntity) await update($, ledgerAtom, ledger => ledgerWith(ledger, change, validation, turn))

  await refresh($)

  let lines: string[] = []

  await update($, healthAtom, health => {
    const told = notesFor(health, change, validation)

    lines = told.lines

    return told.health
  })

  return lines
}

// What follows a khub call that may have changed the workspace, whoever ran it.
async function afterCall(
  $: EngineInterface,
  call: Simple,
  json: unknown,
  by: Change['by'],
  rowId?: string,
): Promise<string[]> {
  if (refusal(json) || !mutates(call)) return []
  if (changesSchema(call)) await loadSchema($)

  const change = changeOf(call, json)

  return afterWrite($, change && { ...change, by }, rowId === undefined ? undefined : { id: rowId, kind: 'call' })
}

// Registers `/khub`. The engine refuses the name where a khub skill owns it, as the plugin's
// own skill does. The mod then shares it: it answers the pane and khub commands, and the
// skill gets the rest.
async function registerCommand($: EngineInterface) {
  try {
    await $.command.register({
      name: 'khub',
      description: 'Open the khub pane, or run any khub command',
      argumentHint: '[khub command]',
    })
    isShared = false
  } catch {
    isShared = true
  }
}

// Takes the pane and khub commands for the mod, and hands anything else to the skill.
// `isSkills` says the command arrived under a skill's own name, which is always shared.
function answerCommand<E extends { args: string }, R>(
  $: EngineInterface,
  e: E,
  next: (e: E) => Promise<R>,
  isSkills: boolean,
): Promise<R | { text?: string; context?: string[] }> {
  const first = e.args.trim().split(/\s+/)[0]
  const isWhole = !isShared && !isSkills
  const isOurs = ws !== null && (isWhole || e.args.trim() === '' || isVerb(first) || e.args.trim() === 'doctor')

  if (!isOurs) return next(e)

  // The engine prints the plugin's name before the text, so the text's own goes.
  return runCommand($, e.args).then(said => (said.text === undefined ? said : { ...said, text: said.text.replace(/^khub:? /, '') }))
}

async function openPane($: EngineInterface, tab?: KhubTab) {
  if (tab !== undefined) await update($, uiAtom, ui => ({ ...ui, tab, diff: null }))

  await $.ui.open({ id: PANE, title: ws?.preset ? `khub · ${ws.preset}` : 'khub' })
}

// The command with no arguments opens the pane. With arguments it runs khub and prints
// what came back.
async function runCommand($: EngineInterface, args: string): Promise<{ text?: string; context?: string[] }> {
  const routed = route(args)

  if (routed.kind === 'open') {
    await openPane($)

    return {}
  }

  if (routed.kind === 'error') return { text: routed.message }

  if (routed.kind === 'doctor') {
    await examine($)

    return { text: doctorText(await read($, doctorAtom)) }
  }

  const asked = fromArgs(routed.argv)

  // The user's own add carries their name, as the agent's carries the agent's.
  const argv = asked.verb === 'add' && asked.flags.author === undefined && userName !== '' ? [...routed.argv, `--author=${userName}`] : routed.argv
  const call = fromArgs(argv)
  const ran = await khub($, takesFormat(call) ? [...argv, '--format', 'json'] : argv)

  if (ran.exit < 0) return { text: `khub did not run: ${ran.note}` }

  const { head, line } = summarize(call, ran.json, '')
  const notes = await afterCall($, call, ran.json, 'user')

  return {
    text: passthrough(call, ran.json, ran.out, ran.note),
    context: [`khub: the user ran \`${head}\`${line === '' ? '' : `, which answered: ${line}`}`, ...notes],
  }
}

// Reads what Browse shows for its place: a type's entities, or one entity with its edges.
async function browse($: EngineInterface, type: string | null, id: string | null, isInPlace = false) {
  const asked = (asking += 1)

  await update($, uiAtom, ui => ({ ...ui, tab: isInPlace ? ui.tab : ('browse' as const), browse: { type, id } }))

  if (id !== null) {
    const [got, neighbors] = await Promise.all([
      khub($, ['get', id, '--format', 'json']),
      khub($, ['neighbors', id, '--format', 'json']),
    ])
    const entity = entityOf(got.json, neighbors.json)

    if (asked !== asking) return

    await update($, viewAtom, view => ({
      ...view,
      entity,
      tree: null,
      problem: entity ? null : (refusal(got.json)?.message ?? firstLine(got.note || got.out)),
    }))
  } else if (type !== null) {
    const ran = await khub($, ['query', '--type', type, '--format', 'json'])
    const problem = refusal(ran.json)?.message ?? (ran.exit === 0 ? null : firstLine(ran.note || ran.out))

    if (asked !== asking) return

    await update($, viewAtom, view => ({ ...view, entities: recordsOf(ran.json), problem }))
  }
}

async function search($: EngineInterface, text: string) {
  await update($, uiAtom, ui => ({ ...ui, query: text }))

  const asked = (asking += 1)

  if (text.trim() === '') return update($, viewAtom, view => ({ ...view, hits: [], note: '' }))

  // The text follows `--`, so a dash-led query is never read as a flag.
  const ran = await khub($, ['search', '--plain', '--limit', '20', '--format', 'json', '--', text])

  if (asked !== asking) return

  await update($, viewAtom, view => ({ ...view, hits: recordsOf(ran.json), note: refusal(ran.json)?.message ?? ran.note }))
}

// Runs one khub write for the user, then brings the pane up to date. Returns why it failed, if it did.
async function write($: EngineInterface, argv: string[]): Promise<Failure | null> {
  const call = fromArgs(argv)
  const ran = await khub($, [...argv, '--format', 'json'])
  const refused = refusal(ran.json)

  if (ran.exit < 0) return { code: '', message: `khub did not run: ${ran.note}` }
  if (refused) return refused

  // A usage error prints no document.
  if (ran.exit !== 0) return { code: '', message: firstLine(ran.note || ran.out) }

  await afterCall($, call, ran.json, 'user')

  const { browse: place } = await read($, uiAtom)
  const removed = call.verb === 'remove' && isRow(ran.json) ? ran.json.id : undefined

  // A removed entity leaves Browse at its type's list, when Browse was showing it.
  await browse($, place.type, removed === place.id ? null : place.id, true)

  return null
}

// Opens a form over a type, with the entities its relations may point at.
async function showForm($: EngineInterface, mode: KhubForm['mode'], typeName: string, id: string | null) {
  const here = ws as Ws
  const type = here.types.find(candidate => candidate.name === typeName)

  if (!type) return

  const targets: KhubForm['targets'] = {}

  if (mode !== 'remove') {
    for (const target of targetTypes(type, here.types)) {
      const ran = await khub($, ['query', '--type', target, '--format', 'json'])

      targets[target] = recordsOf(ran.json).map(record => ({
        value: record.id.split('/')[1] ?? record.id,
        label: record.title === '' ? record.id : `${record.id} · ${record.title}`,
      }))
    }
  }

  const open = (await read($, viewAtom)).entity

  await update($, formAtom, () => openForm(mode, type, open?.id === id ? open : null, targets, id))
  await $.ui.open({ id: FORM, title: 'khub', focus: true, closeOnEscape: true })
}

async function submitForm($: EngineInterface, asDraft: boolean) {
  const form = await read($, formAtom)
  const type = ws?.types.find(candidate => candidate.name === form?.type)

  if (!form || !type) return

  const argv = argvOf(form, type, userName, asDraft)
  let failure: Failure | null = null

  // The edit goes first, so a refused edit removes no edge. The first failure stops the rest.
  for (const each of [...(argv === null ? [] : [argv]), ...unlinksOf(form, type)]) {
    failure = await write($, each)

    if (failure !== null) break
  }

  if (failure !== null) {
    const { code, message } = failure

    await update($, formAtom, current => current && { ...current, error: message, refusal: code })

    return
  }

  await update($, formAtom, () => null)
  await $.ui.close({ id: FORM })
}

// What the last upgrade preview answered, kept across sessions with its time.
type Preview = { at: number; upgrade: string | null; drift: string[] }

// Reads what the workspace needs from the user. The upgrade preview copies the workspace,
// so it runs at most once a day.
async function examine($: EngineInterface) {
  const here = ws as Ws
  const key = `doctor:${here.root}`
  const now = await $.clock.now()
  const stored = await $.store.get(key)
  const last = isRow(stored) && typeof stored.at === 'number' ? (stored as Preview) : null
  const upgrade = last === null || now - last.at > DAY ? await khub($, ['upgrade', '--dry-run', '--format', 'json']) : null
  const found = doctorOf(upgrade?.json)

  // A preview that did not run is asked again next session.
  const fresh: Preview | null = upgrade && upgrade.exit >= 0 ? { at: now, upgrade: found.upgrade, drift: found.drift } : last

  if (fresh !== null && fresh !== last) await $.store.set(key, fresh)

  await update($, doctorAtom, () => ({ upgrade: fresh?.upgrade ?? null, drift: fresh?.drift ?? [] }))
  await show($)
}

// What a schema edit did. The band offers to rewire the agent files and to snapshot.
async function afterSchemaEdit($: EngineInterface): Promise<string[]> {
  const problem = await loadSchema($)
  const diff = problem === null ? (await khub($, ['schema', 'diff', '--format', 'json'])).json : undefined
  const validation = problem === null ? (await khub($, ['validate', '--format', 'json'])).json : undefined

  schemaLine = schemaNotice(problem, diff)

  return [...schemaNotes(problem, diff), ...validateNotes(validation), ...(await afterWrite($, null))]
}

// The schema line leaves the band once the user acted on it.
async function settleSchema($: EngineInterface) {
  schemaLine = null
  await show($)
}

// Reads the schema line again, since a snapshot or a rewire run elsewhere settles it too.
async function recheckSchema($: EngineInterface) {
  if (schemaLine === null) return

  schemaLine = schemaNotice(trouble === '' ? null : trouble, (await khub($, ['schema', 'diff', '--format', 'json'])).json)
  await show($)
}

function paneActions($: EngineInterface): PaneActions {
  const typeOfId = (id: string) => id.split('/')[0] as string

  return {
    tab: tab => background($, update($, uiAtom, ui => ({ ...ui, tab, diff: null }))),
    check: () => background($, refresh($)),
    previewReindex: () =>
      background(
        $,
        khub($, ['reindex', '--dry-run']).then(ran =>
          update($, uiAtom, ui => ({ ...ui, diff: [ran.out.trim(), ran.note].filter(Boolean).join('\n') || 'index.md is current' })),
        ),
      ),
    reindex: () =>
      background(
        $,
        khub($, ['reindex']).then(async ran => {
          if (ran.exit !== 0) return $.ui.toast(`khub reindex failed: ${firstLine(ran.note || ran.out)}`)

          await update($, uiAtom, ui => ({ ...ui, diff: null }))
          $.ui.toast('index.md rebuilt')
        }),
      ),
    closeDiff: () => background($, update($, uiAtom, ui => ({ ...ui, diff: null }))),
    fix: finding => background($, $.prompt.fill({ text: `Fix ${lineOf(finding)}` })),
    answerLenses: id => background($, $.prompt.fill({ text: `Answer the lenses for ${id}.` })),

    browseType: type => background($, browse($, type, null)),
    browseEntity: id => background($, browse($, typeOfId(id), id)),
    impact: (id, predicate) =>
      background(
        $,
        khub($, ['impact', id, '--reverse', '--predicate', predicate, '--format', 'tree']).then(ran =>
          update($, viewAtom, view => ({ ...view, tree: ran.out.trim() || firstLine(ran.note) })),
        ),
      ),
    insertId: id => background($, $.prompt.fill({ text: id, mode: 'insert' })),
    copyId: id => background($, $.ui.copy({ text: id })),
    withClaude: text => background($, $.prompt.fill({ text })),

    add: type => background($, showForm($, 'add', type, null)),
    edit: id => background($, showForm($, 'edit', typeOfId(id), id)),
    link: id => background($, showForm($, 'link', typeOfId(id), id)),
    remove: id => background($, showForm($, 'remove', typeOfId(id), id)),
    unlink: (id, predicate, target) => background($, reportWrite($, ['unlink', id, predicate, target])),
    setDraft: (id, isDraft) => background($, reportWrite($, ['edit', id, 'draft', String(isDraft)])),

    search: text => background($, search($, text)),
    statsScope: scope => background($, update($, uiAtom, ui => ({ ...ui, statsScope: scope }))),
  }
}

// A write from a pane button. A refusal shows as the problem line of Browse and Health.
async function reportWrite($: EngineInterface, argv: string[]) {
  const failure = await write($, argv)

  await update($, viewAtom, view => ({ ...view, problem: failure?.message ?? null }))
}

// Runs a khub command a button asked for. A failure is said, and `then` runs only after success.
async function act($: EngineInterface, argv: string[], then: () => Promise<void>) {
  const ran = await khub($, argv)

  if (ran.exit !== 0) return $.ui.toast(`khub ${argv.join(' ')} failed: ${firstLine(refusal(ran.json)?.message ?? (ran.note || ran.out))}`)

  await then()
}

// Adds lines the model reads after the tool result. The user never sees them.
function withNotes<R extends ToolCallResult>(ran: R, notes: string[]): R {
  return notes.length === 0 || ran.deny !== undefined
    ? ran
    : { ...ran, context: [...(ran.context ?? []), notes.join('\n')] }
}

// Guards a new entity file, then validates what an Edit or Write changed.
async function fileCall<E extends FileCall, R extends ToolCallResult>(
  $: EngineInterface,
  e: E,
  next: (e: E) => Promise<R>,
  guardNewFiles: boolean,
): Promise<R | { deny: string }> {
  if (!ws) return next(e)

  const entity = entityAt(ws, e.file_path)

  if (e.tool === 'Write' && entity && guardNewFiles && !(await $.fs.exists(e.file_path))) {
    return { deny: newFileDenial(entity) }
  }

  const ran = await next(e)

  if (ran.deny !== undefined || ran.isError === true) return ran

  if (entity) {
    const change: Change = { op: 'edit', entity, detail: FILE_EDITED, by: 'agent' }

    return withNotes(ran, await afterWrite($, change, { id: e.tool_use_id, kind: 'file' }))
  }

  if (isSchemaFile(ws, e.file_path)) return withNotes(ran, await afterSchemaEdit($))

  return ran
}

export const register: Register = (on, options) => {
  const guardNewFiles = options.guard_new_files !== false
  const agentAuthor = String(options.agent_author ?? '')

  on('session.start', async ($, e, next) => {
    const found = await detect($, e.cwd, String(options.binary ?? ''))

    if (found === 'no-binary') $.ui.status('not installed')
    if (found === 'old') $.ui.status(`needs khub ${MIN_KHUB} or newer`)

    if (found === 'ok') {
      await registerCommand($)

      // The baseline is taken before the first prompt, so no write lands inside it.
      await refresh($)

      // An unasked pane is seated by the engine on wide terminals only.
      if (options.sidebar === 'wide') background($, openPane($))

      const all = (await $.store.get('stats')) as Record<string, KhubStats> | undefined

      storedStats = all?.[(ws as Ws).root] ?? null
      writtenStats = await read($, statsAtom)
      await update($, allStatsAtom, () => storedStats)
      userName = await $.process.run(['git', 'config', 'user.name']).then(
        ran => (ran.exitCode === 0 ? ran.stdout.trim() : ''),
        () => '',
      )
      background($, examine($))
    }

    return next(e)
  })

  // A turn may follow edits made outside Claude, so the schema and health are read again.
  on('turn.start', ($, e, next) => {
    turn = e.turnId

    if (ws) background($, loadSchema($).then(() => recheckSchema($)).then(() => refresh($)))

    return next(e)
  })

  // The tally of every session is kept per workspace, and written when a turn ends.
  on('turn.complete', async ($, e, next) => {
    if (ws && e.agentId === undefined) {
      const root = ws.root
      const session = await read($, statsAtom)

      // Another session may have written since, so this turn's share joins what the store holds now.
      const held = (await $.store.get('stats')) as Record<string, KhubStats> | undefined
      const all = merged(held?.[root] ?? null, since(session, writtenStats))

      writtenStats = session
      storedStats = all
      await update($, allStatsAtom, () => all)
      await $.store.set('stats', { ...held, [root]: all })
    }

    return next(e)
  })

  on('tool.call', { tool: 'Bash' }, async ($, e, next) => {
    if (!ws) return next(e)

    const here = ws
    const denial = denyBash(e.command)

    if (denial !== undefined) return { deny: denial }

    const parsed = classify(e.command)
    const calls = callsOf(parsed).filter(call => !isElsewhere(here, call.workspace))

    if (calls.length === 0) return next(e)

    const row = memberOf(callRows, { requestId: e.tool_use_id })
    const simple = parsed.kind === 'simple' ? parsed : null

    if (simple) {
      await update($, row, (): KhubCall => ({
        verb: simple.verb,
        kind: 'simple',
        ...summarize(simple, undefined, ''),
        isRunning: true,
        ms: 0,
        outcome: 'ok',
        code: null,
      }))
    }

    // An add that names no author is stamped with the agent's.
    const command = simple ? stamped(e.command, simple, agentAuthor) : null
    const startedAt = await $.clock.now()
    const ran = await next(command === null ? e : { ...e, command })
    const ms = (await $.clock.now()) - startedAt

    // A call the user or a hook refused never ran, so its row is the engine's.
    if (ran.deny !== undefined) {
      await update($, row, () => null)

      return ran
    }

    if (!simple) {
      await update($, statsAtom, stats =>
        withCall(stats, { verb: 'compound', isWrite: false, ms, outcome: 'ok', code: null, bytes: 0, json: undefined, note: '' }),
      )

      if (!calls.some(mutates)) return ran
      if (calls.some(changesSchema)) await loadSchema($)

      return withNotes(ran, await afterWrite($, null))
    }

    // A failed gate or a refusal arrives as an error whose text is what khub printed.
    const output =
      ran.isError === true
        ? { stdout: ran.text ?? '', stderr: '' }
        : (ran.result as { stdout: string; stderr: string })
    const json = parseJson(output.stdout)
    const refused = refusal(json)
    const outcome: KhubCall['outcome'] = refused ? 'refused' : ran.isError === true ? 'gate' : 'ok'
    const code = refused?.code ?? null

    // A prose command prints no document, so its first line is the result.
    const summary = summarize(simple, json, json === undefined ? output.stdout || output.stderr : output.stderr)

    await update($, row, (): KhubCall => ({ verb: simple.verb, kind: 'simple', ...summary, isRunning: false, ms, outcome, code }))
    await update($, statsAtom, stats =>
      withCall(stats, {
        verb: simple.sub ? `${simple.verb} ${simple.sub}` : simple.verb,
        isWrite: isWrite(simple),
        ms,
        outcome,
        code,
        bytes: output.stdout.length,
        json,
        note: output.stderr,
      }),
    )
    await show($)

    if (simple.verb === 'schema' || simple.verb === 'wire') await recheckSchema($)

    return withNotes(ran, await afterCall($, simple, json, 'agent', e.tool_use_id))
  })

  on('tool.call', { tool: 'Edit' }, ($, e, next) => fileCall($, e, next, guardNewFiles))
  on('tool.call', { tool: 'Write' }, ($, e, next) => fileCall($, e, next, guardNewFiles))

  on('tool.call', { tool: 'Read' }, async ($, e, next) => {
    const ran = await next(e)
    const entity = ws ? entityAt(ws, e.file_path) : null

    if (!entity || entity.slug === '*' || ran.deny !== undefined || ran.isError === true) return ran

    await update($, statsAtom, withBypass)

    // `get --edges` lists an inverse only where the schema names one, and `neighbors --in` lists them all.
    const id = `${entity.type}/${entity.slug}`
    const inbound = await khub($, ['neighbors', id, '--in', '--format', 'json'])
    const note = inboundNote(id, inbound.json)

    return withNotes(ran, note === undefined ? [] : [note])
  })

  on('classic.PreToolUse', { tool: 'Bash' }, ($, e, next) => {
    const reason = ws ? askReason(classify(e.command)) : undefined

    return reason === undefined ? next(e) : { ask: reason }
  })

  // Under `gate: block` a turn does not finish on errors this session introduced. Health is
  // read fresh, since the last fix may not have gone through khub.
  on('classic.Stop', async ($, e, next) => {
    if (options.gate !== 'block' || !ws || e.stop_hook_active) return next(e)

    await refresh($)

    const reason = blockReason(await read($, healthAtom))

    return reason === undefined ? next(e) : { block: reason }
  })

  // `/khub` alone and `/khub <khub command>` are the mod's. Anything else under a shared
  // name goes on to the khub skill.
  // `/khub` reaches the plugin's own skill under its full name, and a skill copied into
  // the project under the bare one.
  on('command.run', { command: 'khub' }, ($, e, next) => answerCommand($, e, next, false))
  on('command.run', { command: 'khub:khub' }, ($, e, next) => answerCommand($, e, next, true))

  on('ui.render', { component: 'ToolUse' }, async ($, e, next) => {
    if (!ws || e.props.tool !== 'Bash') return next(e)

    const call = await read($, memberOf(callRows, e))

    if (call === null || (await read($, memberOf(rawRows, e)))) return next(e)

    return toolUseRow($.ui.resolve(e), call, () => background($, update($, memberOf(rawRows, e), () => true)))
  })

  on('ui.render', { component: 'ToolResult' }, async ($, e, next) => {
    if (!ws) return next(e)

    if (e.props.tool === 'Bash') {
      const call = await read($, memberOf(callRows, e))

      return call === null || (await read($, memberOf(rawRows, e))) ? next(e) : toolResultRow($.ui.resolve(e), call)
    }

    if (e.props.tool === 'Edit' || e.props.tool === 'Write') {
      const validation = await read($, memberOf(validationRows, e))

      return validation === null ? next(e) : withValidation($.ui.resolve(e), await next(e), validation)
    }

    return next(e)
  })

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    if (!ws || e.props.hasSurvey) return next(e)

    const notice = await read($, noticeAtom)

    if (notice === null) {
      const health = await read($, healthAtom)

      // Before the first check there is nothing to say yet.
      if (health.passed === null) return next(e)

      const text = statusText(health, await read($, statsAtom))

      return summaryLine($.ui.resolve(e), text, !health.passed, e.props.bodyColumns)
    }

    return bandLine($.ui.resolve(e), notice, e.props.bodyColumns, {
      review: () => background($, openPane($, 'session')),
      check: () => background($, refresh($).then(() => openPane($, 'health'))),
      hide: () => {
        dismissed = notice.parts.map(part => part.text).join(' · ')
        background($, update($, noticeAtom, () => null))
      },
      rewire: () => background($, act($, ['wire'], () => settleSchema($))),
      snapshot: () => background($, act($, ['schema', 'snapshot'], () => settleSchema($))),
    })
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) =>
    paneBody(
      $.ui.resolve(e),
      {
        columns: e.props.bodyColumns,
        rows: e.props.scroll.bodyRows,
        workspace: await read($, workspaceAtom),
        types: await read($, typesAtom),
        ui: await read($, uiAtom),
        ledger: await read($, ledgerAtom),
        health: await read($, healthAtom),
        stats: await read($, statsAtom),
        allStats: await read($, allStatsAtom),
        view: await read($, viewAtom),
        doctor: await read($, doctorAtom),
      },
      paneActions($),
    ),
  )

  on('ui.render', { component: 'Pane', requestId: FORM }, async ($, e, next) => {
    const form = await read($, formAtom)

    if (form === null) return next(e)

    return formView($.ui.resolve(e), form, ws?.types.find(type => type.name === form.type) ?? null, {
      set: (name, value) =>
        background($, update($, formAtom, current => current && { ...current, values: { ...current.values, [name]: value } })),
      force: () => background($, update($, formAtom, current => current && { ...current, isForced: true })),
      showEdges: () =>
        background(
          $,
          update($, formAtom, () => null)
            .then(() => $.ui.close({ id: FORM }))
            .then(() => (form.id === null ? undefined : browse($, form.type, form.id))),
        ),
      submit: asDraft => background($, submitForm($, asDraft)),
      cancel: () => background($, update($, formAtom, () => null).then(() => $.ui.close({ id: FORM }))),
    })
  })
}
