# Testing khub — which gate holds what

CLAUDE.md lists the commands and `docs/ci.md` is the inventory of every CI
job. This file is what each gate covers, the record discipline, and where new
tests are worth adding; `.claude/rules/ci.md` is how to change the workflows.

## The harness is custom on purpose

Two properties no golden-test library offers together: a **whole-tree
manifest** per step (no stray files, no unintended writes — the write-path
oracle) and **exact exit codes**. Borrow ideas from such libraries, never
their file formats: txtar normalizes trailing newlines, exactly where khub's
format contract lives.

## Record discipline

1. Never re-record to make a red suite green. Red is information.
2. `-record` always with `-only <family>/<case>` — `runCase` does
   `os.RemoveAll(expDir)`, so an unscoped record wipes expectations.
3. `tree.manifest` is SHA-256 lines: a reviewer sees *that* a file changed,
   never *what*. The commit message must say what moved, in words.
4. Prove a byte-neutral claim with a clean `parity-run` **before** commit,
   never by re-recording after.

## Nondeterminism seams (do not add more)

`KHUB_PARITY_NOW` (clock — cmd/go ships the same kind of seam in release
binaries; a date or an offset datetime, anything else refuses),
`KHUB_ACTOR` (the writer `generated.by` records), `TTY_COMPATIBLE`/`FORCE_COLOR`
(isatty), pinned git identity + `GIT_CONFIG_GLOBAL`, path normalizers
(`%WS%`/`%HOME%`, symlink-resolving), and `TZ=UTC` (dates are the local civil
date, OKF stamps UTC).

`KHUB_ACTOR` is a seam because actor provenance is product behaviour, not test
scaffolding. The ladder is `--by`, then `KHUB_ACTOR`, then `CLAUDECODE` set
(`agent/claude-code`), then `khub`, and khub never reads a person out of git.
`CLAUDECODE` is set in any Claude Code shell, so every recorder and smoke script
pins `KHUB_ACTOR=khub` above it (the runner's `baseEnv`, `smoke*.sh`,
`record-fixtures.sh`, `parity/scale`); a Go test that asserts a stamp pins it
in `TestMain`.

Trap: the runner pins `COLUMNS=80` **and** pty winsize 80 — two width code
paths agree only because both constants are 80.

On Windows the path normalizer also replaces the 8.3, forward-slash and Git
Bash (`/c/…`) spellings, case-insensitively.

## Windows: what the runner does differently

The expected bytes are shared across all three OSes. Three runner
mechanisms let Windows run them without a second set.

- **`sh_windows`** on a step replaces `sh` on Windows and is ignored
  elsewhere. Use it only when the unix setup has no effect there (`chmod` on
  a directory). It must leave the same observable state.
- **`requires: [write-denial]`** makes the runner probe once whether denying
  writes on a directory binds this account. A create that succeeds under the
  denial (root; a Windows admin with backup and restore privileges) reports
  the case as SKIP with the reason. Any other probe failure is an ERROR and
  fails the run.
- **`mode: pty` on Windows is checked by behaviour.** A ConPTY re-renders
  the screen, so the runner compares exit code and output mode (JSON, human,
  empty) and prints a `PTY-BEHAVIOURAL` line per case. The tree manifest is
  still byte-checked. Pty cases record on darwin or linux only.

CI adds one step outside the runner, because GitHub's Windows runner is such
an admin and the full suite skips `err-os-error` there. The step creates a
non-admin local user and runs that case as it, and fails on a SKIP as well as
a FAIL.

## What golden bytes cannot catch — add tests here

- **Inputs outside the corpus.** `internal/canon/fuzz_test.go` holds five
  targets seeded from `parity/corpus`: `FuzzJSONValid` and
  `FuzzFrontmatterJSONValid` (both encoders emit what `json.Valid` accepts),
  `FuzzReconcatIdentity` (a no-change splice is byte-identical),
  `FuzzSpliceIdempotent`, `FuzzEmitRoundTrip` (emit → load → emit is stable
  in both profiles). Two more cover OKF input. `FuzzMigrateIdempotent`
  (`internal/migrate`) holds that a second migration changes nothing and no
  key is lost. `FuzzImportRead` (`internal/okfimport`) holds that a concept
  file an attacker wrote loads or refuses with a known code, and that no
  link resolves outside the bundle. `go test` runs the seeds; `go test
  -fuzz=FuzzEmitRoundTrip ./internal/canon` explores. Every rustfmt/ruff
  idempotency bug on record was an input nobody's corpus held.
