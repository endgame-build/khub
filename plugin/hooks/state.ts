// The empty values of the session state.

import type { KhubDoctor, KhubHealth, KhubStats, KhubUi, KhubView } from '../types'

export const EMPTY_HEALTH: KhubHealth = {
  counts: null,
  passed: null,
  findings: [],
  baseline: null,
  noted: [],
  cleared: [],
  checkMs: 0,
}

export const EMPTY_STATS: KhubStats = {
  calls: 0,
  reads: 0,
  writes: 0,
  byVerb: {},
  refused: {},
  searches: 0,
  searchEmpty: 0,
  searchTruncated: 0,
  bytes: 0,
  bypass: 0,
  durations: [],
  slowest: null,
}

export const EMPTY_UI: KhubUi = {
  tab: 'session',
  diff: null,
  browse: { type: null, id: null },
  query: '',
  statsScope: 'session',
}

export const EMPTY_VIEW: KhubView = {
  entities: [],
  entity: null,
  tree: null,
  hits: [],
  note: '',
  drafts: [],
  problem: null,
}

export const EMPTY_DOCTOR: KhubDoctor = { upgrade: null, drift: [] }
