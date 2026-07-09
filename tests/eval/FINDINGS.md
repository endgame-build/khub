# Wiring eval — findings (khub 0.6.0)

Run: 26-task firm-ops narrative × {wired, unwired} × 2 reps + LLM judge = 104
real headless `claude -p` operations against always-latest khub. Every prompt is
vague and khub-unaware; the question is whether a wired agent reaches for the CLI
on its own.

## Result

| Condition | Adherence | Completion |
|-----------|-----------|------------|
| wired     | 100% (52/52) | 90% |
| unwired   | 6% (3/52)    | 27% |

Two distinct axes, kept separate on purpose:

- **Adherence** — the agent drove khub (`add`/`edit`/`link`/`get`/`query`) rather
  than writing a raw entity file or grepping. This is the wiring question.
- **Completion** — the exact intended entity or edge landed, schema-valid.

Wired adherence is 100% across create, operate, and read, in both reps, with zero
raw entity-file writes and zero grep-instead-of-query. Strip the managed
`CLAUDE.md` block and the installed skill, and adherence collapses to 6%: the
unwired agent greps instead of querying (24), never discovers khub (21), or writes
a raw Markdown file (4). Judge root-causes: wiring-not-loaded (17), chose-Write-first
(6), schema-not-read (6).

The wiring under test is **soft** — the block says "prefer khub over grepping,"
the skill says the same, and nothing forbids editing entity Markdown directly.
Soft wiring is enough to move adoption from 6% to 100%.

## Why completion is 90%, not 100%

Three wired operations reached for khub correctly but did not land the intended
entity. Reading the transcripts, each is the agent behaving well:

- `mtg-acme`, `mtg-bigco` — the agent ran `khub query`, `khub get --edges`, and
  `khub schema show meeting`, resolved the engagement, then stopped and asked for
  the fields it lacked. The `meeting` type requires `date`, `call_type`, and
  `source`; the prompt supplied none of them. The agent declined to invent them.
- `t-onboard-leo` — asked to "note somewhere" that Leo is onboarded, the agent
  found Leo already existed and set his `description` field instead of creating a
  second entity. Across reps it split between that and a fresh note (50%).

None of these is a wiring failure. An earlier version of the scorer folded
"intended entity landed" into the adherence verdict and marked all three
off-rails; separating the two axes corrected that.

## The one real gap: meeting capture from a vague ask

A conversational ask cannot complete a `meeting` create, because the required set
(`date` + `call_type` + `source`) is not inferable from the ask, and a correct
agent refuses to fabricate it. This is a capture-UX question for khub — whether
conversational meeting capture wants a lighter required set, a draft/partial
create, or a prompt-back for the missing fields — and it is independent of wiring.

## Decision on enforcement

The plan laid out an enforcement ladder: L1 imperative wording in the wired block
and skills, L2 a `PreToolUse` hook that hard-blocks `Write`/`Edit` under entity
paths. The data does not support building either. Wired adherence is already 100%,
with no bypass to block. A Write-blocking hook would not have helped the meetings —
their gap is missing data, not tool choice. Ship the soft wiring as it stands.

## Reproduce

```bash
python tests/eval/run.py --reps 2 --conditions wired,unwired --judge
python tests/eval/report.py /tmp/khub-eval/results.jsonl
```

The runner force-reinstalls khub from this repo first, so a re-measure after any
wiring change tests the new version. Records are per-operation JSONL; `report.py`
re-scores them from stored signals, so the scorecard stays consistent across runs.
