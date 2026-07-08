# Firm-ops preset

**The operating graph of a consulting and delivery firm.** `firm-ops` models a firm as it runs: pipeline deals become delivery engagements, engagements generate meetings and transcripts, delivery yields case studies; and behind all of it sit the people, clients, and partnerships that carry the work. It is the port of firm-hq's hand-rolled schema, and the one preset that ships today.

`khub init firm-ops ./my-hub` seeds a workspace from it. For a hands-on first run, start with [`getting-started.md`](getting-started.md); to author or extend the types yourself, see [`schema.md`](schema.md).

Preset version **0.1.0**. Nine entity types. Fourteen relation predicates: ten declared here, four inherited from the core base.

## What every entity carries

`khub init` merges `core.yaml` into the preset, so every firm-ops entity carries a base block on top of its own fields:

- **Base attributes** — `type`, `draft`, `author`, `created`, `updated`, `title`, `description`, `resource`, and `tags`.
- **Four universal edges** — `related`, `sources`, `references`, `depends_on`, each `any → any` and many-valued.

The tables below list only what firm-ops adds or tightens. A `required` note on `updated`, `created`, or `title` marks a type overriding a base default; an `optional` note relaxes one.

## Relation vocabulary

Fourteen predicates. Ten are firm-ops relations declared on specific types; the last four are the universal edges from `core`, available everywhere.

| Predicate | From → To | Required |
|---|---|---|
| `client` | opportunity, project, case-study → client | yes |
| `owner` | opportunity, project, meeting, partnership, fragment → person | yes (optional on meeting) |
| `partner` | client, opportunity, project → partnership | no |
| `team` | project → person (many) | no |
| `origin_opportunity` | project → opportunity | no |
| `engagement` | meeting → opportunity \| project \| partnership | yes |
| `transcript` | meeting → transcript | no |
| `source_project` | case-study → project | no |
| `related_opportunities` | partnership → opportunity (many) | no |
| `related_projects` | partnership → project (many) | no |
| `related` | any → any (many) | no |
| `sources` | any → any (many) | no |
| `references` | any → any (many) | no |
| `depends_on` | any → any (many) | no |

`engagement` is a union edge: a meeting attaches to exactly one opportunity, project, or partnership. `owner` is the accountability relation (the person answerable for an entity) and stays distinct from the base `author` attribute, which records who wrote the file.

## The nine entities

### opportunity

Layout: folder (`opportunities/{slug}/_index.md`). A pipeline deal; the pre-sale entity that converts into a project.

| Attribute | Type | Notes |
|---|---|---|
| `stage` | enum, required | `prospect`, `proposal-sent`, `won`, `signed`, `lost`: the CRM "Sales" pipeline stages (the deal system of record) |
| `source` | enum | `event`, `referral`, `outbound`, `inbound`, `northwind`, `existing-client` |
| `crm_id` | text | digits (`^[0-9]+$`), the CRM deal id |
| `notes_folder` | text | |
| `notes_folder_id` | text | |
| `updated` | required | overrides the base default |

Relations: `client` → client (required), `owner` → person (required), `partner` → partnership.

### project

Layout: folder (`projects/{slug}/_index.md`). A delivery engagement; the post-sale consulting work.

| Attribute | Type | Notes |
|---|---|---|
| `active` | bool | default `true` (is the engagement ongoing?) |
| `source` | enum | `event`, `referral`, `outbound`, `inbound`, `northwind`, `existing-client` |
| `external_repo` | text | pattern `^endgame-build/[a-z0-9-]+$` |
| `crm_id` | text | digits |
| `budget_id` | text | digits |
| `notes_folder` | text | |
| `notes_folder_id` | text | |
| `updated` | required | overrides the base default |

Relations: `client` → client (required), `owner` → person (required), `team` → person (many), `origin_opportunity` → opportunity, `partner` → partnership.

### meeting

Layout: file (`meetings/{slug}.md`). An engagement touchpoint; a processed note tied to what it is about.

