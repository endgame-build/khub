import { expect, test } from 'claude-code/testing'

import type { KhubEntity, KhubType } from '../types'
import { parseJson } from '../hooks/cli'
import { argvOf, fieldsOf, namingField, openForm, targetTypes, unlinksOf } from '../hooks/forms'
import { F } from './fixtures'

const TYPES = (parseJson(F.schema.stdout) as { types: KhubType[] }).types
const stored = (type: KhubType) => type.relations.filter(relation => !relation.derived)

// Types picked by what they declare, so another preset's schema passes the same tests.
const WITH_ENUM = TYPES.find(type => type.fields.some(field => field.enum !== null)) as KhubType
const WITH_MANY = TYPES.find(type => stored(type).some(relation => relation.many && !relation.to.includes('any'))) as KhubType
const WITH_ONE = TYPES.find(type => stored(type).some(relation => !relation.many)) as KhubType

const entity = (type: KhubType, fields: KhubEntity['fields']): KhubEntity => ({
  id: `${type.name}/x`,
  type: type.name,
  slug: 'x',
  path: 'x.md',
  isDraft: false,
  fields,
  edges: [],
  body: '',
})

test('every type gets a form with the naming field first and khub\'s own fields left out', () => {
  for (const type of TYPES) {
    const fields = fieldsOf(type)
    const names = fields.map(field => field.name)

    expect(names[0]).toBe(namingField(type))
    expect(names.filter(name => ['type', 'created', 'updated', 'draft', 'author'].includes(name))).toEqual([])
    expect(new Set(names).size).toBe(names.length)
    expect(names.filter(name => stored(type).some(relation => relation.predicate === name)).length).toBe(stored(type).length)
  }
})

test('the base attributes follow the type\'s own, and relations come last', () => {
  const names = fieldsOf(WITH_ENUM).map(field => field.name)
  const own = WITH_ENUM.fields.find(field => field.enum !== null)?.name as string
  const firstRelation = names.findIndex(name => stored(WITH_ENUM).some(relation => relation.predicate === name))

  expect(names.indexOf(own)).toBeLessThan(names.indexOf('description'))
  expect(names.indexOf('description')).toBeLessThan(names.indexOf('aliases'))

  if (firstRelation >= 0) expect(names.indexOf('aliases')).toBeLessThan(firstRelation)
})

test('an enum is a choice over its values, a relation is single or many with its targets', () => {
  const declared = WITH_ENUM.fields.find(field => field.enum !== null)
  const choice = fieldsOf(WITH_ENUM).find(field => field.name === declared?.name)
  const many = stored(WITH_MANY).find(relation => relation.many && !relation.to.includes('any'))
  const one = stored(WITH_ONE).find(relation => !relation.many)

  expect(choice).toEqual({
    name: declared?.name,
    kind: 'choice',
    options: declared?.enum,
    isRequired: declared?.required,
    to: [],
  })
  expect(fieldsOf(WITH_MANY).find(field => field.name === many?.predicate)).toEqual({
    name: many?.predicate,
    kind: 'relations',
    options: [],
    isRequired: many?.required,
    to: many?.to,
  })
  expect(fieldsOf(WITH_ONE).find(field => field.name === one?.predicate)?.kind).toBe('relation')
  expect(fieldsOf(WITH_ENUM).find(field => field.name === 'tags')?.kind).toBe('text')
})

test('a bool attribute is a choice between true and false, and a derived relation is no field', () => {
  const type: KhubType = {
    name: 'thing',
    layout: 'file',
    format: 'md',
    path: 'things',
    id_shape: null,
    when: null,
    fields: [
      { name: 'type', type: 'text', required: true, enum: null, pattern: null, default: null },
      { name: 'title', type: 'text', required: true, enum: null, pattern: null, default: null },
      { name: 'active', type: 'bool', required: false, enum: null, pattern: null, default: true },
    ],
    relations: [
      { predicate: 'owner', to: ['person'], kind: 'typed', many: false, required: true, inverse: null, acyclic: false, derived: false },
      { predicate: 'owned', to: ['thing'], kind: 'typed', many: true, required: false, inverse: null, acyclic: false, derived: true },
    ],
  }

  expect(fieldsOf(type).map(field => [field.name, field.kind, field.options])).toEqual([
    ['title', 'text', []],
    ['active', 'choice', ['true', 'false']],
    ['owner', 'relation', []],
  ])
  expect(targetTypes(type, TYPES)).toEqual(['person'])
})

test('the target types of a form are distinct, with any expanded to every type', () => {
  const all = TYPES.map(type => type.name)

  for (const type of TYPES) {
    const targets = targetTypes(type, TYPES)

    expect(new Set(targets).size).toBe(targets.length)
    expect(targets.includes('any')).toBe(false)
    expect(targets.every(target => all.includes(target))).toBe(true)
  }

  // Every type carries the base `any` relations, so every type is a target.
  expect([...targetTypes(WITH_ENUM, TYPES)].sort()).toEqual([...all].sort())
})

