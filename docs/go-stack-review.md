# Go stack review — defect register and fix plan

**Method.** 21 research agents across 7 topics (CLI architecture,
serialization, testing, CI/release, code quality, dependencies, agent-facing
design), each with three lenses: landscape, reference implementations read as
source, and an adversarial pass briefed to argue *against* khub's design.
Several agents measured against this tree (marked MEASURED); every defect
below was then verified by hand at the cited location, at commit `e1df59e`.

**Verdict.** The architecture survived adversarial review intact. Agents
briefed to replace `help.go`, `parseGlobals`, the splice writer, the graph,
`omap`, the renderer and the parity harness each concluded *keep* — and in
several areas khub leads the field (no Go YAML library does lossless
round-trip editing; `WantJSON` on non-TTY; minimal `query` records that
github-mcp-server needed a flag to reach; a fixture XFAIL mechanism with no
equivalent in the golden-testing literature). What the review produced instead
is this defect register, the conventions distilled into `.claude/rules/`
(`go.md`, `cli.md`, `testing.md`, `dependencies.md`, `ci-release.md`), and the
staged plan below.

**Corrections to our own beliefs** (all verified): gonum links only 9 leaf
packages, no BLAS — it is not heavy; ncruces migrated off wazero to
`wasm2go`-translated Go; `gofrs/flock` does support Windows (the exclusion is
right for other reasons); the emitter's oracle is the frozen 1083-case corpus,
not ruamel-the-library (ruamel 0.19.0 shipped emitter changes after cutover);
`cli/cli` does use goreleaser (as builder, fanned per-OS for signing).

## Defect register

### D1 — non-finite floats emit invalid JSON · `internal/canon/value.go` · S

`PyFloatRepr` assumes a finite float. `FormatFloat(±Inf/NaN, 'e', -1, 64)`
returns `"+Inf"`/`"NaN"` with no `e`; `strings.Cut` then hands the letters to
the digit-splitter, which inserts a decimal point into them. MEASURED:

```
EncodeCLI(+Inf) → {"x": +.Inf}    EncodeCLI(-Inf) → {"x": -I.nf}
EncodeCLI(NaN)  → {"x": N.aN}
```

