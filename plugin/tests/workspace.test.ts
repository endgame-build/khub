import { expect, test } from 'claude-code/testing'

import type { KhubType } from '../types'
import { binCandidates, dirname, entityAt, isElsewhere, isOlder, isWindowsPath, relative, UNIX, WINDOWS } from '../hooks/workspace'
import type { Ws } from '../hooks/workspace'

const type = (name: string, layout: string, format: string, path: string): KhubType => ({ name, layout, format, path })

// One type per layout, and a second file type in another format.
const TYPES = [
  type('adr', 'file', 'md', 'knowledge/decisions'),
  type('project', 'folder', 'md', 'projects'),
  type('prd', 'singleton', 'md', 'knowledge/prd.md'),
  type('metric', 'collection', 'yaml', 'metrics.yaml'),
  type('spec', 'file', 'json', 'specs'),
]

const at = (root: string): Ws => ({ root, bin: ['khub'], version: '0.27.0', preset: 'test', presetVersion: '1.0.0', types: TYPES, flavour: UNIX })
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
  expect(UNIX.ancestors('/a/b/c')).toEqual(['/a/b/c', '/a/b', '/a', '/'])
  expect(UNIX.ancestors('/')).toEqual(['/'])
  expect(dirname('/a/b')).toBe('/a')
  expect(dirname('/a')).toBe('/')
})

test('khub is looked for in the setting, else in the repo, else on the path', () => {
  expect(binCandidates('/ws', '')).toEqual([['/ws/node_modules/.bin/khub'], ['khub']])
  expect(binCandidates('/ws', '   ')).toEqual([['/ws/node_modules/.bin/khub'], ['khub']])
  expect(binCandidates('/ws', ' npx  @endgame-build/khub ')).toEqual([['npx', '@endgame-build/khub']])
  expect(binCandidates('/ws', 'node /x/khub.js')).toEqual([['node', '/x/khub.js']])
  expect(binCandidates('/ws', "'/opt/my tools/khub' --verbose")).toEqual([['/opt/my tools/khub', '--verbose']])
})

