# build-hub preset

**One preset for a build project: eleven types that say what the product is, who uses it and what they do, what must hold, why, what exists, who owns it, and what talks to what.** Nothing in flight. A documented ladder for the types that come back only when a question needs them.

This describes preset version **0.6.0**, which ships: the merge of the old `build-hub` (seventeen types) and `build-lite` (six). The preset is the source of truth for its schema; this page is the reading copy. Six of the eleven, `prd`, `arc42`, `requirement`, `adr`, `component` and `repo`, are what `build-lite` 0.3.0 shipped and kb 0.14.0 still ships; the other five are new in 0.6.0.

```bash
khub init build-hub ./my-hub
khub init build-lite ./my-hub    # the same preset; build-lite is an alias
```

## What earns a slot

A type is here only if it is **referenced by id from elsewhere, walked as a graph, or gated by `check`**. Everything else has a cheaper home: prose a human reads start to finish is a body section, a fact stored once and read once is an attribute, and anything that churns with the code is a file in the repo linked via the base `resource`.

What is in flight (a feature spec, a plan, a work package) is not here at all. The framework that runs the build owns it and its directory; two tools writing one directory is one too many. An external spec points *in* through its own frontmatter (`use_cases: [uc-…]`, `requirements: [req-…]`, `decisions: [ad-…]`); khub neither scans it nor resolves those ids.

## The questions it answers

| Question an agent asks mid-build | Type |
|---|---|
| Who is this for, and what can they do? | `actor`, `use-case` |
| What does the product do, at map level? | `capability` |
| What must hold, and where does it hold? | `requirement` |
| Why can't I do X? | `adr`, via `affects` |
| What exists, who owns it, may I build on it? | `component`, with `owner`, `lifecycle`, `tier` |
| Which system is this in, and what do we sit on outside? | `system`; a vendor is a `component` with `kind: external` |
| What is promised, and who consumes it? | `api` |
| Where does the code live? | `repo` |
| What is this product, and how is it shaped? | `prd`, `arc42` |

## Layout

One knowledge root. No collections. Every type is a directory of markdown files with a lowercase prefixed slug, minted from the title; only `adr` carries a date, because a decision legitimately recurs under one title and a registry entry does not.

```
knowledge/
  prd.md                  # singleton — product source of truth; check fails without it
  arc42.md                # singleton — the architecture narrative
  capabilities/           # cap-<slug>
  actors/                 # act-<slug>
  use-cases/              # uc-<slug>
  requirements/           # req-<slug>
  decisions/              # ad-<YYYY-MM-DD>-<slug>
  systems/                # sys-<slug>
  components/             # cmp-<slug>
  apis/                   # api-<slug>
  repos/                  # rp-<slug>
index.md                  # the whole corpus in one file — written by init, refreshed by reindex
```

**Slugs are lowercase.** `add --id` slugifies whatever you pass and id resolution is case-sensitive. Minting reads no siblings, so two branches minting the same title collide on one filename instead of numbering past each other. The scheme is in [`schema.md`](schema.md).

## The eleven types

Every md type ships a body template; headings a type gained after it first shipped are `optional: true`. Each type also carries the base block (`title`, `description`, `resource`, `tags`, `draft`, `author`, `created`, `updated`; edges `related`, `sources`, `references`, `depends_on`).

