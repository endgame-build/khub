# Knowledge Hub

A git-backed, ontology-typed knowledge base. Markdown files with YAML frontmatter are the source of truth; a [LinkML](https://linkml.io) ontology is the type system; a Python library and a generic `khub` CLI provide validated CRUD and graph queries.

Canonical ontology presets — one per engagement type — are the reusable IP. Each engagement gets its own repo seeded from a preset, where the team authors entities and extends the schema as the work demands.

**Status:** design. See [`docs/design-memo.md`](docs/design-memo.md).
