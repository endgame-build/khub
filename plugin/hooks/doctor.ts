// What the workspace needs from the user, read from khub's upgrade preview.

import type { KhubDoctor, KhubNotice, KhubPart } from '../types'
import { isRow, text } from './cli'
import { EMPTY_DOCTOR } from './state'


// `upgrade` is `khub upgrade --dry-run --format json`, undefined when it did not run.
export function doctorOf(upgrade: unknown): KhubDoctor {
  const plan = isRow(upgrade) && upgrade.error === undefined ? upgrade : {}
  const from = text(plan.version_from)
  const to = text(plan.version_to)

  return {
    ...EMPTY_DOCTOR,
    upgrade: from !== '' && to !== '' && from !== to ? `preset ${text(plan.preset)} ${from} → ${to}` : null,
    drift: Array.isArray(plan.schema_drift)
      ? plan.schema_drift.map(entry => (typeof entry === 'string' ? entry : JSON.stringify(entry)))
      : [],
  }
}

// What `/khub doctor` prints.
export function doctorText(doctor: KhubDoctor): string {
  const lines = [
    ...(doctor.upgrade === null ? [] : [`${doctor.upgrade}, applied by \`khub upgrade\``]),
    ...doctor.drift.map(entry => `schema drift ${entry}`),
  ]

  return ['khub doctor', ...(lines.length > 0 ? lines : ['nothing needed'])].join('\n')
}

// The band line for it, or null when nothing is needed.
export function doctorNotice(doctor: KhubDoctor): KhubNotice | null {
  const parts: KhubPart[] = [
    ...(doctor.upgrade === null ? [] : [{ text: doctor.upgrade, tone: 'plain' } as const]),
    ...(doctor.drift.length > 0 ? [{ text: `schema drift ${doctor.drift.length}`, tone: 'warn' } as const] : []),
  ]

  return parts.length > 0 ? { kind: 'doctor', parts, buttons: [] } : null
}
