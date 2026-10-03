// Paths and layouts of a khub workspace. The functions are pure, and the shell holds the workspace value.

import type { KhubType, KhubWorkspace } from '../types'

export type Entity = { type: string; slug: string }
export type Ws = KhubWorkspace & { types: KhubType[] }

export const dirname = (path: string) => path.replace(/\/[^/]*$/, '') || '/'

// Every directory from `cwd` up to the root, nearest first.
export function ancestors(cwd: string): string[] {
  const dirs = [cwd]

  while (dirs[dirs.length - 1] !== '/') dirs.push(dirname(dirs[dirs.length - 1] as string))

  return dirs
}

// The commands to try for khub, in order: the setting, the repo's pinned install, the path.
export function binCandidates(root: string, setting: string): string[][] {
  return setting.trim() ? [setting.trim().split(/\s+/)] : [[`${root}/node_modules/.bin/khub`], ['khub']]
}

// The path under the workspace root, or null outside it. macOS spells /tmp and /var two ways.
export function relative(ws: Ws, path: string): string | null {
  const roots = [ws.root, ws.root.replace(/^\/private/, ''), `/private${ws.root}`]
  const root = roots.find(candidate => path.startsWith(`${candidate}/`))

  return root === undefined ? null : path.slice(root.length + 1)
}

// The entity a file holds, by its type's layout. A collection file holds many, so its slug is `*`.
export function entityAt(ws: Ws, path: string): Entity | null {
  const rel = relative(ws, path)

  if (rel === null) return null

  for (const type of ws.types) {
    if (type.layout === 'singleton' || type.layout === 'collection') {
      if (rel === type.path) return { type: type.name, slug: type.layout === 'singleton' ? type.name : '*' }
      continue
    }

    const ext = `.${type.format}`

    if (!rel.startsWith(`${type.path}/`) || !rel.endsWith(ext)) continue

    const parts = rel.slice(type.path.length + 1, -ext.length).split('/')

    if (type.layout === 'file' && parts.length === 1) return { type: type.name, slug: parts[0] as string }
    if (type.layout === 'folder' && parts.length === 2 && parts[1] === '_index') {
      return { type: type.name, slug: parts[0] as string }
    }
  }

  return null
}

// The oldest khub whose output the mod reads: `search --plain` and `schema diff` arrived with it.
export const MIN_KHUB = '0.27.0'

// Whether what `khub --version` printed names a release before `min`. Text with no version
// in it is a development build, which is taken as new enough. A pre-release of `min` is older.
export function isOlder(printed: string, min: string): boolean {
  const found = /(\d+)\.(\d+)\.(\d+)(-)?/.exec(printed)
  const need = min.split('.').map(Number)

  if (!found) return false

  const have = found.slice(1, 4).map(Number)

  for (let i = 0; i < 3; i += 1) {
    if (have[i] !== need[i]) return (have[i] as number) < (need[i] as number)
  }

  return found[4] !== undefined
}

export const isSchemaFile = (ws: Ws, path: string) =>
  /^\.khub\/((ontology|policy|storage)\.yaml|templates\/[^/]+\.yaml)$/.test(relative(ws, path) ?? '')

// Whether a `-C` value names a workspace other than this one. khub resolves the nearest
// `.khub` at or above the path, so a directory inside the root is this workspace, and a
// relative value is taken as this one.
export const isElsewhere = (ws: Ws, workspace: string | null) =>
  workspace !== null && workspace.startsWith('/') && relative(ws, `${workspace.replace(/\/$/, '')}/.`) === null
