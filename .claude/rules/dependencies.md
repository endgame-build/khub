# Dependencies — the bar and the standing verdicts

Settled "should we just use a library?" questions. Do not re-open without new
evidence.

## The bar (all four)

1. Pure Go — `CGO_ENABLED=0` is non-negotiable (cross-compile, one static binary).
2. Preserves khub's ordering/byte contracts (most libs are map-backed;
   ordering-as-opt-in-sort is a regression).
3. Replaces more code than it adds, counting the shim to keep pinned bytes.
4. Maintained — check last release, not stars.

## Facts about current deps (measured)

- **gonum is not heavy**: links 9 leaf packages, no `mat`/BLAS. The big module
  cache is source the linker never sees. Don't vendor Johnson's to "save" it.
- **ncruces/go-sqlite3 no longer uses wazero** — `wasm2go`-translated Go, zero
  wazero in `go.sum`.
- **regexp2 is backtracking with no default timeout.** Schema `pattern`s are
  author-supplied → the shared cached matcher sets `MatchTimeout` to one second
  (`internal/schema/model.go` `MatchPattern`). Writes and integrity must use it.

## Standing verdicts — keep custom

| Subsystem | Why the library loses |
|---|---|
| `internal/graph` | insertion-ordered adjacency + read-time inverse edges IS the product; gonum is limited to SCC discovery for bounded cycle witnesses; candidates (dominikbraun — dormant 2024, yourbasic — pre-generics) are map-backed |
| FTS5 search | khub exposes raw FTS5 `MATCH` syntax to agents — a replacement must reimplement the query language, not the scoring. bleve changes ranking (re-records fixtures); bluge dormant since 2022; modernc slower |
| Shelling out to `git` | per-file `Log` is go-git's known worst case, v6 still alpha, git2go needs CGO. The real win is **batching** (one `git log --name-only` pass), not swapping |
| `internal/omap` | third-party ordered maps solve absent perf problems, can't fix `any` (values genuinely heterogeneous), and ship `encoding/json` marshalling the choke-point bans. Known edges: `Keys()` aliases the internal slice; `Delete` O(n) — fix in place, ~15 lines |
| Table/help rendering | fixtures are the spec; any lib is a pure-cost re-record. The one real bug (rune count ≠ terminal cells) fixes with `x/text/width`, already a dep |
| `internal/schema/vocab.go` | JSON Schema/CUE = a translation layer + an error-message shim to keep fixtures green; net LOC up. CUE's Go API is pre-redesign |
| `editDistance` (~20 LOC) | threshold tuned so retired command names stay ≥3 away; every lib produces different bytes |
| Frontmatter split | a second splitter = second source of truth for the 1.1/1.2 scan split; no Go frontmatter lib round-trips anyway |
| `internal/fsio` | renameio/natefinch/lockedfile don't fsync the parent dir either; fix ours in place |

`encoding/json` output ban stays: 2 of 3 dialects are expressible in jsontext,
but the CLI dialect's `ensure_ascii=True` is not — and that's the one used
everywhere. Decode via `ojson.go` remains fine.

`os.Root` (Go 1.24+) confines workspace storage operations and resists path
swaps. Keep the explicit schema path checks and symlink refusals around it.

## Hygiene

Check `pkg.go.dev/vuln` before adding anything; `govulncheck` in CI (call-graph
reachability, private-repo safe, catches stdlib CVEs). Pin tool versions —
`version: latest` in CI is an unpinned dependency.
