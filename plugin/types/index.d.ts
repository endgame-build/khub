// The khub mod's session state. The file is self-contained, because the engine refuses a contract that imports.

// One attribute of a type, as `khub schema --format json` lists it under `fields`.
export type KhubField = {
  name: string
  type: string
  required: boolean
  enum: string[] | null
  pattern: string | null
  default: unknown
}

// One relation of a type, as `khub schema --format json` lists it under `relations`.
export type KhubRelation = {
  predicate: string
  to: string[]
  kind: string
  many: boolean
  required: boolean
  inverse: string | null
  acyclic: boolean
  derived: boolean
}

export type KhubType = {
  name: string
  layout: string
  format: string
  path: string
  id_shape: string | null
  when: string | null
  fields: KhubField[]
  relations: KhubRelation[]
}

export type KhubWorkspace = {
  root: string
  bin: string[]
  version: string
  preset: string
  presetVersion: string
}

// `khub status --format json`.
export type KhubCounts = {
  counts: Record<string, number>
  total: number
  draft: number
  active: number
  orphan: number
  stale: number
  stray: number
  malformed: number
}

// One row of a `khub check` bucket. `key` identifies it across runs.
export type KhubFinding = {
  key: string
  bucket: string
  id: string
  message: string
  isError: boolean
}

export type KhubHealth = {
  counts: KhubCounts | null
  passed: boolean | null
  findings: KhubFinding[]

  // Finding keys of the session's first check; null until it has run.
  baseline: string[] | null

  // Finding keys the model has been told about.
  noted: string[]

  // Lines for findings the model was told about that the last check no longer has.
  cleared: string[]
  checkMs: number
}

export type KhubLens = { code: string; name: string }

export type KhubLedgerEntry = {
  op: 'add' | 'edit' | 'link' | 'unlink' | 'remove'
  id: string
  detail: string
  by: 'agent' | 'user'

  // The turn that made the entry, and the turn that last touched it.
  since: string
  turn: string
  errors: number
  gaps: number
  lenses: KhubLens[]
}

export type KhubTone = 'ok' | 'warn' | 'bad' | 'plain'

// One khub call the agent ran through Bash, keyed by tool_use_id.
export type KhubCall = {
  verb: string
  kind: 'simple' | 'compound'
  head: string
  line: string
  tone: KhubTone
  isRunning: boolean
  ms: number
  outcome: 'ok' | 'gate' | 'refused'
  code: string | null
}

// The validate result of an Edit or Write on an entity file, keyed by tool_use_id.
export type KhubValidation = {
  id: string
  line: string
  tone: KhubTone
}

export type KhubTab = 'session' | 'health' | 'browse' | 'search' | 'stats'

// One row of a `khub query` or `khub search` result.
export type KhubRecord = {
  id: string
  title: string
  flags: string[]
  note: string
}

export type KhubEdge = { predicate: string; id: string; direction: 'in' | 'out' }

// One entity as Browse shows it. The body is display text and never enters a note.
export type KhubEntity = {
  id: string
  type: string
  slug: string
  path: string
  isDraft: boolean
  fields: Array<{ name: string; value: string }>
  edges: KhubEdge[]
  body: string
}

// What the pane last fetched from khub for Browse, Search and Health.
export type KhubView = {
  entities: KhubRecord[]
  entity: KhubEntity | null
  tree: string | null
  hits: KhubRecord[]
  note: string
  drafts: KhubRecord[]
  problem: string | null
}

// A form over one type. `values` holds text per field, and a many-valued relation as a comma list.
export type KhubForm = {
  mode: 'add' | 'edit' | 'link' | 'remove'
  type: string
  id: string | null
  values: Record<string, string>
  initial: Record<string, string>
  targets: Record<string, Array<{ value: string; label: string }>>
  error: string | null

  // khub's code for `error`, when it gave one.
  refusal: string | null
  isForced: boolean
}

// What the workspace needs from the user, found at session start.
export type KhubDoctor = {
  upgrade: string | null
  drift: string[]
}

export type KhubUi = {
  tab: KhubTab
  diff: string | null
  browse: { type: string | null; id: string | null }
  query: string
  statsScope: 'session' | 'all'
}

export type KhubStats = {
  calls: number
  reads: number
  writes: number
  byVerb: Record<string, number>
  refused: Record<string, number>
  searches: number
  searchEmpty: number
  searchTruncated: number
  bytes: number
  bypass: number
  durations: number[]
  slowest: { verb: string; ms: number } | null
}

export type KhubPart = { text: string; tone: KhubTone }

// The one line the band shows, as parts joined by ` · `. `kind` picks its buttons.
export type KhubNotice = {
  kind: 'findings' | 'schema' | 'doctor'
  parts: KhubPart[]

  // The actions the line offers, beside the one that hides it.
  buttons: Array<'review' | 'check' | 'rewire' | 'snapshot'>
}

declare module 'claude-code' {
  interface PluginState {
    khub: {
      workspace: KhubWorkspace | null
      types: KhubType[]
      health: KhubHealth
      ledger: KhubLedgerEntry[]
      calls: StateFamily<KhubCall | null>
      validations: StateFamily<KhubValidation | null>
      rawRows: StateFamily<boolean>
      ui: KhubUi
      stats: KhubStats
      allStats: KhubStats | null
      view: KhubView
      form: KhubForm | null
      doctor: KhubDoctor
      notice: KhubNotice | null
    }
  }
}
