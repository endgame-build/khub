import type { RenderElement } from 'claude-code'

import type { KhubLedgerEntry } from '../../types'
import { count as plural } from '../cli'
import type { El } from '../el'
import type { PaneActions, PaneModel } from '../pane'

// What the ledger holds, as in `· 2 added · 1 edge`, with zero parts left out.
export function ledgerCounts(ledger: readonly KhubLedgerEntry[]): string {
  const count = (...ops: Array<KhubLedgerEntry['op']>) => ledger.filter(entry => ops.includes(entry.op)).length
  const parts = [
    [count('add'), 'added'],
    [count('edit'), 'edited'],
    [count('link', 'unlink'), count('link', 'unlink') === 1 ? 'edge' : 'edges'],
    [count('remove'), 'removed'],
  ] as const

  return parts
    .filter(([n]) => n > 0)
    .map(([n, word]) => ` · ${n} ${word}`)
    .join('')
}

// What the session wrote, one row per ledger entry, then the lenses of the entities it touched.
export function sessionTab({ Box, Text, Button }: El, model: PaneModel, actions: PaneActions): RenderElement {
  if (model.ledger.length === 0) return <Text dimColor>No khub writes this session yet.</Text>

  const result = (entry: KhubLedgerEntry) =>
    entry.errors > 0 ? (
      <Text color="red">{plural(entry.errors, 'error')}</Text>
    ) : entry.gaps > 0 ? (
      <Text color="yellow">{plural(entry.gaps, 'gap')}</Text>
    ) : (
      <Text color="green">✓</Text>
    )

  const row = (entry: KhubLedgerEntry) => {
    if (entry.op === 'add' || entry.op === 'edit') {
      return (
        <Box justifyContent="space-between">
          <Box flexShrink={1}>
            {entry.op === 'add' ? <Text color="green">+ </Text> : <Text>~ </Text>}
            <Text wrap="truncate-end">{entry.id}</Text>
          </Box>
          <Box flexShrink={0} marginLeft={1}>
            {result(entry)}
          </Box>
        </Box>
      )
    }

    if (entry.op === 'remove') return <Text wrap="truncate-end">− {entry.id}</Text>

    return (
      <Text wrap="truncate-end" dimColor={entry.op === 'unlink'}>
        {entry.op === 'link' ? '→' : '⇢'} {entry.id} {entry.detail}
      </Text>
    )
  }

  return (
    <Box flexDirection="column">
      <Text dimColor>THIS SESSION{ledgerCounts(model.ledger)}</Text>
      {model.ledger.map(row)}
      {model.ledger
        .filter(entry => entry.lenses.length > 0)
        .map(entry => (
          <Box flexDirection="column" marginTop={1}>
            <Text dimColor wrap="truncate-end">
              LENSES {entry.id}
            </Text>
            {entry.lenses.map(lens => (
              <Box>
                <Box flexShrink={0} marginRight={2}>
                  <Text>{lens.code}</Text>
                </Box>
                <Box flexShrink={1}>
                  <Text dimColor wrap="truncate-end">
                    {lens.name}
                  </Text>
                </Box>
              </Box>
            ))}
            <Box>
              <Button
                key={`lenses-${entry.id}`}
                label="Ask Claude to answer"
                onPress={() => actions.answerLenses(entry.id)}
              />
            </Box>
          </Box>
        ))}
    </Box>
  )
}
