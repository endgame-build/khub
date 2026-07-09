# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What khub is

Schema-bound, agent-facing context management. Entities live in git as Markdown
(YAML frontmatter + body), as `.json`/`.yaml` documents, or as rows of a
single-file collection. A khub schema (authored in khub's own vocabulary) is the
contract; the core library does schema-validated CRUD
and graph queries; the CLI is a thin adapter over it.

Read before non-trivial work: `docs/design-memo.md` (rationale + invariants),
`docs/cli.md` (full command surface + JSON contracts), `docs/collections-design.md`.

## Commands

```bash
uv sync                        # install dev deps
uv run pytest                  # full suite
uv run pytest -m unit          # markers: unit | integration | e2e
uv run pytest tests/test_query.py::test_name   # single test
uv run ruff check src tests
uv run mypy                    # strict; files = src/khub
uv run khub <cmd>              # run the CLI against cwd's workspace
```

mypy is `strict = true`. Ruff line-length 100. Third-party modules without stubs
are listed under `[[tool.mypy.overrides]]` — add there, don't sprinkle `# type: ignore`.

## Architecture

Five layers (design-memo). **All logic lives in `core/`; every surface is a thin,
schema-introspecting adapter with zero per-type code.**

1. **Truth** — Markdown + YAML frontmatter in git. One entity = one file (or one
   collection row). No database is ever the source of truth.
2. **Ontology** — `core/resolve.py` merges the `base` block into every type and
   parses khub-vocabulary YAML into a `ResolvedSchema` (`core/model.py`).
3. **Core library** (`core/`) — the write verbs (`entity.py`), integrity
   (`integrity.py`), graph walks (`graph.py`), query/search (`query.py`,
   `search.py`), git-derived history (`gitlog.py`), projection (`reindex.py`,
   `viz.py`, `backfill.py`), formats/collections (`formats.py`).
4. **Graph projection** — in-memory `networkx` MultiDiGraph, rebuilt per call.
   FTS5 search is in-memory per invocation (never stale). No persisted DB in v1.
5. **Access** — `cli/main.py` wires one `*_cmd.py` per command group over the
   core verbs. A Claude Code skill and MCP server are the same-shape surfaces.

Adding or changing an entity type is a schema edit — **no surface code changes.**
If you find yourself branching on a type name in `cli/` or a surface, that's the
bug; push it into the schema or the generic core path.

### Invariants that shape every change

- **Schema-generic surfaces.** No hardcoded per-type knowledge outside the schema.
- **Graph is derived**, never stored. Inverse edges are computed at read time from
  the single-sided forward edge; never written to disk.
- **`validate` vs `check` are distinct gates.** `validate` = per-entity
  well-formedness + referential integrity over *present* declared fields (missing
  required does NOT block capture). `check` = graph-wide over the active
  (`draft: false`) subgraph: required-completeness, orphans, dangling edges,
  strays, cycles. Don't collapse them.
- **`draft` is a manual publish flag**, never derived from completeness. No verb
  auto-promotes/demotes.
- **Capture is never blocked.** Referential integrity hard-fails on write; a
  missing required field just leaves the entity incomplete for `check` to report.
- **Storage is per-type config**: `layout` (file / folder / collection) × `format`
  (md / json / yaml; collections take json / jsonl / yaml). Non-md entities carry
  prose in a reserved `body` field. Collection writes are lock-serialized
  (`.khub/generated/locks/`, gitignored) and land via atomic replace.

## YAML — always `ruamel.yaml`, never PyYAML

See `.claude/rules/yaml.md`. PyYAML is not a declared dependency; `ruamel.yaml` is
the declared library. Canonical loaders/dumpers: `core/resolve.py` (safe load),
`core/workspace.py` (order-preserving).

## Presets

`src/khub/presets/core.yaml` (the `base` block) + one preset per domain
(`firm-ops.yaml`). `khub init` flattens core + a preset into an engagement's
`.khub/schema.yaml`. The preset is the source of truth for its schema; planning
specs may drift from it (see `docs/firm-ops-preset.md`).
