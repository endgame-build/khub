// The khub mod's session state. The file is self-contained, because the engine refuses a contract that imports.

// How a type is stored, as `khub schema --format json` lists it.
export type KhubType = {
  name: string
  layout: string
  format: string
  path: string
}

export type KhubWorkspace = {
  root: string
  bin: string[]
  version: string
  preset: string
  presetVersion: string
}

// What the last refresh read from `khub query` and `khub check`.
export type KhubSummary = {
  total: number
  draft: number
  passed: boolean
  errors: number
}

// Qualified ids, `type/slug`. `baseline` is the first read of the session, null until then.
export type KhubSession = {
  baseline: string[] | null
  current: string[]
  edited: string[]
}

export type KhubTone = 'ok' | 'warn' | 'bad' | 'plain'

// One list row under a call's result line.
export type KhubRow = {
  text: string
  note: string
  flags: string[]
}

// One khub call the agent ran through Bash. A command's calls are keyed by tool_use_id,
// and the first one carries the command's duration.
export type KhubCall = {
  head: string
  line: string
  tone: KhubTone
  isRunning: boolean
  ms: number
  rows: KhubRow[]
  more: number
}

declare module 'claude-code' {
  interface PluginState {
    khub: {
      workspace: KhubWorkspace | null
      summary: KhubSummary | null
      session: KhubSession
      calls: StateFamily<KhubCall[] | null>
      rawRows: StateFamily<boolean>
    }
  }
}
