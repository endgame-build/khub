# A Go rewrite of khub

Status: **SHIPPED.** Proposed 2026-08-15, gate cleared the same day, cut over
2026-08-16. Kept as the record of why the rewrite happened; nothing below has
been edited to match the outcome, so read it as a snapshot of what was known
then.

Where it was right and wrong: the §5 gate cleared, but on a leg this memo did
not anticipate — token-origin splicing rather than AST manipulation
(`parity/DECISIONS.md` D2). The cross-platform reach argued for in §3.3 was
**not** delivered: Windows is struck, because its locking path has no shipped
khub behaviour to be at parity with (D18). The install-friction, startup-latency
and POSIX-lock arguments all held.

Baseline it was written against: khub 0.18.0, Python 3.11+, ~7.5k lines under
`src/khub` plus a ~9.4k-line suite under `tests/`. Both are deleted; git history
has them.

Library maintenance claims were verified against live repositories on
2026-08-15. Anything asserted about a dependency's status has a date attached
because that is the part of this memo that rots.

## 1. Decision summary

**Rewrite khub in Go, gated on one prototype.** The Go ecosystem covers khub's
dependency surface nearly 1:1, including the two pieces that looked hardest:
`gonum/graph` ships the same Johnson elementary-cycles algorithm networkx
`simple_cycles` uses, and SQLite FTS5 survives with **zero CGO** via a
Wasm-compiled driver, which preserves single-static-binary distribution.

The payoff is a ~10 MB binary with ~5–10 ms cold start, a goreleaser-automated
release matrix, and an install story (fetch a binary, `go install`, Homebrew,
npm-wrapped) that directly attacks the enforcement problem: today there is no
way to make a repo's users have khub.

One risk is real and concentrated: no Go YAML library matches `ruamel.yaml`'s
lossless round-trip. Files in git are the source of truth (design-memo, layer
1), so serialization fidelity is a correctness property, not a nicety. The gate
in §5 is a two-day prototype against real workspace files.

## 2. The problem being solved

khub's architecture is already portable — markdown + YAML in git as truth, an
in-memory graph rebuilt per call, schema-generic surfaces. The pain is
operational, and a rewrite is justified only if it moves these three, because a
language change buys no features:

- **Installation friction.** `uv tool install git+ssh://…` requires uv, Python
  3.11+, and repo SSH access on every machine and agent container that touches
  a workspace. Consuming repos cannot enforce it.
- **Agent-loop latency.** Interpreter startup is ~150–300 ms per invocation.
  The rebuild-per-call design (design-memo, layer 4) means every verb pays it;
  a session making 50 calls burns 10–15 s on startup alone.
- **Platform gaps.** Collection locking is `fcntl.flock` — POSIX-only, as
  `core/entity.py:924` admits ("an msvcrt shim lands if Windows ever matters").

## 3. What we gain

| | |
| --- | --- |
| ~10 MB | static binary, all platforms, no runtime |
| 5–10 ms | cold start, vs 150–300 ms today |
| 0 | CGO dependencies — FTS5 included |
| 6 targets | darwin/linux/windows × amd64/arm64, one config |

### 3.1 Distribution becomes a solved problem

goreleaser turns one config into the full release: static binaries per platform
pair, GitHub Releases with checksums and signing, a Homebrew tap, Scoop,
deb/rpm via nfpm, SBOMs. Three install paths appear that Python cannot offer:

- `go install github.com/endgame-build/khub@latest` — zero infrastructure.
- Fetch one binary from a Release — pinnable by version and checksum, which is
  what devcontainers, CI, and session hooks actually want.
- **npm-wrapped binary.** `jackchuka/mdschema` ships its Go binary as an npm
  package; `npx` users get it without Go. Worth copying, because agent
  toolchains overwhelmingly have Node available.

For enforcement in a consuming repo, the story collapses to "a hook or
devcontainer fetches one pinned, checksummed ~10 MB file." No uv, no Python
version matrix, no venv, no SSH-key requirement if releases are public. The
enforcement *mechanisms* are unchanged — a pinned shim, a SessionStart hook,
`khub check` in CI — but each gets smaller and less fragile.

