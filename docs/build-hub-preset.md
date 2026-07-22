# Build-hub preset

**The system-level description of a build project.** `build-hub` models what a multi-repo software system *is* and *why*, in the repo that sits above the code repos: the durable functional map (capabilities), the interfaces between parties (contracts, external systems), the reasons behind the shape (architecture and product decision records), what must hold (requirements), and what to build next (features, work packages, and the feature/test spec pair). The hub holds only knowledge that is meaningful across repos; anything a pipeline regenerates — facet views, knowledge-graph JSONL, OpenAPI specs — is linked out via `resource`, never duplicated as entities.

`khub init build-hub ./my-hub` seeds a workspace from it. For a hands-on first run, start with [`getting-started.md`](getting-started.md); to author or extend the types yourself, see [`schema.md`](schema.md).

Preset version **0.1.0**. Twelve entity types. Ten relation predicates in eighteen declarations, plus the four universal edges from the core base.

## Scope: one hub per what?

One hub per **system** — the set of repos that can break each other: they share a contract, a capability, a data store, one product brief. **The hub follows coupling**, not org lines and not flow.

| Candidate boundary | Verdict | Why |
|---|---|---|
| per system | the definition | the hub exists to manage change propagation between coupled repos |
| per product | usually right | a product is normally one coupled repo set with one brief — same boundary, different name |
| per value stream | right when aligned | healthy orgs align stream boundaries with coupling (Conway); when they diverge, follow coupling |
| per team | never | teams are orthogonal — a hub per team deletes the space *between* teams, which is where the hub earns its keep; inter-team contracts would lose their single home |

Tie-breaker when unsure: *if this repo changes, whose `check` should trip?* Everything inside one answer's radius is one hub.

The divergence cases resolve without federation:

- **One journey across two loosely-coupled products** (checkout hands off to a separately-shipped billing product over one stable API): two hubs, each modeling the other as an `external-system`. Hubs never federate; they see each other as vendors.
- **One tightly-coupled platform serving several streams**: one hub, several streams drawing from it. Splitting by stream scatters the platform's contracts and breaks "who do we break" exactly where the platform needs it most.
- **Portfolio view** (across systems) is a projection over multiple hubs — the way Backstage projects one — never a bigger hub.

Entry threshold: one team and a couple of repos don't need a hub — a README and a tracker are the right tool. Adopt the hub when the second team or the first cross-repo contract arrives.

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
| `requirements` | feature → requirement (many) | no |
| `repo` | work-package → repo | no |
| `realized_in` | requirement → repo (many) | no |
| `verifies` | test-spec → feature-spec | yes |
| `supersedes` | adr → adr; pdr → pdr; solution-spec → solution-spec | no |
| `affects` | adr, pdr, solution-spec → any (many) | no |
| `provider` | contract → repo \| external-system | yes |
| `consumes` | repo, external-system → contract (many) | no |
| `related` | any → any (many) | no |
| `sources` | any → any (many) | no |
| `references` | any → any (many) | no |
| `depends_on` | any → any (many) | no |

Two conventions drive the edge placement:

