# khub

[![CI](https://github.com/endgame-build/khub/actions/workflows/ci.yml/badge.svg)](https://github.com/endgame-build/khub/actions/workflows/ci.yml)
![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)
![Go 1.25+](https://img.shields.io/badge/go-1.25%2B-00ADD8.svg)

**Schema-bound, agent-facing context management.**

khub gives an AI agent typed, validated, queryable context (structured memory it can navigate and write back to) instead of unstructured documents stuffed into a context window.

![khub: an agent captures into a schema-bound graph that lives on disk as heterogeneous git storage](docs/img/architecture.svg)

One generic engine: entities live in git as Markdown with YAML frontmatter (the default), as `.json`/`.yaml` documents, or as rows of a single-file collection, per-type schema config. The khub schema is the contract: types, attributes, and legal relations, authored in YAML and resolved in memory. A Go core provides schema-validated CRUD and graph queries; a generic `khub` CLI and an agent skill are thin, schema-driven surfaces over it. It ships as one static binary — no runtime to install. The graph is a projection rebuilt from workspace files on demand; no database is ever the source of truth.

Built on top of the [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md). khub's Markdown entities are OKF concepts; on top, khub adds a typed schema, a graph, and the serialization formats and collections OKF lacks. Any workspace projects to a conformant OKF bundle.

**The schema is the operational setup.** It configures what a given hub is *for*. Canonical ontology presets, one per domain (building and maintaining a system, consulting, research, operations), are the reusable IP. Each engagement gets its own repo seeded from a preset, where agents and humans co-author entities and extend the schema as the work demands.

## What it does today

- **Author** — `add` / `get ID...` / `edit` / `link` / `unlink` / `remove`: schema-validated writes, referential integrity hard-fails, capture never blocked, minimal-diff round-trips. One `get` returns one record; multiple IDs return an all-or-nothing array in argument order. A templated type's `add` seeds the body from its template (`--no-template` opts out).
- **Find** — `query` (frontmatter + edge filters), `search` (BM25 full-text over titles, bodies, and fields: FTS5, built in-memory per call, never stale), `neighbors` / `impact` / `history` (graph walks).
- **Gate** — `validate` (per-entity well-formedness, including body structure against the type's template: required section headings as an ordered subsequence; per-section rules report as `gaps`, which never gate) and `check` (graph-wide completeness, dangling edges, strays, cycles, missing required singletons, body shape; orphans informational unless `--strict`, thin bodies informational as `thin`); `stale` reads git at entity altitude, row-accurate even inside collections.
- **Project** — `reindex` (OKF `index.md`), `viz` (Cytoscape HTML), `backfill` (git-derived dates and scaffolding).
- **Store** — per-type `layout` (file / folder / collection / singleton) × `format` (md / json / yaml; collections take json / jsonl / yaml). A singleton is one fixed file whose slug is the type name (`khub get prd`). Non-md entities carry prose in a reserved `body` field. Every existing-workspace mutation holds one workspace-wide lock from scan through publication; atomic writes preserve existing permissions and refuse workspace storage symlinks.
- **Scaffold** — presets are directories (`<name>/{ontology,policy,storage}.yaml` + `templates/*.yaml`); `khub init` copies them into `.khub/` and creates every missing md singleton from its template (creations only — an existing file is never touched); `khub upgrade --dry-run` preflights the complete candidate and tails without workspace artifacts. A real upgrade publishes schema, templates and scaffolds transactionally, preserves edited files as `<name>.bak`, writes the version last, then removes skill copies earlier versions installed and refreshes wiring and `index.md` as non-fatal tails.
- **Wire** — `wire`: link the schema into a project's agent files. Bare `wire` updates whichever of `CLAUDE.md` / `AGENTS.md` exist; `--target claude|agents|both` creates one. `CLAUDE.md` gets `@.khub/ontology.yaml` (+ policy/storage) imports, `AGENTS.md` a schema pointer, both with the command surface — so an agent reasons in the ontology with or without the CLI.

Full command surface and JSON contracts: [`docs/cli.md`](docs/cli.md). Feature history: [`CHANGELOG.md`](CHANGELOG.md). All documentation: [`docs/`](docs/).

## Quickstart

khub is a single static binary for macOS and Linux (amd64, arm64). The primary install is the npm package [`@endgame-build/khub`](https://www.npmjs.com/package/@endgame-build/khub) — one package carrying a prebuilt binary per platform behind a launcher that picks the matching one — pinned per repo, so everyone (and every agent) touching that repo runs the same khub:

```bash
npm install -D @endgame-build/khub                  # exact per-repo pin (required: later commands run it)
npx @endgame-build/khub init firm-ops ./my-hub      # scaffold .khub/ (+ templates, singletons), wire agent files
cd my-hub
npx skills add endgame-build/khub                   # the agent skills (Claude Code: the plugin, below)
```

With no version after the name, `npx @endgame-build/khub` runs the version `package.json` pins.

Upgrading a repo is a one-line `package.json` bump in a PR: any on-disk byte changes a release makes land in that reviewed commit, not in everyone's unrelated diffs. More in [`docs/getting-started.md`](docs/getting-started.md#install).

Author entities. Referential integrity hard-fails on write (a relation to a missing target is rejected), but a missing field never blocks capture:

```bash
khub add client  --name "Acme Corp" --industry manufacturing
khub add person  --name "Dana Lee"  --role partner
khub add project --title "Acme Diagnostic" --client acme-corp --owner dana-lee
```

Then walk the graph and gate it:

```bash
khub query --type project        # → project/acme-diagnostic, with orphan/stale flags
khub neighbors acme-diagnostic   # → its client and owner edges
khub check                       # graph-wide: relations resolve, nothing dangling → passed
```

Every read command takes `--format json` for an agent and prints a Rich table for a human.

**Headless by design.** Every input is a flag; a missing one is a usage error, never a prompt. khub carried an interactive wizard through 0.8.0 behind a gate that switched it off for agents, pipes, and CI — dead weight on exactly the invocation khub is built for. Removed in 0.9.0, along with the `--agent` flag that disabled it.

## Why not a folder of Markdown, a database, or a RAG store?

| Instead of… | What you give up |
|---|---|
| A folder of Markdown / Obsidian | No schema, no typed relations, no integrity gate: nothing rejects a broken or dangling reference, and an agent can't walk the graph. |
| A database | Truth stops being git — no diff, no PR review, no plain-text portability — and the schema lives in migrations instead of one readable file. |
| A vector / RAG store | Retrieval is fuzzy and lossy: no exact relations to traverse, no completeness gate, and writes don't round-trip. |

khub keeps git as the source of truth, then adds a typed schema and a derived graph on top: exact reads, validated writes, and an integrity gate an agent can rely on.

## Presets

A preset is a canonical ontology for one domain — a directory holding its three layer files (`ontology.yaml` for entity types, attributes and legal relations; `policy.yaml` for workspace gates; `storage.yaml` for layouts and paths) and optional body `templates/`. `khub init` copies them into an engagement's `.khub/`, which agents and humans then extend as the work demands. The `base` block every entity carries (`type`, `created`/`updated`, `tags`, the OKF fields, the `any → any` edges) is embedded in the binary and supplied at resolve time — never copied into a workspace.

**Two presets ship today:**

- **`firm-ops`** — the operating graph of a consulting and delivery firm (client, project, person, opportunity, meeting, and more); see [`docs/firm-ops-preset.md`](docs/firm-ops-preset.md).
- **`build-hub`** — the knowledge hub of a build project: eleven types (prd, arc42, capability, actor, use-case, requirement, adr, system, component, api, repo) in one flat `knowledge/`; see [`docs/build-hub-preset.md`](docs/build-hub-preset.md). `build-lite` is an alias — `khub init build-lite` resolves to it and records `build-hub`. New to khub? Start with [`docs/getting-started.md`](docs/getting-started.md).

## Agent skills

khub ships two skills: `khub` (the read and write verbs) and `setup` (install the CLI, set up a project). They live in this repo under `plugin/skills/`, apart from the binary.

**Claude Code — the plugin.** The repo is a plugin marketplace. The `khub` plugin carries both skills and a session mod: compact rows for khub calls, integrity warnings after writes, a status line, and a pane over the graph. See [`docs/plugin.md`](docs/plugin.md).

```text
/plugin marketplace add endgame-build/khub
/plugin install khub@khub
```

**Every other agent — `npx skills`.** opencode, Cursor, Codex, Gemini CLI and any other agent that reads a `SKILL.md` get the two skills, with no mod. Needs Node and access to the repo:

```bash
npx skills add endgame-build/khub             # both skills, into the agents it finds
npx skills add endgame-build/khub -s setup    # only setup, on a machine with no khub yet
```

On a machine with no khub yet, install `setup` and ask the agent to set khub up.

**Status:** v1 engine shipped and in daily use on a live firm-operations corpus that cut over from hand-rolled scripts. Design rationale in [`docs/design-memo.md`](docs/design-memo.md); the collections row model in [`docs/collections-design.md`](docs/collections-design.md).

## License

Apache License 2.0 — see [`LICENSE`](LICENSE).
