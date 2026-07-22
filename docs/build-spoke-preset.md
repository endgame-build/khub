# Build-spoke preset

**The per-code-repo side of a build project.** `build-spoke` is the counterpart of [`build-hub`](build-hub-preset.md): a minimal workspace inside a code repo, holding the two things only that repo can be authoritative about — its public surface (the contracts it provides) and its interior (local technical decisions). Everything system-level lives in the hub; a spoke claims against hub vocabulary and never mints it.

`khub init build-spoke .` seeds it into a code repo. All inventories live under `docs/` — khub must not colonize a code repo's root.

Preset version **0.1.0**. Two entity types. Two relation predicates, plus the four universal edges from the core base.

## The ownership contract

Per the [authorship model](build-hub-preset.md#authorship-hub-spoke-synced-spoke-resident):

- **contract** is the only entity that crosses upward: born here, synced into the hub with `synced_from` provenance, validated hub-side on ingest.
- **tdr** is spoke-resident: a decision about this repo's ORM means nothing across repos. Graduation to system scope is a hand promotion into a hub `adr`/`pdr`.
- Spoke→hub references are plain-text hub slugs (`hub_refs`), never typed edges — cross-workspace edges cannot be integrity-checked in v1.

## The two entities

### contract

Layout: file (`docs/contracts/{slug}.md`), spec file as sibling (`docs/contracts/{slug}.yaml`). Identical shape to the hub's contract minus `provider` — the provider is this repo, injected hub-side at sync from the repo of origin, so it can be neither declared nor wrong.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `kind` | enum, required | `api`, `events`, `data` — must stay identical to build-hub (add-only); guarded by a parity test |
| `status` | enum, required | `proposed`, `active`, `deprecated` |
| `facet_id` | text | travels with the sync |

Relations: none beyond the universal edges. The spec is linked via `resource`; the body carries idempotency rules, auth model, versioning policy.

### tdr

Layout: file (`docs/decisions/{slug}.md`). A technical decision record scoped to this repo: context, options, outcome in the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `status` | enum, required | `proposed`, `accepted`, `rejected` |
| `hub_refs` | list | plain-text hub slugs this decision touches |

Relations: `supersedes` → tdr, `affects` → any (many). Superseded state is the computed inverse of `supersedes`, never stored.

## Deliberately absent

- **insight** — gotchas and learnings live in the instruction/memory layer (CLAUDE.learnings.md, agent memories), not in the entity graph.
- **component/module map** — rots fastest of all doc types; facet regenerates it on demand.
- **runbook, debt** — zero alive instances across the audited engagements.
- **repo self-declaration manifest, feature-spec/test-spec** — explicit decision: no spoke→hub sync for them; repo rows and specs are hub-authored.
- The agent brief stays in AGENTS.md/CLAUDE.md — instructions are not entities.

## See also

- [`build-hub-preset.md`](build-hub-preset.md) — the system-level counterpart and the authorship model.
- [`schema.md`](schema.md) — how presets are authored and extended.
