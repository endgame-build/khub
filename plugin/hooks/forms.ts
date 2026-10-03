// The model of a schema-generated form: its fields, its starting values and the khub call it makes.

import type { KhubEntity, KhubForm, KhubType } from '../types'

export type Field = {
  name: string
  kind: 'text' | 'choice' | 'relation' | 'relations'
  options: string[]
  isRequired: boolean

  // The types a relation may point at. `any` means every type.
  to: string[]
}

// Base attributes khub sets itself, or that the form sets another way. The two submit
// buttons decide `draft`, and the shell passes `author`.
const HIDDEN = new Set(['type', 'created', 'updated', 'draft', 'author'])

// Base attributes every type carries. They follow the type's own.
const BASE = ['description', 'resource', 'tags', 'aliases']

// The field khub mints the id from, `name` before `title`.
export function namingField(type: KhubType): string | undefined {
  return ['name', 'title'].find(name => type.fields.some(field => field.name === name))
}

// The fields a form shows for a type, the naming field first. Fields khub sets itself stay out.
export function fieldsOf(type: KhubType): Field[] {
  const naming = namingField(type)
  const shown = type.fields.filter(field => !HIDDEN.has(field.name))
  const rank = (name: string) => (name === naming ? 0 : BASE.includes(name) ? 2 : 1)
  const ordered = [0, 1, 2].flatMap(tier => shown.filter(field => rank(field.name) === tier))

  const attributes = ordered.map((field): Field => {
    const options = field.enum ?? (field.type === 'bool' ? ['true', 'false'] : [])

    return {
      name: field.name,
      kind: options.length > 0 ? 'choice' : 'text',
      options,
      isRequired: field.required,
      to: [],
    }
  })

  const relations = type.relations
    .filter(relation => !relation.derived)
    .map(
      (relation): Field => ({
        name: relation.predicate,
        kind: relation.many ? 'relations' : 'relation',
        options: [],
        isRequired: relation.required,
        to: relation.to,
      }),
    )

  return [...attributes, ...relations]
}

// The types whose entities a form over `type` offers as relation targets.
export function targetTypes(type: KhubType, all: KhubType[]): string[] {
  const named = type.relations
    .filter(relation => !relation.derived)
    .flatMap(relation => relation.to.flatMap(to => (to === 'any' ? all.map(each => each.name) : [to])))

  return [...new Set(named)]
}

export function openForm(
  mode: KhubForm['mode'],
  type: KhubType,
  entity: KhubEntity | null,
  targets: KhubForm['targets'],
  id: string | null = entity?.id ?? null,
): KhubForm {
  const values: Record<string, string> = {}

  if (mode === 'edit' && entity) {
    for (const field of fieldsOf(type)) {
      const held = entity.fields.find(candidate => candidate.name === field.name)?.value

      // A list reads as comma text with no spaces, which is how khub takes it back.
      if (held !== undefined) values[field.name] = field.kind === 'relations' ? held.replace(/,\s+/g, ',') : held
    }
  }

  return {
    mode,
    type: type.name,
    id,
    values,
    initial: { ...values },
    targets,
    error: null,
    refusal: null,
    isForced: false,
  }
}

// The khub arguments a form submits, or null when it would change nothing. Each value is
// one argument and never passes through a shell.
export function argvOf(form: KhubForm, type: KhubType, author: string, asDraft: boolean): string[] | null {
  const id = form.id ?? ''
  const value = (name: string) => form.values[name] ?? ''

  // A value that starts with a dash rides in the flag's own argument, where khub cannot read it as a flag.
  const flag = (name: string, held: string) => (held.startsWith('-') ? [`--${name}=${held}`] : [`--${name}`, held])

  if (form.mode === 'remove') return ['remove', id, ...(form.isForced ? ['--force'] : [])]

  if (form.mode === 'link') {
    return value('predicate') === '' || value('target') === '' ? null : ['link', id, value('predicate'), value('target')]
  }

  const names = fieldsOf(type).map(field => field.name)

  if (form.mode === 'edit') {
    const emptied = new Set(unlinksOf(form, type).map(unlink => unlink[2]))
    const changed = names.filter(name => value(name) !== (form.initial[name] ?? '') && !emptied.has(name))

    return changed.length === 0 ? null : ['edit', id, ...changed.flatMap(name => flag(name, value(name)))]
  }

  return [
    'add',
    type.name,
    ...names.filter(name => value(name) !== '').flatMap(name => flag(name, value(name))),
    ...(author === '' ? [] : flag('author', author)),
    ...(asDraft ? ['--draft'] : []),
  ]
}

// The unlink calls for each relation an edit form emptied. khub refuses an empty relation
// value on edit, and a shortened list goes through edit as the new list.
export function unlinksOf(form: KhubForm, type: KhubType): string[][] {
  if (form.mode !== 'edit') return []

  return fieldsOf(type)
    .filter(field => field.kind !== 'text' && field.kind !== 'choice')
    .filter(field => (form.values[field.name] ?? '') === '' && (form.initial[field.name] ?? '') !== '')
    .flatMap(field => (form.initial[field.name] ?? '').split(',').map(target => ['unlink', form.id ?? '', field.name, target]))
}
