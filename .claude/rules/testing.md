# Testing khub — which gate holds what

CLAUDE.md lists the commands. This file is what each gate covers, the record
discipline, and where new tests are worth adding.

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
binaries), `TTY_COMPATIBLE`/`FORCE_COLOR` (isatty), pinned git identity +
`GIT_CONFIG_GLOBAL`, path normalizers (`%WS%`/`%HOME%`, symlink-resolving).
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

- **Inputs outside the corpus.** `internal/canon/fuzz_test.go` holds four
  targets seeded from `parity/corpus`: `FuzzJSONValid` (both encoders emit
  what `json.Valid` accepts), `FuzzReconcatIdentity` (a no-change splice is
  byte-identical), `FuzzSpliceIdempotent`, `FuzzEmitRoundTrip` (emit → load →
  emit is stable in both profiles). `go test` runs the seeds; `go test
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
