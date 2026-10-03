import { callsOf, isDestructive } from './parse'
import type { Parsed } from './parse'
import type { Entity } from './workspace'

// The lock lives under `.khub/generated/locks`. Removing the directory above, or everything
// in it, removes the lock too. An upgrade journal beside it is not the lock.
const LOCKS = /\.khub\/generated(\/locks|\/\*|\/?(\s|["']|$))/

// A command word that removes, moves or empties what it names, with or without a path before it.
const REMOVES = /(^|[\s/])(rm|mv|unlink|rmdir|truncate|shred)(\s|$)|\s-delete(\s|$)/

// A redirection that truncates or appends to a path under the locks directory.
const WRITES_OVER = />\s*["']?[^\s"'>]*\.khub\/generated\/locks/

// A `cd` into the generated directory, after which a bare `rm locks` reaches the lock.
const ENTERS = /(^|\s)cd\s+["']?[^\s"']*\.khub\/generated/

const LOCK_DENIAL = [
  "khub: the lock under .khub/generated/locks is khub's own.",
  'Removing, moving or truncating it while khub may hold it breaks write serialization.',
  'Leave it in place.',
].join(' ')

// Why a command that removes or moves the workspace lock is refused, or undefined.
// Reading the lock (ls, cat, stat) passes. This is a seat belt over the common spellings,
// so a command built to dodge it will.
export function denyBash(command: string): string | undefined {
  if (!command.includes('.khub/generated')) return undefined

  const segments = command.split(/&&|\|\||[;|\n]/)
  const entered = segments.findIndex(segment => ENTERS.test(segment))
  const touches = segments.some(
    (segment, i) =>
      (LOCKS.test(segment) && (REMOVES.test(segment) || WRITES_OVER.test(segment))) ||
      (entered >= 0 && i > entered && REMOVES.test(segment)),
  )

  return touches ? LOCK_DENIAL : undefined
}

// What the agent is told when a Write would create an entity file by hand.
export function newFileDenial(entity: Entity): string {
  return [
    `khub: an entity is created with \`khub add ${entity.type} --title "…"\`, never by writing its file.`,
    "khub mints the id, seeds the body from the type's template and validates the write.",
    `Run \`khub schema show ${entity.type}\` for the fields, then edit the file khub creates.`,
  ].join(' ')
}

// The reason to ask the user before a destructive khub call, or undefined. A call inside
// a chain or a pipeline counts too.
export function askReason(parsed: Parsed): string | undefined {
  const call = callsOf(parsed).find(isDestructive)

  if (!call) return undefined

  if (call.verb === 'remove') {
    return `khub remove --force deletes ${call.positional[0] ?? 'an entity'} while other entities still point at it.`
  }

  if (call.verb === 'init') return 'khub init --force scaffolds over an existing workspace.'

  return 'khub upgrade replaces the workspace schema from the shipped preset. `khub upgrade --dry-run` previews it.'
}
