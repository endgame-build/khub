import type { RenderElement } from 'claude-code'

import { count } from '../cli'
import type { El } from '../el'
import type { PaneActions, PaneModel } from '../pane'

// Plain-word search. A hit opens in Browse.
export function searchTab({ Box, Text, Button, Input }: El, model: PaneModel, actions: PaneActions): RenderElement {
  const { hits, note } = model.view
  const field = Input && <Input key="search" placeholder="search" value={model.ui.query} onSubmit={value => actions.search(value)} />

  return (
    <Box flexDirection="column">
      {/* A surface with no text entry draws an `Input` as an empty box. */}
      {field?.type === 'Input' ? field : <Text dimColor>Search needs a text field, which this surface does not have.</Text>}
      {note !== '' && (
        <Text dimColor wrap="truncate-end">
          {note}
        </Text>
      )}
      {hits.map((hit, n) => (
        <Box flexDirection="column">
          <Box justifyContent="space-between">
            <Button key={`hit-${n}`} label={hit.id} plain onPress={() => actions.browseEntity(hit.id)} />
            <Text dimColor>{hit.note}</Text>
          </Box>
          <Text dimColor wrap="truncate-end">
            {'  '}
            {hit.title}
          </Text>
        </Box>
      ))}
      {model.ui.query !== '' && <Text dimColor>{count(hits.length, 'hit')}</Text>}
    </Box>
  )
}
