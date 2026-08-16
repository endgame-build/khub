# Parity decisions

Every accepted divergence from byte-identical Python behaviour, and every
addition to the harness the plan did not specify. The rule from
`docs/history/go-port-plan.md`: *the normalizer never grows silently*. Nothing here is
a licence to diverge further — each entry names what was accepted and why, so
the next reader can challenge it.

## D1 — goccy parser rejects an empty keep-chomped block literal

**Accepted 2026-08-15 (M0).** `keep: |+` with no content parses under ruamel and
fails under goccy's parser (`could not find multi-line content`); the *lexer*
handles it, so T1c reconstruction is unaffected. A workspace file using that
shape reads as malformed under Go and valid under Python.

Why accepted: khub never writes the shape, the malformed-file contract contains
it (one bad file is reported, never a crash), and the M7 differential run will
surface any real-world occurrence. Upstream issue to file at dual-ship time.

## D2 — M0's GO rests on token-splice, not on the AST round-trip

**Accepted 2026-08-15 (M0).** The plan's GO criterion read "T1(AST)+T2 zero-diff
on 100% of canonical+real". Measured: the AST `String()` round-trip is 91/133,
so it is **not** viable as a writer. GO was taken on a different leg the
prototype discovered: **T1c**, lexer token-origin reconstruction, 133/133
byte-exact, with T2 surgical splices 99/99.

Why accepted: T1c is a *stronger* guarantee than the plan asked for — untouched
bytes survive by construction rather than by an emitter round-tripping them
faithfully. The write architecture changed accordingly (goccy is parser-only;
edits are token splices; new files come from `internal/canon`'s emitter, pinned
by T3 at 1089/1089 on both ruamel profiles).

Not yet discharged: the plan's `real/` corpus leg (a live workspace snapshot).
`parity/corpus/real/` is gitignored and was not available in this environment;
it must be run before cutover.

## D3 — `pathNormalizer`: a third normalizer, unconditional

**Accepted 2026-08-15 (M1).** The plan sanctions exactly two named, per-case
normalizers (`cycles-canonical`, `tty-layout-free`). A third was added and is
applied unconditionally to both channels: run-local temp roots (`%WS%`,
`%HOME%`, including the macOS `/private` realpath forms) are substituted before
comparison.

Why accepted: without it, every fixture that embeds an absolute path —
`os_error` messages, `install-skills --global` writes, schema-parse errors —
records a path unique to the run that recorded it, so the suite could never
verify twice. It normalizes *the harness's own nondeterminism*, not khub
behaviour: no khub output shape, key order, or encoding is touched.

Scope guard: substitution is literal-string only, over paths the runner itself
created.

## D4 — `-setup-bin` / `-ported`: the dual-ship bridge

**Accepted 2026-08-15 (M3).** Runner flags the plan did not specify. Steps whose
verb is not in `-ported` run through the Python binary; the rest run through
the binary under test.

Why accepted: every fixture begins with `khub init`, which lands in M6. Without
a bridge, no Go verb could be fixture-verified until the *last* milestone — the
opposite of "each family lands before its Go milestone so the fixtures are the
executable spec". Verified honest: pointing `-bin` at a non-khub binary fails
the case, so a pass genuinely exercises the ported verb.

**RETIRED 2026-08-15.** CI now runs `parity-run -bin ./khub -cases parity/cases`
with no bridge, and the Go binary passes every fixture itself. The flags stay in
the runner for bisecting a single verb during future work, but nothing in CI
passes them.

Why this mattered: while CI still said `-ported "schema"`, the suite reported
0 fail even though the binary failed `cli-contract/bare-invocation` — Python was
carrying 21 of the 22 verbs. A reported number is only as honest as the bridge
it was measured through.

## D5 — `duplicate_type` exemption reason corrected

**Corrected 2026-08-15.** `parity/coverage.yaml` exempts `duplicate_type` as
"multi-file resolve only". The accurate reason: the factory has **zero callers**
in `src/khub/` — it is dead code in the Python baseline. The Go port keeps the
factory (byte-exact taxonomy) but it is equally unreachable.

## D6 — ASCII vs Unicode character classes in the template heading contract