- **prd** (`knowledge/prd.md`, singleton, `required: true`) — the product source of truth. Template: Document Purpose*, Vision, Target Users, Glossary, Features, Non-Goals, Deferred*, Success Metrics, Roadmap*, Alternatives & Recommendation*, Open Questions*, Assumptions Index*, Grounding Evidence*. Lenses: `scope`, `journeys`, `metrics`, `assumptions`. Absorbs the old hub's roadmap and glossary as sections.
- **arc42** (`knowledge/arc42.md`, singleton) — arc42's twelve sections under docs.arc42.org's names; the domain data model lives under Crosscutting Concepts as an ERD. Lenses: `direction`, `deferred`.
- **capability** (`knowledge/capabilities/`) — a coherent chunk of product value, the map every other record hangs off. Title only. Use cases belong to one; requirements are placed in several. This is also facet's join key (`FUNC-*`). Template: a hint plus optional Scope; lens `named` (a capability is a noun phrase a customer would recognise, not a component name).
- **actor** (`knowledge/actors/`) — a human role. Title only; a persona is its body. Template: optional Goals, Permissions. Automated triggers are not actors: a scheduled or event-driven use case carries `trigger` instead of an actor called "System".
- **use-case** (`knowledge/use-cases/`) — one goal one actor achieves, or a longer flow; the length lives in the body. `trigger: human | scheduled | event | external` (required). Edges: `actor` → actor, `capability` → capability, `served_by` → component (many, inverse `serves`). Template follows facet's use-case block: Preconditions, Main flow, Alternative flows*, Exception flows*, Postconditions. Lenses: `actor` (when `trigger: human`, the flow names who performs it), `served` (every step in the main flow names a component that exists in `served_by`), `observable` (postconditions can be checked from outside).
- **requirement** (`knowledge/requirements/`) — a rule the system must satisfy or never violate. `kind: functional | non-functional | constraint | business-rule` (required); `enforcement: ui | backend | database | external | review` (optional; what holds the rule up, and what breaks it silently). Edges: `capabilities` → capability (many), `use_cases` → use-case (many), `realized_in` → component (many). Template: no headings, one statement, EARS-friendly. Lenses: `single`, `testable`, `measurable` (when non-functional), `violation` (when constraint), `realized`. `kind: constraint` and `kind: non-functional` absorb the old hub's boundary and quality-attribute; `kind: business-rule` absorbs facet's rules, whose type becomes a tag.
- **adr** (`knowledge/decisions/`) — any decision record, product or technical: a choice made, rejected or revisited between real alternatives. `status: proposed | accepted | rejected` (required). Edges: `supersedes` → adr (inverse `superseded`, acyclic), `affects` → any (many; the blast-radius query that earns the type). Dated id. Template: Context, Alternatives*, Decision, Consequences, Prevents*. Lenses: `alternatives`, `consequences`, `reversal`, `rule`, `proposed`.
- **system** (`knowledge/systems/`) — a group of components with one owner and one boundary: C4's software system, Backstage's System. Title and `owner`. No stored edges; `component.system` points at it and `system.components` is computed. A one-system workspace never creates one and loses nothing. A vendor is not a system: it has APIs, not components we can name, so it stays a component with `kind: external`.
- **component** (`knowledge/components/`) — a part of the system that can break, be owned, or be swapped. `kind: service | library | external` (required); `stack`; `owner` (text, slug form); `lifecycle: experimental | production | deprecated`; `tier: tier-1 | tier-2 | tier-3`. Edges: `system` → system, `repo` → repo (ours only; an external has no codebase), `consumes` → api (many, inverse `consumed_by`). Base `depends_on` carries component → component and component → external component, which is what makes *what breaks when this vendor changes* a graph walk. Template: optional Responsibilities, Interfaces, Operational notes. Lenses: `boundary`, `blast` (when external), `located` (when service or library), `owned` (an ownerless component is the gap a catalog exists to expose).
- **api** (`knowledge/apis/`) — an interface a component promises: `kind: rest | graphql | grpc | events | data` (required), `status: proposed | active | deprecated` (required). Edge: `provider` → component (required, so `check` catches an API nobody owns; a vendor API's provider is the external component). Consumers are the computed inverse of `component.consumes`. The machine spec (OpenAPI, AsyncAPI, a schema) stays a file linked via `resource`; policy prose (versioning, auth, idempotency) is the body. Template: optional Versioning, Auth, Idempotency. Lenses: `spec` (a live API names its spec file), `consumed` (the consumers are in the graph).
- **repo** (`knowledge/repos/`) — a codebase in the registry: `repo` as `org/name` (pattern, loose; tighten to your org), `status: active | archived` (required). `component.repo` must resolve here. Template: optional What lives here, What does not. Lenses: `remote`, `archived`.

## Relation vocabulary

Every stored edge points *up* the durability ladder and lives on the entity being born; the inverse of each is computed at read time and never written. The field name is the predicate; the verb is how to read it.

| Predicate | From → to | Reads as | Required | Inverse |
|---|---|---|---|---|
| `actor` | use-case → actor | performed by | no (the `actor` lens asks for it when `trigger: human`) | computed |
| `capability` | use-case → capability | belongs to | no | computed |
| `served_by` | use-case → component (many) | served by | no | `serves`: what a component serves |
| `capabilities` | requirement → capability (many) | placed in | no | computed |
| `use_cases` | requirement → use-case (many) | governs | no | computed |
| `realized_in` | requirement → component (many) | realized in | no | computed |
| `supersedes` | adr → adr | supersedes | no; acyclic | `superseded` |
| `affects` | adr → any (many) | affects | no | computed |
| `system` | component → system | part of | no | computed: a system's components |
| `repo` | component → repo | lives in | no | computed |
| `consumes` | component → api (many) | consumes | no | `consumed_by` |
| `provider` | api → component | provided by | yes | computed: an owner's APIs |
| `depends_on` | any → any (many) | depends on | no; acyclic | computed |

Twelve predicates beyond the universal four, twelve declarations. `served_by` is ArchiMate's Serving relation read from the served side; `touches`, the blueprint's word for a step, was rejected because it names an event, not a relationship.

## The three walks

- **What breaks for users if this component dies.** `impact cmp-payments --reverse --predicate served_by`: the use cases whose `served_by` names it, then their actors.
- **What realizes this capability.** The use cases whose `capability` names it, then their `served_by` components. Two hops, never stored: a `component.capabilities` edge would only drift.
- **What breaks when an external API changes.** `impact api-stripe-charges --reverse --predicate consumes`: the components that consume it; then `impact <cmp> --reverse --predicate served_by` on each for the use cases they serve. Two walks, one per predicate.

## Ownership

`owner` is a text attribute on `component` and `system`, written in slug form (`team-payments`). It sits nowhere else:

- Not on `repo`. A monorepo has many owners and CODEOWNERS already says so per path; an external component has no repo yet someone owns the relationship. Repo ownership is derived from the components whose `repo` points at it.
- Not on `use-case` or `requirement`. A flow's owner is a product question the PRD answers.

`team` arrives when the first ownership question is a graph walk ("what does team-payments own"). Then `team` is a name-keyed record with a title and a contact channel, and the `owner` line on `component` changes from `{ type: text }` to `{ to: team }`; values already in slug form resolve without a file edit, and any written as prose show up as dangling, which is the migration list. People stay out: membership churns and lives in HR or Slack.

## Where it sits in ArchiMate

| Layer | khub | ArchiMate 3.2 element |
|---|---|---|
| Motivation | requirement, adr | Requirement, Constraint; a decision has no element |
| Strategy | capability | Capability |
| Business | actor, use-case | Business Actor / Role, Business Process (short) or Value Stream (long) |
| Application | component, system, api | Application Component, a grouping of them, Application Interface / Contract |
| Technology | repo | Artifact; nodes and environments stay in IaC, linked by `resource` |
| Implementation & Migration | nothing | Work Package, Plateau, Deliverable, deliberately absent |

`prd` and `arc42` are documents, not elements. The composite Grouping (domain) and Business Object (entity) are on the ladder.

## Importing from facet

facet-scan's functional view and knowledge graph, and facet-spec's capability specs, model the same spine: a role performs a use case, the use case is bound by business rules, and every rule and behaviour is restated as an EARS requirement, all grouped by capability. The map, one row per facet id family:

| facet artifact | Carries | khub home | What is lost, and where it goes |
|---|---|---|---|
| Role · Actor registry | name, human or automated, permissions, goals | `actor` | automated actors become a `trigger` on the use case |
| `UC-*` · `BP-*` · `FT-*` | actor, preconditions, flows, postconditions, rules, trigger | `use-case` | nothing; the template takes facet's block verbatim |
| `BR-*` | entity, type (six concerns), enforcement, capabilities | `requirement`, `kind: business-rule` | type becomes a tag; enforcement is the attribute; the entity link waits for `entity` |
| `EARS-*` · `ASR-*` | EARS statement, source rule or use case | `requirement`, body in EARS syntax, `use_cases` to the source | the EARS letter becomes a tag |
| `CONST-*` | severity, lifecycle, enforcement, linked ADR and risk | `requirement`, `kind: constraint` | severity becomes a tag; the ADR link is `adr.affects` the other way |
| `FUNC-*` | name, domain, priority | `capability` | nothing |
| `COMP-*` · `INT-*` · `TECH-*` | criticality, layer, protocol, version | `component` with `tier`, `lifecycle`, `stack`; an `INT` is an external component and its `api` | version and radar ring stay in the asset inventory, via `resource` |
| DATA entity · `DOM:*` | owner system; domain type | `entity`, `domain` on the ladder | nothing once they ship |
| `ADR-*` · `RISK-*` · `SEC-*` · `DEPL-*` | decision, risk, control, node | `adr`; the rest are arc42 §11 and §7 or `resource` links | severity on risks and controls stays in facet's files |

Where the facet id lives is not settled here. The design memo calls it `source_id` and wants it on every type, which is the base block, not this preset; until the first import is written a `facet:FUNC-007` tag is enough to find things.

## The ladder

Each step is a schema edit in the workspace's `.khub/ontology.yaml` and a row in `storage.yaml`; no surface code changes. Order matters more than the list.

**1. `entity`** — where is Customer stored, who writes it. Around fifteen components.

```yaml
entity:
  when: a thing the system stores is identified, with one owner and a lifecycle
  attributes: { title: { required: true } }
  relations:
    owner: { to: component, required: true }   # single-writer, schema-enforced
# storage:  entity: { layout: file, path: knowledge/entities, id_prefix: ent }
```

**2. `domain`** — bounded contexts, once components stop being the map.

```yaml
domain:
  when: a bounded area of the problem space is named as having its own language and rules
  attributes:
    title:        { required: true }
    tier:         { enum: [core, supporting, generic] }
    relationship: { enum: [conformist, customer-supplier, partnership, shared-kernel, acl] }
  relations:
    depends_on: { to: domain, many: true }   # narrows the base edge; cycle-checked
# component gains  domain: { to: domain }
# storage:  domain: { layout: file, path: knowledge/domains, id_prefix: dom }
```

**3. `team`** — promote `owner` when ownership is a walk.

```yaml
team:
  when: a team is named as owning something, or as the people to ask
  attributes: { title: { required: true }, channel: { type: text } }
# component.owner and system.owner change from { type: text } to { to: team }
# storage:  team: { layout: file, path: knowledge/teams, id_prefix: team }
```

**4. `quality-attribute` and `boundary`** — only when `requirement.kind` and `enforcement` stop being enough: a target needs `scenario` and `measurement` as queryable fields, or an invariant needs a named enforcement mechanism with its own fan-out.

```yaml
quality-attribute:
  when: a non-functional target is named with a number worth holding to
  attributes:
    title:       { required: true }
    scenario:    { required: true }
    measurement: { type: text }
    enforcement: { enum: [architecture-test, ci-gate, linter, code-review, manual, monitoring-alert] }
  relations:
    applies_to: { to: [system, component], many: true }
boundary:
  when: a limit is stated that separates what we control from what we do not
  attributes:
    title:       { required: true }
    rule:        { required: true }
    enforcement: { enum: [architecture-test, ci-gate, linter, code-review, manual] }
  relations:
    affects: { to: any, many: true }
# storage:  quality-attribute: { layout: file, path: knowledge/quality-attributes, id_prefix: qa }
#           boundary:          { layout: file, path: knowledge/boundaries,          id_prefix: bound }
```

**5. `event`** — only if `api` with `kind: events` is not enough: who reacts to OrderPlaced, by name.

```yaml
event:
  when: a business event is named that more than one component reacts to
  attributes: { title: { required: true } }
  relations:
    produced_by: { to: component, required: true }
# component gains  reacts_to: { to: event, many: true, inverse: consumed_by }
# storage:  event: { layout: file, path: knowledge/events, id_prefix: evt }
```

## What was cut, and where it lives

| Cut | Replaced by |
|---|---|
| `feature-spec`, `test-spec`, `work-package`, `specs/` | the framework that runs the build; an external spec points in through its own frontmatter. kb 0.14.0 made the same cut. |
| `roadmap`, `glossary` | prd sections |
| `erd` | the arc42 Crosscutting Concepts section |
| `pdr` | `adr` is any decision record |
| `baseline` | CI and dashboards; a git corpus records metrics stale by construction |
| `contract` | `api`, under Backstage's name, with `provider` on the api and `consumes` on the component |
| `external-system` | `component` with `kind: external`, the fold build-hub 0.3.0 made; a vendor has APIs, not components we can name |
| `boundary`, `quality-attribute`, `domain`, `entity` | the ladder |
| the nested `knowledge/{product,architecture}` layout | flat; a workspace moving from the old hub relocates files once |
| `facet_id` | a tag until the first import decides `source_id` |

## Narrative roots and the orphan sweep

`prd` and `arc42` declare `orphan: true` in `policy.yaml`. Every stored edge points up the durability ladder and both documents sit above its top, so nothing points at them and they point at nothing; without the flag they would be permanent findings that no authoring could ever close, and `check --strict` could never go green on a correct workspace. The flag is per type, never inferred from `layout: singleton`. See [`schema.md`](schema.md#type-level-gates).

## kb parity

kb 0.14.0 ships those six types with the same ids, paths and predicates. A corpus authored by kb graduates with `khub init build-lite . --force` (the alias) and `check` is clean on it: none of the five new types has entities yet, and no new edge is required. The five additions are khub-only until kb follows; nothing in them changes a file kb wrote.

## Design capture

The `ontology.yaml` header carries the authoring decisions and the cut list; `init` strips comments from the workspace copy, so the header costs an agent nothing. This page is the reading copy.
