// This file handles paths and layouts of a khub workspace. The functions are pure, and the
// shell holds the workspace value.

import type { KhubType, KhubWorkspace } from '../types'

export type Entity = { type: string; slug: string }
// `isWindows` is set when the session runs on Windows. Absent, paths are unix paths.
export type Ws = KhubWorkspace & { types: KhubType[]; isWindows?: boolean }

export const dirname = (path: string) => path.replace(/\/[^/]*$/, '') || '/'

// Tells whether the session's directory is a Windows path: a drive, such as `C:\`, or a
// network share. The engine names no platform, and a unix directory always starts with `/`.
export const isWindowsPath = (cwd: string) => /^[A-Za-z]:[\\/]/.test(cwd) || /^\\\\/.test(cwd)

// The separator a Windows path is spelled with: a slash only when it holds no backslash.
const separatorOf = (path: string) => (path.includes('/') && !path.includes('\\') ? '/' : '\\')

// Returns `name` inside `dir`, with the directory's own separator.
export function childOf(dir: string, name: string, isWindows = false): string {
  return isWindows ? `${dir.replace(/[\\/]+$/, '')}${separatorOf(dir)}${name}` : `${dir}/${name}`
}

// Lists the directories from a Windows path up to its drive or share, nearest first, with
// the path's own separator. `C:\a\b` gives `C:\a\b`, `C:\a` and `C:\`.
function windowsAncestors(cwd: string): string[] {
  const sep = separatorOf(cwd)
  const top = /^(?:[A-Za-z]:|[\\/]{2}[^\\/]+[\\/][^\\/]+)/.exec(cwd)?.[0] ?? ''
  const parts = cwd
    .slice(top.length)
    .split(/[\\/]/)
    .filter(part => part !== '')
  const dirs: string[] = []

  for (let n = parts.length; n >= 0; n -= 1) dirs.push(`${top}${sep}${parts.slice(0, n).join(sep)}`)

  return dirs
}

// Lists every directory from `cwd` up to the root, nearest first.
export function ancestors(cwd: string, isWindows = false): string[] {
  if (isWindows) return windowsAncestors(cwd)

  const dirs = [cwd]

  while (dirs[dirs.length - 1] !== '/') dirs.push(dirname(dirs[dirs.length - 1] as string))

  return dirs
}

// Lists the commands to try for khub, in order: the setting, the repo's pinned install, the path.
// Windows runs neither the shell script nor the `.cmd` npm lays in `node_modules/.bin` without
// a shell, so there the repo's install is the npm package's launcher run by node, which is
// what `khub.cmd` runs.
export function binCandidates(root: string, setting: string, isWindows = false): string[][] {
  if (setting.trim()) return [setting.trim().split(/\s+/)]
  if (!isWindows) return [[`${root}/node_modules/.bin/khub`], ['khub']]

  const launcher = ['node_modules', '@endgame-build', 'khub', 'bin', 'khub.js'].reduce(
    (dir, name) => childOf(dir, name, true),
    root,
  )

  return [['node', launcher], ['khub']]
}

// Returns the file a candidate needs on disk before it is worth running, or null when it is
// looked up on the path. On Windows that is the launcher node runs.
export function installedAt(bin: readonly string[], isWindows = false): string | null {
  if (!isWindows) return (bin[0] ?? '').startsWith('/') ? (bin[0] as string) : null

  const file = bin[1] ?? ''

  return bin.length === 2 && bin[0] === 'node' && isWindowsPath(file) ? file : null
}

// Spells a Windows path for comparison: forward slashes, and a Git Bash drive such as
// `/c/Users` as `c:/Users`.
const windowsForm = (path: string) => path.replace(/\\/g, '/').replace(/^\/(?:cygdrive\/)?([A-Za-z])(?=\/|$)/, '$1:')

// Returns the path under a Windows root, with forward slashes, or null outside it. The
// root matches whatever the separators and the case, since the file system ignores case.
function relativeOnWindows(root: string, path: string): string | null {
  const base = `${windowsForm(root).replace(/\/+$/, '')}/`
  const rel = windowsForm(path)

  return rel.length > base.length && rel.slice(0, base.length).toLowerCase() === base.toLowerCase()
    ? rel.slice(base.length)
    : null
}

// Returns the path under the workspace root, or null outside it. macOS spells /tmp and /var
// two ways.
export function relative(ws: Ws, path: string): string | null {
  if (ws.isWindows === true) return relativeOnWindows(ws.root, path)

  const roots = [ws.root, ws.root.replace(/^\/private/, ''), `/private${ws.root}`]
  const root = roots.find(candidate => path.startsWith(`${candidate}/`))

  return root === undefined ? null : path.slice(root.length + 1)
}

// Returns the entity a file holds, by its type's layout. A collection file holds many, so
// its slug is `*`.
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

// The oldest khub whose output the mod reads. `search --plain` and `schema diff`, which the
// row summaries read, arrived with it.
export const MIN_KHUB = '0.27.0'

// Tells whether what `khub --version` printed names a release before `min`. Text with no
// version in it is a development build, which is taken as new enough. A pre-release of
// `min` is older.
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

// Tells whether a `-C` value names a workspace other than this one. khub resolves the
// nearest `.khub` at or above the path, so a directory inside the root is this workspace,
// and a relative value is taken as this one. On Windows an absolute value starts with a
// drive, a slash or a backslash.
export function isElsewhere(ws: Ws, workspace: string | null): boolean {
  if (workspace === null) return false

  if (ws.isWindows === true) {
    return /^(?:[A-Za-z]:)?[\\/]/.test(workspace) && relative(ws, `${workspace.replace(/[\\/]$/, '')}/.`) === null
  }

  return workspace.startsWith('/') && relative(ws, `${workspace.replace(/\/$/, '')}/.`) === null
}
