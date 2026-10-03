# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What khub is

Schema-bound, agent-facing context management. Entities live in git as Markdown
(YAML frontmatter + body), as `.json`/`.yaml` documents, or as rows of a
single-file collection. A khub schema (authored in khub's own vocabulary) is the
contract; the core packages do schema-validated CRUD and graph queries; the CLI
is a thin adapter over them.

khub is a single static Go binary. It was a Python package through 0.18.0; that
implementation was retired at the Go cutover and is recoverable from git history.
Comments across `internal/` still cite the Python module each package ports
(`core/entity.py`, `cli/_render.py`) — those citations are deliberate provenance,
not stale references.

Read before non-trivial work: `docs/design-memo.md` (rationale + invariants),
`docs/cli.md` (full command surface + JSON contracts), `docs/collections-design.md`.

## Commands

```bash
go build -o khub ./cmd/khub    # build the CLI
go test ./...                  # full suite
go test ./internal/query/ -run TestName     # single test
gofmt -l ./cmd ./internal ./parity          # must print nothing
go vet ./...
golangci-lint run              # the choke-point rules (see below)
./khub <cmd>                   # run against cwd's workspace
```

The parity suite is the executable spec:

```bash
go build -o parity-run ./parity/runner
./parity-run -bin "$PWD/khub"                                    # every fixture
./parity-run -bin "$PWD/khub" -coverage parity/coverage.yaml -subset-of "$PWD/khub"
./parity-run -bin "$PWD/khub" -record -only <family>/<case>      # re-record one case
bash smoke.sh                                                    # end-to-end, both build presets
bash parity/tools/gen_cli_reference.sh                           # regenerate docs/cli-reference.md
```

The Claude Code plugin lives under `plugin/` (the two agent skills and the mod),
listed by the marketplace manifest `.claude-plugin/marketplace.json`. The binary
carries no skills. Its gates (CI runs these as the `plugin` job; see
`.claude/rules/plugin.md`):

```bash
claude plugin validate --strict .          # the marketplace manifest
claude plugin validate --strict plugin     # the plugin manifest and what the hooks call
claude plugin test plugin                  # the plugin's tests
bash plugin/tools/record-fixtures.sh       # re-record khub's output the tests read
```

The npm channel has its own gates (CI runs these as the `npm-package` job):

```bash
shellcheck npm/build-packages.sh npm/smoke.sh
node --check npm/khub/bin/khub.js
npm/smoke.sh                   # assemble, pack, install and run the npm package
```

`parity/cases/**/expected/` holds raw bytes — stdout, stderr, exit code, and a
tree manifest per step. **Those bytes are the contract.** A change that moves
them is a behaviour change: re-record deliberately, review the diff, and say why
in the commit. Never re-record to make a red suite green.

## Architecture

Five layers (design-memo). **All logic lives in `internal/`; every surface is a
thin, schema-introspecting adapter with zero per-type code.**

1. **Truth** — Markdown + YAML frontmatter in git. One entity = one file (or one
   collection row). No database is ever the source of truth.
2. **Ontology** — `internal/schema/resolve.go` merges the three authored layers
   (`.khub/ontology.yaml` — the domain; `policy.yaml` — workspace gates;
   `storage.yaml` — layouts/paths/prefixes/templates) with the base block
   EMBEDDED in the binary (`presets/core/ontology.yaml`, never copied into a
   workspace; an authored `ontology.base` is rejected — a type overrides a base
   attribute by redeclaring it) into a `ResolvedSchema`
   (`internal/schema/model.go`). Layers merge on top-level key, not filename.
3. **Core** — the write verbs (`internal/entity/`), integrity
   (`internal/integrity/{validate,check}.go`), graph walks (`internal/graph/`),
   query/search (`internal/query/`, `internal/search/`), git-derived history
   (`internal/gitlog/`), projection (`internal/reindex/`, `internal/viz/`,
   `internal/backfill/`), serialization and collections (`internal/canon/`).
4. **Graph projection** — an in-memory ordered adjacency rebuilt per call; gonum
   finds strongly connected components for bounded cycle witnesses. FTS5 search is in-memory per invocation
   (never stale). No persisted index.
5. **Access** — `internal/cli/root.go` wires one `*.go` per command group over
   the core verbs.

Adding or changing an entity type is a schema edit — **no surface code changes.**
If you find yourself branching on a type name in `internal/cli/`, that's the bug;
push it into the schema or the generic core path.

### Choke-point rules (lint-enforced)

- Only `internal/canon` may import a YAML library.
- Only `internal/canon/jsonio.go` writes JSON bytes — never `encoding/json` on an
  output path. khub emits three distinct JSON dialects (CLI stdout, on-disk,
  jsonl row) and `encoding/json` matches none of them.
- `omap.Map` at every API boundary, never `map[string]any`. Key order is
  contract — JSON field order, frontmatter canon, schema declaration order.

### Invariants that shape every change

- **Schema-generic surfaces.** No hardcoded per-type knowledge outside the schema.
- **Graph is derived**, never stored. Inverse edges are computed at read time from
  the single-sided forward edge and identified by source type plus predicate; never written to disk.
- **`validate` vs `check` are distinct gates.** `validate` = per-entity
  well-formedness + referential integrity over *present* declared fields (missing
  required does NOT block capture). `check` = graph-wide over the active
  (`draft: false`) subgraph: required-completeness, orphans, dangling edges,
  strays, cycles. Don't collapse them.
- **`draft` is a manual publish flag**, never derived from completeness. No verb
  auto-promotes/demotes.
- **Capture is never blocked.** Referential integrity hard-fails on write; a
  missing required field just leaves the entity incomplete for `check` to report.
- **Writes are workspace-serialized and atomic.** One stable
  `.khub/generated/locks/workspace.lock` covers scan, validation, and commit for
  every existing-workspace mutation. Keep lock files while a process may hold them. Publication uses
  unique sibling temporaries, preserves permissions, and refuses unsafe paths and symlinks.
- **Storage is per-type config**: `layout` (file / folder / collection) × `format`
  (md / json / yaml; collections take json / jsonl / yaml). Non-md entities carry
  prose in a reserved `body` field. Collections use the same workspace lock and
  atomic publication as every other mutation; `.khub/generated/` is gitignored.

Each invariant has a sentinel fixture under `parity/cases/invariants/`.

## The on-disk YAML format is ruamel-shaped

`internal/canon` reproduces ruamel.yaml's emitter byte-for-byte — both profiles
(round-trip at width 80, safe at width 4096) — and its 1.1-vs-1.2 scalar
resolution split. This is a property of **khub's file format**, not a leftover of
the Python implementation: entities must stay diff-stable for git and readable by
anything else that touches them.

`parity/yamlgate` is the standing regression for it (`go run ./parity/yamlgate
parity/corpus`), and `parity/tools/gen_*_cases.py` regenerate its corpora.
See `.claude/rules/yaml.md`.

## Presets

`presets/core/ontology.yaml` (the `base` block — embedded, supplied to every
resolve, never copied into a workspace) + one preset DIRECTORY per domain
(`<preset>/{ontology,policy,storage}.yaml` + optional
`<preset>/templates/*.yaml` body templates). `khub init` copies a preset's three
layer files into an engagement's `.khub/` and the templates to
`.khub/templates/`. The tree is embedded into the binary by `embed.go` at the
module root (a `//go:embed` pattern cannot contain `..`, so the embed cannot live
under `internal/`).

The preset is the source of truth for its schema; planning specs may drift from
it (see `docs/firm-ops-preset.md`, `docs/build-hub-preset.md`).