| Attribute | Type | Notes |
|---|---|---|
| `created` | optional | relaxes the base default: source meetings carry none |
| `date` | datetime, required | |
| `call_type` | enum, required | `client`, `sales`, `partner`, `internal` |
| `source` | enum, required | `recording`, `manual` |
| `note_id` | text | |
| `attendees` | list | |

Relations: `engagement` → opportunity \| project \| partnership (required, union), `transcript` → transcript, `owner` → person.

### transcript

Layout: file (`transcripts/{slug}.md`). Raw meeting capture; the unprocessed Recorder export.

| Attribute | Type | Notes |
|---|---|---|
| `created` | optional | relaxes the base default |
| `source` | enum, required | `recording` |
| `note_id` | text, required | |
| `date` | datetime, required | |
| `title` | required | overrides the base default |
| `creator` | text, required | |
| `creator_email` | text, required | |
| `notes_folder` | text, required | |
| `notes_folder_id` | text | |
| `calendar_event_id` | text | |
| `attendees` | list | |

Relations: none beyond the universal edges.

### fragment

Layout: file (`fragments/{slug}.md`). An atomic thought or note that matures through stages.

| Attribute | Type | Notes |
|---|---|---|
| `stage` | enum, required | `raw`, `mature`, `synthesis`, `promoted` |
| `promoted_to` | text | a `resource` link-out to a reference area, not an edge |
| `confidence` | number | |

Relations: `owner` → person (required). The writer, keyed on `owner` rather than `author`.

### case-study

Layout: file (`case-studies/{slug}.md`). A proven outcome drawn from a delivered engagement.

| Attribute | Type | Notes |
|---|---|---|
| `published` | bool, required | |
| `industry` | text | |
| `phase` | enum | `diagnose`, `prove`, `scale`, `full-cycle` |
| `updated` | required | overrides the base default |

Relations: `client` → client (required), `source_project` → project.

### partnership

Layout: folder (`partnerships/{slug}/_index.md`). A business-development relationship that feeds opportunities and projects.

| Attribute | Type | Notes |
|---|---|---|
| `partner` | text, required | the partner name, source of the slug |
| `active` | bool | default `true` |
| `primary_contact` | text | |
| `notes_folder` | text | |
| `notes_folder_id` | text | |
| `updated` | required | overrides the base default |

Relations: `owner` → person (required), `related_opportunities` → opportunity (many), `related_projects` → project (many).

### person

Layout: file (`identity/team/{slug}.md`). A team member; the target of `owner` and `team` edges across the graph.

| Attribute | Type | Notes |
|---|---|---|
| `name` | text, required | |
| `role` | enum, required | `consultant`, `engineer`, `manager`, `partner` |
| `department` | enum | `engineering`, `consulting`, `delivery-management`, `people`, `sales` |

Relations: none beyond the universal edges.

### client

Layout: file (`clients/{slug}.md`). A client organization; the party behind opportunities, projects, and case studies.

| Attribute | Type | Notes |
|---|---|---|
| `name` | text, required | |
| `industry` | text | |
| `crm_id` | text | digits |
| `website` | text | |
| `updated` | required | overrides the base default |

Relations: `partner` → partnership.

## Design note: three types the original capture had

The firm-hq capture carried three more types that the shipped preset drops, each with zero live entities:

- **`build`** — a product/engineering engagement. After the lifecycle refinement it differed from `project` only by a `tech_stack` field.
- **`decision`** — an ADR-style durable record. Dropping it also removed firm-ops's self-referential `supersedes` edge and its derived `superseded_by` inverse. The engine still supports derived inverses generically; firm-ops just no longer demonstrates them.
- **`isms-doc`** — a compliance artifact.

Add any of them back with a schema edit when the firm starts writing those entities.

## See also

- [`schema.md`](schema.md) — how presets are authored and extended.
- [`cli.md`](cli.md) — the full command surface.
- [`getting-started.md`](getting-started.md) — a hands-on run on this preset.