test('an add form starts empty and an edit form starts from the entity', () => {
  const targets = { thing: [{ value: 'x', label: 'thing/x · X' }] }
  const many = stored(WITH_MANY).find(relation => relation.many)?.predicate as string
  const naming = namingField(WITH_MANY) as string
  const held = entity(WITH_MANY, [
    { name: 'type', value: WITH_MANY.name },
    { name: naming, value: 'Search' },
    { name: many, value: 'a, b' },
  ])

  expect(openForm('add', WITH_MANY, null, targets)).toEqual({
    mode: 'add',
    type: WITH_MANY.name,
    id: null,
    values: {},
    initial: {},
    targets,
    error: null,
    refusal: null,
    isForced: false,
  })

  const edit = openForm('edit', WITH_MANY, held, targets)

  expect(edit.id).toBe(`${WITH_MANY.name}/x`)
  expect(edit.values).toEqual({ [naming]: 'Search', [many]: 'a,b' })
  expect(edit.initial).toEqual(edit.values)
  expect(openForm('remove', WITH_MANY, held, {}).values).toEqual({})
  expect(openForm('link', WITH_MANY, held, {}).id).toBe(`${WITH_MANY.name}/x`)
})

test('an add form submits every filled field, the author and the draft flag', () => {
  const naming = namingField(WITH_ENUM) as string
  const choice = WITH_ENUM.fields.find(field => field.enum !== null) as KhubType['fields'][number]
  const form = { ...openForm('add', WITH_ENUM, null, {}), values: { [naming]: 'Use RE2 patterns', [choice.name]: 'x y', tags: '' } }

  expect(argvOf(form, WITH_ENUM, 'the-user', false)).toEqual([
    'add',
    WITH_ENUM.name,
    `--${naming}`,
    'Use RE2 patterns',
    `--${choice.name}`,
    'x y',
    '--author',
    'the-user',
  ])
  expect(argvOf(form, WITH_ENUM, '', true)?.slice(-1)).toEqual(['--draft'])
  expect(argvOf(form, WITH_ENUM, '', true)?.includes('--author')).toBe(false)
})

test('an edit form submits only what changed, an emptied field as an empty value', () => {
  const naming = namingField(WITH_ENUM) as string
  const held = entity(WITH_ENUM, [
    { name: naming, value: 'Search' },
    { name: 'description', value: 'old' },
  ])
  const form = openForm('edit', WITH_ENUM, held, {})

  expect(argvOf(form, WITH_ENUM, 'the-user', false)).toBe(null)
  expect(argvOf({ ...form, values: { ...form.values, description: '' } }, WITH_ENUM, 'the-user', false)).toEqual([
    'edit',
    `${WITH_ENUM.name}/x`,
    '--description',
    '',
  ])
  expect(argvOf({ ...form, values: { ...form.values, [naming]: 'Find', tags: 'a,b' } }, WITH_ENUM, '', true)).toEqual([
    'edit',
    `${WITH_ENUM.name}/x`,
    `--${naming}`,
    'Find',
    '--tags',
    'a,b',
  ])
})

test('a link form waits for both picks, and a remove form forces only when told', () => {
  const held = entity(WITH_ONE, [])
  const link = openForm('link', WITH_ONE, held, {})
  const remove = openForm('remove', WITH_ONE, held, {})

  expect(argvOf(link, WITH_ONE, '', false)).toBe(null)
  expect(argvOf({ ...link, values: { predicate: 'depends_on' } }, WITH_ONE, '', false)).toBe(null)
  expect(argvOf({ ...link, values: { predicate: 'depends_on', target: 'y' } }, WITH_ONE, 'the-user', false)).toEqual([
    'link',
    `${WITH_ONE.name}/x`,
    'depends_on',
    'y',
  ])
  expect(argvOf(remove, WITH_ONE, 'the-user', false)).toEqual(['remove', `${WITH_ONE.name}/x`])
  expect(argvOf({ ...remove, isForced: true }, WITH_ONE, '', false)).toEqual(['remove', `${WITH_ONE.name}/x`, '--force'])
})

test('an emptied relation leaves the edit and becomes one unlink per target', () => {
  const naming = namingField(WITH_MANY) as string
  const many = stored(WITH_MANY).find(relation => relation.many)?.predicate as string
  const held = entity(WITH_MANY, [
    { name: naming, value: 'Search' },
    { name: many, value: 'a, b' },
  ])
  const form = openForm('edit', WITH_MANY, held, {})
  const emptied = { ...form, values: { ...form.values, [many]: '' } }
  const id = `${WITH_MANY.name}/x`

  expect(unlinksOf(form, WITH_MANY)).toEqual([])
  expect(argvOf(emptied, WITH_MANY, '', false)).toBe(null)
  expect(unlinksOf(emptied, WITH_MANY)).toEqual([
    ['unlink', id, many, 'a'],
    ['unlink', id, many, 'b'],
  ])

  // A shortened list is the new list.
  expect(argvOf({ ...form, values: { ...form.values, [many]: 'a' } }, WITH_MANY, '', false)).toEqual(['edit', id, `--${many}`, 'a'])
})

test('a value that starts with a dash rides in the flag\'s own argument', () => {
  const naming = namingField(WITH_ENUM) as string
  const form = { ...openForm('add', WITH_ENUM, null, {}), values: { [naming]: '--draft' } }

  expect(argvOf(form, WITH_ENUM, '', false)).toEqual(['add', WITH_ENUM.name, `--${naming}=--draft`])
})