### 3.2 Startup latency matches the architecture

khub rebuilds its graph and FTS index per invocation on purpose: correctness
over caching, never stale. That choice amplifies fixed per-call overhead, and
interpreter startup is the dominant fixed cost. At ~5–10 ms process start the
rebuild-per-call philosophy is nearly free at khub's scale (hundreds of
entities), and khub feels instant in the agent tool-call loop it is built for.

### 3.3 Windows support, incidentally

`gofrs/flock` gives the same advisory-lock semantics cross-platform (flock on
Unix, LockFileEx on Windows). The port deletes the one acknowledged platform
hole instead of carrying it forward.

### 3.4 One deployable, no environment drift

No wrong-Python tickets, no venv activation, no install-time dependency
resolution, no transitive-dependency surprises — the PyYAML-reaches-the-env
class of problem (`.claude/rules/yaml.md`) disappears because the binary *is*
the environment. Supply-chain surface shrinks to what is compiled in.

### 3.5 A Go-only borrow: mdschema as a library

`jackchuka/mdschema` (Go, MIT, releasing steadily through 2026-08) validates
markdown **body structure** against YAML schemas — heading trees, required
sections, expression matchers like `slug(filename) == slug(heading)`, link
existence. That is precisely the layer khub ignores today. If khub is Go,
mdschema is importable, and per-type body schemas become a near-free capability
rather than a subsystem to build. No other candidate language gets this.

## 4. The verified Go stack

| khub today | Go replacement | Fit |
| --- | --- | --- |
| ruamel.yaml | goccy/go-yaml (MIT) | **Gate.** Key order solved (`MapSlice`). Comments: opt-in `CommentToMap` with known holes (flow-style unextractable). The de-facto yaml.v3 successor — `cli/cli` migrated to it. yaml.v3 was archived 2025-04; `go.yaml.in/yaml` is the conservative fallback. See §5. |
| python-frontmatter | ~15 lines of our own | Every Go frontmatter library is parse-only. Splitting on `---` ourselves is also the only way to keep serialization fidelity under our control. |
| pydantic (2 files) | santhosh-tekuri/jsonschema v6 (Apache-2.0) | Compiles schemas at runtime over dynamic data — khub's exact shape, since the schema is loaded from YAML, never compile-time structs. Custom vocabularies supported. CUE is the richer, heavier alternative worth a spike, not the default. |
| networkx | gonum/graph (BSD-3) | `multi.DirectedGraph` is a true MultiDiGraph; `traverse.BreadthFirst` takes an `until(node, depth)` predicate; `topo.DirectedCyclesIn` is Johnson's 1975 elementary circuits — the same algorithm as `nx.simple_cycles` (`core/integrity.py:739`). Cost: int64 node IDs need a slug↔id bimap. (dominikbraun/graph ruled out: stale since 2024-12, no cycle enumeration, no multigraph.) |
| sqlite3 FTS5 | ncruces/go-sqlite3 (MIT) | SQLite compiled to Wasm and translated to Go: **CGO-free with FTS5 enabled** in the `embed` build. The `:memory:` per-invocation index (`core/search.py`) ports 1:1, MATCH syntax included, and cross-compilation stays trivial. Fallbacks: modernc.org/sqlite (pure Go, huge generated dep) or bleve `NewMemOnly` (BM25 since v2.5) if we drop SQL. |
| typer + rich | cobra + fang + lipgloss + glamour | At or above parity. cobra maps 1:1 onto the `*_cmd.py`-per-group structure; glamour fills the `rich.Markdown` role. Actively maintained. |
| subprocess `git log` | keep subprocess | go-git's per-file `Log()` walks every commit diffing in-process (documented slow). A khub workspace is a git checkout by definition, so the binary is present. Same conclusion as `core/gitlog.py` reached. |
| fcntl + os.replace | gofrs/flock (BSD-3) + temp-and-rename | Cross-platform locks; fixes §2's POSIX hole. Hand-roll atomic replace (~30 lines) — google/renameio exports nothing on Windows. |
| — | goreleaser (MIT) | The whole release matrix from one config, contingent on CGO staying off, which every choice above preserves. |
| — *(new)* | jackchuka/mdschema (MIT) | Importable body-structure validation (§3.5). Young — 77★, single maintainer — so vendor if bus factor worries. |

