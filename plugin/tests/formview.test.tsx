import { expect, test } from 'claude-code/testing'
import type { Engine } from 'claude-code/testing'
import type { On, RenderSurface } from 'claude-code'

import type { KhubEntity, KhubForm, KhubType } from '../types'
import { parseJson } from '../hooks/cli'
import { fieldsOf, namingField, openForm } from '../hooks/forms'
import { formView } from '../hooks/formview'
import type { FormActions } from '../hooks/formview'
import { F } from './fixtures'
import { fakeHost } from './helpers'

const SURFACES = ['terminal', 'desktop'] as const
const TYPES = (parseJson(F.schema.stdout) as { types: KhubType[] }).types
const stored = (type: KhubType) => type.relations.filter(relation => !relation.derived)

// A type with an enum, a single-valued relation and a many-valued one, whatever the preset calls it.
const TYPE = TYPES.find(
  type =>
    type.fields.some(field => field.enum !== null) &&
    stored(type).some(relation => !relation.many && !relation.to.includes('any')) &&
    stored(type).some(relation => relation.many),
) as KhubType
const NAMING = namingField(TYPE) as string
const CHOICE = TYPE.fields.find(field => field.enum !== null) as KhubType['fields'][number]
const ONE = stored(TYPE).find(relation => !relation.many && !relation.to.includes('any')) as KhubType['relations'][number]
const MANY = stored(TYPE).find(relation => relation.many) as KhubType['relations'][number]
const TARGETS = { [ONE.to[0] as string]: [{ value: 'target-a', label: 'x/target-a · Target A' }] }

const ENTITY: KhubEntity = {
  id: `${TYPE.name}/thing`,
  type: TYPE.name,
  slug: 'thing',
  path: 'thing.md',
  isDraft: false,
  fields: [{ name: NAMING, value: 'Thing' }],
  edges: [],
  body: '',
}

const pane = {
  title: 'form',
  isFocused: true,
  bodyColumns: 60,
  placement: 'inline' as const,
  scroll: { offset: 0, bodyRows: 30 },
  view: {},
}

// Draws the form from the test's own hook, beneath the mod, so the surface validates the
// tree and each act reaches the actions. Returns what the acts ran.
function stage(on: On, form: KhubForm, type: KhubType | null = TYPE) {
  const ran: unknown[][] = []
  const actions: FormActions = {
    set: (name, value) => void ran.push(['set', name, value]),
    submit: asDraft => void ran.push(['submit', asDraft]),
    cancel: () => void ran.push(['cancel']),
    force: () => void ran.push(['force']),
    showEdges: () => void ran.push(['showEdges']),
  }

  fakeHost(on)
  on('ui.render', ($, e) => formView($.ui.resolve(e), form, type, actions))

  return ran
}

const mount = <P extends RenderSurface>($: Engine, surface: P) =>
  $.ui.mount({ plugin: 'test', surface, component: 'Pane', requestId: 'form', props: pane })

test('an add form has a control per field, the minting line and its buttons', async ($, on) => {
  stage(on, openForm('add', TYPE, null, TARGETS))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)
    const fields = fieldsOf(TYPE)
    const controls = [...(await ui.findAll({ type: 'Input' })), ...(await ui.findAll({ type: 'Select' }))]

    expect(await ui.find({ type: 'Text', text: new RegExp(`^Add ${TYPE.name}$`) })).toBeDefined()
    expect(await ui.find({ type: 'Text', text: /^esc to cancel$/ })).toBeDefined()

    // A many-valued relation with targets to offer has a second control, which appends a pick.
    const picks = fields.filter(field => field.kind === 'relations' && field.to.some(to => to === 'any' || TARGETS[to] !== undefined))

    expect(controls.map(found => found.key).sort()).toEqual(
      [...fields.map(field => `field-${field.name}`), ...picks.map(field => `pick-${field.name}`)].sort(),
    )
    expect(await ui.find({ type: 'Text', text: new RegExp(`^id is minted from ${NAMING}$`) })).toBeDefined()
    expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual(['submit', 'submit-draft', 'cancel'])
    expect((await ui.find({ key: 'submit' }))?.props).toMatchObject({ label: 'Add', variant: 'primary' })
    expect((await ui.find({ key: 'cancel' }))?.props).toMatchObject({ role: 'dismiss' })
    await ui.unmount()
  }
})

test('a choice offers its values and unset only when it may stay empty', async ($, on) => {
  stage(on, openForm('add', TYPE, null, TARGETS))

  const ui = await mount($, 'terminal')
  const choice = (await ui.find({ key: `field-${CHOICE.name}` }))?.props.options as Array<{ value: string }>
  const relation = (await ui.find({ key: `field-${ONE.predicate}` }))?.props.options
  const values = choice.map(option => option.value)

  expect(values).toEqual(CHOICE.required ? CHOICE.enum : ['', ...(CHOICE.enum as string[])])
  expect(relation).toEqual(
    ONE.required
      ? TARGETS[ONE.to[0] as string]
      : [{ value: '', label: '(unset)' }, ...(TARGETS[ONE.to[0] as string] as object[])],
  )
  expect((await ui.find({ key: `field-${MANY.predicate}` }))?.props.placeholder).toBe('pick below, or type slugs with commas')
})

test('a pick joins a many-valued relation\'s list and leaves the offer', async ($, on) => {
  const options = [
    { value: 'a', label: 'x/a · A' },
    { value: 'b', label: 'x/b · B' },
  ]

  // The relation takes `any` or its first type, and both read this one list.
  const targets = { [MANY.to[0] === 'any' ? 'x' : (MANY.to[0] as string)]: options }
  const ran = stage(on, { ...openForm('add', TYPE, null, targets), values: { [MANY.predicate]: 'a' } })
  const ui = await mount($, 'terminal')
  const key = `pick-${MANY.predicate}`

  expect((await ui.find({ key }))?.props.options).toEqual([options[1]])

  await ui.select({ key, value: 'b' })
  expect(ran).toEqual([['set', MANY.predicate, 'a,b']])
})

