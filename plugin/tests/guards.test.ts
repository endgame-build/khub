import { expect, test } from 'claude-code/testing'

import { askReason, denyBash, newFileDenial } from '../hooks/guards'
import { classify } from '../hooks/parse'

const LOCK = '.khub/generated/locks/workspace.lock'
const REMOVE = 'khub remove --force deletes cmp-search while other entities still point at it.'
const UPGRADE = 'khub upgrade replaces the workspace schema from the shipped preset. `khub upgrade --dry-run` previews it.'

test('removing, moving or truncating the workspace lock is refused', () => {
  for (const command of [
    `rm ${LOCK}`,
    `rm -rf ws/.khub/generated/locks`,
    `rm -rf .khub/generated`,
    `rm -rf "ws/.khub/generated/"`,
    `rm -rf .khub/generated/*`,
    `cd ws && mv ${LOCK} /tmp/old.lock`,
    `cd .khub/generated && rm -rf locks`,
    `cd "ws/.khub/generated"; ls; rm -rf locks`,
    `unlink "${LOCK}"`,
    `rmdir .khub/generated/locks`,
    `truncate -s 0 ${LOCK}`,
    `/bin/rm ${LOCK}`,
    `find .khub/generated/locks -type f -delete`,
    `find .khub/generated/locks -exec rm {} \;`,
    `: > ${LOCK}`,
    `echo x >> "${LOCK}"`,
  ]) {
    expect(denyBash(command)).toBe(
      [
        "khub: the lock under .khub/generated/locks is khub's own.",
        'Removing, moving or truncating it while khub may hold it breaks write serialization.',
        'Leave it in place.',
      ].join(' '),
    )
  }
})

test('reading the lock, or removing something else, passes', () => {
  for (const command of [
    `ls -la .khub/generated/locks`,
    `cat ${LOCK}`,
    `stat ${LOCK} > /tmp/lock.txt`,
    `rm -rf .khub/generated/upgrade-123`,
    `rm notes.md && ls .khub/generated/locks`,
    `cd .khub/generated && ls -la`,
    `rm -rf node_modules`,
    `khub check --format json`,
  ]) {
    expect(denyBash(command)).toBe(undefined)
  }
})

test('the denial of a hand-made entity file names the commands to use', () => {
  expect(newFileDenial({ type: 'adr', slug: 'ad-2026-01-15-by-hand' })).toBe(
    [
      'khub: an entity is created with `khub add adr --title "…"`, never by writing its file.',
      "khub mints the id, seeds the body from the type's template and validates the write.",
      'Run `khub schema show adr` for the fields, then edit the file khub creates.',
    ].join(' '),
  )
})

test('a destructive khub call gets a reason to ask, others none', () => {
  expect(askReason(classify('khub remove cmp-search --force'))).toBe(REMOVE)
  expect(askReason(classify('khub remove cmp-search --force=true'))).toBe(REMOVE)
  expect(askReason(classify('khub init build-hub . --force'))).toBe(
    'khub init --force scaffolds over an existing workspace.',
  )
  expect(askReason(classify('npx @endgame-build/khub upgrade'))).toBe(UPGRADE)

  expect(askReason(classify('khub remove cmp-search'))).toBe(undefined)
  expect(askReason(classify('khub remove cmp-search --force=false'))).toBe(undefined)
  expect(askReason(classify('khub upgrade --dry-run'))).toBe(undefined)
  expect(askReason(classify('khub add adr --title x && khub check'))).toBe(undefined)
  expect(askReason(classify('ls'))).toBe(undefined)
})

test('a destructive call inside a chain or a pipeline asks too', () => {
  expect(askReason(classify('khub remove cmp-search --force && khub check'))).toBe(REMOVE)
  expect(askReason(classify('khub check; khub remove cmp-search --force'))).toBe(REMOVE)
  expect(askReason(classify('khub remove cmp-search --force 2>&1 | head'))).toBe(REMOVE)
  expect(askReason(classify('khub remove cmp-search --force | tee out.txt'))).toBe(REMOVE)
  expect(askReason(classify('khub status && khub upgrade > upgrade.log'))).toBe(UPGRADE)
})

test('a destructive call in a command read off the text asks, without the id', () => {
  expect(askReason(classify('(cd ws && khub remove cmp-search --force)'))).toBe(
    'khub remove --force deletes an entity while other entities still point at it.',
  )
  expect(askReason(classify('(khub upgrade)'))).toBe(UPGRADE)
  expect(askReason(classify('(khub upgrade --dry-run)'))).toBe(undefined)
  expect(askReason(classify('(khub remove cmp-search)'))).toBe(undefined)
})
