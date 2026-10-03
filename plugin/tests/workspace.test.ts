import { expect, test } from 'claude-code/testing'

import type { KhubType } from '../types'
import { ancestors, binCandidates, dirname, entityAt, isElsewhere, isOlder, isSchemaFile, relative } from '../hooks/workspace'
import type { Ws } from '../hooks/workspace'

const type = (name: string, layout: string, format: string, path: string): KhubType => ({
  name,
  layout,
  format,
  path,
  id_shape: null,
  when: null,
  fields: [],
  relations: [],
})

// One type per layout, and a second file type in another format.
const TYPES = [
  type('adr', 'file', 'md', 'knowledge/decisions'),
  type('project', 'folder', 'md', 'projects'),
  type('prd', 'singleton', 'md', 'knowledge/prd.md'),
  type('metric', 'collection', 'yaml', 'metrics.yaml'),
  type('spec', 'file', 'json', 'specs'),
]

const at = (root: string): Ws => ({ root, bin: ['khub'], version: '0.27.0', preset: 'test', presetVersion: '1.0.0', types: TYPES })
const WS = at('/ws')

test('a file maps to its entity through the layout of its type', () => {
  expect(entityAt(WS, '/ws/knowledge/decisions/ad-2026-01-15-use-re2-patterns.md')).toEqual({
    type: 'adr',
    slug: 'ad-2026-01-15-use-re2-patterns',
  })
  expect(entityAt(WS, '/ws/projects/acme-diagnostic/_index.md')).toEqual({ type: 'project', slug: 'acme-diagnostic' })
  expect(entityAt(WS, '/ws/knowledge/prd.md')).toEqual({ type: 'prd', slug: 'prd' })
  expect(entityAt(WS, '/ws/metrics.yaml')).toEqual({ type: 'metric', slug: '*' })
  expect(entityAt(WS, '/ws/specs/s-1.json')).toEqual({ type: 'spec', slug: 's-1' })
})

test('a file that fits no layout is no entity', () => {
  for (const path of [
    '/elsewhere/knowledge/decisions/ad-1.md',
    '/ws2/knowledge/decisions/ad-1.md',
    '/ws/knowledge/decisions/drafts/ad-1.md',
    '/ws/knowledge/decisions/ad-1.yaml',
    '/ws/knowledge/decisions',
    '/ws/projects/acme-diagnostic.md',
    '/ws/projects/acme-diagnostic/notes.md',
    '/ws/projects/acme-diagnostic/sub/_index.md',
    '/ws/specs/s-1.md',
    '/ws/knowledge/arc42.md',
    '/ws/README.md',
    '/ws',
  ]) {
    expect(entityAt(WS, path)).toBe(null)
  }
})

test('the two spellings of a temporary path name the same workspace', () => {
  const entity = { type: 'adr', slug: 'ad-1' }

  expect(entityAt(at('/tmp/ws'), '/private/tmp/ws/knowledge/decisions/ad-1.md')).toEqual(entity)
  expect(entityAt(at('/private/tmp/ws'), '/tmp/ws/knowledge/decisions/ad-1.md')).toEqual(entity)
  expect(relative(at('/private/tmp/ws'), '/tmp/ws/index.md')).toBe('index.md')
})

test('a path is relative to the root, or outside it', () => {
  expect(relative(WS, '/ws/knowledge/prd.md')).toBe('knowledge/prd.md')
  expect(relative(WS, '/ws')).toBe(null)
  expect(relative(WS, '/elsewhere/x.md')).toBe(null)
})

test('the schema files are the three layers and the templates', () => {
  for (const name of ['ontology.yaml', 'policy.yaml', 'storage.yaml', 'templates/adr.yaml']) {
    expect(isSchemaFile(WS, `/ws/.khub/${name}`)).toBe(true)
  }

  for (const path of [
    '/ws/.khub/config.yaml',
    '/ws/.khub/schema.applied.yaml',
    '/ws/.khub/templates/nested/adr.yaml',
    '/ws/.khub/templates/adr.yaml.bak',
    '/ws/knowledge/prd.md',
    '/elsewhere/.khub/ontology.yaml',
  ]) {
    expect(isSchemaFile(WS, path)).toBe(false)
  }
})

test('a -C value is elsewhere only when it is another absolute path', () => {
  expect(isElsewhere(WS, null)).toBe(false)
  expect(isElsewhere(WS, './ws')).toBe(false)
  expect(isElsewhere(WS, '.')).toBe(false)
  expect(isElsewhere(WS, '/ws')).toBe(false)
  expect(isElsewhere(WS, '/ws/')).toBe(false)
  expect(isElsewhere(WS, '/private/ws')).toBe(false)
  expect(isElsewhere(at('/private/tmp/ws'), '/tmp/ws')).toBe(false)
  expect(isElsewhere(WS, '/other')).toBe(true)

  // khub resolves the nearest `.khub` at or above a `-C` path, so a directory inside is this workspace.
  expect(isElsewhere(WS, '/ws/nested')).toBe(false)
  expect(isElsewhere(WS, '/wsx')).toBe(true)
})

test('the directories above a path run up to the root', () => {
  expect(ancestors('/a/b/c')).toEqual(['/a/b/c', '/a/b', '/a', '/'])
  expect(ancestors('/')).toEqual(['/'])
  expect(dirname('/a/b')).toBe('/a')
  expect(dirname('/a')).toBe('/')
})

test('khub is looked for in the setting, else in the repo, else on the path', () => {
  expect(binCandidates('/ws', '')).toEqual([['/ws/node_modules/.bin/khub'], ['khub']])
  expect(binCandidates('/ws', '   ')).toEqual([['/ws/node_modules/.bin/khub'], ['khub']])
  expect(binCandidates('/ws', ' npx  @endgame-build/khub ')).toEqual([['npx', '@endgame-build/khub']])
})

test('a version before the one the mod reads is older, and a build with no version is not', () => {
  expect(isOlder('0.26.3', '0.27.0')).toBe(true)
  expect(isOlder('khub version 0.9.12', '0.27.0')).toBe(true)
  expect(isOlder('0.27.0', '0.27.0')).toBe(false)
  expect(isOlder('0.27.1', '0.27.0')).toBe(false)
  expect(isOlder('1.0.0', '0.27.0')).toBe(false)
  expect(isOlder('dev', '0.27.0')).toBe(false)
  expect(isOlder('0.27.0-rc1', '0.27.0')).toBe(true)
  expect(isOlder('v0.27.1-rc1', '0.27.0')).toBe(false)
})
