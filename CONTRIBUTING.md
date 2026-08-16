# Contributing to khub

Thanks for helping improve khub. The bar is simple: keep the engine
schema-generic and the gates green.

## Dev setup

khub is a Go program. Go 1.25+, nothing else — no interpreter, no package
manager, no virtualenv.

```bash
go build -o khub ./cmd/khub
./khub --help
```

## The gates

Run these before you open a PR; CI runs the same on every push and pull request:

```bash
gofmt -l ./cmd ./internal ./parity   # must print nothing
go vet ./...
go test ./...
golangci-lint run                    # choke-point rules (see below)

go build -o parity-run ./parity/runner
./parity-run -bin "$PWD/khub" -cases parity/cases                # every fixture
./parity-run -coverage parity/coverage.yaml -cases parity/cases \
             -subset-of "$PWD/khub"                              # no uncovered command/flag/error code
bash smoke.sh                                                    # end to end, both build presets
```

Run one test with `go test ./internal/query/ -run TestName`.

Two more that only matter when you touch what they cover:

```bash
go run ./parity/yamlgate parity/corpus   # if you touch internal/canon
bash parity/tools/gen_cli_reference.sh   # if you add or change a flag
```

## The fixtures are the spec

`parity/cases/**/expected/` holds raw bytes — stdout, stderr, exit code, and a
manifest of the resulting file tree, per step. **Changing those bytes changes
the contract.** Re-record deliberately, review the diff, and say why in the
commit message:

```bash
./parity-run -bin "$PWD/khub" -record -only <family>/<case>
```

Never re-record to make a red suite green. If a change is a deliberate
divergence rather than a fix, it belongs in
[`parity/DECISIONS.md`](parity/DECISIONS.md).

These fixtures were recorded from the Python implementation khub had through
0.18.0, which is why comments across `internal/` cite the Python module each
package ports. That provenance is deliberate; the Python source itself is gone
(retired at the Go cutover, recoverable from git history).

## What review looks for

- **Schema-generic surfaces.** All logic lives in `internal/`; the CLI is a thin,
  schema-introspecting adapter with zero per-type code. Branching on a type name
  inside a surface is the bug — push it into the schema or the generic core path.
- **The choke points**, which are lint-enforced: only `internal/canon` may import
  a YAML library, only `internal/canon/jsonio.go` writes JSON bytes (khub emits
  three distinct dialects and `encoding/json` matches none of them), and
  `omap.Map` at every API boundary because key order is contract.
- **`validate` and `check` stay distinct** — per-entity well-formedness versus
  the graph-wide gate. Capture is never blocked on write.
- Read [`docs/design-memo.md`](docs/design-memo.md) for the invariants before
  non-trivial work. Each has a sentinel fixture under `parity/cases/invariants/`.

## Commits and releases

Keep commits scoped and their messages descriptive. Update
[`CHANGELOG.md`](CHANGELOG.md) (Keep a Changelog format) for anything
user-facing. Releases are tag-based — see [`RELEASING.md`](RELEASING.md).
