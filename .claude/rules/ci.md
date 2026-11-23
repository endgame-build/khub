# CI in khub — the rules for changing it

`docs/ci.md` is the inventory of every job and what it proves. This file is
what to keep true when adding or changing one. Each rule was paid for.

## Where a check belongs

- **Per PR** only what is deterministic and under about two minutes, in
  `ci.yml`. Add checks that need neither macOS nor Windows to `go-extras`,
  not to the matrix leg: the critical path does not grow, and the step can
  become a required check alone.
- **Nightly** (`nightly.yml`) is for what is wrong per commit and right once
  a day: scale, fuzzing past the seeds, benchmarks, the coverage union, a
  fresh vulnerability query, a snapshot release. A nightly job never uses
  `continue-on-error`: it reports success to `needs`, so the `report` job
  never sees the failure. Something that must stay report-only prints its
  verdict to the summary and exits 0 on purpose, with the promotion criterion
  stated in a comment beside it.
- **Report first, gate second.** A new check with a baseline or a threshold
  starts report-only and is promoted by a stated criterion ("zero FAIL and
  zero INVALID on unchanged main for three to four weeks"), never by feel.

## Workflow mechanics

- Actions are pinned by commit SHA with the version in a comment; copy the
  SHAs already in `ci.yml` and pin a new one deliberately. `permissions: {}`
  at the root, a per-job grant. Every job has `timeout-minutes`.
- `defaults.run.shell: bash` where a step pipes into `tee`: without
  `pipefail` the failure is swallowed.
- Never a workflow-level `paths:` filter on anything that may become a
  required check: a filtered-out run reports nothing and the check hangs
  pending. Decide inside the job with `git diff --name-only "$BASE...HEAD"`
  over `fetch-depth: 0`, run always, and say why the rest was skipped.
- `workflow_dispatch` resolves the workflow file on the default branch. A
  workflow a PR adds cannot be dispatched from that PR; test it with a
  temporary `pull_request` trigger, marked as such, reverted before merge.
- No Python on any build, test or release path. Go helpers, bash drivers,
  node for the plugin. `jq` only in the ubuntu-only compat script.
- Every shell script CI runs is shellchecked (the `npm-package` job lints the
  root smokes and parity drivers, the `plugin` job its own tools). A
  file-wide directive goes before the first command; a per-command one before
  the compound command it governs; a reason goes on its own comment line.
- `CGO_ENABLED=0` is what ships and what CI tests. The race detector needs
  cgo and sets it to 1 for that one step.
- setup-go's cache saves only on a key miss. A growing corpus (fuzz) rides in
  an explicit `cache/restore` + `cache/save` pair keyed per run with a prefix
  `restore-keys`. Do not cache the scale corpora: they are khub's own output
  and the only correct key hashes nearly every `.go` file.

## Measurement

- Never gate on wall-clock milliseconds. Gate on what travels: child CPU
  time (min of eleven interleaved repeats) for exponents and ratios, exact
  byte counts, deterministic coverage. Wall time is a backstop at most.
- A performance baseline is valid only on the machine class that recorded
  it. `parity/scale/gate` checks goos, core count and CPU model and, off the
  recording machine, downgrades the per-command delta and the ratios to
  notes; only the absolute exponent ceiling judges. Record on the runner with
  the nightly's `rebaseline` input, never from a laptop.
- The parity runner builds each child's environment from scratch, so
  `GOCOVERDIR` rides in through `-bin "env GOCOVERDIR=… khub-cover"`; the
  counter directory must sit outside every workspace the suite creates.
- The scale corpus is generated, never committed, and the generator's
  manifest is the independent oracle. Nothing in a scale script spells an id
  or a title; handles come from the manifest through
  `parity/scale/corpus.Handles`.

## Recorded numbers

`parity/scale/footprint.json`, `parity/scale/baseline.json`,
`parity/coverage-floor.txt`, `plugin/budget.json` and
`plugin/tests/fixtures.ts` follow the fixture discipline: exact, re-recorded
in the commit that moves them, with the delta explained, never to make a red
run green. A tolerance would let ten small growths through with no review
moment; the re-record is the review.

## Cross-version

`parity/compat/versions.txt` lists every published minor from the plugin's
`MIN_KHUB` to the newest release, and the `self` job checks that floor. Add
the new version after each release; remove lines only in the commit that
raises `MIN_KHUB`. The script's assertions are driven by the upgrade report
and the storage roots, never by a literal path: `knowledge/` is one preset's
name.