**Accepted 2026-08-15 (M3).** `internal/template/template.go` ports two Python
regexes whose `\d` / `\s` are Unicode-aware in Python and ASCII-only in Go's
`regexp`. A heading numbered with, say, Devanagari digits, or separated by an
exotic Unicode space, would strip in Python and not in Go.

Why accepted: the affected input is authored markdown headings in a khub
workspace; no shipped preset or fixture exercises non-ASCII digits or exotic
spaces there. Revisit if a real workspace does.

## D7 — the parity date tags are harness-only

**Bounded 2026-08-15 (M2).** The corpora encode dates as `{"__date__": "…"}`
because JSON has no date type. That unwrapping now lives ONLY in
`canon.DecodeOrderedJSON` (the corpus reader). Entity documents go through
`DecodeOrderedJSONStrict`, which does not unwrap, so a real `.json` entity
carrying a field literally named `__date__` round-trips as data.

Recorded because the two decoders previously shared the unwrap, which would
have silently retyped such a field.

## D8 — bare `khub` prints help to stdout, not stderr

**Corrected 2026-08-15.** `docs/history/go-port-plan.md` A.1 asserted "Bare `khub` →
help on **stderr**, exit 2". The recorded fixture disagrees:
`parity/cases/cli-contract/bare-invocation/expected/steps/01.stderr` is empty
and the 5328-byte help lands on stdout, exit 2.

The fixture is the contract; the plan text was wrong and has been corrected.
Exit 2 was right.

## D9 — the real-corpus leg, discharged

**Run 2026-08-15 (M0, closing D2's open item).** The gate was run against two
live workspaces:

| Workspace | Files | T1c (byte-exact reconstruction) |
|---|---|---|
| `khub-test` (firm-ops) | 6 | 6/6 |
| `firm-ops-khub` (production) | 1781 | **1780/1781** |

The single diff is a line carrying **trailing whitespace after a scalar**
(`$ref: '…'   `) in an archived OpenAPI spec — a raw `.yml` file khub never
reads as an entity. Lexer token origins drop that trailing run.

Not a parity divergence: **ruamel does not preserve it either.** Verified
end-to-end — inject `name: Trail Co   ` into an entity, run `khub edit` under
Python, and the trailing spaces are gone. Go and Python therefore agree on
every file khub actually writes; the gate was simply holding the port to a
stricter standard than the baseline meets.

Recorded rather than "fixed" because the honest statement is narrower than
"byte-exact on everything": **reconstruction is byte-exact except for trailing
whitespace after a scalar, which neither implementation preserves.**

## D10 — set-iteration order when one relation value resolves to several nodes

**Accepted 2026-08-15 (M3).** `build_graph` iterates the `set` that
`resolve_target` returns, so CPython's hash order decides edge-insertion order
when a single relation value resolves to MORE THAN ONE node (a bare slug on an
`any`-kind edge that several types carry). Edge-insertion order is observable
in `neighbors` output.

Not reproducible in Go, and not worth reproducing: it is unspecified in Python
too (it varies with PYTHONHASHSEED). `BuildGraph` sorts those nodes by
`Node.Less` instead, which is deterministic and matches Python whenever Python
itself is deterministic. Single-node resolution — every ordinary edge — is
unaffected.

## D11 — do NOT import ncruces/go-sqlite3/embed

**Landmine, recorded 2026-08-15 (M3).** The go-port-plan and the memo both name
`ncruces/go-sqlite3` with "the `embed` build". Since v0.35 that package is a
deprecated no-op whose `init()` **prints to stdout** — which would corrupt every
`--format json` document khub emits.

FTS5 is now a registerable extension: call `ext/fts5.Register(conn)` per
connection before the DDL. A registration failure maps to `fts_unavailable`,
exactly as a failed `CREATE VIRTUAL TABLE` does.

Recorded because the plan's wording invites re-adding the import.

Related good news on risk R10 (FTS5 drift): ncruces ships SQLite 3.53.4 against
the local CPython's 3.53.0, and the error detail text is byte-identical once
ncruces' `sqlite3: <result-code text>: ` wrapper is stripped down to bare
`sqlite3_errmsg()` — which is what CPython puts in `str(OperationalError)`.
Snippet, bm25 ranking, and tie order all match. Pinned by
`TestSearchBadQueryDetailMatchesCPython`.

## D12 — three recorded divergences, visible on every run

**Accepted 2026-08-15 (M3-M6).** Three fixtures cannot match byte-for-byte
because the differing bytes come from a THIRD-PARTY library, not from khub
logic. Each case carries a `divergence:` line in its `case.yaml`, so the runner
reports it as **XFAIL with the reason printed every run** rather than
normalizing it away. An unexpected pass is reported as **XPASS**, so a stale
entry gets noticed instead of rotting.

| Case | What differs | What matches |
|---|---|---|
| `collections/malformed-whole-file` | the parenthetical quotes the JSON parser's own message (`Expecting value: line 1 column 1 (char 0)` vs Go's `invalid character 'o' in literal null`) | the message prefix (`Refusing to write repos.jsonl: cannot round-trip it (…)`), the error code, the exit code |
| `integrity/err-schema-family` | the schema-parse detail quotes the YAML parser's own message (PyYAML vs goccy, including its caret diagram) | prefix, code, exit code |
| `read-graph/search-basic` | one row's bm25 score in the last decimal place: `-0.6039964337975564` vs `…65` (SQLite 3.53.0 vs 3.53.4) | ranking, ordering, snippets, and every other field |

