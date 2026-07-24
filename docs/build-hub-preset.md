# Build-hub preset

**The knowledge hub of a build project.** `build-hub` models what a software system *is*, *why it
is shaped this way*, and *what to build next*, in the repo that sits above the code repos: the
functional map (capabilities), the obligations (requirements), the governance layer (domains,
entities, boundaries, quality attributes), the topology (components, repos, contracts, external
systems, baselines), the reasons (architecture and product decision records), and the work spine
(feature specs, test specs, work packages). The hub holds every cross-repo fact; anything a
pipeline regenerates — facet views, knowledge-graph JSONL — is linked out via `resource`, never
duplicated as entities.

`khub init build-hub ./my-hub` seeds a workspace from it. For a hands-on first run, start with
[`getting-started.md`](getting-started.md); to author or extend the types yourself, see
[`schema.md`](schema.md).

Preset version **0.2.0**. Sixteen entity types. Seventeen relation predicates in twenty-five
declarations beyond the four universal edges from the core base (`domain.depends_on` narrows the
universal edge to a typed `domain → domain`).

**Spoke repos carry no khub workspace.** A spoke is code plus one plain `entities.yaml`
field-schema file (validated by the spoke's own CI, `resource`-linked from hub entity records).
Every decision — repo-local included — is a hub `adr`/`pdr`. The corpus is layout-invariant:
monorepo and multi-repo use this same preset; only the delivery topology differs (multi-repo pins
a versioned contracts artifact; monorepo reads HEAD).

## Scope: one hub per what?

One hub per **system** — the set of repos that can break each other: they share a contract, a
capability, a data store, one product brief. **The hub follows coupling**, not org lines and not
flow.

| Candidate boundary | Verdict | Why |
|---|---|---|
| per system | the definition | the hub exists to manage change propagation between coupled repos |
| per product | usually right | a product is normally one coupled repo set with one brief — same boundary, different name |
| per value stream | right when aligned | healthy orgs align stream boundaries with coupling (Conway); when they diverge, follow coupling |
| per team | never | teams are orthogonal — a hub per team deletes the space *between* teams, which is where the hub earns its keep |

Tie-breaker when unsure: *if this repo changes, whose `check` should trip?* Everything inside one
answer's radius is one hub. Two loosely-coupled products are two hubs, each modeling the other as
an `external-system`; a portfolio view is a projection over multiple hubs, never a bigger hub.
Entry threshold: one team and a couple of repos don't need a hub — adopt it when the second team
or the first cross-repo contract arrives.

## Layout: two knowledge roots

- **`knowledge/`** — durable truth, split `product/` (what and why, product-side) vs
  `architecture/` (how-shaped and what-must-hold). Directories by domain, types by term — the
  `decisions/` pair (`knowledge/product/decisions/`, `knowledge/architecture/decisions/`) is the
  pattern.
- **`specs/`** — delivery state at the workspace root: a different cadence with a different
  writer (the tracker sync flips `work-package.status`; everything else is reviewed prose).

Narrative documents — PRD, roadmap, arc42, overview, glossary — are **not** entity types. They
live in the same two roots as plain prose, outside the inventory directories, and link entity
slugs inline. Two stock-storage conventions keep prose near its records without tripping the
stray gate: ERD prose lives at `knowledge/architecture/entities/_index.md` (file scans skip
`_index.md`), and machine specs live under `contracts/specs/` (a subdirectory — invisible to the
single-level scan).

## The three altitudes

- **capability** — what the system does. Durable, survives reorganizations of the work.
- **feature-spec** — a bounded change to the system, and its record. There is no separate
  "feature" container: the FS carries the work status and the placement/obligation edges.
- **work-package** — an executable slice of a feature-spec. The unit an agent or engineer picks up.

## Storage forms and naming

One rule decides file vs collection: **prose a human reviews → one file per record, ID-enumerated
slug; homogeneous wiring → a registry collection row, name-keyed** (the registry file is the
enumeration).

| Form | Types · slug scheme |
|---|---|
| File, ID-enumerated | adr `AD-NNN-slug` · pdr `PD-NNN-slug` · boundary `BOUND-NNN-slug` · quality-attribute `QA-NNN-slug` · requirement `FR-NNN`/`CST-NNN` · capability `CAP-NNN-slug` · component `CMP-NNN-slug` · feature-spec `FS-NNN-slug` · test-spec `TS-NNN-slug` · work-package `WP-NNN-slug` |
| File, name-keyed | domain · entity · contract · external-system (natural-name identity; contracts name-keyed so `consumes: readings-api` reads) |
| Collection (yaml) | `knowledge/architecture/repos.yaml` · `knowledge/architecture/baselines.yaml` |

## What every entity carries

`khub init` merges `core.yaml` into the preset, so every build-hub entity carries the base block:
attributes `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`,
`tags`; universal edges `related`, `sources`, `references`, `depends_on`. The tables below list
only what build-hub adds or tightens. Every markdown type tightens `title` to required (it mints
the slug).

## Relation vocabulary

| Predicate | From → To | Required |
|---|---|---|
| `capabilities` | requirement, feature-spec → capability (many) | no |
| `requirements` | feature-spec → requirement (many) | no |
| `realized_in` | requirement → repo (many) | no |
| `feature` | work-package → feature-spec | yes |
| `repo` | work-package → repo; component → repo | wp no · component yes |
| `verifies` | test-spec → feature-spec | yes |
| `supersedes` | adr → adr; pdr → pdr; feature-spec → feature-spec | no |
| `affects` | adr, pdr, boundary → any (many) | no |
| `drivers` | adr → quality-attribute (many) | no |
| `produces` | adr → boundary (many) | no |
| `depends_on` | domain → domain (many; narrowed from the universal edge) | no |
| `reads` | domain → entity (many) | no |
| `owner` | entity → domain | yes (single) |
| `applies_to` | quality-attribute → domain \| component (many) | no |
| `domains` | component → domain (many) | no |
| `provider` | contract → component \| external-system | yes |
| `consumes` | component, external-system → contract (many) | no |
| `component` | baseline → component | no |

Two conventions drive the edge placement:

- **Upstream linking.** Every stored edge points up the durability ladder — `work-package →
  feature-spec → requirement → capability` — and the edit lands on the entity being born, never
  on one that already exists. Reverse directions (consumed_by, an FS's work packages, which
  feature-specs satisfy a requirement, a superseded record) are computed at read time.
- **Split by churn** (contracts). `provider` lives on the contract: required and near-immutable,
  so `check` catches a contract nobody owns. `consumes` lives on each consuming component or
  external-system — a new consumer is a one-line edit in its own file, and the contract never
  accumulates a stale consumer list.

## The sixteen entities

### Product — what and why

- **capability** (`knowledge/product/capabilities/`) — the durable functional map; `facet_id`
  joins the facet-synthesis inventory. Vocabulary is hub-owned: work claims against these slugs.
- **requirement** (`knowledge/product/requirements/`) — `kind: functional | constraint |
  business-rule`; EARS-friendly prose in the body; `capabilities` places it, `realized_in` is the
  stored backstop for reality outside the work spine. NFRs are quality-attribute entities, not
  requirements. The PRD narrates and links `FR-NNN` slugs; coverage (every requirement carries at
  least one inbound `requirements` edge from a live feature-spec) is a graph check.
- **pdr** (`knowledge/product/decisions/`) — product decision record; `status: proposed |
  accepted | rejected`, `supersedes` self-typed, `affects → any`.

### Architecture — how-shaped and what-must-hold

- **adr** (`knowledge/architecture/decisions/`) — the pdr shape plus `drivers →
  quality-attribute` (why) and `produces → boundary` (what invariant it created).
- **domain** (`knowledge/architecture/domains/`) — the bounded context. `tier: core | supporting
  | generic`; `depends_on → domain` is the cycle-checked predicate; `relationship` (conformist ·
  customer-supplier · partnership · shared-kernel · acl) is a plain attribute qualifying its
  dependency posture — khub edges carry no properties. **The domain body is the blueprint
  narration**: responsibilities, ubiquitous language, acceptance criteria.
- **entity** (`knowledge/architecture/entities/`) — identity plus a one-paragraph definition.
  `owner → domain` is single-valued and **required**: the single-writer rule is schema-enforced —
  a second authoritative writer is unrepresentable. `reads` edges from other domains make
  read-without-ownership a lintable fact. Field detail churns with code and lives in the owning
  spoke's `entities.yaml` via `resource`.
- **boundary** (`knowledge/architecture/boundaries/`) — an extend-never-weaken invariant:
  `scope`, `enforcement` (architecture-test · ci-gate · linter · code-review · manual), the
  one-sentence `rule`, and `affects` fan-out. ADRs `produce` boundaries; blueprints' acceptance
  criteria cite them.
- **quality-attribute** (`knowledge/architecture/quality-attributes/`) — a concrete measurable
  `scenario` with `measurement` and `enforcement`; `applies_to` domains or components. Absorbs
  classic NFR lists.
- **component** (`knowledge/architecture/components/`) — the deployable: `kind`, `stack`, a
  required `repo` edge (the component↔codebase mapping), the `domains` it hosts, and the churny
  `consumes` side of contract edges. The body describes the deployable — stack rationale,
  operational notes.
- **repo** (`knowledge/architecture/repos.yaml`, collection) — a pure remotes record: `repo`
  (org/name, loosely pattern-pinned — tighten to your org), `status: active | archived`.
- **contract** (`knowledge/architecture/contracts/`, **yaml-format file entities**) — hub-authored
  interface records: `kind: api | events | data`, `status: proposed | active | deprecated`,
  required `provider → component | external-system`. Policy prose (idempotency, auth model,
  versioning) rides the reserved `body` field; the machine spec is a standalone
  `contracts/specs/<slug>.openapi.yaml` linked via `resource`, so codegen and contract tests
  consume it directly. Consumers are the computed inverse of `consumes`.
- **external-system** (`knowledge/architecture/external-systems/`) — a vendor or neighboring
  product; `consumes` the contracts we emit to it (e.g. webhooks).
- **baseline** (`knowledge/architecture/baselines.yaml`, collection) — quality bars: `metric`,
  `value` (text — accommodates `"80"` and `"99.9%"` alike), `direction`, `as_of`, `source`, and a
  `component` edge.

### Specs — delivery state

- **feature-spec** (`specs/feature-specs/`) — the feature record: `status: planned | active |
  done | dropped`, `capabilities` (placement), `requirements` (obligation), `supersedes`
  self-typed.
- **test-spec** (`specs/test-specs/`) — `verifies → feature-spec`, required. The FS→TS pair is
  what upgrades "we merged code" into "the rule demonstrably holds."
- **work-package** (`specs/work-packages/`) — the executable slice: required `feature` edge, a
  `repo` routing edge (one repo; a slice spanning repos gets split), coarse `status` the tracker
  sync flips on merge.

## Authorship: everything is hub-authored

Previous versions of this preset family split authorship three ways (hub-authored, spoke-synced
contracts, spoke-resident TDRs) and shipped a `build-spoke` counterpart preset. **v0.2.0 removes
all of it.** The litmus that survived: knowledge with cross-repo meaning is hub-authored, and all
of it now lives in this one workspace; knowledge that churns with code (field schemas) stays in
the spoke as a plain file the hub links via `resource`, with the spoke's own CI keeping it honest.
Contract changes ride hub PRs; a provider team reviews there. No sync machinery, no provenance
stamps, no cross-workspace references.

## The implementation loop

Nothing is ever marked "implemented" — hand-flipped status is the first thing to rot. The PR
merge is the only real event, it enters the graph in one place (the tracker flips `work-package
status → done`), and everything else is traversal:

```
PR merged in a spoke
  → work-package status → done          (tracker sync)
  → all of the feature-spec's slices done?   (traversal)
  → feature-spec.requirements            (the obligation binding)
  → requirement implemented — in the repos those slices' `repo` edges name
```

Two grades of knowing: **claimed** — every feature-spec binding the requirement has all
work-packages done; **verified** — additionally, the FS's `test-spec` suite is green in the
spoke's CI. `requirement.realized_in` is the stored backstop for reality the work spine never
touched (brownfield bootstrap, hotfixes); derivation is the audit, and disagreement between the
two is a lint, not a debate.

## Bootstrap from facet (legacy import)

`facet_id` on capability / requirement / adr / contract is the import traceability spine. Legacy
import is a one-time curated promotion (facet output → hub entities); facet reruns feed a
reconcile report, never an overwrite. `pdr` carries no `facet_id` — facet has no product-decision
concept.

## Design note: what was deliberately left out

- **feature and solution-spec** (removed in 0.2.0) — the FS is the feature record; RFC-style
  multi-component designs distill directly into adrs and contracts.
- **build-spoke preset** (removed in 0.2.0) — spokes carry no typed knowledge.
- **Ceremonial inventories** (stakeholder map, risk register, traceability matrix, environments)
  — only ever existed as frozen one-shot pipeline output. Add back via schema edit if the
  engagement starts writing them.
- **Component/module maps, runbooks, debt and insight types** — rot fastest, or live in the
  instruction/memory layer, not the entity graph.
- **Edge properties, per-type heading contracts, ignore globs** — considered as engine features
  during the paved-road-hub convergence and found unnecessary: a plain attribute, hub-side
  validators, and the `_index.md`/subdirectory conventions cover the same ground with stock
  storage.

## See also

- [`schema.md`](schema.md) — author or extend types; `.khub/schema.yaml` is the editable copy.
- [`firm-ops-preset.md`](firm-ops-preset.md) — the consulting-firm operating graph.
- [`collections-design.md`](collections-design.md) — the registry row model.
