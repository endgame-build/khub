# CI — what runs, when, and what each run proves

khub's continuous integration is three workflows and one release pipeline.
This file is the inventory: every job, what it proves, how, and when it runs.
`.claude/rules/ci.md` holds the rules for changing any of it;
`.claude/rules/testing.md` says which gate holds which property and where a
new test belongs.

Three principles shape every piece. **No Python on any build, test or release
path**: Go helpers, bash drivers, node for the plugin. **Recorded numbers
follow the parity discipline**: exact, re-recorded deliberately in the commit
that moves them, never to go green. **Nothing gates on wall-clock
milliseconds**: measurement showed they swing up to 3.5× between runs of
unchanged code on a shared runner.

## On every pull request

### `ci.yml` · `go` — ubuntu, macOS, Windows

Windows is experimental and non-blocking (`continue-on-error`). Every leg
builds with `CGO_ENABLED=0`, the static binary that ships; the race detector
is the one step that overrides it, in `go-extras`.

| Step | Proves | How |
|---|---|---|
| No internal names ship | no session links, private paths or registry references in the tree | `git grep`, plain ERE |
| gofmt, `go vet`, `GOOS=windows go vet` | formatting; vet on both targets from the blocking leg | toolchain |
| `go mod tidy -diff`, govulncheck, actionlint | a tidy module; reachable advisories; valid workflows and shellchecked `run:` blocks | pinned tools, ubuntu only |
| golangci-lint, native and `GOOS=windows` | the choke-point rules: only `internal/canon` imports YAML, only `jsonio.go` writes JSON | depguard, pinned version |
| `go test ./...` | the unit suite, with the fuzz seed corpora (five targets in `internal/canon`, one in `internal/migrate`, one in `internal/okfimport`) | toolchain |
| Parity suite | exact stdout, stderr, exit code and a whole-tree manifest per step; the executable spec, 151 cases | `parity/runner`, unaided |
| err-os-error as a standard user | the write-denial case the admin runner skips | Windows only |
| Coverage map | every command, flag, error code, format and mode reaches a recorded fixture, and the binary offers nothing the map does not list | `-coverage parity/coverage.yaml -subset-of` |
| CLI smoke | `smoke-windows.sh`: all of `smoke.sh` (every verb over both presets, exit codes asserted; eight writers on one collection file in three formats; edit idempotence; NFC and NFD titles; one OKF chain of review, trust, supersede, expiry, the draft lifecycle, export and import into a fresh workspace), then git-dated stale and backfill, line endings, serve's TTY refusal | bash, every OS |
| CLI reference is current | `docs/cli-reference.md` regenerates unchanged from the binary's help | regenerate, `git diff --exit-code` |
| YAML round-trip gate | the emitter reproduces ruamel byte for byte: T1c round trip, T2 surgical edit, T3 emitter differential in both profiles, with counts that fail closed | `parity/yamlgate` |

### `ci.yml` · `go-extras` — ubuntu, beside the matrix

| Step | Proves | How |
|---|---|---|
| Race detector | no data race in the parallel entity parse (`index.each()`) or anywhere `go test ./...` reaches | `CGO_ENABLED=1 go test -race -cpu 4 ./...` |
| Shuffled, twice | no order dependence or leaked package state | `go test -shuffle=on -count=2 ./...`; the seed prints on failure |
| Scale smoke (300) | khub's answers at n against the generator's own model (orphans, closures, degrees, stale and expired counts, trust tiers, superseded reviews), the mutation cases, the OKF gates (`check --stale`, `check --require-verified`), an `export --okf` then `import --okf` round trip into a fresh workspace, and determinism (a regenerated corpus is byte-identical) | `parity/scale/smoke.sh --scale 300`, full mode |
| Footprint pin | every read's stdout bytes, stderr bytes, exit code and row count equal `parity/scale/footprint.json` exactly; the corpus is fingerprinted so a generator change reads as "corpus moved" | `parity/scale/footprint`, inside the scale smoke |
| Binary coverage floor | the parity suite's statement coverage of `internal/cli` is at or above `parity/coverage-floor.txt`; the profile is byte-identical across runs | `parity/tools/coverage-floor.sh` with a `go build -cover` binary |

### `ci.yml` · `npm-package` — ubuntu

| Step | Proves |
|---|---|
| shellcheck | every script CI runs: the npm assembler and its smoke, the three root smokes, the parity drivers |
| POSIX syntax | the two `sh` scripts parse under `sh -n` |
| Browser model tests | `internal/serve`'s page model, under node |
| Launcher shim parses | `npm/khub/bin/khub.js` |
| Assembly smoke | the package assembles from fixture archives, packs, installs under `--ignore-scripts`, runs, refuses a tampered archive and a snapshot version |

### `ci.yml` · `plugin` — ubuntu

| Step | Proves |
|---|---|
| shellcheck | `plugin/tools/*.sh` |
| Marketplace and plugin manifests | `claude plugin validate --strict` on both |
| Fixtures match the built binary | `tests/fixtures.ts` re-records byte-identical from this checkout's khub (node, no Python) |
| Plugin tests | the 113 tests over the hooks, against recorded khub output |
| Static budgets | each SKILL.md's bytes and description chars, and the wire block's bytes per preset and agent file, at or under `plugin/budget.json` |
| Skills name real flags | every backticked `khub …` in both skills and the wired block is a call the built binary accepts; field flags read from the live schema |
| Projected token cost | `claude plugin details` into the job summary (report only) |

