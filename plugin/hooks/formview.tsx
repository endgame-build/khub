import type { RenderElement } from 'claude-code'

import type { KhubForm, KhubType } from '../types'
import type { El } from './el'
import { fieldsOf, namingField } from './forms'
import type { Field } from './forms'

export type FormActions = {
  set: (name: string, value: string) => void
  submit: (asDraft: boolean) => void
  cancel: () => void

  // For a remove khub refused over inbound edges.
  force: () => void
  showEdges: () => void
}

type Option = { value: string; label: string }

const UNSET: Option = { value: '', label: '(unset)' }
const TITLES: Record<KhubForm['mode'], string> = { add: 'Add', edit: 'Edit', link: 'Link', remove: 'Remove' }

// The entities a relation may point at, across its target types. `any` takes every type fetched.
function targetsOf(form: KhubForm, to: string[]): Option[] {
  const types = to.includes('any') ? Object.keys(form.targets) : to

  return types.flatMap(type => form.targets[type] ?? [])
}

// A form as a dialog body: a title, khub's refusal when there is one, a row per field, then the buttons.
export function formView(el: El, form: KhubForm, type: KhubType | null, actions: FormActions): RenderElement {
  const { Box, Text, Button, Input, Select } = el
  const subject = form.mode === 'add' ? form.type : (form.id ?? form.type)
  const cancel = <Button key="cancel" label="Cancel" role="dismiss" onPress={() => actions.cancel()} />

  const title = (
    <Box justifyContent="space-between">
      <Text bold wrap="truncate-end">
        {TITLES[form.mode]} {subject}
      </Text>
      <Box flexShrink={0} marginLeft={1}>
        <Text dimColor>esc to cancel</Text>
      </Box>
    </Box>
  )

  // A surface with no text entry still hands over an `Input`, which draws as an empty box there.
  const hasEntry = Input !== undefined && (<Input key="probe" onSubmit={() => undefined} />).type === 'Input'

  if (!Input || !Select || !hasEntry) {
    return (
      <Box flexDirection="column">
        {title}
        <Text dimColor>Forms need text entry, which this surface does not have.</Text>
        <Box marginTop={1}>{cancel}</Box>
      </Box>
    )
  }

  const fields = type ? fieldsOf(type) : []
  const width = Math.max(10, ...fields.map(field => field.name.length + 3))
  const value = (name: string) => form.values[name] ?? ''

  // A Select over `options`, with `(unset)` first unless the field must be set.
  const pick = (name: string, options: Option[], isRequired: boolean) => {
    const offered = isRequired && options.length > 0 ? options : [UNSET, ...options]
    const held = offered.some(option => option.value === value(name)) ? value(name) : undefined

    return <Select key={`field-${name}`} options={offered} value={held} onSelect={picked => actions.set(name, picked)} />
  }

  const control = (field: Field) => {
    if (field.kind === 'choice') {
      return pick(
        field.name,
        field.options.map(option => ({ value: option, label: option })),
        field.isRequired,
      )
    }

    if (field.kind === 'relation') return pick(field.name, targetsOf(form, field.to), field.isRequired)

    // A pick joins the list, and the list stays editable as text, which is how a target leaves it.
    if (field.kind === 'relations') {
      const held = value(field.name) === '' ? [] : value(field.name).split(',')
      const offered = targetsOf(form, field.to).filter(option => !held.includes(option.value))

      return (
        <Box flexDirection="column">
          <Input
            key={`field-${field.name}`}
            value={value(field.name)}
            placeholder="pick below, or type slugs with commas"
            onInput={typed => actions.set(field.name, typed)}
            onSubmit={typed => actions.set(field.name, typed)}
          />
          {offered.length > 0 && (
            <Select
              key={`pick-${field.name}`}
              options={offered}
              onSelect={picked => actions.set(field.name, [...held, picked].join(','))}
            />
          )}
        </Box>
      )
    }

    return (
      <Input
        key={`field-${field.name}`}
        value={value(field.name)}
        onInput={typed => actions.set(field.name, typed)}
        onSubmit={typed => actions.set(field.name, typed)}
      />
    )
  }

  const row = (label: string, body: RenderElement) => (
    <Box>
      <Box width={width} flexShrink={0}>
        <Text>{label}</Text>
      </Box>
      {body}
    </Box>
  )

  const rows = () => {
    if (form.mode === 'remove') {
      return [<Text>This deletes {subject}. khub refuses while other entities point at it.</Text>]
    }

    if (form.mode === 'link') {
      const relations = (type?.relations ?? []).filter(relation => !relation.derived)
      const chosen = relations.find(relation => relation.predicate === value('predicate'))
      const predicates = relations.map(relation => ({ value: relation.predicate, label: relation.predicate }))

      return [
        row('predicate', pick('predicate', predicates, false)),
        row('target', pick('target', chosen ? targetsOf(form, chosen.to) : [], false)),
      ]
    }

    return fields.map(field => row(`${field.name}${field.isRequired ? ' *' : ''}`, control(field)))
  }

  const submitLabel =
    form.mode === 'add' ? 'Add' : form.mode === 'edit' ? 'Save' : form.mode === 'link' ? 'Link' : form.isForced ? 'Remove anyway' : 'Remove'
  const naming = type ? namingField(type) : undefined

  // khub refused the remove because other entities point at this one.
  const isHeld = form.mode === 'remove' && form.refusal === 'inbound_edge_refusal'

  return (
    <Box flexDirection="column">
      {title}
      {form.error !== null && (
        <Text color="red" wrap="wrap">
          {form.error}
        </Text>
      )}
      <Text> </Text>
      {rows()}
      {form.mode === 'add' && naming !== undefined && <Text dimColor>id is minted from {naming}</Text>}
      <Box marginTop={1} columnGap={1}>
        <Button key="submit" label={submitLabel} variant="primary" onPress={() => actions.submit(false)} />
        {form.mode === 'add' && <Button key="submit-draft" label="Add as draft" onPress={() => actions.submit(true)} />}
        {isHeld && <Button key="show-edges" label="Show edges" onPress={() => actions.showEdges()} />}
        {isHeld && !form.isForced && <Button key="force" label="Force remove" onPress={() => actions.force()} />}
        {cancel}
      </Box>
    </Box>
  )
}
