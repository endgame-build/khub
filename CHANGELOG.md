# Changelog

Notable changes to khub. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); khub is pre-release, so everything newer than the v1 engine sits under Unreleased until the first tag.

## [Unreleased]

### Added — 2026-07-08: single-file collections (`layout: collection`)

One file holds every entity of a type as a row (`format: json|jsonl|yaml`; `path` names the file, format derivable from its suffix). The `project-repos.yaml` storage shape becomes first-class typed entities.

- Row identity: yaml/json collections are mappings keyed by slug; jsonl rows carry a reserved `slug` key. Duplicate slugs are malformed in all three formats (json via a duplicate-key-rejecting parse, never last-wins). `slug`/`type`/`body` are reserved row keys, rejected as schema field names on collection types.
- Every verb and gate works on rows. `add`/`get`/`edit`/`search` records add `locator: "path#slug"`; `get --format raw` prints only the row.
- Writes: one locked read-modify-write path — exclusive flock on `.khub/generated/locks/<type>.lock`, fresh in-lock uniqueness gate (the O_EXCL replacement), temp + fsync + atomic rename. No-op link/unlink never rewrite the file.
- Malformed contract (v1): any bad row makes the whole file malformed — no rows load, writes to the type refuse, derivative dangling reports are suppressed into a counted `suppressed_dangling` on `check`. Missing/empty file = zero entities.
- Git at row altitude: `log` attributes a commit to the rows whose values changed (blob diff per commit); `stale` judges a row only on its own `updated` (never the file's commit date); `backfill` skips collection types and reports it.
- Contract: `docs/collections-design.md`.

### Added — 2026-07-07: per-entity serialization formats (`format: json|yaml`)

A type may store entities as `.json`/`.yaml` documents instead of md — a single mapping, pure metadata.

- Prose rides in a reserved `body` field (string or null): `--body`/`--body-file` write it, `get` returns it as the body, the key never reaches frontmatter output or query filters. New `--body <text>` flag on `add`/`edit` alongside `--body-file`.
- `core/formats.py` is the one serialization strategy module (scan, verbs, gitlog, and search all dispatch through it); md keeps its fence semantics byte-for-byte.
- The meta-schema whitelists formats per layout; `jsonl` is collection-only, `gjson` stays named-but-undefined and rejected.

### Added — 2026-07-07: full-text search (`khub search`)

The `build-graph.py` replacement's missing half — BM25-ranked FTS5 over title + full body, built `:memory:` per invocation (stdlib sqlite3, zero new deps, derived and never stale).

- Raw MATCH syntax passes through (`"phrases"`, `OR`, `NEAR`, `prefix*`); malformed expressions are located errors. `--type` prunes at index-build time; non-md entities index their `body` field plus every scalar string field.
- Real-corpus recall: 75 hits for `modernization` on the HQ port vs 29 from the retired 500-char `.hq-graph.sqlite` index, ~250 ms end to end.

## [0.1.0] — 2026-07-07 (unreleased baseline)

The v1 engine, proven by the firm-hq cutover (241 entities, `kb.py`/`build-graph.py`/`hq.schema.yml` retired):

- Schema-generic core over Markdown-in-git: `init` (presets, measured non-destructive force-seed), `compile` (LinkML + Pydantic + JSON Schema, optional extra), schema introspection, `status`.
- Authoring verbs (`add`/`get`/`edit`/`link`/`unlink`/`remove`): referential-integrity hard-fail, capture never blocked, minimal-diff ruamel round-trip, O_EXCL slug minting, inbound-edge delete guard.
- Query and graph (`query`/`neighbors`/`impact`/`history`): in-memory networkx projection rebuilt per command.
- Integrity loop (`validate`/`check`/`stale`/`log`): declared-subset validation, graph-wide completeness/dangling/strays/cycles, orphans informational (`--strict` gates), git history at entity altitude.
- Projections (`reindex`/`viz`/`backfill`): OKF `index.md`, Cytoscape HTML, additive git-date/scaffolding backfill.
- CLI agent contract: JSON on a pipe, uniform `id`/`type`/`slug` records, located errors, reads never gate.
