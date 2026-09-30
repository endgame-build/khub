# Dependencies — the bar, and what stays custom

## The bar (all four)

1. Pure Go — `CGO_ENABLED=0` is non-negotiable (cross-compile, one static binary).
2. Preserves khub's ordering/byte contracts (most libs are map-backed;
   ordering-as-opt-in-sort is a regression).
3. Replaces more code than it adds, counting the shim to keep pinned bytes.
4. Maintained — check last release, not stars.

## Facts about current deps

- **gonum** links a handful of leaf packages, no `mat`/BLAS; it is used only
  for strongly connected components.
- **ncruces/go-sqlite3** is `wasm2go`-translated Go: no wazero, no CGO.
- **Author regexes compile as RE2, never a backtracking engine.** Schema
  `pattern`s and template `{pattern:}` rules are author-supplied and run over
  untrusted values; stdlib `regexp` matches in linear time, so no timeout is
  needed. Do not add a lookaround-capable engine to win syntax back.
- `os.Root` (Go 1.24+) confines workspace storage operations and resists path
  swaps. Keep the explicit schema path checks and symlink refusals around it.

## What stays custom, and why

- `internal/graph` — insertion-ordered adjacency plus read-time inverse edges
  is the product; a map-backed graph library cannot promise either.
- FTS5 search — khub exposes raw FTS5 `MATCH` syntax to agents; a replacement
  would have to reimplement the query language, not just the scoring.
- Git history — shell out to `git`; the win is batching one `git log` pass,
  not a library.
- `internal/omap` — third-party ordered maps cannot fix `any` (values are
  genuinely heterogeneous) and ship `encoding/json` marshalling the choke
  point bans. Known edges, pinned by `omap_test.go`: `Keys()` aliases the
  backing slice; `Delete` is O(n), accepted at khub's record sizes.
- Table/help rendering — fixtures are the spec; any library is a pure-cost
  re-record.
- `internal/schema/vocab.go` — a JSON Schema or CUE layer would be a
  translation layer plus an error-message shim to keep fixtures green.
- The frontmatter splitter — a second splitter is a second source of truth
  for the YAML 1.1/1.2 scan split.
- `internal/fsio` — the atomic-write libraries do not fsync the parent
  directory either; fix ours in place.
- `encoding/json` stays banned on output paths: the CLI dialect's ASCII
  escaping is not expressible with the standard encoder. Decoding through
  `ojson.go` is fine.

## Hygiene

Check `pkg.go.dev/vuln` before adding anything; `govulncheck` runs in CI
(call-graph reachability, catches stdlib CVEs). Pin tool versions —
`version: latest` in CI is an unpinned dependency.