## 5. The weak link: YAML round-trip

khub's first invariant is that files in git are truth, which makes
serialization fidelity a correctness property: a verb touching one frontmatter
field must not churn the rest of the file. `ruamel.yaml` gives Python a lossless
CST — comments, key order, formatting survive edits. **Nothing in Go matches
that.** yaml.v3 is archived (2025-04); goccy/go-yaml preserves key order
trivially but handles comments through an opt-in position-map mechanism with
documented holes (flow-style comments cannot be extracted at all).

Three things shrink the risk to something testable:

1. **khub already writes canonical formatting.** The serializer is ours and
   deterministic — `core/workspace.py` disables key sorting deliberately, and
   width is pinned. Files khub itself wrote round-trip through any
   order-preserving encoder.
2. **Comments only bite on human-annotated YAML that khub later rewrites.** A
   narrow slice: hand-edited frontmatter comments and an annotated
   `.khub/schema.yaml`. It is measurable — grep a real workspace for comments
   inside frontmatter before assuming.
3. **The AST path exists.** goccy exposes its full AST; if the convenience API
   loses comments in practice, direct AST manipulation is the escape hatch —
   more code, full fidelity.

**Go / no-go gate.** Before committing: a two-day prototype that reads every
entity file and `.khub/schema.yaml` from a real workspace, performs a no-op edit
through goccy/go-yaml, and diffs against the original. Zero-diff (or
acceptable-diff) → proceed. Comment loss on files that matter → TypeScript
becomes the fallback candidate, at the cost of ~60 MB binaries, no FTS5, and a
hand-written cycles algorithm.

## 6. Why not Rust or TypeScript

| | Go | Rust | TypeScript |
| --- | --- | --- | --- |
| YAML round-trip | partial — goccy: order yes, comments opt-in | **weakest** — serde_yaml archived; the only true analog (yaml-edit) is 6 months old at 7★ | **best anywhere** — eemeli/yaml: lossless CST + comment-preserving Document API |
| Cycle enumeration | gonum: exact Johnson's | satellite crate (graph-cycles) | none — hand-write ~120 loc |
| FTS5 parity | CGO-free (ncruces) | rusqlite bundled (C build) | none viable — `node:sqlite` ships without FTS5, so minisearch BM25 and changed query semantics |
| Binary | ~10 MB, 5–10 ms | ~3–8 MB, 5–10 ms | ~60 MB, 5–15 ms (bun compile) |
| Cross-compile | trivial, no CGO | zigbuild/cross + C toolchain for sqlite | bun targets all majors |
| Iteration speed | fast builds | 1–2 min clean builds | fast |
| Unique upside | mdschema importable; goreleaser + `go install` + npm wrapper | peak performance, unneeded at khub's scale | remark for body validation; runtime zod is a beaten path (Astro, Velite) |

**Rust is dominated for this workload**: strictly worse than Go on the
load-bearing YAML invariant, slower iteration, a C-toolchain drag for FTS5
parity, and no capability Go lacks at khub's scale.

**TypeScript is the genuine runner-up** and inverts the trade: the best
file-fidelity story in any language and the richest markdown ecosystem, against
chunky binaries, no FTS5, and a hand-rolled integrity algorithm. Because khub
controls its own serialization, Go's YAML gap is narrow *in practice* while
TypeScript's binary gap is permanent — which is why Go wins the tiebreak,
contingent on §5.

## 7. Honest costs

- **Effort.** ~7.5k source lines plus a ~9.4k-line test suite to translate.
  Realistically 4–8 weeks of focused, agent-assisted work to parity, plus a
  dual-ship stabilization period.
- **Two implementations during migration**, with drift risk until cutover.
  Mitigated by §8's golden fixtures.
- **A real, narrow fidelity regression risk** on comment-bearing YAML —
  bounded by the §5 gate, not eliminated by it.
- **Convenience lost.** pydantic validators, typer decorators, pytest fixtures:
  the Go equivalents are all more verbose. Expect ~10–12k lines for identical
  behavior.