- **Upstream linking.** Every stored edge points up the durability ladder — `work-package → feature → requirement → capability` (weeks → months → years → lifetime) — and time flows down it: the edit lands on the entity being born, never on one that already exists. The reverse directions (`consumed_by`, a feature's work packages, which features satisfy a requirement, a superseded decision) are computed at read time, never stored.
- **Split by churn** (contracts). `provider` lives on the contract: it is required and near-immutable, so `check` catches a contract nobody owns. `consumes` lives on each consumer — the repo row or external-system file its owner already edits — so a new consumer is a one-word edit in its own file, and the contract never accumulates a stale consumer list.

## The twelve entities

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

Relations: `capabilities` → capability (many) — *placement*: where in the system map this change belongs. `requirements` → requirement (many) — *obligation*: which statements this change satisfies, and the spine implementation status derives from. The two edges answer different questions; disagreement between them is information, not drift.

### work-package

Layout: file (`work-packages/{slug}.md`). An executable slice of a feature; acceptance criteria live in the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `status` | enum, required | `planned`, `active`, `done`, `dropped` |

Relations: `feature` → feature (required), `repo` → repo — where the slice lands, single-valued on purpose: a slice spanning repos gets split, and the schema says so.

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

### solution-spec

Layout: file (`specs/solution/{slug}.md`). The architectural counterpart of the feature-spec: where a feature-spec connects requirements and product decisions on the behavior side, a solution-spec connects components and architecture decisions on the structure side — the standalone design document for a change that spans repos.

It carries **RFC semantics**: a point-in-time intended design, never a living architecture document. RFCs age well precisely because they don't pretend to be current — accepted choices distill into `adr`s, surfaces into `contract`s, and the next design supersedes this one rather than editing it.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `status` | enum, required | `proposed`, `accepted`, `rejected` |
| `facet_id` | text | `TDR-NN` — as-built design imported at bootstrap |

Relations: `supersedes` → solution-spec, `affects` → any (many) — the components it connects: repos, contracts, capabilities, features.

### requirement

Layout: file (`requirements/{slug}.md`). A system-level requirement that belongs to no single work package; EARS-friendly prose in the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `kind` | enum, required | `functional`, `quality`, `constraint`, `business-rule` |
| `facet_id` | text | `EARS-`/`NFR-`/`CONST-XXX` — import traceability |

Relations: `capabilities` → capability (many), `realized_in` → repo (many) — the stored backstop for reality the work spine never touched: brownfield bootstrap and hotfixes. For tracked work, implementation status derives from the graph (see [The implementation loop](#the-implementation-loop)); derivation audits the claim.

### adr

Layout: file (`decisions/architecture/{slug}.md`). An architecture decision record: context, options, outcome in the body.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |
| `status` | enum, required | `proposed`, `accepted`, `rejected` |
| `facet_id` | text | `ADR-`/`TDD-XXX` — import traceability |

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

Contracts are born in their provider's spoke and synced into the hub; vendor contracts are the hand-authored exception. See [Authorship](#authorship-hub-spoke-synced-spoke-resident).

### external-system

Layout: file (`external-systems/{slug}.md`). A third-party dependency agents must not guess about: vendor APIs, PMS integrations, payment providers.

| Attribute | Type | Notes |
|---|---|---|
| `title` | required | |

Relations: `consumes` → contract (many) — for surfaces we expose to the vendor, such as webhooks. A vendor API we call is a contract whose `provider` is the external system.

## Authorship: hub, spoke-synced, spoke-resident

Every entity has exactly one authoring home. The litmus test for spoke authorship: does exactly one spoke naturally own it, can CI next to the code keep it honest, and does it still mean something across repos? All three yes → spoke-authored and synced to the hub. Cross-repo meaning missing → spoke-authored but spoke-resident. Single ownership missing → hub-authored.

| Class | Types | Mechanics |
|---|---|---|
| Hub-authored | capability, feature, work-package, requirement, adr (system scope), pdr, external-system, vendor contracts | authored in place; no sync |
| Spoke-authored, hub-synced | contract (born in its provider repo) — the only synced type | copied up by a sync with `synced_from` provenance; hub `validate`/`check` run on ingest |
| Spoke-authored, spoke-resident | repo-local decisions (TDRs) — see [`build-spoke-preset.md`](build-spoke-preset.md) | never synced; promoted to hub entities by hand when they graduate to system scope |

One rule keeps the classes stable: **vocabulary is hub-owned; spokes claim against it.** Capabilities are many-realized, so spoke-minted capabilities would fragment the map (three teams minting "reservations", "reservation-mgmt", "booking"). Spokes reference hub capability slugs; the claims travel, the vocabulary never does.

Contracts are the only entities that cross the boundary upward — a spoke is authoritative for its public surface, and the contract is that surface. Repo rows and feature/test specs stay hub-authored; repo interiors (TDRs, insights) stay home.

### The contract lifecycle

A contract entity is born in its provider's spoke — `docs/contracts/<slug>.md` in this same `contract` schema, spec file as sibling — from `proposed` onward. The provider team flips status, writes the semantics prose, and evolves the spec behind contract tests and breaking-change gates. The hub's `contracts/` inventory is a synced projection of those files: the sync stamps provenance, derives `provider` from the repo of origin, and refuses slug collisions across spokes rather than dedupe-renaming (a silently renamed contract breaks consumer edges). `consumes` edges stay on hub `repos.jsonl` rows, so the blast-radius query resolves against one node, and a spoke deleting a still-consumed contract surfaces as dangling edges at the next hub `check` — the cross-repo breaking-change tripwire.

Two exceptions are hand-authored in hub `contracts/`, distinguishable by the absent `synced_from`: vendor contracts (no spoke exists; the spec is a vendored snapshot under `specs/contract/`, pinned deliberately — the vendor drifting is their event), and the rare design-first contract whose provider repo does not exist yet (create the repo first where possible; the proposed contract should be its first commit).

`specs/contract/` holds hub-side draft specs and vendor snapshots only; a living spec for an implemented contract lives in its provider repo, and Backstage or any other catalog is a read-side projection of this graph, never a source.

## The implementation loop

Nothing is ever marked "implemented" — hand-flipped status is the first thing to rot. The PR merge is the only real event, it enters the graph in one place (the tracker flips work-package `status`), and everything else is traversal:

```
PR merged in a spoke
  → work-package status → done          (tracker sync — already happens)
  → all of the feature's slices done?   (traversal)
  → feature.requirements                (the obligation binding)
  → requirement implemented — in the repos those slices' `repo` edges name
```

Two grades of knowing:

- **Claimed** — every feature binding the requirement has all work-packages done. Derived from merge events, drift-free.
- **Verified** — additionally, the feature's `test-spec` suite is green in the spoke's CI. The FS→TS pair is what upgrades "we merged code" into "the rule demonstrably holds."

"Partial" is a fact, not a label: claimed in one repo, unverified, absent in another. The one stored exception is `requirement.realized_in` — for reality the work spine never touched (brownfield bootstrap, hotfixes). For tracked work it is at most a cached conclusion; derivation is the audit, and disagreement between the two is a lint, not a debate.

## Bootstrap from facet (legacy import)

A legacy estate enters this same thin structure. Run the recovery pipelines (facet-scan per repo, facet-synthesis for the system), then promote the entity-like outputs into hub entities — a **one-time curated promotion**, after which the hub owns them. Re-running facet later feeds a reconcile report against the curated hub, never an overwrite. That keeps the "generated is never resident" rule intact: these entities stop being generated the moment humans adopt them.

| facet output | Hub home | Import rule |
|---|---|---|
| capabilities (`UCAP`/`FUNC`) | capability | `facet_id` join |
| EARS / NFR / constraints / business rules | requirement | kind: EARS → functional or business-rule, NFR → quality, CONST → constraint; `realized_in` records where they already hold |
| ADR collection + tech decisions (`TDD`) | adr | inferred (implicit) decisions enter `proposed`, humans accept; product-flavored ones re-homed to pdr by hand — facet has no product-decision concept |
| contract specs (OpenAPI / AsyncAPI / DDL / SLO) | contract | provider assigned from the owning repo |
| external integrations (`INT`) | external-system + contract | a provider absent from the repo inventory becomes an external-system |
| as-built technical design (`TDR`) | solution-spec, `accepted` | a point-in-time record of the design *as found*; `sources` → the TDR |
| repo inventory | repos.jsonl | slug = git basename (facet's merge join key) |
| deep views, risk registers, threat models, use-case inventories | not entities | linked via `resource`; regenerable |

Statuses are assigned "as found"; every imported entity carries its `facet_id`. Afterwards, `check` reports the unwired remainder — that is the curation backlog, not an error.

## Design note: what was deliberately left out

The type list was cut against the doc types that stay alive in real engagement hubs (umbrella, hooli, vandelay). These did not make it, each with the same profile — only ever written once, as generated pipeline output, then frozen:

- **stakeholder**, **term/glossary**, **environment**, **risk**, **milestone**, **persona**, **pattern** — zero hand-maintained instances anywhere. Add any of them back with a schema edit the day the engagement starts writing them.
- **The spoke side** lives in its own preset — [`build-spoke-preset.md`](build-spoke-preset.md). Spoke→hub references are plain-text hub slugs, since cross-workspace edges cannot be integrity-checked in v1.
- **The product brief** stays a plain file in the hub repo; singletons do not need a schema type.

## See also

- [`schema.md`](schema.md) — how presets are authored and extended.
- [`firm-ops-preset.md`](firm-ops-preset.md) — the consulting-firm operating graph.
- [`cli.md`](cli.md) — the full command surface.
