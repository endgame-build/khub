# khub documentation

khub's docs, organized by [Diátaxis](https://diataxis.fr): learning, tasks, reference, and background. New to khub? Read them top to bottom.

## Tutorial — learn by doing

- [Getting started](getting-started.md) — build a firm-ops hub from scratch: add entities, walk the graph, and gate it with `check`. Every command is verified against a live run.

## How-to guides — get a task done

- [Author and extend a schema](schema.md) — the `base` block, attributes and relations, storage config, and `--strict`.
- Wire khub into an agent — see [Agent skills](../README.md#agent-skills) in the README and the `khub wire` entry in the [CLI reference](cli.md).
- [Use khub inside Claude Code](plugin.md) — the plugin: the skills, a status band above the prompt, compact rows for khub calls.

## Reference — look it up

- [CLI](cli.md) — every command, its flags, the JSON record shape, and validation semantics.
- [CLI reference](cli-reference.md) — the `--help` text of every command, regenerated from the binary.
- [firm-ops preset](firm-ops-preset.md) — the operating graph of a consulting and delivery firm: 9 entity types, 14 relation predicates, per-type attributes.
- [build-hub preset](build-hub-preset.md) — the knowledge hub of a build project: eleven types, the edges and the walks they buy. `build-lite` is an alias that resolves to it.
- [Collections](collections-design.md) — the single-file collection row model: identity, locking, malformed handling, git attribution.

## Explanation — understand the design

- [Concepts](concepts.md) — the mental model in seven ideas: Markdown is truth, the derived graph, validate vs check, and more.
- [Design memo](design-memo.md) — the five-layer engine, technology choices, and the invariants every change preserves.

---

Contributing? See [`CONTRIBUTING.md`](../CONTRIBUTING.md). Release process: [`RELEASING.md`](../RELEASING.md). History: [`CHANGELOG.md`](../CHANGELOG.md).
