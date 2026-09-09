# Go in khub — where khub inverts the defaults

Standard Go advice mostly applies here. This file covers only the deliberate
inversions. Each was argued and decided; applying the normal rule by reflex
breaks a contract.

## Error text is product, not diagnostics

The Go team refuses to stabilize error strings ([golang/go#49356](https://github.com/golang/go/issues/49356)) —
correct for libraries. khub is a CLI whose error text is printed verbatim and
pinned by fixtures, so it inverts this. Consequences:

- **Never `%w`-wrap at an emission point.** Wrapping changes `Error()`, and
  `Error()` is the output. Context goes in `Located`'s struct fields
  (`Type`/`Relation`/`Target`) — that is why they exist.
- **Always match with `errors.As`, never bare type assertions** — assertions
  silently stop matching the day anyone wraps upstream.
- **No sentinel-per-condition, no `err113`.** `internal/errs` factories build
  code + message together so they cannot drift. `Located.Code` is the machine
  channel; the message is pinned prose.
- **No `errors.Join` at the surface** — its `Error()` concatenates with `\n`,
  not khub's report format. `check`/`validate` aggregate findings as data.
- Messages are capitalized Click-style sentences. staticcheck's **ST1005 stays
  disabled deliberately**, not accidentally.

## No logging library, ever

stdout AND stderr are fixture-pinned; a second writer with its own format is a
contract violation. If tracing is ever needed: a ~10-line `debugf` to
`io.Discard` unless `KHUB_DEBUG` is set — it cannot leak into fixtures by
construction. Never `slog`/`logrus`/`zap`.

## The lint set is small on purpose

Measured on this tree: `default: all` → ~3,866 non-test findings, roughly
1 true bug per 3,800. A curated set (`staticcheck` minus ST1005, `errorlint`,
`unused`, `errcheck` with the `std-error-handling` preset) is what
`.golangci.yml` enables, held at zero findings with the issue caps lifted. Several
popular linters attack khub's contracts: `wrapcheck` demands forbidden `%w`,
`mnd` flags the pinned emitter widths, **`misspell` rewrites string literals
including golden messages — never enable it**. Adopt linters tree-wide, not
via `--new-from-merge-base`: `internal/canon` is both the riskiest and the
most stable package, so a diff filter systematically misses it.

## Architecture rules: depguard first, then a `go test`

Import bans go in `.golangci.yml` depguard (`files:` form, as the YAML rule
does). Anything depguard can't express becomes a plain test in the spirit of
Go's own `src/go/build/deps_test.go` — never a golangci `.so` plugin (plugin
build step + custom binary for what a test does free).

## Determinism is contract

Never range a map and emit in that order. Insertion order is the graph's
ordering rule; preserve it through any new traversal. gonum finds strongly
connected components; khub chooses and sorts one real deterministic cycle
witness per cyclic component (`internal/graph/cycles.go`).

## Provenance comments stay; rotted strings are bugs

Comments citing the Python module a package ports are deliberate provenance —
keep them (`search.go` "The Python build either has FTS5 compiled in",
`gitlog.go` "Python builds no env" are correct as comments and must stay). A
**shipped string** describing the retired implementation is the opposite: a bug
regardless of how faithfully a fixture preserves it. `wire.go`'s "uv tool
install" hint was the last one; replacing it with the npm command was a
deliberate fixture re-record. Grep before adding an install hint anywhere.

## Layout

`cmd/khub` + `internal/` + `parity/`. No `pkg/` — `golang-standards/project-layout`
is not a Go-team standard. `embed.go` at module root is forced (`//go:embed`
cannot contain `..`), not stylistic.
