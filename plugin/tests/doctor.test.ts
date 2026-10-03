import { expect, test } from 'claude-code/testing'

import { parseJson } from '../hooks/cli'
import { doctorNotice, doctorOf, doctorText } from '../hooks/doctor'
import { F } from './fixtures'
import type { Fixture } from './fixtures'

const json = (fixture: Fixture) => parseJson(fixture.stdout)
const EMPTY = { upgrade: null, drift: [] }

test('an upgrade shows only when the preset version moves', () => {
  const plan = json(F.upgrade_dry) as Record<string, unknown>

  expect(doctorOf(plan)).toEqual(EMPTY)
  expect(doctorOf({ ...plan, version_to: '0.7.0', schema_drift: ['types.adr', { op: 'added' }] })).toEqual({
    upgrade: 'preset build-hub 0.6.0 → 0.7.0',
    drift: ['types.adr', '{"op":"added"}'],
  })
})

test('a preview that did not run, or was refused, asks for nothing', () => {
  expect(doctorOf(undefined)).toEqual(EMPTY)
  expect(doctorOf(json(F.get_missing))).toEqual(EMPTY)
})

test('the band line names what is needed, and is absent when nothing is', () => {
  expect(doctorNotice(EMPTY)).toBe(null)
  expect(doctorNotice({ upgrade: 'preset build-hub 0.6.0 → 0.7.0', drift: ['a'] })).toEqual({
    kind: 'doctor',
    parts: [
      { text: 'preset build-hub 0.6.0 → 0.7.0', tone: 'plain' },
      { text: 'schema drift 1', tone: 'warn' },
    ],
    buttons: [],
  })
})

test('the doctor command prints one line per thing needed, or that nothing is', () => {
  expect(doctorText(EMPTY)).toBe('khub doctor\nnothing needed')
  expect(doctorText({ upgrade: 'preset build-hub 0.6.0 → 0.7.0', drift: ['types.adr'] })).toBe(
    ['khub doctor', 'preset build-hub 0.6.0 → 0.7.0, applied by `khub upgrade`', 'schema drift types.adr'].join('\n'),
  )
})
