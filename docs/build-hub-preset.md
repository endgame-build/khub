# Build-hub preset

**The system-level description of a build project.** `build-hub` models what a multi-repo software system *is* and *why*, in the repo that sits above the code repos: the durable functional map (capabilities), the interfaces between parties (contracts, external systems), the reasons behind the shape (architecture and product decision records), what must hold (requirements), and what to build next (features, work packages, and the feature/test spec pair). The hub holds only knowledge that is meaningful across repos; anything a pipeline regenerates — facet views, knowledge-graph JSONL, OpenAPI specs — is linked out via `resource`, never duplicated as entities.

`khub init build-hub ./my-hub` seeds a workspace from it. For a hands-on first run, start with [`getting-started.md`](getting-started.md); to author or extend the types yourself, see [`schema.md`](schema.md).

Preset version **0.1.0**. Eleven entity types. Eight relation predicates in fourteen declarations, plus the four universal edges from the core base.

## The three altitudes

Three type names sit close together; the boundary matters for agents:

- **capability** — what the system does. Durable, survives reorganizations of the work.
- **feature** — a bounded change to the system. A work container with a lifecycle.
- **work-package** — an executable slice of a feature. The unit an agent or engineer picks up.

## What every entity carries

`khub init` merges `core.yaml` into the preset, so every build-hub entity carries the base block on top of its own fields:

