# Reimplement khub in Go — 1:1 external parity

Status: **EXECUTED**, cut over 2026-08-16 — companion to
[go-rewrite-memo.md](./go-rewrite-memo.md). Baseline it was written against:
khub 0.18.0.

**Appendix A is still live.** It is the enumeration `parity/coverage.yaml` is
written against, and `parity-run -coverage` fails CI on any row without a
fixture. Treat the rest of this document as history and Appendix A as reference.

Three places execution departed from the plan, all recorded in
`parity/DECISIONS.md`:

- **M0 cleared on a different leg** — token-origin splicing, not the AST path
  this plan made primary (D2).
- **Windows is struck, not deferred** (D18). The plan gates cutover on "Windows
  green minus tagged POSIX-lock cases"; that criterion cannot be met the way the
  others are, because `LockFileEx` has no prior khub behaviour to compare
  against.
- **`tests/eval/**` was deleted and restored.** This plan lists it under "not
  ported … runs against either binary as-is", which licensed leaving it
  unported; the cutover commit removed it with the rest of `tests/` and a review
  caught it.

## Context

`docs/go-rewrite-memo.md` (merged as #37, status **proposed, gated**) decides: rewrite khub in Go, contingent on one go/no-go gate — a YAML round-trip prototype (memo §5). The rewrite buys no features; it attacks three operational pains: install friction (`uv tool install git+ssh://…` on every machine/agent container), agent-loop latency (150–300 ms interpreter startup × rebuild-per-call design), and the POSIX-only `fcntl.flock` collection lock. Payoff: ~10 MB static binary, 5–10 ms cold start, 6-target release matrix, `go install`/Homebrew/npm-wrapped distribution.

**Scope**: external API and interface one-to-one identical to Python khub 0.18.0 —

- Byte-pinned: every `--format json` stdout document, error envelope `{"error":{"code","message"}}`, exit codes (0/1/2), on-disk file bytes (frontmatter, YAML/JSON/JSONL entities, collections, `.khub/schema.yaml`, wire blocks, skills), stream routing (JSON errors → stdout, prose errors → stderr).
- Content-pinned (decided): human TTY output — lines, values, order, literals (`True`/`False`, `✓ — → …`) assert; box-drawing geometry does not. lipgloss tables, no Rich byte-emulation.
- Layout (decided): **same repo, `go.mod` at repo root**, `cmd/khub/` alongside `src/khub/`, dual-ship in one history.

**Full-coverage rule**: the API is ported in full, no exceptions — Appendix A enumerates every command, flag, JSON contract, error code, env behavior, and on-disk surface, and the M1 coverage gate (`parity-run -coverage`) mechanically fails CI until every row has fixtures on both implementations.

Baseline at 0.18.0, verified against source: 21 core modules + 15 CLI files (~7.5k lines), 22 commands + `schema` sub-app, `LocatedError` taxonomy (23 factories + `os_error`) with byte-exact message templates, output gate in `cli/_render.py` (`want_json = fmt=="json" or not is_tty()`; `is_tty` = Rich ladder `TTY_COMPATIBLE` → `FORCE_COLOR` → `isatty`), `BrokenPipeError` re-raised, `json.dumps(data, default=str)` on the emit path.

## Phase 0 — M0: the go/no-go YAML gate (memo §5; nothing else starts first)

Throwaway prototype `parity/yamlgate/` (Go). Three tests per corpus file:

- **T1 no-op round-trip**: parse → re-emit → require zero byte diff. Run both goccy modes: (a) convenience (`MapSlice`) and (b) AST (`parser.ParseBytes(src, parser.ParseComments)` → render). If only (b) passes, the AST path is promoted from escape hatch to the primary write path.
- **T2 surgical edit**: set `updated:` on the mapping (or one collection row); every diff hunk must touch only that key's line(s). Comments, key order, sibling rows byte-preserved.
- **T3 emit-from-scratch differential**: ~10k randomized meta maps (strings incl. trailing `\n`, dates, bools, nulls, lists, nested maps, >80-col strings) — Python ruamel dump (width-80 RT profile AND width-4096 init profile) vs the Go emitter. ruamel's quirks (fold-at-80 with trailing space before the fold, 2-space continuation, dash-offset-0 sequences, `{}` empty maps, double-quoted `\n`-bearing strings) will not fall out of goccy's encoder — plan is a small hand-written emitter (`internal/canon/yamlio.go`) over goccy's AST for khub's value shapes; T3 gates it.

Corpus: `canonical/` (scripted from Python 0.18.0: init all 3 presets, add/edit/link across every layout×format cell — must be 100% zero-diff), `annotated/` (hand-built comment-bearing frontmatter, yaml-collection header/inter-row/trailing comments, flow-style-with-comment — goccy's documented hole, anchors on input, `yes/no/on/off` scalars, block scalars), `real/` (gitignored; point at a live workspace — the memo's "grep a real workspace" made executable).

**GO**: T1(AST)+T2 zero-diff on 100% of canonical+real; annotated comment classes preserved (convenience or AST, documented per class); T3 byte-equal (failures fixed, not waived). **NO-GO**: comment loss on files that matter even via direct AST manipulation → TypeScript fallback per memo; the rest of this plan does not start. Output: one-page `parity/yamlgate/REPORT.md`, committed.

## Go architecture

Module `github.com/endgame-build/khub`, Go 1.23+, CGO off everywhere (preserves goreleaser trivial cross-compile). Everything under `internal/` — nothing importable, mirroring Python's only-`__version__`-public stance.

```
cmd/khub/main.go                  # calls internal/cli.Execute(); error → exit code

internal/version/version.go       # const Version = "0.18.0"; -ldflags override at release

# Layer 1 — serialization canon (the ONLY I/O dialect package)
internal/omap/                    # insertion-ordered map[string]any; the universal record type
internal/canon/frontmatter.go     # ~15-line "---" splitter + render (exact error strings)
internal/canon/yamlio.go          # goccy AST load (comments kept, duplicate-key = hard error);
                                  # DumpRT (ruamel width-80 profile), DumpWide (width-4096 init profile)
internal/canon/jsonio.go          # three Python dialects: CLI stdout (", "/": ", \uXXXX ensure_ascii,
                                  # NO <>& escaping, default=str), Disk (indent 2, raw UTF-8, trailing \n),
                                  # JSONL row (slug first, spaced separators)
internal/canon/collection.go      # load/dump_collection, render_row, PopBody, FTSBody
internal/fsio/atomic.go           # temp-sibling + fsync + rename (os.replace parity), dir fsync
internal/fsio/lock.go             # gofrs/flock on .khub/generated/locks/<type>.lock

# Layer 2 — ontology
internal/schema/vocab.go          # SchemaFile/BaseBlock/AttrDecl/RelationDecl/TypeDecl/IdPrefixDecl;
                                  # tri-state *bool + raw-key presence set (model_fields_set analog)
internal/schema/vocab.schema.json # go:embed JSON Schema of the vocabulary (additionalProperties:false)
internal/schema/validate.go       # santhosh-tekuri/jsonschema v6 + Go code for the cross-field
                                  # storage matrix; error → raw_linkml_smuggled etc.
internal/schema/resolve.go        # base merge (attr facet-merge vs relation whole-replacement)
internal/schema/model.go          # ResolvedSchema/Type/Attribute/Relation/IdPrefix/StorageConfig
internal/values/values.go         # BOOLISH, AsBool, IsNumber (reject inf/nan/1e999), IsDateish, Present
internal/errs/errs.go             # Located{Code,Message,Type,Relation,Target}, all factories
                                  # byte-exact; Usage error type

# Layer 3 — core verbs
internal/workspace/               # locate.go (walk-up find), init.go (ordered steps, rollback,
                                  # gitignore), presets.go (registry over fs.FS; --preset-source = os.DirFS)
internal/template/                # BodyTemplate load/render/heading contract; fence stripper is a
                                  # hand-rolled line scanner (the Python _FENCE regex uses a
                                  # backreference — RE2 cannot compile it; verified template.py:45)
internal/entity/                  # entity.go create/update/link/unlink/delete + meta key canon
                                  # (type,created,updated,draft, attrs-in-schema-order, rels, extras);
                                  # slug.go (slugify, 100-char base, minting, ordinals);
                                  # resolve.go (resolve_id, ambiguity); doc.go (RT read/write,
                                  # _mutate_collection: lock → re-read → mutate → atomic swap)

# Layer 4 — derived projections (graph DERIVED, never stored)
internal/index/                   # scan, canonical_slug (casefold), resolve_target fan-out,
                                  # stray/malformed containment
internal/graph/                   # OWN ordered adjacency (insertion order; walk determinism is
                                  # contract) + gonum multi.DirectedGraph projection via
                                  # NodeID↔int64 bimap; cycles.go = topo.DirectedCyclesIn (Johnson)
internal/query/  internal/search/ # filter ladder; ncruces/go-sqlite3 :memory: FTS5, exact DDL+bm25 SQL
internal/integrity/validate.go    # per-entity ladder (distinct gate)
internal/integrity/check.go       # graph-wide: incomplete/orphans/dangling/strays/misplaced/
                                  # cycles/singleton lists/suppressed_dangling (distinct gate)
internal/project/ internal/gitlog/ internal/reindex/ internal/viz/ internal/backfill/
internal/introspect/ internal/wire/ internal/skill/

# Layer 5 — thin CLI adapter (zero per-type code)
internal/cli/root.go              # cobra: -C/--workspace, eager --version, registration order
                                  # (init, status, add, get, edit, link, unlink, remove, query, search,
                                  # neighbors, impact, history, validate, check, stale, reindex, viz,
                                  # backfill, wire, install-skills, schema); no completion cmds;
                                  # bare khub → help on stderr, exit 2
internal/cli/render.go            # the gate: IsTTY (TTY_COMPATIBLE→FORCE_COLOR→isatty), WantJSON,
                                  # Emit, Guard, Fail (stdout/stderr asymmetry), EPIPE swallow
internal/cli/fields.go            # parse_fields: --k v | --k=v | bare k v; '-'→'_' keys;
                                  # trailing key → usage exit 2
internal/cli/table.go             # lipgloss tables (content-pinned, not Rich-byte)
internal/cli/{entity,query,search,graph,integrity,gitlog,projection,status,
              schema,init,wire,skill,backfill}.go    # one file per *_cmd.py

internal/presets/embed.go         # //go:embed of the preset tree (embed reaches src/khub/presets
                                  # from a root-adjacent package, or presets move to /presets at
                                  # cutover — during dual-ship Python's copy stays authoritative)
internal/skills/embed.go          # embedded skills FS (wheel force-include replacement)
internal/assets/embed.go          # cytoscape.min.js
```

**Choke-point rule (lint-enforced, depguard):** only `internal/canon` imports goccy; only `canon/jsonio.go` marshals JSON; no `map[string]any` in exported signatures — `omap.Map` everywhere. Key order is contract, not cosmetics: JSON record field order, frontmatter meta canon, schema declaration order (status counts, reindex sections), extras in CLI input order.

**Dynamic data:** zero per-entity-type Go code (the schema-generic invariant). Compile-time structs exist only for the vocabulary meta-model and resolved/report shapes. Entity data = `omap.Map` always. Vocabulary validation = embedded JSON Schema (jsonschema v6) + Go storage-matrix checks with exact messages. Entity-value validation = the hand-rolled short-circuit ladder (enum → pattern → bool → number → date → list) driven by `ResolvedAttribute` — messages and order are contract, so no jsonschema here. User `pattern:` values compile via `dlclark/regexp2` (Python flavor) wrapped `\A(?:pat)\z` to emulate `fullmatch`.

**Python module → Go map (highlights; full tree above):** `cli/_render.py`→`internal/cli/render.go`; `core/formats.py`→`internal/canon/*`; `core/schema_model.py`(pydantic)→`internal/schema/vocab.go`+`vocab.schema.json`; `core/entity.py`→`internal/entity/*` (4 files); `core/graph.py`→`internal/graph/*`; `core/integrity.py`→`internal/integrity/{validate,check}.go` (never merged — distinct gates); each `cli/*_cmd.py`→ same-named `internal/cli/*.go`.

**Error → exit mapping:** success + idempotent no-ops + empty results = 0; `errs.Located`/OS errors via Guard (JSON envelope stdout | prose stderr) = 1; `validate` not-ok / `check` not-passed / prose remove-refusal = 1; usage (missing args with hand-crafted messages like `"Missing argument 'TYPE' (e.g. project)."`, unknown flag/command, bare `khub` help→stderr) = 2 — cobra's default exit-1-on-usage must be overridden with a custom classifier + `SilenceErrors`.

**Invariants, mapped to enforcement:** schema-generic surfaces (bespoke-schema sentinel fixtures; no per-type code review gate); graph derived never stored (exhaustive tree manifest — any invented cache file fails the diff); validate ≠ check (separate packages, separate report shapes, separate fixtures); draft manual (no verb mutates `draft` except explicit flag/field — sentinel fixture); capture never blocked (add with missing required exits 0 and writes; referential integrity alone hard-fails pre-write — paired sentinel fixtures).

## Parity harness (`parity/`)

```
parity/cases/<family>/<case>/case.yaml      # setup (preset|literal workspace, scripted git), mode, steps
                         /expected/steps/NN.{stdout,stderr,exit}   # raw bytes per step, streams separate
                         /expected/tree.manifest                   # "sha256 exec-bit relpath" sorted;
                                                                   # excludes .git/** and .khub/generated/**
parity/runner/            # Go parity-run: -bin <anything> -record|-verify; PTY mode via creack/pty
parity/tools/record.sh    # runs recorder against Python khub from requirements.lock-pinned venv
parity/tools/pyshim/      # sitecustomize.py freezes date.today via KHUB_PARITY_NOW
parity/corpus/ + yamlgate/
parity/DECISIONS.md       # every accepted diff, reviewed; the normalizer never grows silently
```

- **Recorder:** pinned env (`TZ=UTC LANG=C.UTF-8 LC_ALL=C.UTF-8 COLUMNS=80 HOME/XDG → tmp, umask 022`, `GIT_AUTHOR_DATE`/`GIT_COMMITTER_DATE` per recipe). Clock seam: recorder shim pins Python's `date.today()`; Go reads the same `KHUB_PARITY_NOW` env through one `internal/clock.Now()` seam, active only when set. Expectations committed; re-record only via reviewed diff — the recorded bytes ARE the spec.
- **Modes:** `pipe` (default; no TTY → JSON everywhere incl. write verbs and errors), `human` (`TTY_COMPATIBLE=1 NO_COLOR=1 COLUMNS=80` → deterministic prose), `pty` (real PTY, no env hints — proves the isatty leg + `khub schema | head -1` EPIPE pass-through).
- **Normalization policy:** machine channels byte-identical, two named rules only: `cycles-canonical` (rotate each cycle to min `type/slug`, sort list — Johnson enumeration order is implementation-defined; membership+count exact) and `tty-layout-free` (human/pty channels: strip ANSI + box-drawing + inter-cell padding, compare cell tokens, row order, literals). Explicitly rejected: JSON key-order, separator, ensure_ascii, float-format, trailing-newline normalization — all byte-pinned (float repr additionally pinned by a 10k-double differential fuzz, Python `repr` vs `strconv.FormatFloat('g',-1,64)`, divergences fixed not normalized).
- **Families** (frozen from Python 0.18.0 before each milestone; sources: the pytest suite, `smoke.sh`'s 73 assertions, getting-started transcripts): `codec-json`, `codec-yaml`, `cli-contract` (gate, envelope, exit trichotomy, missing-arg strings, removed surfaces stay exit-2, `--version`), `read-graph`, `write-path`, `collections`, `integrity`, `git-stale`, `init-wire-skills`, `tty-prose` (content-pinned), `smoke`, `presets-e2e`, `schema-generic` + `invariants` sentinels (5 cases, one per invariant).
- **pytest mapping:** replaced-by-fixtures (all CLI-driven families: cli_contract, entity_*, query, search, neighbors, impact_history, status, schema, projection, wire, skill, build_*, firm_ops, gitlog, smoke.sh); hand-translated Go unit tests (resolve_vocab, resolve_base_merge — tri-state `*bool` inheritance, resolve_targets, id_prefix, formats, format_entities, hardening files, ruamel-specific collection assertions, TTY-gate precedence, rollback/failure-injection idioms); dropped (`tests/eval/**` — agent-behavior eval, consumes the CLI so it runs against either binary unported).
- **smoke.sh:** one-line change — `KHUB="${KHUB:-uv run --project $REPO khub}"` (today hard-coded at `smoke.sh:11`) so the same script drives both binaries.

## Milestones (dependency-ordered; acceptance = named families green; no calendar estimates)

| M | Lands | Accepts |
|---|---|---|
| **M0** | yamlgate prototype + corpus | GO/NO-GO per Phase 0; `REPORT.md` committed |
| **M1** | `parity/runner` + recorder + full fixture corpus recorded from Python | every family passes 100% against **Python** (proves the harness); streams pinned separately (CliRunner never did); **coverage gate green: every Appendix A row — each command × mode (pipe/human) × outcome (success/error/usage), each flag, each format special, each JSON key, each error code — maps to ≥1 recorded fixture; `parity-run -coverage` computes this from the appendix table and FAILS CI on any gap** |
| **M2** | foundation kernel: `omap`, `canon` (3 JSON dialects, 2 YAML profiles, duplicate-key-strict loader), `fsio`, `values`, `errs`, `cli/render` gate; no commands | `codec-json` + `codec-yaml` byte-exact via test shim; `values.py` table tests ported |
| **M3** | `schema` (vocab+validate+resolve+model), `index`, `graph`, `query`, `search`, `workspace/locate`, `project`, `introspect`; commands: root, `schema`(+types/show/edges), `status`, `get`(+`--edges`,`raw`), `query`(+`ids`), `search`, `neighbors`, `impact`, `history` | `cli-contract`(read subset), `read-graph`, `tty-prose`(read) green on Go |
| **M4** | `entity` (meta canon, slug minting, O_EXCL), `fsio/lock` in anger, `template`; commands: `add`, `edit`, `link`, `unlink`, `remove` | `write-path`, `collections` byte-exact on stdout AND trees; M0 corpus re-run through real verbs (edit-one-field → rest untouched) |
| **M5** | `integrity` (validate ladder + check report + gonum cycles), `gitlog` (subprocess), `reindex` (incl. `difflib.unified_diff`-format port), `viz`, `backfill`; commands: `validate`, `check`, `stale`, `reindex`, `viz`, `backfill` | `integrity`, `git-stale` green; cycle fixtures via `cycles-canonical` |
| **M6** | `workspace/init` (ordered steps, rollback), `wire`, `skill`, all embeds; commands: `init`, `wire`, `install-skills` | `init-wire-skills` byte-exact (schema.yaml, config.yaml, singletons, wire blocks, gitignore lines); remaining `cli-contract` |
| **M7** | dual-run CI, differential fuzz mode, goreleaser (6 targets, CGO off), Homebrew tap, npm wrapper; docs rewrite (install paths) here, not before | all families + `smoke` green on Go; cutover criteria below |

## Risk register (Python↔Go gaps that silently break parity; bites / detected / mitigation)

- **R1 regex flavor**: `template.py:45` `_FENCE` uses a backreference (RE2-fatal, verified); user `pattern:` may use lookarounds; `fullmatch`≠`MatchString`. → `integrity` family + a pattern-flavor fixture. Hand-rolled fence line-scanner; `regexp2` wrapped `\A(?:…)\z`.
- **R2 JSON encoding**: Go `encoding/json` escapes `<>&`, no spaced separators, raw UTF-8 — three Python dialects all differ. → `codec-json`. Never call `encoding/json` on output paths; `canon/jsonio` hand-writes tokens over `omap`; float-repr differential fuzz.
- **R3 YAML 1.1 vs 1.2 scalars**: `yes/no/on/off`, octals, sexagesimals, unquoted dates must resolve exactly as ruamel does (affects `fts_body`: non-string values are NOT indexed). → `codec-yaml` scalar corpus. Pin a resolver table in `canon/yamlio`.
- **R4 emitter fidelity**: ruamel's 80-col fold w/ trailing space, 2-space continuation, dash-offset-0; init profile width 4096. → M0 gate + `codec-yaml` + on-disk compares. Own the emitter over goccy AST.
- **R5 insertion-order reliance**: key order is contract everywhere; Go maps randomized. → every byte family. `omap` mandatory at all API boundaries (lint).
- **R6 casefold vs ToLower**: `canonical_slug`/target matching use `str.casefold()` ('ß'→'ss'); `slugify` uses `lower()`; link `changed` compares case-sensitively. → Unicode fixture set (ß/İ/Café) recorded from Python. `x/text` Fold at casefold sites only.
- **R7 sort semantics**: composite sorts are tuple-compares (`impact` by `(depth,(type,slug))`; `stale` reverse-tuple) — never joined-string compares. → impact/stale tie fixtures.
- **R8 path separators**: JSON `path` values, locators `file#slug`, messages embed `/`-relative paths. → all byte families on a Windows runner in M7. `filepath.ToSlash` at the boundary; slash-join semantics internally.
- **R9 flock vs fcntl**: gofrs/flock = flock on Unix (same lock table — Go and Python interoperate during dual-ship), LockFileEx on Windows (new territory, no Python precedent). → explicit two-process race harness in `collections` (byte-diff can't catch this). Keep lock → in-lock re-read → tmp/fsync/rename verbatim.
- **R10 FTS5 drift**: ncruces pins one SQLite; CPython's varies — snippet windowing, bm25 ties (broken by rowid = insertion), `bad_search_query` embeds SQLite's own version-dependent detail string. → search fixtures (diacritics, ties, invalid queries). Pin expectations to ncruces where SQLite's own text leaks; record the accepted delta in `DECISIONS.md`; keep DDL/SQL byte-identical; deterministic rowid insertion order.
- **R11 git subprocess env**: Python inherits the full env (no scrubbing — verified) and `--format=%cd --date=short`. → `git-stale` under a scrubbed-then-polluted env matrix in dual-run. Replicate inheritance exactly; strict `YYYY-MM-DD` parse; rc≠0/empty → nil as Python.
- **R12 TTY gate + rendering**: Rich defines the gate itself (`TTY_COMPATIBLE`→`FORCE_COLOR`→isatty, `NO_COLOR`/`TERM=dumb`); isatty-only flips the whole suite. → gate unit tests + pty fixtures. Port the ladder exactly; tables content-pinned per decision.
- **R13 number semantics**: Go `ParseFloat` accepts `inf`/`1e999`→+Inf where Python's ladder rejects; unbounded Python ints (30-digit frontmatter int must round-trip). → number fixtures + big-int round-trip. Explicit IsInf/IsNaN rejection; big-int-preserving scalar in `canon`.
- **R14 cycle enumeration**: same Johnson algorithm, different rotation/order. → cycle fixtures via `cycles-canonical` normalizer; canonicalize before rendering.

Backstop for everything: the M7 differential job — random verb sequences (grammar over the CLI) run against both binaries on identical seed workspaces, diffing stdout/stderr/exit/tree actual-vs-actual; every divergence is minimized into a committed golden case before its fix merges.

## Cutover criteria (objective, all required) and release

1. 100% golden cases green on darwin/linux amd64+arm64; Windows green minus explicitly-tagged POSIX-lock cases.
2. Differential mode: zero unexplained divergences across 50 seeds × 40-verb sequences on every merge since the last divergence fix.
3. yamlgate green on canonical + annotated + a fresh real-workspace snapshot.
4. All hand-translated Go tests green; `smoke.sh` green with `KHUB=./khub`.
5. `parity/DECISIONS.md` has no open entries; the 5 invariant sentinels green.

Then: Python side tagged and frozen, recorder archived, `parity/` retained as the permanent Go regression suite; goreleaser matrix + Homebrew tap + npm-wrapped binary ship; docs install paths rewritten.

## Out of scope until cutover (memo §9 — hedge, not scope creep)

`khub schema infer`/`schema diff` (clean-room; the AGPL reference is never read side-by-side), mdschema body-structure schemas, published vocabulary JSON Schema as editor DX, glamour markdown report rendering (a NEW format value, never a change to existing gates), pinned session hook + `khub check` CI template. Removed/planned surfaces (`log`, `path`, `build`, `export --okf`, `diff-preset`, `rename`, `validate --fix`, `--agent`, `neighbors --both`, completion flags) stay exit-2 forever.

## Verification

1. **M0**: `go run ./parity/yamlgate -corpus parity/corpus` → REPORT.md; decision recorded before anything else starts.
2. **Harness sanity (M1)**: `parity-run -bin "python -m khub.cli.main" -verify` → 100% (the spec describes Python).
3. **Every milestone**: `parity-run -bin ./khub` for that milestone's families + `go test ./...` + `uv run pytest` (Python must stay green — dual-ship).
4. **M7**: dual CI (both binaries, same recipes), `parity-run -differential`, `KHUB=./khub bash smoke.sh`, Windows runner.
5. **Cutover**: checklist above, each item a CI job, no judgment calls.

## Appendix A — Complete API surface (no exceptions; every row gets fixtures, the coverage gate enforces it)

Extracted from `cli/main.py`, all 15 `*_cmd.py`, `cli/_render.py`, and `docs/cli.md` at 0.18.0. This is the normative port checklist: the Go binary reproduces every row; `parity-run -coverage` fails CI if any row lacks a fixture.

### A.1 Entry point and global surface

- Binary `khub` (= `khub.cli.main:main`). Help text `"khub — schema-bound context management."`. Bare `khub` → help on **stdout**, exit **2** (the recorded fixture is the
contract; an earlier draft of this line said stderr — see parity/DECISIONS.md D8). No completion commands exist (`add_completion=False`) — `--install-completion`/`--show-completion` must NOT exist.
- Global (before the command name): `-C, --workspace <path>` (walk `path` + parents for `.khub/`; none → `no_workspace` error), `--version` (eager: prints bare version + `\n`, exit 0, works outside any workspace), `--help`.
- Output gate: `want_json(fmt) = fmt=="json" or not is_tty()`; `is_tty()` = Rich `Console().is_terminal` = `TTY_COMPATIBLE` ("0"→false, "1"→true) → `FORCE_COLOR` (any non-empty → true) → `stdout.isatty()`. Applies to writes too. `--format` is an **unvalidated free string** — unknown values fall through the gate (no error). Specials: `ids` (query, search — bare slugs, bypasses gate), `raw` (get — `view.raw`, no trailing newline, bypasses emit), `tree` (impact), `json` (everywhere).
- Success emit: `json.dumps(data, default=str)` — separators `", "`/`": "`, `ensure_ascii=True`, no HTML escaping — one line + `\n`.
- Failure: `guard` catches `LocatedError` → `_fail`: if `want_json` → `{"error":{"code","message"}}` on **stdout**; else message on **stderr**; exit **1**. `OSError` → same with code `os_error`, message `"{TypeName}: {err}"`. `BrokenPipeError` re-raised (never rendered). Commands without `--format` (`wire`, `reindex`, `viz`, `backfill`) still emit the JSON envelope on a pipe (guard sees fmt=None → gate on TTY).
- Usage errors, exit **2**, plain text: unknown option ("No such option"), unknown command ("No such command"), `BadParameter`, hand-crafted missing-arg echoes (below), bare `khub`.
- Dynamic fields (`add`, `edit`, `query`; `allow_extra_args + ignore_unknown_options`): `parse_fields` accepts `--k v`, `--k=v`, bare `k v`; `-`→`_` in KEYS only; trailing key without value → `BadParameter("Field '{k}' has no value")` exit 2; insertion-ordered result.
- Registration order (help listing): init, status, add, get, edit, link, unlink, remove, query, search, neighbors, impact, history, validate, check, stale, reindex, viz, backfill, wire, install-skills, schema.

### A.2 Commands — args, options (defaults), JSON contract, exit behavior

**init `[PRESET] [PATH=.]`** — `--preset-source <path>`, `--name <str>`, `--force`, `--no-wire`, `--format`. Missing PRESET → stderr `"Missing argument 'PRESET' (e.g. firm-ops)."` exit 2. JSON: `path, preset, version, name, source, entity_files_modified, seeded_over_corpus, singletons_created, preserved` [+ `wire: [{path, action}]`] [+ `wire_error`] + `skill_hint` (`"khub install-skills"`, or `"khub -C {path} install-skills"` when PATH ≠ cwd). Wire tail runs unless `--no-wire`; a wire failure is caught into `wire_error`, never unwinds. Human: `"Initialized {preset} workspace at {path}"` / `"…; {n} entity files modified"`, `"created singletons: …"`, `"preserved {n} workspace-owned file(s): a, b, c"` (+ ` …` beyond 3), per-wire `"{action} {name}"`, `"wire skipped: {err}"` on stderr, then the skill hint block.

**status** — `--format`. JSON: `counts` (per-type, schema declaration order), `total, draft, active, orphan, stale, okf_conformant` [+ `stray`] [+ `malformed`]. Human: `total==0` → `"Workspace initialized; no entities yet"`; else table `status` (type/count, section: `draft / active` = `"{d} / {a}"`, orphan, stale, [stray], [malformed], `OKF-conformant` = `yes|no`).

**add `[TYPE]`** (dynamic) — `--id <slug>`, `--draft`, `--strict`, `--body <str>`, `--body-file <path>` (`-` = stdin; with `--body` → `BadParameter("Pass --body or --body-file, not both")` exit 2), `--no-template` (refused on templated types), `--format`. Missing TYPE → `"Missing argument 'TYPE' (e.g. project)."` exit 2. Minting: `<prefix>-NNN-<slug>` when `id_prefix` declared else `NNN-<slug>`; explicit `--id` verbatim (collision → error; minting auto-suffixes). Singleton slug = type name. JSON: `id ("type/slug"), type, slug, path, draft` [+ `locator` for collection rows]. Human: relpath line, then `"Created {type} '{slug}' ({draft|active})"`.

**get `[ID]`** — `--edges`, `--format` (`json|table|raw|text`). Missing ID → `"Missing argument 'ID'."` exit 2. JSON: `id, type, slug, path, frontmatter, body` [+ `locator`] [+ `edges: [{predicate, target, derived}]` only when `--edges`]. `raw`: exact stored bytes (collection: just the row), `nl=False`. Human: table `{type}/{slug}` (field/value rows, section per predicate `"{p} ({derived|stored})"`, section `body` if non-empty).

**edit `[ID]`** (dynamic) — `--strict`, `--body` (`''` clears; absent = unchanged), `--body-file`, `--format`. Bumps `updated`, re-validates. JSON: same record as add. Human: `"Updated {type} '{slug}'"`.

**link / unlink `[ID] [PREDICATE] [TARGET]`** — `--format`. Any arg missing → `"Provide ID PREDICATE TARGET."` exit 2. Idempotent, exit 0 both ways. JSON: `id, type, slug, predicate, target, changed`. Human link: `"Linked {slug} --{p}--> {t}"` | `"Edge already present"`; unlink: `"Unlinked {slug} --{p}--> {t}"` | `"No edge {p} -> {t} on {slug}"`.

**remove `[ID]`** — `--force`, `--format`. JSON: `{id, type, slug, removed: true}`. Human: `"Removed {type} '{slug}'"`. Refusal (inbound edges, no `--force`): JSON mode → standard `inbound_edge_refusal` envelope; TTY → message + one stderr line per edge `"  {src_type}/{src_slug} --{predicate}-->"`; exit 1.

**query** (dynamic) — `--type`, `--tag`, `--has <name>`, `--missing <name>` (relation, declared inverse, OR attribute — absent/null/empty counts as missing), `--orphan`, `--stale`, `--draft`, `--active`, `--limit <int>`, `--format` (`text|json|ids`). JSON: array of `{id, type, slug, title, draft, orphan, stale}`. `ids`: bare slugs. Human: `"No entities match"` | table `query` (id, type, title, draft, orphan, stale; bools as `True`/`False`).

**search `TEXT`** — `--type`, `--limit <int>=20`, `--format` (`text|json|ids`). Raw FTS5 MATCH passes through (terms, `"phrases"`, `OR`, `NEAR`, `prefix*`). JSON: array of `{id, type, slug, title, score, snippet, path}` [+ `locator`]; no draft/orphan/stale keys. Human: `"No entities match"` | table `search` (id, title, snippet).

**neighbors `ID`** — `--predicate`, `--in`, `--out` (neither = both; `--both` was removed → exit 2), `--depth <int>=1`, `--format`. JSON: array of `{id, type, slug, predicate, direction, derived, depth}`. Human: `"No neighbors"` | table `neighbors` (id, predicate, direction, depth).

**impact `ID`** — `--predicate="depends_on"`, `--reverse`, `--format` (tree|json). **Own gate**: `fmt=="json" or (fmt!="tree" and not is_terminal)` → plain `json.dumps` (NO `default=str`). JSON: array of `{id, type, slug, depth}`. Tree: `"  "*depth + "{type}/{slug}"`; ≤1 node → `"No downstream impact"`. `--format tree` forces tree on a pipe.

**history `ID`** — `--predicate="supersedes"`, `--limit <int>`, `--format`. JSON: array of `{id, type, slug, superseded_by}`. Human: `"No supersession history"` | table `history` (id, superseded_by; null → `—` U+2014).

**validate `[TARGET=all]`** — `--strict`, `--format`. JSON: `{count, errors: [{id, type, slug, field, reason}]}` (NO `fixed` key). Human: per-error `"{id}: {field}: {reason}"` then `"Validated {count} entities; {n} errors"`. Emits report first, THEN exit 1 when errors exist.

**check** — `--strict`, `--format`. JSON key order fixed: `passed, incomplete[{id,type,slug,missing_fields,missing_relations}], orphans[], dangling[{id,type,slug,predicate,target}], strays[], misplaced[{path,type,expected}], malformed[], cycles[[]], suppressed_dangling, missing_singletons[], draft_singletons[], draft_required_singletons[], strict`. Human passed: informational orphan/draft-singleton lines then `"Graph check passed"`. Human failed, fixed line order: incomplete → dangling → orphans → strays → misplaced (`"… — no command can see it"`) → malformed → `"({n} dangling edges suppressed pending the malformed collection fix)"` → `"cycle {a -> b -> c}"` → missing/draft singletons. Orphans fail the gate only under `--strict`; `orphan: true` types exempt entirely. Exit 1 when `passed=false`.

**stale** — `--days <int>` (default = workspace `stale_days`, 90 in firm-ops), `--format`. JSON: array of `{id, type, slug, effective_date, age, source}`. Human: optional first line `"No git history; using updated field only"`, then `"No stale entities"` | table `stale`.

**reindex** — `--dry-run`; NO `--format` (errors still gate to JSON envelope). Dry-run: unified diff verbatim (`nl=False`) | `"index.md is up to date"`. Real: `"Reindexed {n} entities into index.md"` | `"Reindexed 0 entities"`.

**viz** — `--out <str>="viz.html"`, `--open` (opens `file://` URI), `--type <str>`; NO `--format`. Prints `"Wrote {out} ({n} nodes, {m} edges)"` (raw `--out` string).

**backfill** — `--type <str>`, `--dry-run`; NO `--format`, text only. Dry-run: per entity `"{id}: {f1, f2}"` (first-seen order) | `"No changes"`. Real: `"No git history; dates not backfilled"` | `"Backfilled dates on {n} entities"`, then `"Scaffolded frontmatter on {n} entities"` if any. Both end with `"Skipped collection types ({a, b}): row-level git dates land with row-diff attribution"` when applicable.

**wire** — `--target <claude|agents|both>` (invalid → `bad_target`; omitted = update-existing-only mode), `--dry-run`; NO `--format`. Dry-run: preview text. Real: `"{created|updated|unchanged} {filename}"` per file. Bare `wire` writes both `CLAUDE.md` and `AGENTS.md`, creating missing ones. Idempotent, minimal-diff managed blocks.

**install-skills** — `--target` repeatable (`claude|agents|opencode`, default all three), `--skill` repeatable, `--global` (no workspace required), `--dry-run`, `--format`. JSON: `{scope: "project"|"global", skills[], dry_run, writes: [{path, action}]}`. Human: `"No skills to install"` | table `install-skills ({scope})` + `"{would write|wrote} {changed} of {total} files ({scope} scope)"`. Project scope gitignores the three skill dirs.

**schema** (sub-app, `invoke_without_command=True`, help `"Introspect the active schema."`; each takes `--format`):
- `schema` → `{provenance: {preset, version}, types: [type_view…]}`; table `schema: {preset}@{version}` (type, fields, relations, layout).
- `schema types` → JSON array of names; table `types`.
- `schema show TYPE` → `{name, layout, format, path, required, orphan, when, fields: [{name, type, required, enum, pattern, default}], relations: [{predicate, to[], kind, many, required, inverse, acyclic}]}`; unknown → `unknown_type`. Table `{name} ({layout})` — required as `✓`, relations as `"→ " + targets`.
- `schema edges` → JSON array of `{predicate, from[], to[], kind, many, required, inverse, acyclic, derived: false}`; table `edges` (`card` = `many|one`).

### A.3 Error codes (all byte-exact messages; each gets a triggering fixture)

`unknown_relation_target, missing_base, raw_linkml_smuggled, duplicate_type, unknown_preset, target_not_empty, unknown_type, no_workspace, bad_target, referential_integrity, invalid_slug, strict_unknown_field, enum_violation, pattern_violation, number_violation, lookup_error, filter_error, fts_unavailable, bad_search_query, ambiguity_error, illegal_predicate, cardinality_violation, inbound_edge_refusal, os_error` (+ inline codes surfaced by write/init paths, e.g. `slug_taken`, `singleton_exists`, `malformed_projection` — enumerated during M1 recording; the coverage gate counts codes from `errs.go`, so none can be missed).

### A.4 Environment and non-CLI surface

- Env: `TTY_COMPATIBLE`, `FORCE_COLOR`, `NO_COLOR`/`TERM=dumb` (via Rich), `COLUMNS`; git env inherited un-scrubbed (R11); `KHUB_PARITY_NOW` (test-only clock seam, both implementations).
- On-disk surfaces (byte contracts): entity files in all layout×format cells; `.khub/schema.yaml`, `.khub/config.yaml`, `.khub/templates/*.yaml`, `.khub/generated/` gitignore; `index.md` (reindex); `viz.html`; wire blocks in `CLAUDE.md`/`AGENTS.md`; installed skill files; lock files under `.khub/generated/locks/`.
- Version: single `0.18.x`-series constant (Go: `internal/version` + `-ldflags`); Python keeps `pyproject` + `__init__` in sync manually during dual-ship.
- Removed surfaces stay exit-2 forever: `log`, `path`, `build`, `export --okf`, `diff-preset`, `rename`, `validate --fix`, `--agent`, `neighbors --both`, completion flags.
- Not ported: `tests/eval/**` (consumes the CLI; runs against either binary as-is). Python library import surface is not public API (docs promise only the CLI); no MCP server exists in 0.18.

### A.5 Coverage gate (the "no exceptions" enforcement)

`parity-run -coverage` parses this appendix's machine-readable twin (`parity/coverage.yaml`, generated once from the Typer app + `errs.py` and hand-reviewed) and asserts every (command × flag × mode × outcome), every JSON key, every format special, and every error code appears in ≥1 recorded fixture. CI-failing from M1 onward. The Typer-generated `docs/cli-reference.md` drift check gains a Go twin: `khub --help`/subcommand help snapshots as fixtures, so flag-surface drift between the binaries is a test failure, not a docs bug.

## Critical files

- `docs/go-rewrite-memo.md` — the gate + fixed library stack (goccy/go-yaml, gonum, ncruces/go-sqlite3 wasm FTS5, cobra+fang+lipgloss+glamour, gofrs/flock, jsonschema v6, subprocess git, goreleaser, regexp2 addition justified by R1)
- `src/khub/cli/_render.py` — the output gate being ported (guard/emit/_fail/want_json)
- `src/khub/core/formats.py`, `core/workspace.py` — the serialization canon being reproduced
- `src/khub/core/entity.py` — largest port (meta canon, minting, collection mutation)
- `src/khub/core/errors.py` — byte-exact message templates
- `tests/test_cli_contract.py`, `smoke.sh` — seed material for the fixture corpus