### `compat.yml`

| Job | Proves | When |
|---|---|---|
| `self` | the cross-version script's own mechanics, OLD = NEW, no network; and that `parity/compat/versions.txt` starts at the plugin's `MIN_KHUB` | every PR, blocking |
| `matrix` | each published version in `versions.txt` authors a workspace that this binary reads raw, previews (nothing moves), upgrades, upgrades again (nothing pending, nothing migrated), reads, and edits once (a one-file diff); backups named equal backups present; the entity files modified are exactly the ones the upgrade's `migrated` names, and none is deleted; a tree that still carries the retired `draft` key must fail its raw `validate` with errors that each name `khub upgrade`; the forward half (OLD reads a NEW tree) prints WARN and never fails the run | nightly; on dispatch; on a PR whose diff touches `internal/{workspace,schema,wire,reindex}`, `presets/` or `parity/compat` (decided in the job with git). Report-only for one release cycle |

## Nightly

`nightly.yml` runs on a schedule and on `workflow_dispatch`. No job uses
`continue-on-error`: that reports success to `needs` and the failure is never
seen. A red scheduled run opens or updates the issue "Nightly CI is red";
green closes it.

| Job | Proves | How |
|---|---|---|
| `scale` | the scale smoke at 550 with the determinism pass, on ubuntu and macOS | `parity/scale/smoke.sh --scale 550` |
| `bench` | how every read and write grows with the corpus, on both clocks; a write cycle on a copy of each corpus that must come back byte-identical | `parity/scale/bench`, 100/550/2000, eleven interleaved repeats, corpora regenerated every night |
| `bench` · gate | the rule in `parity/scale/gate` against `baseline.json`: CPU-min growth exponent over 1.15 or over the baseline + 0.10, CPU ratio to `schema show` over 1.35× the baseline, wall ratio to `--version` over 2× as a backstop; a run whose control grew is INVALID, not a failure | report-only until unchanged `main` has been quiet for three to four weeks; off the recording machine only the ceiling judges, so dispatch `rebaseline: true` once on the runner class and commit the uploaded baseline |
| `bench` · benchstat | HEAD against the last release tag, built in a worktree in the same job, HEAD's benchmarks on both sides, six interleaved rounds | `parity/scale/benchcmp.sh`; `allocs/op` is the column that travels between machines |
| `bench` · profiles | CPU profiles of the in-process benchmarks when the gate fails | artifact |
| `fuzz` × 7 | every fuzz target past its seed corpus: the five canon properties (JSON validity from JSON and from md frontmatter, no-change splice identity, splice idempotence, emit round trip), migration idempotence, and reading an attacker's OKF concept file | 90s per target, the growing corpus cached between nights; a crasher is uploaded and fails the job; committed under `testdata/fuzz/<target>/` it fails `go test` until fixed or recorded as a known gap |
| `coverage` | the union of parity, both smokes, the scale smoke and the unit tests, per package, with `cover.html` | `GOCOVERDIR` per producer, `go tool covdata merge` |
| `vuln` | advisories published between pushes | govulncheck |
| `snapshot` | the six cross-compiles and the archive layout, then the real artifacts through the npm assembler and launcher smoke | goreleaser `--snapshot`, `npm/smoke.sh dist <version>` |
| `report` | one tracking issue while red | `gh`, `issues: write`, no checkout |

## On a tag

`release.yml` re-runs the unit suite, parity and the smoke, checks the tag
against `internal/version`, builds with a pinned goreleaser, publishes to npm
by trusted publishing, then installs the published package on ubuntu, macOS
and Windows (non-blocking) and runs it. See `RELEASING.md`.

## What is not in CI, and where it lives

- **Plugin evals** (an agent with the skill loaded, scored against a no-plugin
  arm) need an API key, real model calls and a sandbox on the runner — #140.
- **The token trend** (what an agent spends per task) and `count_tokens`
  calibration of the footprint — #141.
- **`tsc` over the plugin** needs type declarations only an interactive session
  lays — #142.

## Recorded numbers

Every file below is a contract like a parity fixture: a change is a deliberate
re-record, reviewed, with the reason in the commit.

| File | Holds | Re-record with |
|---|---|---|
| `parity/cases/**/expected/` | bytes per step | `parity-run -record -only <family>/<case>` |
| `parity/scale/footprint.json` | bytes per read at scale 300 | `footprint -record` |
| `parity/scale/baseline.json` | growth exponents and ratios | the nightly's `rebaseline` dispatch, then commit the artifact |
| `parity/coverage-floor.txt` | the `internal/cli` floor | edit, upward, after a CI reading |
| `plugin/budget.json` | the skills' and wire block's size | `plugin/tools/budget.sh --write` |
| `plugin/tests/fixtures.ts` | khub's JSON the mod reads | `plugin/tools/record-fixtures.sh` |
| `parity/compat/versions.txt` | the published versions tested against | add the new version after each release |
