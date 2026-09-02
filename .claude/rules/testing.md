# Testing khub — which gate holds what

CLAUDE.md lists the commands. This file is what each gate covers, the record
discipline, and where new tests are worth adding.

## Why the parity harness beats testscript (evaluated; keep ours)

Two properties `rogpeppe/testscript` lacks: the **whole-tree manifest** per
step ("no stray files, no unintended writes" — the write-path oracle) and
**exact exit codes** (testscript's `!` is boolean, its `stdout` is regexp-grep
not bytes). txtar also normalizes trailing newlines — exactly where khub's
format contract lives. Borrow its ideas, never its file format. Same verdict
for golden libs (goldie/autogold/cupaloy): `parity/expected/` is stricter.

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
binaries), `TTY_COMPATIBLE`/`FORCE_COLOR` (isatty), pinned git identity +
`GIT_CONFIG_GLOBAL`, path normalizers (`%WS%`/`%HOME%`, symlink-resolving).
Trap: the runner pins `COLUMNS=80` **and** pty winsize 80 — two width code
paths agree only because both constants are 80.

## What golden bytes cannot catch — add tests here

- **Inputs outside the corpus.** Zero `func Fuzz` in the repo. Four targets,
  seeded from `parity/corpus`: `FuzzJSONValid` (`json.Valid(EncodeCLI(v))`),
  `FuzzReconcatIdentity`, `FuzzSpliceIdempotent`, `FuzzEmitRoundTrip`. Every
  rustfmt/ruff idempotency bug on record was an input nobody's corpus held.
- **Non-idempotency.** T1c proves verbatim round-trip; add gofmt's loop:
  format the formatter's own output, assert no change.
- **Cross-process locking.** flock serialization has no test, and `-race`
  cannot see it (separate address spaces) — needs N processes writing one
  collection.
- **Unicode filesystems.** No `unicode/norm` anywhere; `casefold.go` has no
  test file. macOS NFD vs NFC on slugs is the live bug shape.
- **Binary coverage.** `go build -cover` + parity run with `GOCOVERDIR`
  *outside* the workspace (else the manifest picks up counter files), merged
  via `go tool covdata`. Report only, no threshold. Signal: `internal/cli`
  near-total from parity alone — a drop means per-type branching crept in.
- **Scale.** Every fixture holds a dozen entities; the bugs that only appear
  at n (a walk that never terminates, an index that drops rows, a scan gone
  quadratic) live in `parity/scale/`: a Go generator drives `khub add` for
  ~550 entities and writes an **independent** ground-truth manifest (orphans,
  closures, degree, stale counts from its own graph model, never read back
  from khub); `smoke.sh` there asserts khub against it, timed, with the
  mutation cases (cycle, dangle, `check` vs `check --strict`); `bench` fits a
  growth exponent per command across 100/550/2000. Not in CI per commit — run
  it before a release or after touching `index`, `graph`, `integrity`,
  `query`, `search`. Nothing in it spells an id or a title, so an id-scheme
  change never touches it.

## The dead oracle

`parity/runner/differential.go` + `workflows.go` (654 LOC: seeded random verb
sequences, adversarial values — `İstanbul`, `yes`/`017`, CRLF — full-tree diff
per step) require `-bin-b`, which died with Python. CI never runs it. Revive
(khub vs last release, or self-invariants) before writing any new
property-testing code.

## Not worth it

`-race` as coverage (zero goroutines in `internal/`+`cmd/` — tripwire only,
needs CGO=1), mutation testing, OSS-Fuzz enrolment, Windows CI.