- **Zero user-visible features ship from the rewrite itself**, while the space
  keeps moving — Basic Memory is converging on schema-boundness from the loose
  end (a different paradigm, but shipping fast with far stronger distribution).
  §9 is the hedge: the port carries new capabilities rather than only
  reproducing the present.
- **Enforcement still is not automatic.** The binary makes bootstrap smaller and
  Python-free; a consuming repo still needs the pinned hook and `khub check` in
  CI. That wiring is owed either way.

## 8. Port plan

1. **Gate (2 days).** The §5 round-trip prototype against a real workspace.
   The only step before a commit decision.
2. **Golden fixtures (2–3 days).** Freeze the CLI's JSON contracts (`docs/cli.md`)
   into a language-neutral suite: input workspace → command → expected JSON and
   exit code. Snapshot current behavior by running it against Python khub, which
   then serves as the executable spec for every Go milestone.
3. **Core read path (week 1–2).** `resolve` → `model` → `index` → `graph` →
   `query`/`search`. Read-only verbs first: they exercise schema resolution,
   frontmatter parsing, and the graph without touching write fidelity.
4. **Write path and integrity (week 2–4).** Entity verbs, the `validate` and
   `check` gates, locking, atomic writes, collections. The YAML round-trip work
   lands here and the fixture suite earns its keep.
5. **Long tail (week 4–6).** `gitlog`, `reindex`/`viz`/`backfill`,
   `init`/presets/templates, skills install, wire.
6. **Dual-ship (2–4 weeks).** Ship Go khub with Python still installable; run
   both against a real workspace in CI on the same fixtures. Cut over when they
   agree for a full release cycle. goreleaser, Homebrew, and the npm wrapper
   land here.

## 9. Capabilities to add during the port

Cheap while the code is open, and they turn the rewrite from pure reproduction
into product motion:

- **`khub schema infer` / `khub schema diff`** — bootstrap a schema from an
  existing untyped workspace (frequency analysis over observed fields →
  required/optional) and report drift between the declared schema and actual
  usage. The natural path from "a repo full of markdown" to "a khub workspace."
  Implement from the behavioral description only; the closest reference
  implementation is AGPL and must not be read side-by-side while writing this.
- **Body-structure schemas per entity type** — via mdschema as a library (§3.5).
- **Ship a JSON Schema of khub's own schema vocabulary** — editors then
  autocomplete and validate `.khub/schema.yaml` for free. Pure DX leverage.
- **Agent-readable report output** — a markdown rendering of `check`/`validate`
  results alongside the JSON contract. Raw JSON reads poorly when an agent
  surfaces it in a transcript, and glamour makes the formatted variant nearly
  free.
- **Distribution surfaces** — Homebrew tap, npm wrapper, and a pinned-version
  session hook plus a `khub check` CI template for consuming repos. This is the
  enforcement answer, shipped as product.

## 10. Sources

Ecosystem surveys (Go, Rust, TypeScript, and prior art) were run against live
sources on 2026-08-15. Primary references: [goccy/go-yaml](https://github.com/goccy/go-yaml) ·
[yaml/go-yaml](https://github.com/yaml/go-yaml) ·
[gonum](https://github.com/gonum/gonum) ·
[ncruces/go-sqlite3](https://github.com/ncruces/go-sqlite3) ·
[santhosh-tekuri/jsonschema](https://github.com/santhosh-tekuri/jsonschema) ·
[CUE](https://cuelang.org) ·
[cobra](https://github.com/spf13/cobra) ·
[glamour](https://github.com/charmbracelet/glamour) ·
[gofrs/flock](https://github.com/gofrs/flock) ·
[goreleaser](https://github.com/goreleaser/goreleaser) ·
[jackchuka/mdschema](https://github.com/jackchuka/mdschema) ·
[eemeli/yaml](https://github.com/eemeli/yaml) (TypeScript fallback) ·
[markdown-oxide](https://github.com/Feel-ix-343/markdown-oxide) (architecture
study) · [zk](https://github.com/zk-org/zk) (GPL — study only, do not copy).
