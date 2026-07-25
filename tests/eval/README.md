# khub wiring eval

Does a real agent, dropped into a wired khub workspace, actually use the CLI on
vague, khub-unaware asks — or does it write Markdown / grep? This harness runs
real headless Claude Code agents against a wired workspace (and an unwired
control), lets them populate and operate it, and scores every operation
on-/off-rails with a failure taxonomy.

**Measurement only** — it changes nothing in khub. It reads the *current* repo's
`wire.build_block` + `skills/`, and force-reinstalls `khub` from this repo
before running, so it always tests the latest wiring.

## Run

```bash
python tests/eval/run.py --n 3 --reps 1 --conditions wired            # smoke
python tests/eval/run.py --reps 2 --conditions wired,unwired --judge  # the mass run
python tests/eval/report.py /tmp/khub-eval/results.jsonl              # scorecard
```

- `--n` caps the task batch (default: all of `tasks.py`); `--reps` re-runs the
  whole batch (stochastic variance); `--conditions` = `wired` and/or `unwired`;
  `--model` (default `sonnet`); `--judge` adds an LLM root-cause on off-rails ops.
- Each task is one `claude -p ... --output-format stream-json --verbose
  --dangerously-skip-permissions`, cwd = the workspace, **without `--bare`** (so
  the CLAUDE.md block + `@.khub/schema.yaml` import + skill load). Tasks run in
  order against an accumulating copy, so compounding failures are measured.

## Cost

Every task is a real agent turn. A full pass is `tasks × conditions × reps`
agent runs (~100+), which is real API spend and takes a while — it is **not**
part of `pytest`. Start with `--n` small.

## Scoring

Deterministic signals from each transcript + the post-op khub state:
`used_khub` (a `Bash` `khub add|edit|link|get|query|…`), `bypass_write`
(`Write`/`Edit` under a schema entity dir), `bypass_read` (grep/cat over entity
files), and `khub check`/`get` to confirm the intended entity/edge landed.
Off-rails categories: `F1 bypass-write`, `F2 bypass-read`, `F3 khub-misuse`,
`F4 no-op`, `F5 wiring-not-loaded`.

## Files

`base.py` (pre-flight + wired/unwired fixtures) · `tasks.py` (the ordered ask
batch + machine-checkable intents) · `run.py` (runner + scorer) · `report.py`
(scorecard + taxonomy).

## Known confound: the operator's user-level CLAUDE.md

The agents under test run as the operator, so `~/.claude/CLAUDE.md` is in their context
alongside the workspace block being measured. It cannot be isolated cheaply: `CLAUDE_CONFIG_DIR`
redirects the config directory but takes authentication with it, and the credentials are not a
file that can be copied alongside (Keychain-backed on macOS), so an isolated run cannot log in.

It is therefore **detected rather than prevented**. `parse_stream` sets `host_instructions` on
any op whose final reply recites the host's instructions instead of doing the task — observed
once as a reply of "PROTOCOL ACTIVE: - Stop on failure, words before tools ...". Treat those ops
as contaminated and exclude them; a run with several is not a clean measurement of the wiring.

The practical consequence: absolute rates carry an unknown tax from whatever the operator's own
instructions tell an agent to do. Comparisons WITHIN one run (wired vs unwired, cue A vs cue B)
share the tax and remain valid, which is why the evals are designed around in-run controls.
