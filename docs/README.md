# khub documentation

khub's docs, organized by [Diátaxis](https://diataxis.fr): learning, tasks, reference, and background. New to khub? Read them top to bottom.

## Tutorial — learn by doing

- [Getting started](getting-started.md) — build a firm-ops hub from scratch: add entities, walk the graph, and gate it with `check`. Every command is verified against a live run.

## How-to guides — get a task done

- [Author and extend a schema](schema.md) — the `base` block, attributes and relations, storage config, and `--strict`.
- Wire khub into an agent — see [Use it from Claude Code](../README.md#use-it-from-claude-code) in the README and the `khub wire` entry in the [CLI reference](cli.md).

## Reference — look it up

- [CLI](cli.md) — every command, its flags, the JSON record shape, and validation semantics.
- [firm-ops preset](firm-ops-preset.md) — the shipped ontology: 9 entity types, 14 relation predicates, per-type attributes.
- [Collections](collections-design.md) — the single-file collection row model: identity, locking, malformed handling, git attribution.

## Explanation — understand the design

- [Concepts](concepts.md) — the mental model in seven ideas: Markdown is truth, the derived graph, validate vs check, and more.
- [Design memo](design-memo.md) — rationale, the five-layer engine, technology choices, and the invariants every change preserves.

---

Contributing? See [`CONTRIBUTING.md`](../CONTRIBUTING.md). Release process: [`RELEASING.md`](../RELEASING.md). History: [`CHANGELOG.md`](../CHANGELOG.md).