Why not normalized: a normalizer would hide the whole class, including a real
future regression in the same field. Why not "fixed": reproducing another
language's parser error text would mean hard-coding CPython's phrasing for
inputs khub does not control, and the bm25 delta is a 1-ULP artifact of the
SQLite build, not of khub.

Before cutover, decide per row whether the JSON/YAML detail text should be
replaced with khub's OWN phrasing in BOTH implementations — that would remove
two of the three permanently, at the cost of a deliberate contract change.

## D13 — the splice preserves authored formatting where ruamel normalizes it

**Accepted 2026-08-15 (M4), and it is the divergence most likely to matter.**

ruamel's round-trip loader carries comments on a full CST, so a khub edit
**re-emits the whole document** and normalizes every line while keeping the
comments. The Go splice does the opposite: it rewrites only the changed value
tokens and leaves every other byte alone. On a hand-formatted, comment-bearing
entity the two therefore differ on lines nobody edited:

| | Python | Go |
|---|---|---|
| redundant quotes | `quoted: plain word` | `quoted: "plain word"` |
| implicit null | `empty: null` | `empty:` |
| sequence indent | `- a` | `  - a` |

Values, key order, and the comments themselves are identical. Only comment-free
documents take the byte-pinned emitter, so ordinary khub-written files are
unaffected — this shows up exactly on files a human hand-formatted AND
commented.

Pinned by `parity/cases/write-path/comment-bearing-edit`, recorded from Python
and marked `divergence:`, so it is reported on every run. Before that fixture
existed, neither gate could see this: no fixture seeded a comment-bearing
entity, and `smoke-diff.sh` only edits freshly-added (comment-free) ones.

**The path to closing it** is to invert the design: emit the document
canonically (the T3-pinned emitter already reproduces ruamel byte-for-byte),
then splice the COMMENTS back onto their keys — which is what ruamel is
actually doing. That is the correct end state and is deliberately not attempted
here.

## D14 — harness and packaging additions beyond the plan

**Recorded 2026-08-15.** Files the plan did not name, none of them product code:

- `embed.go` at the repo ROOT (package `khub`, exporting `PresetsData` and
  `SkillsData`). `go:embed` cannot reach outside its own package directory, and
  `src/khub/presets/` and `skills/` share no ancestor below the module root.
  The plan anticipated a root-adjacent embed package but named it
  `internal/presets/embed.go`, which cannot work. Cost: one importable path
  outside `internal/`, against "everything under internal/". It collapses at
  cutover, when the presets move to `/presets`.
- `parity/tools/pykhub.sh` (the Python wrapper the recorder drives),
  `parity/tools/smoke-diff.sh` (the differential smoke against a real
  workspace), and the `gen_*.py` recorders for each differential corpus.