- **Base attributes** — `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, and `tags`.
- **Four universal edges** — `related`, `sources`, `references`, `depends_on`, each `any → any` and many-valued.

The tables below list only what build-hub adds or tightens. Every markdown type tightens `title` to required (it mints the slug).

## Relation vocabulary

| Predicate | From → To | Required |
|---|---|---|
| `capabilities` | feature, requirement, repo → capability (many) | no |
| `feature` | work-package, feature-spec → feature | yes |
| `requirements` | work-package → requirement (many) | no |
| `verifies` | test-spec → feature-spec | yes |
| `supersedes` | adr → adr; pdr → pdr | no |
| `affects` | adr, pdr → any (many) | no |
| `provider` | contract → repo \| external-system | yes |
| `consumes` | repo, external-system → contract (many) | no |
| `related` | any → any (many) | no |
| `sources` | any → any (many) | no |
| `references` | any → any (many) | no |
| `depends_on` | any → any (many) | no |

Two conventions drive the edge placement:

- **Upstream linking.** Leaves point at roots: work packages point at features, features at capabilities, consumers at contracts. The reverse directions (`consumed_by`, a feature's work packages, a superseded decision) are computed at read time, never stored.
- **Split by churn** (contracts). `provider` lives on the contract: it is required and near-immutable, so `check` catches a contract nobody owns. `consumes` lives on each consumer — the repo row or external-system file its owner already edits — so a new consumer is a one-word edit in its own file, and the contract never accumulates a stale consumer list.

## The eleven entities

### capability

Layout: file (`capabilities/{slug}.md`). A durable unit of the functional map; what the system does, independent of how the work is sliced.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | overrides the base default; mints the slug |
| `facet_id` | text | `UCAP-XXX` — joins the facet-synthesis inventory |

Relations: none beyond the universal edges. Everything else points here.

### feature

Layout: file (`features/{slug}.md`). A bounded change to the system; the work container.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `status` | enum, required | `planned`, `active`, `done`, `dropped` |

Relations: `capabilities` → capability (many).

### work-package

Layout: file (`work-packages/{slug}.md`). An executable slice of a feature; acceptance criteria live in the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `status` | enum, required | `planned`, `active`, `done`, `dropped` |

Relations: `feature` → feature (required), `requirements` → requirement (many).

Status is deliberately coarse: khub is the spec-of-record, and the execution tracker (beads, Jira) owns fine-grained state. Four values keep the sync a trivial mapping. No status field in this preset carries a schema default — `add` applies none, so status is always an explicit statement, and a missing one is exactly what `check` reports.

### feature-spec

Layout: file (`specs/feature/{slug}.md`). The elaboration of a feature before implementation — forge's FS artifact as a hub entity. The spec prose is the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |

Relations: `feature` → feature (required).

### test-spec

Layout: file (`specs/test/{slug}.md`). The test counterpart of a feature spec — forge's TS artifact; scenarios and coverage mapping in the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |

Relations: `verifies` → feature-spec (required). The FS→TS pairing as a graph edge.

### requirement

Layout: file (`requirements/{slug}.md`). A system-level requirement that belongs to no single work package; EARS-friendly prose in the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `kind` | enum, required | `functional`, `quality`, `constraint`, `business-rule` |

Relations: `capabilities` → capability (many).

### adr

Layout: file (`decisions/architecture/{slug}.md`). An architecture decision record: context, options, outcome in the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `status` | enum, required | `proposed`, `accepted`, `rejected` |

Relations: `supersedes` → adr, `affects` → any (many). A decision is *superseded* when another decision's `supersedes` edge points at it — the state is computed, never stored, so it cannot drift.

### pdr

Layout: file (`decisions/product/{slug}.md`). A product decision record; same shape as `adr`, separate type by design.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `status` | enum, required | `proposed`, `accepted`, `rejected` |

Relations: `supersedes` → pdr, `affects` → any (many).

### repo

Layout: collection (`repos.jsonl`, one row per code repo). The hub↔spoke join point.

| Attribute | Type | Notes |
|---|---|---|
| `repo` | text, required | `org/name`, pattern `^[a-z0-9._-]+/[a-z0-9._-]+$` — tighten to your org in the engagement schema |
| `status` | enum, required | `active`, `archived` |

Relations: `capabilities` → capability (many), `consumes` → contract (many).

Convention: the slug is the git basename — the same join key facet's merge-kg uses (`codebase`). Slug and repo name may diverge; the row's `body` notes why.

### contract

Layout: file (`contracts/{slug}.md`). One entity per interface surface — not per endpoint, not per spec file. A provider exposing a REST API and emitting events is two contracts.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `kind` | enum, required | `api` (REST/GraphQL/gRPC — the linked spec says which), `events` (queues, webhooks), `data` (shared schema/DB/file coupling) |
| `status` | enum, required | `proposed`, `active`, `deprecated` |
| `facet_id` | text | `OAPI-XXX` / `AAPI-XXX` — joins facet-contract output |

Relations: `provider` → repo \| external-system (required, union).

The authoritative spec is linked via `resource`. The body carries what a spec cannot: idempotency rules, auth model, versioning policy, known consumer assumptions.

### external-system

Layout: file (`external-systems/{slug}.md`). A third-party dependency agents must not guess about: vendor APIs, PMS integrations, payment providers.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |

Relations: `consumes` → contract (many) — for surfaces we expose to the vendor, such as webhooks. A vendor API we call is a contract whose `provider` is the external system.

## Design note: what was deliberately left out

The type list was cut against the doc types that stay alive in real engagement hubs (umbrella, hooli, vandelay). These did not make it, each with the same profile — only ever written once, as generated pipeline output, then frozen:

- **stakeholder**, **term/glossary**, **environment**, **risk**, **milestone**, **persona**, **pattern** — zero hand-maintained instances anywhere. Add any of them back with a schema edit the day the engagement starts writing them.
- **The spoke preset** (per-code-repo types) is deferred. Spoke→hub references are plain-text hub slugs regardless, since cross-workspace edges cannot be integrity-checked in v1.
- **The product brief** stays a plain file in the hub repo; singletons do not need a schema type.

## See also

- [`schema.md`](schema.md) — how presets are authored and extended.
- [`firm-ops-preset.md`](firm-ops-preset.md) — the consulting-firm operating graph.
- [`cli.md`](cli.md) — the full command surface.
