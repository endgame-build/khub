# Firm-ops preset

**The operating graph of a consulting and delivery firm.** `firm-ops` models a firm as it runs: pipeline deals become delivery engagements, engagements generate meetings and transcripts, delivery yields case studies; and behind all of it sit the people, clients, and partnerships that carry the work.

`khub init firm-ops ./my-hub` seeds a workspace from it. For a hands-on first run, start with [`getting-started.md`](getting-started.md); to author or extend the types yourself, see [`schema.md`](schema.md).

The tables between `GENERATED` markers on this page are generated from `khub schema --format json` against a workspace seeded from the preset; regenerate them with `go run ./parity/tools/presetdocs` (it builds khub fresh from the checkout), and CI fails when they drift from `presets/firm-ops/`. The prose around them is written by hand.

<!-- BEGIN GENERATED summary -->

Preset version **0.3.0**. 9 entity types, and 14 relation predicates: 10 declared by the preset and 4 from the base block.

Every type also carries the base block, which is embedded in the binary (`khub schema base` prints it):

- **Base attributes**: `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, `tags`, `aliases`.
- **Base edges**: `related` → any (many); `sources` → any (many); `references` → any (many); `depends_on` → any (many, acyclic).

<!-- END GENERATED summary -->

Ids are bare slugs of `name` (opportunity, person, client) or `title` (the rest) — no prefix, no date, no ordinal — and every type requires the one it mints from; a repeated one is refused (`pass --id <slug>`).

The type sections below list only what firm-ops adds or tightens on top of the base block. An attribute marked *base, redeclared* is a type overriding a base default, such as a `required` `updated` or an optional `created`.

## Relation vocabulary

Firm-ops relations are declared on specific types; the base edges are available everywhere.

<!-- BEGIN GENERATED relations -->

| Predicate | From | To | Many | Required | Acyclic | Inverse |
|---|---|---|---|---|---|---|
| `client` | opportunity, project, case-study | client | no | yes | no |  |
| `owner` | opportunity, project, fragment, partnership | person | no | yes | no |  |
| `partner` | opportunity, project, client | partnership | no | no | no |  |
| `team` | project | person | yes | no | no |  |
| `origin_opportunity` | project | opportunity | no | no | no |  |
| `engagement` | meeting | opportunity \| project \| partnership | no | yes | no |  |
| `transcript` | meeting | transcript | no | no | no |  |
| `owner` | meeting | person | no | no | no |  |
| `source_project` | case-study | project | no | no | no |  |
| `related_opportunities` | partnership | opportunity | yes | no | no |  |
| `related_projects` | partnership | project | yes | no | no |  |
| `related` | every type | any | yes | no | no |  |
| `sources` | every type | any | yes | no | no |  |
| `references` | every type | any | yes | no | no |  |
| `depends_on` | every type | any | yes | no | yes |  |

<!-- END GENERATED relations -->

`engagement` is a union edge: a meeting attaches to exactly one opportunity, project, or partnership. `owner` is the accountability relation (the person answerable for an entity) and stays distinct from the base `author` attribute, which records who wrote the file.

## The entities

### opportunity

A pipeline deal; the pre-sale entity that converts into a project.

<!-- BEGIN GENERATED type opportunity -->

Layout `folder`, format `md`, stored at `opportunities/{slug}/_index.md`, ids `slug`.

Record one when a possible piece of work with a named client is first mentioned — record it at 'we might', not at 'we won'.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `updated` (base, redeclared) | date | yes |  |  |
| `name` | text | yes |  |  |
| `stage` | text | yes | `prospect`, `proposal-sent`, `won`, `signed`, `lost` |  |
| `source` | text | no | `referral`, `outbound`, `inbound`, `partner`, `event`, `existing-client` |  |
| `crm_id` | text | no | pattern `^[0-9]+$` |  |
| `notes_folder` | text | no |  |  |
| `notes_folder_id` | text | no |  |  |

Relations: `client` → client (required); `owner` → person (required); `partner` → partnership.

<!-- END GENERATED type opportunity -->

`stage` follows the deal pipeline of the sales system of record, and `crm_id` is the deal's id in the CRM.

### project

A delivery engagement; the post-sale consulting work.

<!-- BEGIN GENERATED type project -->

Layout `folder`, format `md`, stored at `projects/{slug}/_index.md`, ids `slug`.

Record one when work is agreed and has an owner; the opportunity that produced it becomes its origin.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `updated` (base, redeclared) | date | yes |  |  |
| `title` (base, redeclared) | text | yes |  |  |
| `active` | bool | no |  | `true` |
| `source` | text | no | `referral`, `outbound`, `inbound`, `partner`, `event`, `existing-client` |  |
| `external_repo` | text | no | pattern `^[a-z0-9-]+/[a-z0-9-]+$` |  |
| `crm_id` | text | no | pattern `^[0-9]+$` |  |
| `budget_id` | text | no | pattern `^[0-9]+$` |  |
| `notes_folder` | text | no |  |  |
| `notes_folder_id` | text | no |  |  |

Relations: `client` → client (required); `owner` → person (required); `team` → person (many); `origin_opportunity` → opportunity; `partner` → partnership.

<!-- END GENERATED type project -->

`active` says whether the engagement is ongoing. `external_repo` names the delivery repo as `owner/name`.

### meeting

An engagement touchpoint; a processed note tied to what it is about.

<!-- BEGIN GENERATED type meeting -->

Layout `file`, format `md`, stored at `meetings/{slug}.md`, ids `slug`.

Record one when a call happens that produced a decision, a commitment, or a next step.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `created` (base, redeclared) | date | no |  |  |
| `title` (base, redeclared) | text | yes |  |  |
| `date` | datetime | yes |  |  |
| `call_type` | text | yes | `client`, `sales`, `partner`, `internal` |  |
| `source` | text | yes | `recording`, `manual` |  |
| `note_id` | text | no |  |  |
| `attendees` | list | no |  |  |

Relations: `engagement` → opportunity | project | partnership (required); `transcript` → transcript; `owner` → person.

<!-- END GENERATED type meeting -->

`created` is optional because source meetings carry none; `date` is when the call happened.

### transcript

Raw meeting capture; the unprocessed export from the recording tool.

<!-- BEGIN GENERATED type transcript -->

Layout `file`, format `md`, stored at `transcripts/{slug}.md`, ids `slug`.

Record one when a recording or written record of a call exists and needs to be findable later.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `created` (base, redeclared) | date | no |  |  |
| `title` (base, redeclared) | text | yes |  |  |
| `source` | text | yes | `recording` |  |
| `note_id` | text | yes |  |  |
| `date` | datetime | yes |  |  |
| `creator` | text | yes |  |  |
| `creator_email` | text | yes |  |  |
| `notes_folder` | text | yes |  |  |
| `notes_folder_id` | text | no |  |  |
| `calendar_event_id` | text | no |  |  |
| `attendees` | list | no |  |  |

Relations: only the base edges.

<!-- END GENERATED type transcript -->

### fragment

An atomic thought or note that matures through stages.

<!-- BEGIN GENERATED type fragment -->

Layout `file`, format `md`, stored at `fragments/{slug}.md`, ids `slug`.

Record one when something worth keeping is said that fits no other type yet — a signal, a quote, an observation.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `title` (base, redeclared) | text | yes |  |  |
| `stage` | text | yes | `raw`, `mature`, `synthesis`, `promoted` |  |
| `promoted_to` | text | no |  |  |
| `confidence` | number | no |  |  |

Relations: `owner` → person (required).

<!-- END GENERATED type fragment -->

`owner` is the fragment's writer, keyed on `owner` rather than `author`. `promoted_to` is a plain-text link-out to the reference area the fragment was promoted into, like the base `resource`.

### case-study

A proven outcome drawn from a delivered engagement.

<!-- BEGIN GENERATED type case-study -->

Layout `file`, format `md`, stored at `case-studies/{slug}.md`, ids `slug`.

Record one when delivered work becomes a story worth telling outside the firm.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `updated` (base, redeclared) | date | yes |  |  |
| `title` (base, redeclared) | text | yes |  |  |
| `published` | bool | yes |  |  |
| `industry` | text | no |  |  |
| `phase` | text | no | `diagnose`, `prove`, `scale`, `full-cycle` |  |

Relations: `client` → client (required); `source_project` → project.

<!-- END GENERATED type case-study -->

### partnership

A business-development relationship that feeds opportunities and projects.

<!-- BEGIN GENERATED type partnership -->

Layout `folder`, format `md`, stored at `partnerships/{slug}/_index.md`, ids `slug`.

Record one when a standing relationship with another firm is established or changes.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `updated` (base, redeclared) | date | yes |  |  |
| `title` (base, redeclared) | text | yes |  |  |
| `partner` | text | yes |  |  |
| `active` | bool | no |  | `true` |
| `primary_contact` | text | no |  |  |
| `notes_folder` | text | no |  |  |
| `notes_folder_id` | text | no |  |  |

Relations: `owner` → person (required); `related_opportunities` → opportunity (many); `related_projects` → project (many).

<!-- END GENERATED type partnership -->

`partner` holds the partner's name.

### person

A team member; the target of `owner` and `team` edges across the graph.

<!-- BEGIN GENERATED type person -->

Layout `file`, format `md`, stored at `identity/team/{slug}.md`, ids `slug`.

Record one when a human is named who we will refer to again — ours, a client's, or a partner's.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `name` | text | yes |  |  |
| `role` | text | yes | `consultant`, `engineer`, `manager`, `partner` |  |
| `department` | text | no | `engineering`, `consulting`, `delivery-management`, `people`, `sales` |  |

Relations: only the base edges.

<!-- END GENERATED type person -->

### client

A client organization; the party behind opportunities, projects, and case studies.

<!-- BEGIN GENERATED type client -->

Layout `file`, format `md`, stored at `clients/{slug}.md`, ids `slug`.

Record one when an organisation is named that we work with, pitch to, or are introduced to.

| Attribute | Type | Required | Values | Default |
|---|---|---|---|---|
| `updated` (base, redeclared) | date | yes |  |  |
| `name` | text | yes |  |  |
| `industry` | text | no |  |  |
| `crm_id` | text | no | pattern `^[0-9]+$` |  |
| `website` | text | no |  |  |

Relations: `partner` → partnership.

<!-- END GENERATED type client -->

## Design note: three types the original capture had

Earlier versions of the preset carried three more types that the shipped one drops, each with no entities in practice:

- **`build`** — a product/engineering engagement. After the lifecycle refinement it differed from `project` only by a `tech_stack` field.
- **`decision`** — an ADR-style durable record. Dropping it also removed firm-ops's self-referential `supersedes` edge and its derived `superseded_by` inverse. The engine still supports derived inverses generically; firm-ops no longer demonstrates them.
- **`isms-doc`** — a compliance artifact.

Add any of them back with a schema edit when the firm starts writing those entities.

## See also

- [`schema.md`](schema.md) — how presets are authored and extended.
- [`cli.md`](cli.md) — the full command surface.
- [`getting-started.md`](getting-started.md) — a hands-on run on this preset.