- `parity-run` flags beyond D4's `-setup-bin`/`-ported`: `-subset-of` (asserts
  the Go command set is a SUBSET of Python's — added after cobra's default
  `completion` shipped unnoticed), `-diffs`, and the `PRESETSRC` argv
  substitution used by corpus presets.

## D15 — `pty` mode, and what a real terminal turned out to prove

**Landed 2026-08-16.** The plan named `pty` (real PTY, no env hints) as the
third mode; nothing implemented it, so `stdout.isatty()` — the LAST rung of the
gate ladder and the one a human actually hits — had never run in either
implementation. `parity/runner/pty.go` implements it, and the new `tty-gate`
family records four cases from Python that the Go binary then matches.

Four harness choices, each one a contract:

- **Two ptys, not one.** A terminal is one device shared by stdout and stderr;
  the harness records the channels separately, so each gets its own pty pair.
  Nothing khub inspects can tell: the gate reads `isatty(stdout)` only, and
  Rich's `Console` is built on `sys.stdout`.
- **No output post-processing.** A pty in cooked mode maps `\n` to `\r\n` — a
  transformation the TERMINAL performs, not khub. The slave's termios has
  `OPOST` cleared before the child starts, so the recorded bytes are exactly
  the bytes the process wrote. The alternative (record CRLF, strip it before
  comparing) would have been a normalizer able to hide a `\r` khub genuinely
  emitted. **No CR normalization exists anywhere in the runner**; verified by
  grep over the recorded fixtures — not one `\r`.
- **`TERM=dumb`.** Rich reads `TERM`: any ordinary value gives it a colour
  system and it writes SGR into the table, while the port writes none.
  Recording that and leaning on `tty-layout-free` to strip it would certify a
  colour divergence as a pass. `dumb` puts Rich on the port's footing
  (`_detect_color_system` returns `None` for a dumb terminal) and the fixtures
  hold zero escape bytes on both sides. The residual gap is listed below rather
  than papered over.
- **Stdin stays off the pty.** The gate keys on stdout. An ordinary reader keeps
  EOF working (closing a pty master yields `EIO`, not a clean EOF) and stops the
  terminal echoing a step's stdin back into the recorded stdout.

What the family pins, all of it previously untested:

| Case | Mode / env | Asserts |
|---|---|---|
| `tty-gate/isatty-tty` | pty | no `--format` on a real terminal → human tables (and `--format json` still wins there) |
| `tty-gate/isatty-pipe` | pipe, `TTY_COMPATIBLE: ""` | the same seven steps down a real pipe with the env rung neutralised → JSON |
| `tty-gate/tty-compatible-beats-isatty` | pty, `TTY_COMPATIBLE: "0"` | rung 1 BEATS a real terminal — including the failure shape: envelope on stdout, empty stderr, exit 1 |
| `tty-gate/force-color-beats-isatty` | pty, `FORCE_COLOR: ""` | rung 2 set-and-empty beats a real terminal (`FORCE_COLOR` appeared in no case at all before this) |

`isatty-pipe` neutralises `baseEnv`'s `TTY_COMPATIBLE=0` by declaring the key
empty; Rich reads `environ.get("TTY_COMPATIBLE", "")` and Go switches on
`Getenv`, so empty and unset are the same fall-through, and `os/exec` keeps the
last value for a duplicate key. That is a documented stdlib rule, not a trick,
and it avoids inventing a fourth mode for "pipe with no hint".

Checked honest: copying `isatty-tty` to pipe mode with its pty-recorded
expectations FAILS on every read (table vs JSON). The fixture is load-bearing.

## D16 — `khub schema | head -1` is not an EPIPE trigger

**Measured 2026-08-16, while trying to fixture the EPIPE contract.**
`docs/history/go-port-plan.md` §Modes names `khub schema | head -1` as the EPIPE probe,
and the comments in `internal/cli/render.go` and `cmd/khub/main.go` name
`khub schema | head`. It is not a probe. `schema` emits 12012 bytes of JSON as
a SINGLE line, so `head -1` must
read all of it before it sees a newline, khub's write lands entirely in the pipe
buffer, and the process exits 0 having never seen `EPIPE`. Ten runs, `khub=0`
every time. A test built on it would have passed for the wrong reason.

