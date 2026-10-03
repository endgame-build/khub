import type { RenderElement } from 'claude-code'

import type {
  KhubDoctor,
  KhubFinding,
  KhubHealth,
  KhubLedgerEntry,
  KhubStats,
  KhubTab,
  KhubType,
  KhubUi,
  KhubView,
  KhubWorkspace,
} from '../types'
import type { El } from './el'
import { browseTab } from './tabs/browse'
import { healthTab } from './tabs/health'
import { searchTab } from './tabs/search'
import { sessionTab } from './tabs/session'
import { statsTab } from './tabs/stats'

export type PaneModel = {
  columns: number
  rows: number
  workspace: KhubWorkspace | null
  types: KhubType[]
  ui: KhubUi
  ledger: KhubLedgerEntry[]
  health: KhubHealth
  stats: KhubStats
  allStats: KhubStats | null
  view: KhubView
  doctor: KhubDoctor
}

export type PaneActions = {
  tab: (tab: KhubTab) => void
  check: () => void
  previewReindex: () => void
  reindex: () => void
  closeDiff: () => void
  fix: (finding: KhubFinding) => void
  answerLenses: (id: string) => void

  // Browse: null is the type list, a type is its entities, an id is one entity.
  browseType: (type: string | null) => void
  browseEntity: (id: string) => void
  impact: (id: string, predicate: string) => void
  insertId: (id: string) => void
  copyId: (id: string) => void
  withClaude: (text: string) => void

  // Writes. Each opens a form or a confirmation, or runs one khub call.
  add: (type: string) => void
  edit: (id: string) => void
  link: (id: string) => void
  unlink: (id: string, predicate: string, target: string) => void
  setDraft: (id: string, isDraft: boolean) => void
  remove: (id: string) => void

  search: (text: string) => void
  statsScope: (scope: 'session' | 'all') => void
}

export type TabView = (el: El, model: PaneModel, actions: PaneActions) => RenderElement

// The tabs in row order.
export const TABS: ReadonlyArray<{ tab: KhubTab; label: string; hotkey: string; view: TabView }> = [
  { tab: 'session', label: 'Session', hotkey: '1', view: sessionTab },
  { tab: 'health', label: 'Health', hotkey: '2', view: healthTab },
  { tab: 'browse', label: 'Browse', hotkey: '3', view: browseTab },
  { tab: 'search', label: 'Search', hotkey: '4', view: searchTab },
  { tab: 'stats', label: 'Stats', hotkey: '5', view: statsTab },
]

// The pane is a tab row, then the active tab's body. A Button takes no bold or inverse,
// so the active tab is a label and the other four are the buttons.
export function paneBody(el: El, model: PaneModel, actions: PaneActions): RenderElement {
  const { Box, Text, Button } = el
  const view = TABS.find(({ tab }) => tab === model.ui.tab)?.view ?? sessionTab

  return (
    <Box flexDirection="column">
      <Box flexWrap="wrap" columnGap={1}>
        {TABS.map(({ tab, label, hotkey }) =>
          tab === model.ui.tab ? (
            <Text bold inverse>{` ${label} `}</Text>
          ) : (
            <Button key={`tab-${tab}`} label={label} hotkey={hotkey} plain onPress={() => actions.tab(tab)} />
          ),
        )}
      </Box>
      <Text> </Text>
      {view(el, model, actions)}
    </Box>
  )
}
