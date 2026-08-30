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
  the CLAUDE.md block + the `@.khub/*.yaml` layer imports + skill load). Tasks run in
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

## build-lite: `run_build_lite.py`

The firm-ops harness (`run.py` + `tasks.py`) measures an empty workspace. `run_build_lite.py`
measures a preset laid down INSIDE a copy of a real code repo, so the agent has source to read
while it captures — which is the situation khub actually ships into.

```
export PATH=<venv-with-khub>/bin:$PATH        # preflight asserts the pinned build; never installs
python run_build_lite.py --seed-repo <repo-copy> --conditions wired,unwired --reps 1
python run_build_lite.py --task-set cues --reps 5 --conditions wired --seed-repo <repo-copy>
```

Three task sets (`--task-set`), each isolating one question:

| set | n | question |
|---|---|---|
| `narrative` (default) | 27 | wiring adherence end to end, ordered and accumulating |
| `statements` | 8 | activation — does a bare stated fact become a record? |
| `cues` | 4 | which imperative triggers capture, with the failing variant as in-run control |

It overrides exactly three firm-ops-shaped things and edits nothing in `base.py`/`run.py`:
`preflight` asserts the PATH khub instead of force-installing over the operator's global one,
`build_wired` takes the preset as a parameter and can seed a real repo, and the turn cap is a
flag (`--max-turns`, default 30 — `run.py`'s hardcoded 14 assumes an empty workspace, and a
smoke run hit it mid-exploration on a real codebase).

**Read results from in-run controls only.** Cross-run comparisons are unsafe: the turn cap
differs from the firm-ops harness, and see the confound below.

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