- **Non-idempotency.** T1c proves verbatim round-trip; add gofmt's loop:
  format the formatter's own output, assert no change.
- **Cross-process locking.** `-race` cannot see separate processes. Keep the
  process-level mutation race tests that prove the workspace lock covers the
  full scan/validate/commit window, including different entity types. The
  init/upgrade tails run inside the library's lock through the `Tails` hook;
  `internal/workspace/tails_test.go` proves it with a non-blocking `flock`
  probe from inside the hook. `fsio.Locked` is not re-entrant, so a tail that
  calls a locking entry point from there deadlocks — that is what the `Held`
  variants exist for.
- **Unicode filesystems.** macOS hands out decomposed (NFD) names. Slug
  minting spells Latin accents out (`internal/entity/slug_unicode_test.go`),
  so an NFC and an NFD title mint one slug, and lookup composes before
  folding (`internal/index/casefold_test.go`); keep both when touching
  slugify or casefold. `smoke.sh`'s unicode block pins the invariant that
  must hold on every filesystem, APFS included: one index row per file,
  `check` passing.
- **Binary coverage.** `go build -cover` + parity run with `GOCOVERDIR`
  *outside* the workspace (else the manifest picks up counter files), merged
  via `go tool covdata`. The runner builds each child's environment from
  scratch, so the directory rides in through `-bin "env GOCOVERDIR=… khub-cover"`.
  The merged profile is byte-identical across runs, so `internal/cli` is
  gated per PR against `parity/coverage-floor.txt` (84% from parity alone;
  the rest is OS-error and `viz --open` paths that differ by machine — a drop
  means per-type branching crept in). The nightly reports the union with the
  unit tests and both smokes, per package, with `cover.html`.
- **Scale.** Every fixture holds a dozen entities; the bugs that only appear
  at n (a walk that never terminates, an index that drops rows, a scan gone
  quadratic) live in `parity/scale/`: a Go generator drives `khub add` and
  `khub verify` for ~550 entities and writes an **independent** ground-truth
  manifest (orphans, closures, degree, stale and expired counts, trust tiers,
  superseded reviews and the import counts, all from its own model, never
  read back from khub); `smoke.sh` there asserts khub against it, timed,
  with the mutation cases (cycle, dangle, `check` vs `check --strict`), the
  OKF gates (`check --stale`, `check --require-verified`) and an export and
  import round trip; `bench` fits a growth exponent per command across
  100/550/2000, export and import included. Run the bench by hand before a
  release or after touching `index`, `graph`, `integrity`, `query`,
  `search`, `export`, `okfimport`. Nothing in it spells an id or a title,
  so an id-scheme change never touches it. Per PR, `go-extras` runs it at
  300 in full mode — the regenerate-and-diff pass is the one check that a
  map order leaks into written bytes at n — and `parity/scale/footprint`
  pins the bytes every read prints there (`footprint.json`, exact,
  re-recorded deliberately).
  Nightly, 550 on two OSes, the bench with both clocks and the write cycle
  on a copy that must come back byte-identical, and `parity/scale/gate`
  against `baseline.json` (CPU-min exponents and ratios; wall time only as a
  backstop — see `parity/scale/README.md` for why). Report-only until
  unchanged main has been quiet for weeks.
- **The race detector needs cgo.** CI builds static (`CGO_ENABLED=0`, what
  ships); the `go-extras` race step is the one that sets it to 1. Its target
  is the parallel entity parse in `index.each()`; the cross-process lock
  tests are invisible to it by design and stay what they are.
- **Fuzzing past the seeds.** `go test` runs the seed corpus; the nightly
  runs each fuzz target for 90s with the growing corpus cached between
  nights. A crasher lands under `testdata/fuzz/<target>/` and, committed,
  fails plain `go test` until it is fixed or recorded as a known gap beside
  the ones already in that package's `fuzz_test.go` (canon's are two
  loader gaps in goccy and one emitter gap that is ruamel's own, reproduced
  byte for byte).
- **Other versions' workspaces.** `parity/compat/smoke.sh OLD NEW` has an
  older published binary author a workspace and this one read it raw,
  upgrade it, upgrade it again and edit it, with the assertions driven by
  the upgrade report and the storage roots. The self row runs on every PR;
  the published-version matrix (`parity/compat/versions.txt`, floor =
  the plugin's `MIN_KHUB`) when the upgrade surface changes, and nightly.
