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
