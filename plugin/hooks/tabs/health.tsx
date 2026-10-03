import type { RenderElement } from 'claude-code'

import type { KhubCounts, KhubFinding, KhubHealth } from '../../types'
import type { El } from '../el'
import type { PaneActions, PaneModel } from '../pane'

// Findings a bucket shows before `… n more`.
const SHOWN = 8

// What the `Code` element takes at most.
const CODE_LIMIT = 10_000

// The counts, as in `146 entities · 3 draft · 2 orphan · 6 stale`. Zero parts are left out and entities always show.
export function countsLine(counts: KhubCounts): string {
  const parts = [
    [counts.draft, 'draft'],
    [counts.orphan, 'orphan'],
    [counts.stale, 'stale'],
  ] as const

  return [`${counts.total} entities`, ...parts.filter(([n]) => n > 0).map(([n, word]) => `${n} ${word}`)].join(' · ')
}

type Bucket = { bucket: string; isError: boolean; rows: KhubFinding[] }

// A finding the session's first check did not have.
export const isNew = (health: KhubHealth, finding: KhubFinding) =>
  health.baseline !== null && !health.baseline.includes(finding.key)

// Findings by bucket in first-seen order, error buckets before informational ones.
export function buckets(findings: readonly KhubFinding[]): Bucket[] {
  const groups: Bucket[] = []

  for (const finding of findings) {
    const group = groups.find(candidate => candidate.bucket === finding.bucket)

    if (group) group.rows.push(finding)
    else groups.push({ bucket: finding.bucket, isError: finding.isError, rows: [finding] })
  }

  return [...groups.filter(group => group.isError), ...groups.filter(group => !group.isError)]
}

// The longest head of `source` that fits the `Code` element and ends at a line end.
export function cutAtLine(source: string, limit = CODE_LIMIT): string {
  if (source.length <= limit) return source

  const end = source.lastIndexOf('\n', limit)

  return source.slice(0, end < 0 ? limit : end)
}

// The workspace's state: the check's verdict, counts, findings by bucket, and the index preview.
export function healthTab({ Box, Text, Button, Code }: El, model: PaneModel, actions: PaneActions): RenderElement {
  const { health, ui, doctor } = model
  const needsUser = doctor.upgrade !== null || doctor.drift.length > 0
  const fresh = health.findings.filter(finding => isNew(health, finding)).length

  const verdict =
    health.passed === null ? (
      <Text dimColor>checking…</Text>
    ) : health.passed ? (
      <Text color="green">✓ passing</Text>
    ) : (
      <Text color="red">
        ✗ failing{fresh > 0 ? ` · ${fresh} new this session` : ''}
      </Text>
    )

  const row = (finding: KhubFinding) => (
    <Box>
      <Box flexShrink={0}>
        <Text>
          {'  '}
          {finding.id}
        </Text>
      </Box>
      <Box flexShrink={1} marginLeft={1}>
        <Text dimColor wrap="truncate-end">
          {finding.message}
        </Text>
      </Box>
      {isNew(health, finding) && (
        <Box flexShrink={0} marginLeft={1}>
          <Text color="yellow">new</Text>
        </Box>
      )}
      {finding.isError && (
        <Box flexShrink={0} marginLeft={1}>
          <Button
            key={`fix-${health.findings.indexOf(finding)}`}
            label="fix"
            plain
            dimColor
            onPress={() => actions.fix(finding)}
          />
        </Box>
      )}
    </Box>
  )

  return (
    <Box flexDirection="column">
      {model.view.problem !== null && (
        <Text color="red" wrap="truncate-end">
          {model.view.problem}
        </Text>
      )}
      <Box>
        <Text dimColor>CHECK </Text>
        {verdict}
      </Box>
      {health.counts && <Text wrap="truncate-end">{countsLine(health.counts)}</Text>}
      {buckets(health.findings).map(group => (
        <Box flexDirection="column">
          {group.isError ? (
            <Text color="red">
              {group.bucket}
              {'  '}
              {group.rows.length}
            </Text>
          ) : (
            <Text dimColor>
              {group.bucket}
              {'  '}
              {group.rows.length}
              {'  '}informational
            </Text>
          )}
          {group.rows.slice(0, SHOWN).map(row)}
          {group.rows.length > SHOWN && (
            <Text dimColor>
              {'  '}… {group.rows.length - SHOWN} more
            </Text>
          )}
        </Box>
      ))}
      {model.view.drafts.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text dimColor>DRAFTS</Text>
          {model.view.drafts.map((draft, n) => (
            <Box>
              <Box flexShrink={0}>
                <Text>{draft.id}</Text>
              </Box>
              <Box flexShrink={1} marginLeft={1}>
                <Text dimColor wrap="truncate-end">
                  {draft.title}
                </Text>
              </Box>
              <Box flexShrink={0} marginLeft={1} columnGap={1}>
                <Button key={`publish-${n}`} label="publish" plain onPress={() => actions.setDraft(draft.id, false)} />
                <Button key={`remove-${n}`} label="remove" plain dimColor onPress={() => actions.remove(draft.id)} />
              </Box>
            </Box>
          ))}
        </Box>
      )}
      {needsUser && (
        <Box flexDirection="column" marginTop={1}>
          <Text dimColor>WORKSPACE</Text>
          {doctor.upgrade !== null && <Text wrap="truncate-end">{doctor.upgrade}</Text>}
          {doctor.drift.map(line => (
            <Text dimColor wrap="truncate-end">
              {line}
            </Text>
          ))}
        </Box>
      )}
      <Box marginTop={1} columnGap={1}>
        <Button key="check" label="Run check" variant="primary" onPress={() => actions.check()} />
        <Button key="reindex" label="Reindex" onPress={() => actions.previewReindex()} />
      </Box>
      {ui.diff !== null && (
        <Box flexDirection="column" marginTop={1}>
          {ui.diff.startsWith('---') ? (
            <Code source={cutAtLine(ui.diff)} format="diff" />
          ) : (
            <Text dimColor wrap="truncate-end">
              {ui.diff}
            </Text>
          )}
          <Box marginTop={1} columnGap={1}>
            <Button key="reindex-apply" label="Rebuild index.md" onPress={() => actions.reindex()} />
            <Button key="diff-close" label="Close" onPress={() => actions.closeDiff()} />
          </Box>
        </Box>
      )}
    </Box>
  )
}
