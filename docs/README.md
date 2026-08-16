# khub documentation

khub's docs, organized by [Diátaxis](https://diataxis.fr): learning, tasks, reference, and background. New to khub? Read them top to bottom.

## Tutorial — learn by doing

- [Getting started](getting-started.md) — build a firm-ops hub from scratch: add entities, walk the graph, and gate it with `check`. Every command is verified against a live run.

## How-to guides — get a task done

- [Author and extend a schema](schema.md) — the `base` block, attributes and relations, storage config, and `--strict`.
- Wire khub into an agent — see [Use it from Claude Code](../README.md#use-it-from-claude-code) in the README and the `khub wire` entry in the [CLI reference](cli.md).

## Reference — look it up

- [CLI](cli.md) — every command, its flags, the JSON record shape, and validation semantics.
- [firm-ops preset](firm-ops-preset.md) — the consulting-firm operating graph: 9 entity types, 14 relation predicates, per-type attributes.
- [build-hub preset](build-hub-preset.md) — the knowledge hub of a build project: 20 types across knowledge/{product,architecture} + specs/; spokes carry no khub workspace.
- [build-lite preset](build-lite-preset.md) — build-hub cut to necessity: 6 types, what each cut replaces, and the order the rest comes back.
- [Collections](collections-design.md) — the single-file collection row model: identity, locking, malformed handling, git attribution.

## Explanation — understand the design

- [Concepts](concepts.md) — the mental model in seven ideas: Markdown is truth, the derived graph, validate vs check, and more.
- [Design memo](design-memo.md) — rationale, the five-layer engine, technology choices, and the invariants every change preserves.
- [A Go rewrite of khub](go-rewrite-memo.md) — proposed, gated: what a port gains, the verified Go stack per dependency, why not Rust or TypeScript, and the YAML round-trip prototype that decides it.
- [build-lite, standalone](build-lite-standalone.md) — the preset reimplemented as one skill plus one dependency-free script for opencode: what earns code when the only caller is an agent, what was cut, and why the corpus still graduates to khub.
- [IWE, feature by feature](iwe-comparison.md) — the closest independent sibling reviewed against khub: where the architectures converge, why its document-only validation is no threat to the graph contract, and the eight things worth taking.
- [OntoGraph, reviewed](ontograph-review.md) — an OWL visualization service, dead since 2019 and unbuildable: why it is rejected as a tool, and the three things worth taking anyway (a schema-diagram idea, a peer-reviewed citation, a caveated test corpus).
- [Feature candidates](feature-candidates.md) — the related-work proposal backlog: numbered, pick-and-choose, with what was explicitly rejected and why.

---

Contributing? See [`CONTRIBUTING.md`](../CONTRIBUTING.md). Release process: [`RELEASING.md`](../RELEASING.md). History: [`CHANGELOG.md`](../CHANGELOG.md).