test('typing, picking and pressing reach the actions', async ($, on) => {
  const ran = stage(on, openForm('add', TYPE, null, TARGETS))

  for (const surface of SURFACES) {
    const ui = await mount($, surface)

    ran.length = 0
    await ui.input({ key: `field-${NAMING}`, text: 'Index' })
    await ui.select({ key: `field-${CHOICE.name}`, value: (CHOICE.enum as string[])[0] as string })
    await ui.select({ key: `field-${ONE.predicate}`, value: 'target-a' })
    await ui.press({ key: 'submit' })
    await ui.press({ key: 'submit-draft' })
    await ui.press({ key: 'cancel' })

    expect(ran).toEqual([
      ['set', NAMING, 'Index'],
      ['set', CHOICE.name, (CHOICE.enum as string[])[0]],
      ['set', ONE.predicate, 'target-a'],
      ['submit', false],
      ['submit', true],
      ['cancel'],
    ])
    await ui.unmount()
  }
})

test('an edit form holds the entity\'s values, shows khub\'s refusal and saves', async ($, on) => {
  const form = { ...openForm('edit', TYPE, ENTITY, TARGETS), error: "'x' is not a valid value" }

  stage(on, form)

  const ui = await mount($, 'terminal')

  expect(await ui.find({ type: 'Text', text: new RegExp(`^Edit ${ENTITY.id}$`) })).toBeDefined()
  expect((await ui.find({ key: `field-${NAMING}` }))?.props.value).toBe('Thing')
  expect((await ui.find({ type: 'Text', text: /is not a valid value/ }))?.props.color).toBe('red')
  expect((await ui.findAll({ type: 'Button' })).map(button => [button.key, button.props.label])).toEqual([
    ['submit', 'Save'],
    ['cancel', 'Cancel'],
  ])
  expect(await ui.find({ type: 'Text', text: /id is minted/ })).toBe(undefined)
})

test('a link form picks a predicate, then a target among that relation\'s types', async ($, on) => {
  const ran = stage(on, { ...openForm('link', TYPE, ENTITY, TARGETS), values: { predicate: ONE.predicate } })
  const ui = await mount($, 'terminal')
  const predicates = (await ui.find({ key: 'field-predicate' }))?.props.options as Array<{ value: string }>

  expect(predicates.map(option => option.value)).toEqual(['', ...stored(TYPE).map(relation => relation.predicate)])
  expect((await ui.find({ key: 'field-predicate' }))?.props.value).toBe(ONE.predicate)
  expect((await ui.find({ key: 'field-target' }))?.props.options).toEqual([
    { value: '', label: '(unset)' },
    ...(TARGETS[ONE.to[0] as string] as object[]),
  ])

  await ui.select({ key: 'field-target', value: 'target-a' })
  await ui.press({ key: 'submit' })

  expect(ran).toEqual([['set', 'target', 'target-a'], ['submit', false]])
  expect((await ui.find({ key: 'submit' }))?.props.label).toBe('Link')
})

test('a remove form offers force only after khub refused, and says so once forced', async ($, on) => {
  const plain = openForm('remove', TYPE, ENTITY, {})
  const refused = {
    ...plain,
    error: "Refusing to remove: 2 inbound edges resolve to it. Pass --force to override",
    refusal: 'inbound_edge_refusal',
  }

  // Any other failure offers no force.
  const failed = { ...plain, error: 'khub did not run', refusal: '' }
  const forms: Record<string, KhubForm> = { plain, refused, failed, forced: { ...refused, isForced: true } }
  const ran: unknown[][] = []

  fakeHost(on)
  on('ui.render', ($, e) =>
    formView($.ui.resolve(e), forms[e.requestId] as KhubForm, TYPE, {
      set: (name, value) => void ran.push(['set', name, value]),
      submit: asDraft => void ran.push(['submit', asDraft]),
      cancel: () => void ran.push(['cancel']),
      force: () => void ran.push(['force']),
      showEdges: () => void ran.push(['showEdges']),
    }),
  )

  const keys = async (requestId: string) => {
    const ui = await $.ui.mount({ plugin: 'test', surface: 'terminal', component: 'Pane', requestId, props: pane })

    return { ui, buttons: (await ui.findAll({ type: 'Button' })).map(button => [button.key, button.props.label]) }
  }

  expect((await keys('plain')).buttons).toEqual([['submit', 'Remove'], ['cancel', 'Cancel']])

  const second = await keys('refused')

  expect(second.buttons).toEqual([['submit', 'Remove'], ['show-edges', 'Show edges'], ['force', 'Force remove'], ['cancel', 'Cancel']])
  expect((await keys('failed')).buttons).toEqual([['submit', 'Remove'], ['cancel', 'Cancel']])
  await second.ui.press({ key: 'force' })
  expect(ran).toEqual([['force']])
  expect((await keys('forced')).buttons).toEqual([['submit', 'Remove anyway'], ['show-edges', 'Show edges'], ['cancel', 'Cancel']])
})

test('a surface with no text entry gets one line and Cancel', async ($, on) => {
  const ran = stage(on, openForm('add', TYPE, null, TARGETS))
  const ui = await mount($, 'mobile')

  expect(await ui.find({ type: 'Text', text: /^Forms need text entry/ })).toBeDefined()
  expect((await ui.findAll({ type: 'Button' })).map(button => button.key)).toEqual(['cancel'])

  await ui.press({ key: 'cancel' })
  expect(ran).toEqual([['cancel']])
})