A real trigger needs output that is both larger than the pipe buffer and
multi-line, or a reader that stops mid-line. Both were run against both
binaries, from an identical workspace:

| Probe | Path exercised | Python | Go |
|---|---|---|---|
| `khub get <300 KB body> --format raw \| head -1` | the raw bypass | exit 1, stderr empty | exit 1, stderr empty |
| `khub get <300 KB body> \| head -c 100` | `emit`/`Emit` | exit 1, stderr empty | exit 1, stderr empty |

So the two implementations agree, and the contract is narrower than "not an
error": **EPIPE is never DRESSED as an error** — no traceback, no `os_error`
envelope, nothing on stderr — but the exit code is **1**, not 0. Python gets
there through Click's own `except OSError … errno.EPIPE → PacifyFlushWrapper;
sys.exit(1)`; Go gets there because `cmd/khub` ignores `SIGPIPE`, Guard passes
the write error through untouched, and `Execute` maps a returned error to 1.
The `signal.Ignore` is load-bearing: the same probe against a Go binary WITHOUT
it exits 141, the runtime having died on the signal.

Not fixtured, and deliberately not: a step is `argv` appended to the binary
under test, so no `case.yaml` can express a pipeline. `sh:` steps cannot stand
in either — they have no way to name the binary under test (only `PRESETSRC` is
substituted, and only inside `argv`), their output is discarded, and a non-zero
exit aborts the case. Closing this needs ONE of: a `pipe_through:` field on an
argv step (runner builds `<bin> <argv> | <sh>` and records the binary's own
exit via `PIPESTATUS`), or a `%BIN%` substitution plus recorded output for `sh:`
steps. Either is a real harness feature and was left for a decision rather than
invented here.

## Open items before cutover

- ~~`pty` mode (plan §Modes) is unimplemented~~ — **closed by D15.** The isatty
  rung now has a fixture on both branches, and rungs 1 and 2 are pinned as
  beating a real terminal.
- EPIPE remains **unfixtured** (D16), though now measured on both binaries and
  on both output paths. It needs a harness feature to express a pipeline; the
  two candidate shapes are in D16.
- Nobody compares COLOURED output. The port emits no ANSI at all; Python emits
  SGR whenever Rich has a colour system. Both TTY families dodge the question —
  `human` by `NO_COLOR=1`, `pty` by `TERM=dumb` — and `tty-layout-free` would
  strip the difference anyway. Decide before cutover whether the port owes
  colour at all; if it does, that is product work, not a fixture.
- ~~`FORCE_COLOR=1` on a PIPE (rung 2's TRUE branch)~~ — **closed.** Recorded as
  `tty-gate/force-color-beats-pipe`, exactly the shape sketched here: `mode:
  pipe` with `env: {TTY_COMPATIBLE: "", FORCE_COLOR: "1", TERM: dumb}`. Both
  branches of rung 2 now have a fixture.
- ~~`--help` snapshot fixtures (plan A.5) are not recorded~~ — **closed at
  cutover** by `cli-contract/help-surface`, which records the root listing and
  all 25 subcommand helps; `docs/cli-reference.md` is generated from the same
  commands. The coverage gate still skips `--help` and `--format` as dimensions.
- The coverage gate asserts commands, params, error codes, format specials, and
  modes. Plan A.5 also names a JSON-key dimension and an outcome dimension;
  neither is asserted yet, and mode/param coverage is global rather than
  per-command.
- The `smoke` and `presets-e2e` families named in the plan have no cases yet;
  `smoke.sh` is now overridable (`KHUB=…`) so the family can be added by
  pointing it at either binary.

## D17 — the cutover: the fixtures stop describing Python

**Date 2026-08-16.** The Python implementation is deleted. Up to this point every
recorded byte answered "does Go match Python?", and the four entries below were
filed as divergences because the answer was "no, and here is why that is
correct". With Python gone the question changes to "does Go still do what it did
yesterday?", so those four were re-recorded from the Go binary and their
`divergence:` markers removed. The suite now runs **113 cases, 0 xfail.**

What each of the four actually was, restated as the baseline it became:

| Case | Now recorded as |
|---|---|
| `read-graph/search-basic` | ncruces SQLite's bm25 score (1 ULP from CPython's build) |
| `integrity/err-schema-family` | goccy's parser message inside khub's `schema_error` envelope |
| `collections/malformed-whole-file` | Go's JSON parser message inside the `malformed_entity` envelope |
| `write-path/comment-bearing-edit` | the splice's byte preservation |

The last one is worth naming as an improvement rather than a difference: ruamel
re-emits a document from its CST, so an edit normalized `"plain word"` to plain,
`empty:` to `empty: null`, and a 2-space sequence indent to 0. The token splice
leaves all three exactly as the author wrote them. khub now preserves more of the
file than it used to.

### Evidence the cutover rested on

- 112 fixtures green, unaided, before any of this changed
- differential at the plan's full scale: 50 seeds × 40 verbs, 0 divergences
- `smoke.sh` 75/75 against the binary
- yamlgate t1c 136/136, t2 99/99, T3 1089/1089 on both profiles
- the gates re-run on macOS, which had never been covered by CI

### Gates that moved rather than disappeared

| Retired (Python) | Where its job went |
|---|---|
| `uv run pytest` | `go test ./...` |
| `uv run ruff` / `mypy` | `gofmt`, `go vet`, `golangci-lint` |
| Typer's `docs/cli-reference.md` generator | `parity/tools/gen_cli_reference.sh`, driven by real `--help` output, plus the new `cli-contract/help-surface` fixture |
| pyproject-vs-`__init__` version check | one constant in `internal/version` |
| "wheel carries the skills" | `internal/skills` tests, which already pinned both skill names |
| `-differential` against Python | old-Go vs new-Go across a change |

### Open items this closes, and one it does not

`--help` snapshots are now recorded, closing that item. The EPIPE fixture,
`FORCE_COLOR=1` on a pipe, and the JSON-key/outcome coverage dimensions remain
open — none of them depended on Python.

Colour is now the one place the binary is **worse** than what it replaced: the
recorded human fixtures carry Rich's `ESC[1m`/`ESC[3m` and khub emits none, which
is exactly what `tty-layout-free` has been absorbing in 9 cases. That normalizer
stays until colour lands, at which point those cases get re-recorded and it can
be deleted, byte-pinning the whole suite.

## D18 — Windows is dropped, and D17's fixture claim was overstated

**Date 2026-08-16.** Two corrections a review of the cutover surfaced.

### Windows

`docs/history/go-rewrite-memo.md` §3.3 sold cross-platform reach as a benefit of the
rewrite, and the port plan's first cutover criterion asks for "Windows green
minus explicitly-tagged POSIX-lock cases". Neither is being delivered, and until
now nothing recorded that.

The criterion cannot be met in the sense the others are. khub serializes
collection writes with an advisory lock; on Windows that is `LockFileEx`, which
**no khub has ever shipped or been tested against**, so there is no prior
behaviour to be at parity with — R9 said as much when the risk was first filed.
Every other cutover criterion compares the port against something real. This one
would compare it against nothing.

So `.goreleaser.yml` builds darwin and linux only, `install.sh` refuses any other
`uname -s`, and the criterion is struck rather than left open for someone to
mistake for pending work. Adding Windows later is a feature with its own testing
story, not a box to tick here.

### D17 overstated the deletion commit

D17 said the cutover's fixture diff "must be exactly those 4 cases". The four
re-records were exactly right and no stray expectation moved — that part held.
But the same commit also **added** `cli-contract/help-surface` (26 steps) and
regenerated `docs/cli-reference.md`, because retiring Typer's doc generator left
the `--help` surface unpinned and the two had to land together. The honest claim
is narrower: no EXISTING recorded bytes changed except those four.

### `tests/eval/` should not have been deleted

The cutover removed `tests/` wholesale, taking the agent-wiring eval with it.
The port plan lists `tests/eval/**` under "Not ported … consumes the CLI; runs
against either binary as-is" — which licensed leaving it unported, not deleting
it. Restored, with its two Python couplings replaced: it reads the version from
`internal/version/version.go` instead of `pyproject.toml`, and builds the
checkout into `.eval-bin/` on PATH instead of running `uv tool install --force`
against the operator's global tool directory (which was the more invasive of the
two behaviours anyway).
