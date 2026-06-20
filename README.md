# Knowledge Hub (khub)

**Structured, schema-bound context management for analytical and operational work.**

khub gives an AI agent typed, validated, queryable context — structured memory it can navigate and write back to — instead of unstructured documents stuffed into a context window.

One generic engine: every entity is one Markdown file with YAML frontmatter, held in git. A [LinkML](https://linkml.io) ontology is the contract — types, attributes, and legal relations. A Python core library provides schema-validated CRUD and graph queries; a generic `khub` CLI and a Claude Code skill are thin, schema-driven surfaces over it. The graph is a projection rebuilt from the Markdown on demand — no database is ever the source of truth.

Built on top of the [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md). A khub workspace is a conformant OKF bundle; khub is an OKF implementation and extension, layering a typed schema and graph over the same markdown-plus-frontmatter substrate.

**The schema is the operational setup.** It configures what a given hub is *for*. Canonical ontology presets — one per domain (building and maintaining a system, consulting, research, operations) — are the reusable IP. Each engagement gets its own repo seeded from a preset, where agents and humans co-author entities and extend the schema as the work demands.

**Status:** design — engine and v1 seed schema locked. See [`docs/design-memo.md`](docs/design-memo.md).