Reachable end to end: any field holding `.inf`/`.nan` makes
`khub get --format json` unparseable. Also a parity divergence — Python's
`json.dumps` emits `Infinity`/`NaN`. Fix: decide policy (reject at validation
vs Python's spelling), record it, guard before the `Cut`.

### D2 — `--format json` can still return prose · `internal/cli/render.go` · S

`Guard` returns `*errs.Usage` unhandled (root prints stderr prose, exit 2), so
an agent that passed `--format json` gets a Rich panel and no envelope. Fix:
route `Usage` through `Fail` under `WantJSON`, keeping exit 2.

### D3 — envelope discards the locator it already has · `internal/cli/render.go` · S

`Fail` receives `Located{Type,Relation,Target}` and emits only
`code`+`message`. Add the three keys when non-empty (additive; pin `omap`
order deliberately).

### D4 — column widths count runes, not cells · `internal/cli/root.go:433` · M

`runeLen = len([]rune(s))`. CJK = 2 cells, combining marks/ZWJ = 0 → non-ASCII
titles break box alignment. Rich uses `cell_len`, so this diverges from the
ported original. Fix with `x/text/width` (already a dep). Check first whether
any fixture exercises non-ASCII cells; if none, lands with ~zero churn.

### D5 — `stale`/`backfill` are O(N) subprocesses × O(N) walks · `internal/gitlog/gitlog.go`, `internal/backfill/backfill.go` · M

One full `git log --format=%cd -- <path>` per entity, called in a loop. Fix:
one `git log --name-only --format=%H%x00%cd` pass building a path→date map.
Zero new deps. Measure on a large engagement first to size the win.

### D6 — atomic write never fsyncs the parent dir · `internal/fsio/atomic.go` · S

`AtomicWrite` syncs the file, then `os.Rename` — the rename is not durable
until the directory is synced (loseable on crash, ext4/xfs). Temp name is a
fixed `.tmp`, so exclusion rests on the flock alone; use an `os.CreateTemp`
sibling. ~15 lines. Do NOT touch the deliberate plain-write path for per-item
entities (inode behaviour is intentional, per the package doc).

### D7 — a schema `pattern` can hang the process · `internal/entity/entity.go` (`fullMatch`) · S

regexp2 is backtracking with **no timeout checked unless `MatchTimeout` is
set**; patterns are author-supplied. Set `MatchTimeout` (documented as
near-free). Optional: try stdlib `regexp` first, fall back only when RE2
rejects the construct.

### D8 — Python-rotted shipped strings · S

- `internal/errs/errs.go:208` — "SQLite FTS5 is unavailable in this **Python**
  build" in a CGO-off Go binary.
- `internal/wire/wire.go:196` — "`uv tool install git+ssh://…`" tells users to
  install the retired package, **pinned** in
  `parity/cases/init-wire-skills/wire-targets/expected/steps/02.stdout` —
  the suite faithfully preserves wrong advice. Deliberate re-record.

Comments citing Python modules stay (provenance); shipped strings describing
Python are bugs.

### D9 — dead code contradicting its header · `internal/canon/splice.go:459` · S

`carriesComment`: zero callers (found independently by reading and by
`unused`); the package header still describes the abandoned comment-only
splice policy that lines ~115–123 overrule. Decide: wire into the
`ErrNoSplice` fallback (which can silently drop comments today) or delete and
reconcile the header.

### D10 — Click-fidelity gaps, all fixture-free · `internal/cli/root.go`, `help.go` · S

- `--version=1` / `--help=x` print and exit 0; Click errors
  ("Option '--version' does not take a value.", exit 2).
- `helpWidth()` probes stdout only; Rich probes fds 0,1,2.
- `helpWidth()` lets `COLUMNS` beat `TERM=dumb`; Rich checks dumb first —
  invisible only because the runner pins both widths to 80. Document that
  coupling in `parity/DECISIONS.md` regardless.

### D11 — SIGPIPE disposition leaks into children · `cmd/khub/main.go` · S

`signal.Ignore` sets `SIG_IGN`, inherited across exec — spawned `git` and the
URL opener inherit it. `signal.Notify` gives identical EPIPE-on-stdout
(MEASURED both ways) with clean children (handler resets to `SIG_DFL` on exec).

### D12 — help never checks `IsTTY()` · `internal/cli/help.go` · M

The sole output path that doesn't. MEASURED: with `TTY_COMPATIBLE=0` the
reference doc still holds 6,561 box chars; help pages are 34–58% chrome; the
near-miss stderr fixture spends 621 bytes on 46 chars of payload (13.5×), and
column-79 padding wraps descriptions mid-sentence — the pattern long-context
benchmarks penalize hardest. **Decision taken: gate help output only**; error
prose/tables/trees stay pinned. Deliberate re-record of the help fixtures
(~43), rationale in the commit.

## Staged plan

| Stage | Contents | Fixtures move? |
|---|---|---|
| 1 | D1 D2 D3 D6 D7 D9 D10 D11 + errs.go half of D8 — each small, independent, proven byte-neutral by a clean `parity-run` before commit (D2/D3 add JSON-path bytes only where output is today unparseable or absent) | no (re-record only where D1/D2 policy adds output) |
| 2 | D12 help gate · wire.go half of D8 · D4 if any fixture has non-ASCII cells | yes — one commit each, rationale in message |
| 3 | D5 batched git log (measure first) | no |
| 4 | Test layer, value order: revive `-differential` (654 LOC dead oracle) → 4 canon fuzz targets → gofmt idempotency loop → `-record` safety rails (`CUE_UPDATE=diff`-style preview, content diff) → binary coverage via `GOCOVERDIR` → cross-process lock test → NFC/NFD fixture + normalization policy | no |
| 5 | Lint per `.claude/rules/go.md` (staticcheck −ST1005, errorlint, unused, errcheck+preset; depguard ratchets verified green today: `encoding/json`→canon only, cobra→cli only, cli→cmd only, forbidigo on `fmt.Print*` outside cli) · CI gaps per `.claude/rules/ci-release.md`, starting with immutable releases | no |

Verification for every stage: the CONTRIBUTING.md gates, plus
`./parity-run -bin "$PWD/khub" -cases parity/cases` clean **before** commit
for anything claimed byte-neutral.

## Open questions

1. GHEC? Flips artifact attestations from reject to adopt (private Sigstore,
   no public log).
2. Was retiring `-differential` deliberate at cutover, or an oversight?
3. Is `internal/cli` meant to be embedded (in-process harness)? Flips
   `var state State` from cosmetic to blocking.
4. Do agents actually run `khub --help`, or only what SKILL.md lists? If the
   latter, D12 drops in value and fixing SKILL.md's discovery order
   (`schema types` → `schema show <type>` instead of the ~4.6K-token
   `schema --format json`) rises.
5. Does `internal/reindex` place `okf_version` only at the bundle root, per
   OKF v0.2 — and is khub targeting v0.1 or v0.2? Related: `export --okf`
   must materialize typed relations as body links or the graph is lost to
   OKF-only consumers.
