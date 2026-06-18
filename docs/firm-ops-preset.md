# Firm-Operations Preset (HQ): Entity, Property, and Relation Capture

**Status:** captured from the live `firm-hq` repo, 2026-06-18. This is the full ontology of the firm HQ, read as a khub preset candidate. The design memo names four canonical presets: engineering, consulting, research, **firm operations (HQ-style)**. HQ is the working instance of the fourth. Source of truth read: `firm-hq/.claude/hq.schema.yml` (the firm's encoded model), cross-checked against `frontmatter.md`, `build-graph.py`, `kb.py`, and the live graph.

## The Verdict

HQ is a reusable preset. The design memo already predicted this: *"firm-hq is the working precedent for the projection-and-validation pattern; khub generalizes it ... and is itself the kind of operational hub an HQ preset would produce."* HQ runs the same five-layer engine khub specifies, on a hand-rolled schema instead of LinkML. Porting it to khub means expressing `hq.schema.yml` as a LinkML ontology and swapping `kb.py` for the generic core library. The model below is the IP: the firm's judgment about how to model running a consulting firm.

The preset's spine is the **opportunity → project/build** lifecycle (the deal becomes the work), wrapped by a **client/partnership/person directory**, fed by a **meeting/transcript activity stream**, and governed by **decision** and **isms-doc** records. Twelve entity types, nineteen relation predicates.

## Engine, as HQ Runs It Today (Maps 1:1 to khub's Five Layers)

| khub layer | khub spec | HQ today | Port action |
|---|---|---|---|
| 1 Source of truth | Markdown + YAML frontmatter, git | identical | none |
| 2 Ontology | LinkML (YAML) | `hq.schema.yml` (custom `edges:` + `nodes:` YAML) | translate to LinkML |
| 3 Core library | Python validate/create/get/update/delete/link/query | `kb.py`: `validate`, `query`, `report`, `reindex`, `backfill` | replace with generic core |
| 4 Graph projection | in-memory (v1), SQLite fast-follow | already SQLite: `build-graph.py` → `.hq-graph.sqlite` (`nodes`, `edges`, `fts5`) | HQ is ahead of v1 here |
| 5 Access | CLI + Claude Code skill | `kb.py` CLI + `.claude/` skills, hooks, rules | generalize CLI |

**Integrity loop, HQ → khub:** `kb.py validate` → khub `validate` (per-entity well-formedness + referential integrity). `report --orphans` → khub `check` (no inbound edges). `report --stale` (>30 days) → khub `stale`. `report --impact <slug>` → khub blast-radius query. `backfill` (dates from git) → khub `stale` git backfill. HQ has no `khub log` analog and no `draft|active` lifecycle; it uses per-type `stage`/`status` enums instead.

**Edge mechanic (the invariant that already matches):** `build-graph.py` walks frontmatter, and only fields the schema marks `edge:` become graph edges; the field name is the predicate, the value is the target slug, stored single-sided on the source node (`build-graph.py:117-134`). This is khub invariant #4 exactly, with one exception flagged in the porting notes (`supersedes`/`superseded_by` are both stored).

## Relation Vocabulary: All 19 Predicates

Only these frontmatter fields are edges. Everything else is opaque metadata.

| Predicate | From | To | Card | Meaning |
|---|---|---|---|---|
| `owner` | any | person | 1 | Person accountable |
| `client` | opportunity, project, build, case-study | client | 1 | Client entity |
| `team` | project, build | person | N | ENDGAME people assigned |
| `engagement` | meeting | opportunity, project, build, partnership | 1 | Parent entity slug |
| `origin_opportunity` | project, build | opportunity | 1 | Conversion lineage |
| `source_project` | case-study | project, build | 1 | Source engagement |
| `transcript` | meeting | transcript | 1 | Processed transcript path |
| `supersedes` | decision | decision | 1 | Replaces |
| `superseded_by` | decision | decision | 1 | Replaced by (stored inverse; see porting notes) |
| `affects` | decision | any | N | Entities impacted |
| `promoted_to` | fragment | file | 1 | Reference-area destination |
| `controls` | isms-doc | external | N | ISO 27001 Annex A control IDs |
| `partner` | opportunity, project, build, client | partnership | 1 | Partnership involvement |
| `related` | any | any | N | Free-form cross-links |
| `related_opportunities` | partnership | opportunity | N | Pipeline from partner |
| `related_projects` | partnership | project, build | N | Engagements from partner |
| `sources` | any | any | N | Backing evidence |
| `references` | any | any | N | Soft link |
| `depends_on` | any | any | N | Hard dependency |

`related`, `sources`, `references`, `depends_on` are universal (`any → any`); the rest are typed. Note `partner` is declared `from: [opportunity, project, build]` in the edge vocabulary, yet the `client` node also carries it; capture reflects actual usage.

## Entities: Full Inventory

Twelve node types. Each is one `.md` file with frontmatter. `req` = required for validation. Fields marked **→** are graph edges.

### 1. opportunity: Pipeline Deal

Identity: `opportunities/{slug}/CLAUDE.md`. The pre-sale entity; converts to a project or build.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `opportunity` |
| `client` | string | ✓ | **→ client** |
| `stage` | enum | ✓ | nurturing, discovery, proposal, negotiation, closed-won, closed-lost |
| `owner` | string | ✓ | **→ person** |
| `created` | date | ✓ | |
| `updated` | date | ✓ | |
| `source` | enum | | event, referral, outbound, inbound, northwind, existing-client |
| `airtable_id` | string | | `^rec[A-Za-z0-9]+$` (legacy CRM, no longer written) |
| `crm_id` | string | | `^[0-9]+$` (CRM deal id) |
| `external_repo` | string | | `^endgame-build/[a-z0-9-]+$` |
| `notes_folder` | string | | meetings-triage routing key |
| `notes_folder_id` | string | | rename-proof routing key (`fol_…`) |
| `partner` | string | | **→ partnership** |
| `related` | list | | **→ any** |
| `tags` | list | | |
| `confidence` | float | | 0–1 |
| `last_confirmed` | date | | |
| `sources` | list | | **→ any** |

### 2. project: Delivery Engagement

Identity: `projects/{slug}/CLAUDE.md`. The post-sale consulting engagement. Shares its path with `build`, discriminated by `type`.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `project` |
| `client` | string | ✓ | **→ client** |
| `stage` | enum | ✓ | diagnose, prove, scale, complete |
| `owner` | string | ✓ | **→ person** |
| `external_repo` | string | ✓* | `^endgame-build/[a-z0-9-]+$` (required per frontmatter.md; absent on internal projects in the live graph) |
| `created` | date | ✓ | |
| `updated` | date | ✓ | |
| `source` | enum | | event, referral, outbound, inbound, northwind, existing-client |
| `airtable_id` | string | | `^rec[A-Za-z0-9]+$` |
| `crm_id` | string | | `^[0-9]+$` (project id) |
| `budget_id` | string | | `^[0-9]+$` (companion budget-deal) |
| `origin_opportunity` | string | | **→ opportunity** (conversion lineage) |
| `notes_folder` | string | | |
| `notes_folder_id` | string | | |
| `partner` | string | | **→ partnership** |
| `team` | list | | **→ person** |
| `related` | list | | **→ any** |
| `tags` | list | | |
| `confidence` | float | | 0–1 |
| `last_confirmed` | date | | |
| `sources` | list | | **→ any** |

### 3. build: Product/Engineering Engagement

Identity: `projects/{slug}/CLAUDE.md`. The "we build it" variant of project; same path, `type: build`. Adds `tech_stack`; different lifecycle.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `build` |
| `client` | string | ✓ | **→ client** |
| `stage` | enum | ✓ | scoping, building, delivered, maintaining, complete |
| `owner` | string | ✓ | **→ person** |
| `external_repo` | string | ✓ | `^endgame-build/[a-z0-9-]+$` |
| `created` | date | ✓ | |
| `updated` | date | ✓ | |
| `source` | enum | | event, referral, outbound, inbound, northwind, existing-client |
| `airtable_id` | string | | `^rec[A-Za-z0-9]+$` |
| `crm_id` | string | | `^[0-9]+$` |
| `budget_id` | string | | `^[0-9]+$` |
| `origin_opportunity` | string | | **→ opportunity** |
| `notes_folder` | string | | |
| `notes_folder_id` | string | | |
| `partner` | string | | **→ partnership** |
| `team` | list | | **→ person** |
| `tech_stack` | list | | |
| `related` | list | | **→ any** |
| `tags` | list | | |
| `confidence` | float | | 0–1 |
| `last_confirmed` | date | | |
| `sources` | list | | **→ any** |

### 4. meeting: Engagement Touchpoint

Identity: `{projects,opportunities,partnerships}/*/meetings/*.md`. A processed meeting note, attached to a parent engagement and (optionally) its raw transcript.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `meeting` |
| `date` | date | ✓ | event date |
| `engagement` | string | ✓ | **→ opportunity \| project \| build \| partnership** (parent slug) |
| `call_type` | enum | ✓ | client, sales, partner, internal (internal = all attendees @end.game) |
| `source` | enum | ✓ | recording, manual |
| `note_id` | string | | Recorder note id; primary dedup key |
| `transcript` | string | | **→ transcript** |
| `attendees` | list | | items `{ name, email }` |
| `owner` | string | | **→ person** |
| `tags` | list | | |

### 5. transcript: Raw Meeting Capture

Identity: `inbox/*/*.md`, `*/meetings/transcripts/*.md`. The unprocessed Recorder export; lands in `inbox/`, gets routed by `notes_folder`. Largest node population in the live graph (140).

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `transcript` |
| `source` | const | ✓ | `recording` |
| `note_id` | string | ✓ | Recorder note id |
| `date` | date-time | ✓ | |
| `title` | string | ✓ | |
| `creator` | string | ✓ | |
| `creator_email` | string | ✓ | format: email |
| `notes_folder` | string | ✓ | |
| `notes_folder_id` | string | | |
| `calendar_event_id` | string | | nullable |
| `attendees` | list | | items `{ name, email }` |

### 6. fragment: Atomic Thought

Identity: `fragments/{owner}/*.md`. A partner's personal note; matures through stages and may be promoted into a reference area.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `fragment` |
| `owner` | string | ✓ | **→ person** (also the folder partition) |
| `stage` | enum | ✓ | raw, mature, synthesis, promoted |
| `created` | date | ✓ | |
| `updated` | date | | |
| `tags` | list | | |
| `related` | list | | **→ any** |
| `promoted_to` | string | | **→ file** (reference-area destination) |
| `confidence` | float | | 0–1 |
| `sources` | list | | **→ any** |

### 7. decision: Durable Record (ADR-Style)

Identity: `archive/decisions/*.md`. A firm decision with a supersession chain and impact set. The `decided_by` field is a plain name list, not a typed edge to person.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `decision` |
| `date` | date | ✓ | when decided |
| `status` | enum | ✓ | active, superseded, withdrawn |
| `decided_by` | list | ✓ | names (untyped) |
| `created` | date | ✓ | |
| `updated` | date | ✓ | |
| `supersedes` | string | | **→ decision** |
| `superseded_by` | string | | **→ decision** (stored inverse; porting note) |
| `affects` | list | | **→ any** |
| `tags` | list | | |

### 8. case-study: Proven Outcome

Identity: `case-studies/*.md` (published under `engagement/case-studies/` in the live repo). A client outcome derived from a delivered engagement.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `case-study` |
| `client` | string | ✓ | **→ client** |
| `published` | boolean | ✓ | |
| `created` | date | ✓ | |
| `updated` | date | ✓ | |
| `industry` | string | | |
| `source_project` | string | | **→ project \| build** |
| `phase` | enum | | diagnose, prove, scale, full-cycle |
| `tags` | list | | |
| `related` | list | | **→ any** |

### 9. isms-doc: Compliance Artifact

Identity: `security/**/*.md`. An ISO 27001 ISMS document, linked to Annex A controls by id. Owner: Noor.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `isms-doc` |
| `doc_kind` | enum | ✓ | policy, procedure, plan, risk-register, control, audit-record, asset-inventory, template, questionnaire |
| `status` | enum | ✓ | draft, review, approved, implemented, retired |
| `owner` | string | ✓ | **→ person** |
| `reviewed` | date | ✓ | |
| `created` | date | ✓ | |
| `updated` | date | ✓ | |
| `next_review` | date | | |
| `controls` | list | | **→ external**, items `^[A-Z]\.[0-9]+(\.[0-9]+)?$` (Annex A ids) |
| `tags` | list | | |

### 10. partnership: BD Relationship

Identity: `partnerships/{partner}/CLAUDE.md`. A business-development relationship that feeds opportunities and projects. It carries inbound edges from the pipeline (`related_opportunities`, `related_projects`).

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `partnership` |
| `partner` | string | ✓ | partner name (the slug source; not an edge here) |
| `stage` | enum | ✓ | prospect, active, dormant, ended |
| `owner` | string | ✓ | **→ person** |
| `created` | date | ✓ | |
| `updated` | date | ✓ | |
| `primary_contact` | object | | `{ name, role, email }` |
| `related_opportunities` | list | | **→ opportunity** |
| `related_projects` | list | | **→ project \| build** |
| `notes_folder` | string | | |
| `notes_folder_id` | string | | |
| `tags` | list | | |

### 11. person: ENDGAME Team Member

Identity: `identity/team/{slug}.md`. The accountability target for `owner` and `team` edges across the graph.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `person` |
| `name` | string | ✓ | |
| `role` | enum | ✓ | partner, associated-partner, business-consultant, director, principal, staff, senior, lead, hr |
| `department` | enum | | engineering, consulting, delivery-management, people, sales |
| `created` | date | ✓ | |
| `updated` | date | | |
| `tags` | list | | |

### 12. client: Client Organization

Identity: `clients/{slug}.md`. The organization behind opportunities, projects, builds, and case studies.

| Field | Type | Req | Constraint / edge |
|---|---|---|---|
| `type` | const | ✓ | `client` |
| `name` | string | ✓ | |
| `industry` | string | | |
| `airtable_id` | string | | `^rec[A-Za-z0-9]+$` |
| `crm_id` | string | | `^[0-9]+$` (company id) |
| `partner` | string | | **→ partnership** |
| `website` | string | | |
| `created` | date | ✓ | |
| `updated` | date | ✓ | |
| `tags` | list | | |

## Reference Areas: What HQ Holds That Is Not a Graph Node

HQ is more than the typed graph. Roughly half the repo is untyped reference markdown that carries no frontmatter and never validates (by design; `frontmatter.md` lists what is excluded). A khub firm-ops preset either leaves these as plain content or, later, models them with a generic `Document` type. Capturing them so the model is complete:

| Area | Path | Holds | Graph status |
|---|---|---|---|
| Identity | `identity/` | mission, values, principles, proposition, insights | untyped reference (except `identity/team/` = person nodes) |
| Brand | `identity/brand/` | personality, story, messaging, positioning | untyped reference |
| Engagement | `engagement/` | model, playbooks, icp, offerings, practices | untyped reference |
| Playbooks | `engagement/playbooks/` | academy, ai-deployment, build-handover, delivery-transformation | untyped reference |
| Proposals | `engagement/proposals/` | live + generated (`out/`) | untyped reference |
| Templates | `engagement/templates/`, `security/_templates/` | reusable docs; full ISO 27001/22301 template tree | untyped reference |
| Tools | `engagement/tools/` | facet, flow, forge, factory, hq, endgame plugin suite | untyped reference |
| Security | `security/` | ISMS working tree (the `**/*.md` that DO validate become isms-doc nodes) | mixed |
| Research | `research/` | competitive intelligence (currently near-empty) | untyped reference |
| Inbox | `inbox/` | unprocessed transcripts (become transcript nodes on routing) | staging |
| Archive | `archive/` | completed projects, past meetings; `archive/decisions/` = decision nodes | mixed |
| Fragments | `fragments/{owner}/` | partner thinking (fragment nodes) | typed |

External integrations the model references but does not own: **CRM** (`crm_id`, `budget_id`: deals, projects, companies, budgets), **Recorder** (`notes_folder*`, `note_id`: transcripts), **Airtable** (`airtable_id`: legacy CRM, read-only), **endgame-build/** GitHub org (`external_repo`), **ISO 27001 Annex A** (`controls`). In khub terms these are `source_id`/`external_url` aliases, not entities.

## Cutover: khub Replaces HQ's Incumbent Engine

v1 builds khub and cuts firm-hq over to it: install khub, seed from the firm-ops preset, and retire the bespoke scripts. HQ becomes a khub workspace. Because Markdown is the source of truth, this is an engine swap, not a data migration; the entity files stay where they are.

**Retired (khub takes over):**

| Incumbent | Replaced by |
|---|---|
| `.claude/hq.schema.yml` | `.khub/schema.yaml`: LinkML firm-ops preset, `core` flattened in |
| `kb.py` validate/query/report | khub core library + `khub` CLI (`validate`, `check`, `stale`, `query`, `log`) |
| `build-graph.py` → `.hq-graph.sqlite` | khub SQLite projection under `.khub/generated/` (gitignored) |
| `.claude/rules/frontmatter.md` | derived from the schema at runtime, never hand-maintained |

**Kept (outside khub's remit):**

- The entity `.md` files stay untouched; khub reads the same frontmatter in place.
- Integration automation: `notes-export.py`, `crm.py`, `airtable.py`, `meetings-triage`, the GitHub workflows. These are HQ's ingestion and sync, outside the generic engine. They keep running and call khub's library where they now call `kb.py`.
- Reference areas (`identity/`, `engagement/`, `security/` templates) stay plain markdown, with no engine needed.

**What the cutover needs (v1 delivers the first three; search waits on the fast-follow):**

- **The firm-ops preset.** The LinkML port of `hq.schema.yml` (12 types, 19 edges) from this capture. In v1.
- **`index.md` regeneration.** HQ's `kb.py reindex` keeps the nav page current; `khub reindex` reproduces it. In v1.
- **Date backfill from git.** HQ's `kb.py backfill` maps to `khub backfill`, extended to insert missing frontmatter. In v1.
- **SQLite + FTS search.** HQ runs `fts5` today; khub's in-memory v1 has no full-text search, so `kb.py` stays for search until the SQLite fast-follow lands.

**Migration steps (one-time):**

1. Author the firm-ops LinkML preset in the hub repo from this capture; resolve the five porting notes below.
2. `khub init firm-ops` against a branch of firm-hq → writes `.khub/` (config + flattened schema).
3. **Id strategy (resolved).** id = slug, bare on disk, so HQ's `initech-pov` folder and its bare relation values (`client: initech`, `engagement: initech-pov`) map through untouched. Typed edges resolve by the schema-known target type; polymorphic edges resolve by slug and need a `type/slug` qualifier only if a slug turns out ambiguous across types; HQ's are globally unique today, so none do. CRM/Recorder/Airtable ids become `source_id` aliases. No edge rewrite.
4. `khub validate` + `khub check` over the imported graph; fix referential breaks (the porting notes predict where they land).
5. Repoint the integration scripts from `kb.py` calls to khub's library.
6. Retire `kb.py`, `build-graph.py`, `hq.schema.yml`; delete `.hq-graph.sqlite` (regenerated).

**Status gate:** v1 cuts HQ over for `validate`/`check`/`query`/`reindex`/`backfill`, running read-only against the live files first, then taking over. Full retirement of `kb.py` waits on the SQLite/FTS fast-follow for search parity. This capture is the input to step 1.

## Porting Notes: HQ Schema → khub LinkML Preset

Five divergences to resolve when this becomes a real khub preset. None is a blocker; each is a deliberate choice the engine forces.

1. **Stored inverse edge.** HQ stores both `supersedes` and `superseded_by` on decisions. khub invariant #4 forbids storing the inverse; derive `superseded_by` from `supersedes`. Drop the field on port.
2. **Slug = id.** The slug is bare and serves as the id; nothing is prefixed. Uniqueness is `(type, slug)`, with the file path as the unique key. HQ's globally-unique slugs resolve directly, including across polymorphic edges; a `type/slug` qualifier is only needed if two types ever share a slug. Layout (file vs folder, entry filename) is per-preset schema config, overridable per type. CRM/Recorder/Airtable ids ride along as `source_id` aliases.
3. **No lifecycle field.** HQ has no universal `draft|active`; it relies on per-type `stage`/`status` enums. khub adds `status: draft|active` for required-relation completeness. Add it; map "closed/complete/retired" terminal stages as needed.
4. **Path-shared types.** `project` and `build` both live at `projects/{slug}/CLAUDE.md`, discriminated by the `type` field, not the folder. In khub both declare the same `folder` layout under `projects/`, and the engine reads `type` from frontmatter, so the shared directory carries over cleanly.
5. **`partner` edge `from`-list.** The edge vocabulary omits `client` from `partner`'s `from`, yet `client` uses it. Tighten the LinkML `domain` to include `client`, or drop it from client. Cosmetic, yet it would fail a strict `check`.

Lower-priority: `decided_by` is an untyped name list, not a `person` edge; promote it to an edge if person-level decision attribution matters. `controls` points at `external` (ISO ids), which khub models as `source_id`/`external_url`, not nodes.

## Completeness Checklist

All 12 node types captured: opportunity, project, build, meeting, transcript, fragment, decision, case-study, isms-doc, partnership, person, client. All 19 edge predicates captured: owner, client, team, engagement, origin_opportunity, source_project, transcript, supersedes, superseded_by, affects, promoted_to, controls, partner, related, related_opportunities, related_projects, sources, references, depends_on. Reference areas and external integrations captured. Cutover plan (khub replacing the incumbent engine) included. Nothing in `hq.schema.yml` is omitted.
