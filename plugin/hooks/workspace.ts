// This file handles paths and layouts of a khub workspace. The functions are pure, and the
// shell holds the workspace value.

import type { KhubType, KhubWorkspace } from '../types'

export type Entity = { type: string; slug: string }

// What the session's platform changes. The shell picks one from the shape of the session's
// directory, once, and hands it to the functions here and in parse.ts.
export type Flavour = {
  // Lists every directory from `cwd` up to the root, nearest first.
  ancestors: (cwd: string) => string[]

  // Returns `name` inside `dir`, with the directory's own separator.
  childOf: (dir: string, name: string) => string

  // Returns the path under `root`, or null outside it.
  under: (root: string, path: string) => string | null

  isAbsolute: (path: string) => boolean

  // Lists the commands to try for khub when the setting names none.
  installs: (root: string) => string[][]

  // Returns the file a candidate needs on disk before it is worth running, or null when it
  // is looked up on the path.
  installedAt: (bin: readonly string[]) => string | null

  // The binary's file name, and a path separator, as regular expression sources.
  name: string
  separator: string
}

export type Ws = KhubWorkspace & { types: KhubType[]; flavour: Flavour }

export const dirname = (path: string) => path.replace(/\/[^/]*$/, '') || '/'

// Tells whether the session's directory is a Windows path: a drive, such as `C:\`, or a
// network share. The engine names no platform, and a unix directory always starts with `/`.
export const isWindowsPath = (cwd: string) => /^[A-Za-z]:[\\/]/.test(cwd) || /^\\\\/.test(cwd)

// The separator a Windows path is spelled with: a slash only when it holds no backslash.
const separatorOf = (path: string) => (path.includes('/') && !path.includes('\\') ? '/' : '\\')

const windowsChildOf = (dir: string, name: string) => `${dir.replace(/[\\/]+$/, '')}${separatorOf(dir)}${name}`

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

function unixAncestors(cwd: string): string[] {
  const dirs = [cwd]

  while (dirs[dirs.length - 1] !== '/') dirs.push(dirname(dirs[dirs.length - 1] as string))

  return dirs
}

// cmd may take these characters in a command line as its own.
const CMD_SPECIAL = /[%!^&|<>"()]/

// Windows runs neither the shell script nor the `.cmd` npm lays in `node_modules/.bin`
// without a shell, so there the repo's install is the npm package's launcher run by node,
// which is what `khub.cmd` runs. The last candidate reaches npm's global `khub.cmd` through
// cmd. The shell passes khub fixed words and the workspace root, so that candidate is left
// out for a root cmd would read.
function windowsInstalls(root: string): string[][] {
  const launcher = ['node_modules', '@endgame-build', 'khub', 'bin', 'khub.js'].reduce(windowsChildOf, root)

  return [['node', launcher], ['khub'], ...(CMD_SPECIAL.test(root) ? [] : [['cmd', '/c', 'khub']])]
}

// Spells a Windows path for comparison: forward slashes, and a Git Bash drive such as
// `/c/Users` as `c:/Users`.
const windowsForm = (path: string) => path.replace(/\\/g, '/').replace(/^\/(?:cygdrive\/)?([A-Za-z])(?=\/|$)/, '$1:')

// Returns the path under a Windows root, with forward slashes. The root matches whatever
// the separators and the case, since the file system ignores case.
function windowsUnder(root: string, path: string): string | null {
  const base = `${windowsForm(root).replace(/\/+$/, '')}/`
  const rel = windowsForm(path)

  return rel.length > base.length && rel.slice(0, base.length).toLowerCase() === base.toLowerCase()
    ? rel.slice(base.length)
    : null
}

// macOS spells /tmp and /var two ways.
function unixUnder(root: string, path: string): string | null {
  const roots = [root, root.replace(/^\/private/, ''), `/private${root}`]
  const found = roots.find(candidate => path.startsWith(`${candidate}/`))

  return found === undefined ? null : path.slice(found.length + 1)
}

// A unix session takes paths as spelled, and the binary is `khub` alone.
export const UNIX: Flavour = {
  ancestors: unixAncestors,
  childOf: (dir, name) => `${dir}/${name}`,
  under: unixUnder,
  isAbsolute: path => path.startsWith('/'),
  installs: root => [[`${root}/node_modules/.bin/khub`], ['khub']],
  installedAt: bin => ((bin[0] ?? '').startsWith('/') ? (bin[0] as string) : null),
  name: 'khub',
  separator: '/',
}

// Git Bash runs a Windows session's commands, so a path may take either separator and a
// Git Bash drive, and the binary may carry its extension and any case. A verb is always
// lowercase, so the name spells its case out rather than ignoring it.
export const WINDOWS: Flavour = {
  ancestors: windowsAncestors,
  childOf: windowsChildOf,
  under: windowsUnder,
  isAbsolute: path => /^(?:[A-Za-z]:)?[\\/]/.test(path),
  installs: windowsInstalls,
  installedAt: bin => (bin.length === 2 && bin[0] === 'node' && isWindowsPath(bin[1] ?? '') ? (bin[1] as string) : null),
  name: '[Kk][Hh][Uu][Bb](?:\\.[Ee][Xx][Ee]|\\.[Cc][Mm][Dd])?',
  separator: '[\\\\/]',
}

export const flavourOf = (cwd: string): Flavour => (isWindowsPath(cwd) ? WINDOWS : UNIX)

// Splits the `binary` setting into words at whitespace. A double or single quote that opens
// a word and has its closing quote keeps a path with spaces in one word. Any other quote is
// a plain character, as is a backslash, so `C:\Users\O'Brien\khub.exe` stays one word. An
// empty word (`''`) is dropped, so a setting of only empty words is an empty setting.
export function settingWords(setting: string): string[] {
  const words = [...setting.matchAll(/(["'])([\s\S]*?)\1(\S*)|\S+/g)].map(found =>
    found[1] === undefined ? found[0] : `${found[2]}${found[3]}`,
  )

  return words.filter(word => word !== '')
}

// Lists the commands to try for khub, in order: the setting, the repo's pinned install, the path.
export function binCandidates(root: string, setting: string, flavour: Flavour): string[][] {
  const words = settingWords(setting)

  return words.length > 0 ? [words] : flavour.installs(root)
}

// Returns the path under the workspace root, or null outside it.
export const relative = (ws: Ws, path: string): string | null => ws.flavour.under(ws.root, path)

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
// and a relative value is taken as this one.
export function isElsewhere(ws: Ws, workspace: string | null): boolean {
  if (workspace === null || !ws.flavour.isAbsolute(workspace)) return false

  return relative(ws, `${workspace.replace(new RegExp(`${ws.flavour.separator}$`), '')}/.`) === null
}
