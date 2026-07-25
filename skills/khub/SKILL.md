---
name: khub
description: Use when working in a khub workspace (a repository with a .khub/ directory) to read or write typed, schema-validated context instead of grepping Markdown. Covers retrieval (query, get, neighbors, impact, history, search) and authoring (add, edit, link, unlink, remove). Discovers the active preset, types, and schema at runtime.
allowed-tools: Bash
---

# khub: typed context for agents

khub gives you typed, validated, queryable context held as Markdown in git. In a khub workspace (any directory with a `.khub/` above it), prefer khub over reading or grepping files: it returns typed records and enforces the schema on every write. The core library is the only place logic lives; this skill maps your intent onto the CLI and adds none.

If `khub` is not installed (`command not found`), load the `setup` skill first — it installs the CLI and sets up the project.

## Discover the ontology first

Never hardcode a type or a field. Read the live schema:

- `khub status --format json` — counts per type, draft/active, and the active preset.
- `khub schema --format json` — every type with its fields, enums, required flags, and relations.
- `khub schema show <type> --format json` — one type's shape; build an `add`/`edit` from it.

The schema lives at `.khub/schema.yaml`; the active preset (`firm-ops`, `build-hub`, or `build-lite`) is recorded in `.khub/config.yaml`. `khub wire` links the schema into the agent context files (`CLAUDE.md`, `AGENTS.md`), so the ontology may already be in your context.

## Retrieve

- `khub query --type <t> [--<field> <v>] [--tag <t>] [--has <pred>] [--missing <pred>] --format json` — filter by frontmatter; `--missing` surfaces gaps.
- `khub get <id> --edges --format json` — one entity plus its derived inverse edges.
- `khub neighbors <id> --format json` — one-hop adjacency.
- `khub impact <id> --format json` — transitive closure over a predicate (blast radius).
- `khub history <id> --format json` — a supersession chain.
- `khub search <text> --format json` — BM25 full-text over titles, bodies, and fields.

## Know when to write

Most missed capture is not a wrong command — it is no command, because a terse ask reads as
conversation. "Policy: credentials must never appear in output. Note it." is a `requirement`,
not a reply.

Each type declares `when` — the moment to capture it, in the domain's own language. Read them
from `khub schema --format json` (or the wired block, which lists them), and treat them as
triggers: when the moment occurs, record it and say you did. Propose the record in place rather
than asking permission for each one — capture is never blocked, and a `--draft` entity is the
right answer when you are unsure it belongs.

A stated fact about the system is a write. A question about the system is a query.

## Write

Agent and human write the same graph through the same gates; there is no approval step.
Every write goes through the CLI — the only path that validates and resolves relations. If an
entity file gets hand-edited anyway, run `khub validate` on it immediately: an unvalidated
hand-edit is how a workspace acquires a field no default gate will report.

- `khub add <type> --<field> <v> [--body <text>] [--draft] --format json` — mint an entity. Set relation fields inline (`--client acme-corp`). A relation to a missing target is rejected; a missing required field is captured anyway and reported later by `khub check`.
- `khub edit <id> <field> <v> --format json` — change a field; `edit <id> draft false` publishes.
- `khub link <id> <predicate> <target>` / `khub unlink <id> <predicate> <target> --format json` — relations, idempotent. Read `changed` to tell a write from a no-op; both exit 0.
- `khub remove <id> --format json` — delete; refuses while an inbound edge resolves to it unless `--force`.

## Rules

- Every read AND write emits JSON on a pipe; a table is only for a TTY. `--format json` makes it explicit.
- Build every write from `khub schema show`, never from a hardcoded shape.
- On a failed command, read the located error (field, reason, or unresolved target) and correct the call.
- Gate before you commit: `khub validate` per entity, `khub check` graph-wide.
- Run `khub validate --strict` in CI. Capture is never blocked, so a typo'd field name (`--knid`) is written to frontmatter and neither default gate reports it; `--strict` is what turns undeclared keys into a finding.
- `khub search` and `khub query --has/--missing` both reach attribute values, so find an entity by what it holds (`search python`, `query --type component --missing repo`) rather than listing and filtering yourself.