test('a quoted setting keeps a Windows path with spaces, and its backslashes, in one word', () => {
  expect(binCandidates('C:\\ws', '"C:\\Program Files\\khub\\khub.exe" --flag', WINDOWS)).toEqual([
    ['C:\\Program Files\\khub\\khub.exe', '--flag'],
  ])
  expect(binCandidates('C:\\ws', 'node "C:\\my tools\\khub.js"', WINDOWS)).toEqual([['node', 'C:\\my tools\\khub.js']])
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

// A Windows workspace, as Claude Code names its directory there.
const WIN = { ...at('C:\\Work\\me\\ws'), flavour: WINDOWS }

test('a Windows directory is told from a unix one by its spelling', () => {
  expect(isWindowsPath('C:\\Work\\me\\ws')).toBe(true)
  expect(isWindowsPath('d:/work/ws')).toBe(true)
  expect(isWindowsPath('\\\\server\\share\\ws')).toBe(true)
  expect(isWindowsPath('/ws')).toBe(false)
  expect(isWindowsPath('/c/Work/me/ws')).toBe(false)
  expect(isWindowsPath('C:relative')).toBe(false)
})

test('the directories above a Windows path run up to its drive or share, in its own separator', () => {
  expect(WINDOWS.ancestors('C:\\Work\\me\\ws')).toEqual(['C:\\Work\\me\\ws', 'C:\\Work\\me', 'C:\\Work', 'C:\\'])
  expect(WINDOWS.ancestors('C:\\Work\\me\\')).toEqual(['C:\\Work\\me', 'C:\\Work', 'C:\\'])
  expect(WINDOWS.ancestors('C:\\')).toEqual(['C:\\'])
  expect(WINDOWS.ancestors('D:/work/ws')).toEqual(['D:/work/ws', 'D:/work', 'D:/'])
  expect(WINDOWS.ancestors('\\\\server\\share\\ws')).toEqual(['\\\\server\\share\\ws', '\\\\server\\share\\'])
  expect(WINDOWS.childOf('C:\\Work\\me\\ws', '.khub')).toBe('C:\\Work\\me\\ws\\.khub')
  expect(WINDOWS.childOf('C:\\', '.khub')).toBe('C:\\.khub')
  expect(WINDOWS.childOf('D:/work', '.khub')).toBe('D:/work/.khub')
  expect(UNIX.childOf('/ws', '.khub')).toBe('/ws/.khub')
  expect(UNIX.childOf('/', '.khub')).toBe('//.khub')
})

test('on Windows khub is looked for in the setting, else in the npm launcher run by node, else on the path', () => {
  const launcher = 'C:\\ws\\node_modules\\@endgame-build\\khub\\bin\\khub.js'

  expect(binCandidates('C:\\ws', '', WINDOWS)).toEqual([['node', launcher], ['khub'], ['cmd', '/c', 'khub']])
  expect(binCandidates('C:\\ws', 'C:\\tools\\khub.exe', WINDOWS)).toEqual([['C:\\tools\\khub.exe']])
  expect(WINDOWS.installedAt(['node', launcher])).toBe(launcher)
  expect(WINDOWS.installedAt(['khub'])).toBe(null)
  expect(WINDOWS.installedAt(['C:\\tools\\khub.exe'])).toBe(null)
  expect(WINDOWS.installedAt(['cmd', '/c', 'khub'])).toBe(null)

  // cmd would read a `&` or a `%` in the root it is handed, so such a root never goes through it.
  expect(binCandidates('C:\\A&B\\ws', '', WINDOWS).at(-1)).toEqual(['khub'])
  expect(binCandidates('C:\\100%\\ws', '', WINDOWS).at(-1)).toEqual(['khub'])

  // Unix is unchanged: an absolute program must exist, and a bare name is looked up.
  expect(UNIX.installedAt(['/ws/node_modules/.bin/khub'])).toBe('/ws/node_modules/.bin/khub')
  expect(UNIX.installedAt(['khub'])).toBe(null)
  expect(UNIX.installedAt(['npx', '@endgame-build/khub'])).toBe(null)
})

test('a Windows file maps to its entity whatever its separators and the case of its root', () => {
  const adr = { type: 'adr', slug: 'ad-1' }

  for (const path of [
    'C:\\Work\\me\\ws\\knowledge\\decisions\\ad-1.md',
    'C:/Work/me/ws/knowledge/decisions/ad-1.md',
    'c:\\work\\ME\\ws\\knowledge\\decisions\\ad-1.md',
    'C:\\Work\\me\\ws/knowledge/decisions/ad-1.md',
    '/c/Work/me/ws/knowledge/decisions/ad-1.md',
  ]) {
    expect(entityAt(WIN, path)).toEqual(adr)
  }

  expect(entityAt(WIN, 'C:\\Work\\me\\ws\\projects\\acme\\_index.md')).toEqual({ type: 'project', slug: 'acme' })
  expect(entityAt(WIN, 'C:\\Work\\me\\ws\\metrics.yaml')).toEqual({ type: 'metric', slug: '*' })
  expect(relative(WIN, 'C:\\Work\\me\\ws\\.khub\\ontology.yaml')).toBe('.khub/ontology.yaml')
  expect(relative(WIN, 'C:\\Work\\me\\ws')).toBe(null)
  expect(relative(WIN, 'C:\\Work\\me\\ws2\\x.md')).toBe(null)
  expect(relative(WIN, 'D:\\Work\\me\\ws\\x.md')).toBe(null)
  expect(relative({ ...WIN, root: 'C:\\' }, 'C:\\notes\\x.md')).toBe('notes/x.md')

  // A unix workspace keeps its exact spelling: a backslash is part of a name there.
  expect(relative(WS, '/WS/knowledge/prd.md')).toBe(null)
  expect(relative(WS, '/ws\\knowledge\\prd.md')).toBe(null)
})

test('a Windows -C value is elsewhere only when it is another absolute path', () => {
  for (const value of ['C:\\Work\\me\\ws', 'c:/work/me/ws/', '/c/Work/me/ws', 'C:\\Work\\me\\ws\\nested', '.', './ws', 'C:ws']) {
    expect(isElsewhere(WIN, value)).toBe(false)
  }

  for (const value of ['C:\\Work\\me\\other', 'D:\\Work\\me\\ws', '/d/Work/me/ws', '/tmp/ws', '\\\\server\\share']) {
    expect(isElsewhere(WIN, value)).toBe(true)
  }
})
