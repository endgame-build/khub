import type { RenderElement } from 'claude-code'

import type { KhubEdge, KhubEntity } from '../../types'
import type { El } from '../el'
import type { PaneActions, PaneModel } from '../pane'

// What the `Markdown` element takes at most.
const BODY_LIMIT = 10_000

// Field pairs the line under an entity's id shows.
const FIELDS_SHOWN = 4

// The types of the schema, each with how many entities it holds.
function typeList({ Box, Text, Button }: El, model: PaneModel, actions: PaneActions): RenderElement {
  const counts = model.health.counts?.counts ?? {}

  return (
    <Box flexDirection="column">
      <Text dimColor>TYPES</Text>
      {model.types.map(type => (
        <Box justifyContent="space-between">
          <Button key={`type-${type.name}`} label={type.name} plain onPress={() => actions.browseType(type.name)} />
          <Text dimColor>{String(counts[type.name] ?? 0)}</Text>
        </Box>
      ))}
    </Box>
  )
}

// The entities of one type.
function entityList({ Box, Text, Button }: El, model: PaneModel, actions: PaneActions, type: string): RenderElement {
  return (
    <Box flexDirection="column">
      <Box columnGap={1}>
        <Button key="back" label="‹ types" plain dimColor onPress={() => actions.browseType(null)} />
        <Text bold>{type}</Text>
      </Box>
      <Box columnGap={1}>
        <Button key="add" label="Add" onPress={() => actions.add(type)} />
        <Button key="add-claude" label="Add with Claude" onPress={() => actions.withClaude(`Add a ${type}: `)} />
      </Box>
      {model.view.entities.length === 0 && <Text dimColor>No entities.</Text>}
      {model.view.entities.map(record => (
        <Box>
          <Box flexShrink={0}>
            <Button key={`entity-${record.id}`} label={record.id} plain onPress={() => actions.browseEntity(record.id)} />
          </Box>
          <Box flexShrink={1} marginLeft={1}>
            <Text dimColor wrap="truncate-end">
              {record.title}
            </Text>
          </Box>
          {record.flags.length > 0 && (
            <Box flexShrink={0} marginLeft={1}>
              <Text color="yellow">{record.flags.join(' ')}</Text>
            </Box>
          )}
        </Box>
      ))}
    </Box>
  )
}

// One entity, with its fields, its edges, the actions on it and its body.
function entityDetail(el: El, model: PaneModel, actions: PaneActions, entity: KhubEntity): RenderElement {
  const { Box, Text, Button, Markdown } = el
  const fields = entity.fields.slice(0, FIELDS_SHOWN).map(field => `${field.name} ${field.value}`)
  const outbound = entity.edges.filter(edge => edge.direction === 'out')
  const inbound = entity.edges.filter(edge => edge.direction === 'in')

  const row = (edge: KhubEdge, n: number, isFirstOfPredicate: boolean) => (
    <Box columnGap={1}>
      <Box flexShrink={0}>
        <Text>{edge.predicate}</Text>
      </Box>
      <Button key={`edge-${n}`} label={edge.id} plain onPress={() => actions.browseEntity(edge.id)} />
      {edge.direction === 'out' && (
        <Button key={`unlink-${n}`} label="×" plain dimColor onPress={() => actions.unlink(entity.id, edge.predicate, edge.id)} />
      )}
      {edge.direction === 'in' && isFirstOfPredicate && (
        <Button
          key={`impact-${edge.predicate}`}
          label="impact"
          plain
          dimColor
          onPress={() => actions.impact(entity.id, edge.predicate)}
        />
      )}
    </Box>
  )

  return (
    <Box flexDirection="column">
      <Box columnGap={1}>
        <Button key="back" label={`‹ ${entity.type}`} plain dimColor onPress={() => actions.browseType(entity.type)} />
        <Text bold wrap="truncate-end">
          {entity.id}
        </Text>
        {entity.isDraft && <Text color="yellow">draft</Text>}
      </Box>
      {fields.length > 0 && (
        <Text dimColor wrap="truncate-end">
          {fields.join(' · ')}
        </Text>
      )}
      {outbound.length > 0 && <Text dimColor>EDGES OUT</Text>}
      {outbound.map((edge, i) => row(edge, i, false))}
      {inbound.length > 0 && <Text dimColor>EDGES IN</Text>}
      {inbound.map((edge, i) =>
        row(edge, outbound.length + i, inbound.findIndex(other => other.predicate === edge.predicate) === i),
      )}
      {model.view.tree !== null && <Text dimColor>{model.view.tree}</Text>}
      <Box marginTop={1} columnGap={1} flexWrap="wrap">
        <Button key="edit" label="Edit" onPress={() => actions.edit(entity.id)} />
        <Button key="link" label="Link" onPress={() => actions.link(entity.id)} />
        <Button
          key="draft"
          label={entity.isDraft ? 'Publish' : 'Mark as draft'}
          onPress={() => actions.setDraft(entity.id, !entity.isDraft)}
        />
        <Button key="remove" label="Remove" onPress={() => actions.remove(entity.id)} />
      </Box>
      <Box columnGap={1} flexWrap="wrap">
        <Button key="edit-claude" label="Edit with Claude" onPress={() => actions.withClaude(`Edit ${entity.id}: `)} />
        <Button key="body-claude" label="Write body" onPress={() => actions.withClaude(`Write the body of ${entity.id}: `)} />
        <Button key="insert" label="Insert id" onPress={() => actions.insertId(entity.id)} />
        <Button key="copy" label="Copy id" onPress={() => actions.copyId(entity.id)} />
      </Box>
      {entity.body !== '' && (
        <Box marginTop={1}>
          <Markdown text={entity.body.slice(0, BODY_LIMIT)} />
        </Box>
      )}
    </Box>
  )
}

// The graph by hand. The type list leads to a type's entities, and those to one entity.
export function browseTab(el: El, model: PaneModel, actions: PaneActions): RenderElement {
  const { Box, Text } = el
  const { type, id } = model.ui.browse
  const entity = model.view.entity

  const level =
    id !== null ? (
      entity !== null && entity.id === id ? (
        entityDetail(el, model, actions, entity)
      ) : (
        <Text dimColor>loading…</Text>
      )
    ) : type !== null ? (
      entityList(el, model, actions, type)
    ) : (
      typeList(el, model, actions)
    )

  return (
    <Box flexDirection="column">
      {model.view.problem !== null && (
        <Text color="red" wrap="truncate-end">
          {model.view.problem}
        </Text>
      )}
      {level}
    </Box>
  )
}
