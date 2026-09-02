# build-lite preset

**build-hub cut to necessity: seven entity types for a small build project, and a documented order for growing back into the full preset.**

Preset version **0.2.0**. Seven entity types (five graph records + two narrative singletons). Five relation predicates in six declarations beyond the four universal edges from the core base. The preset is a directory: `{ontology,policy,storage}.yaml` + `templates/*.yaml`, copied to `.khub/` at init. Every one of the seven md types ships a body template.

```bash
khub init build-lite ./my-hub
```

## What earns a slot

A type is here only if it is **referenced by id from elsewhere, walked as a graph, or gated by `check`**. Everything else has a cheaper home: prose a human reads start to finish is a body, a fact stored once and read once is an attribute, and anything that churns with the code is a file in the repo linked via the base `resource`.

The seven answer the five questions an agent asks mid-build:

| Question | Type |
|---|---|
| What am I building? | `feature-spec` |
| What must hold? | `requirement` |
| Why can't I do X? | `adr` (via `affects` — the blast-radius walk) |
| Where does the code live, and what talks to what? | `component`, `repo` |
| What is this product, and how is it shaped? | `prd`, `arc42` |

## Layout

One knowledge root plus `specs/`. No collections — lite has no homogeneous wiring left to keep in a registry; repos are a directory of md files like everything else, so a repo carries prose of its own.

```
knowledge/
  prd.md                  # singleton — absorbs build-hub's roadmap + glossary
  arc42.md                # singleton — absorbs build-hub's erd (the data model under Crosscutting Concepts)
  requirements/           # req-<slug>
  decisions/              # ad-<YYYY-MM-DD>-<slug>
  components/             # cmp-<slug>
  repos/                  # rp-<slug>
specs/                    # fs-<slug>
index.md                  # the whole corpus in one file — written by init, refreshed by reindex
```

**Ids carry no ordinal.** `add` mints `<prefix>-<slug>` from the title — `req-idempotent-retry`, `cmp-public-api` — and `adr` alone adds the date it was minted (`ad-2026-07-28-use-postgres`): a decision legitimately recurs under one title, a registry entry does not, and the same title on any other type refuses with `slug_taken` (pass `--id` if you really need both). Minting reads no siblings, so two branches minting the same title collide on one filename instead of numbering past each other. **Slugs are lowercase.** `add --id` slugifies whatever you pass and id resolution is case-sensitive, so write `cmp-api`, never `CMP-API`. The scheme is in [`schema.md`](schema.md).

## The seven entities

Every type ships a template in `templates/`; the section names are the ones a reader outside khub already knows, and every heading a type gained after it first shipped is `optional: true` (see [`schema.md`](schema.md#body-templates-template-in-storage)). Optional headings are marked * below.

- **prd** (`knowledge/prd.md`, singleton, `required: true`) — the product source of truth; `check` fails while it is absent. Template: Document Purpose*, Vision (25–200 words), Target Users, Glossary, Features, Non-Goals, Deferred*, Success Metrics, Roadmap*, Alternatives & Recommendation*, Open Questions*, Assumptions Index*, Grounding Evidence*. Lenses: `scope`, `journeys`, `metrics`, `assumptions`.
- **arc42** (`knowledge/arc42.md`, singleton) — arc42's twelve sections under docs.arc42.org's names. Required: Context and Scope, Solution Strategy, Building Block View (asks for one mermaid block), Crosscutting Concepts (the domain data model lives here, as an ERD), Architecture Decisions, Risks and Technical Debt. Optional: Introduction and Goals, Architecture Constraints, Runtime View, Deployment View, Quality Requirements, Glossary, and the four appendices Deferred Decisions, Open Questions, Assumptions Index, Grounding Evidence. Lenses: `direction`, `deferred`.
- **requirement** (`knowledge/requirements/`) — `kind: functional | non-functional | constraint | business-rule` (required), `realized_in` the components that satisfy it. The template declares no headings (`sections: []`): the body is one statement, and the file carries a hint and five lenses — `single`, `testable`, `measurable` (when `kind: non-functional`), `violation` (when `kind: constraint`), `realized`. `kind: constraint` absorbs build-hub's `boundary` and `kind: non-functional` its `quality-attribute`: state the invariant or the target, its measurement, and how it is enforced in the body. The id is a literal `req-`, not derived from `kind`, so `edit <id> kind …` never makes the id wrong.
- **adr** (`knowledge/decisions/`) — `status: proposed | accepted | rejected` (required), `supersedes` another adr, `affects: any`. That last edge is what earns the type its slot: it is the blast-radius query. Dated id. Template: Context (≥25 words), Alternatives*, Decision (≥15 words; hedging such as `tbd` or `we should consider` inside it is a `body_rule` gap), Consequences (≥20 words), Prevents*. Lenses: `alternatives`, `consequences`, `reversal`, `rule`, `proposed` (when `status: proposed`).
- **component** (`knowledge/components/`) — a required `kind: service | library | external`, `stack` as plain text, and a `repo` edge to the registry (ours only; an external omits it). `kind: external` is a vendor or neighbouring product: no `repo`, `stack` names the vendor, base `resource` carries its API docs. Base `depends_on` carries component → component and component → external, which is what makes *what breaks when this vendor changes* a graph walk rather than a grep. Template: a hint plus optional Responsibilities, Interfaces, Operational notes. Lenses: `boundary`, `blast` (when `kind: external`), `located` (when `kind: service` or `library`).
- **repo** (`knowledge/repos/`) — `repo` as a remote path (`org/name`, or the full group path where the host nests them; the pattern is loose — tighten it to your org), `status: active | archived` (required). The registry: `component.repo` must resolve here. Template: a hint plus optional What lives here, What does not. Lenses: `remote`, `archived` (when `status: archived`).
- **feature-spec** (`specs/`) — `status: planned | active | done | dropped` (required), `requirements` it satisfies, `supersedes` a prior spec. The FS **is** the work record: no separate feature container, no work package, no test spec. Template: Summary, Scope, Behaviour, Acceptance criteria — all four required, no section rules. Lenses: `scope`, `acceptance`.

## Relation vocabulary

| Predicate | From → To | Required |
|---|---|---|
| `requirements` | feature-spec → requirement (many) | no |
| `realized_in` | requirement → component (many) | no |
| `repo` | component → repo | no (an external has no codebase) |
| `supersedes` | adr → adr; feature-spec → feature-spec | no |
| `affects` | adr → any (many) | no |

`superseded` is never stored — it is the computed inverse of the inbound `supersedes` edge. Both `supersedes` declarations are `acyclic: true`, so a supersession loop is a `check` finding.

## What was cut, and what replaces it

Thirteen build-hub types are absent. None of them were dropped without a home. (A fourteenth row is listed for orientation: `external-system` is not a build-hub type to cut — build-hub 0.3.0 removed it too, the same way.)

| Cut | Replaced by |
|---|---|
| `capability` | base `tags` on requirement — it is a grouping axis, and `feature-spec → requirements` already carries placement. Its real job in build-hub is the UCAP-XXX facet-import spine, which lite does not do (no `facet_id` anywhere here). |
| `pdr` | the prd body. Anything that constrains implementation is an `adr`. |
| `boundary`, `quality-attribute` | `requirement.kind: constraint` and `kind: non-functional`. **This is the cut with the most real loss** — `enforcement`, `measurement` and `scenario` stop being queryable attributes and become body prose; the `measurable` and `violation` lenses are what keeps that prose honest. First one back. |
| `domain`, `entity` | arc42 sections (Building Block View, and the data model under Crosscutting Concepts). At this size the components *are* the map, and field-level detail lives in the code. |
| `contract` | the machine spec (OpenAPI/AsyncAPI) is already a file on disk — hang it off the providing component's base `resource`. The entity earns its slot when two repos consume it independently. |
| `external-system` † | `component` with `kind: external` — the same fold build-hub itself made in 0.3.0, so components carry over unchanged on a move up. † not a lite-only cut. |
| `baseline` | CI and dashboards. A git corpus records metrics stale by construction. |
| `work-package` | the feature-spec **is** the work unit; there is nothing to split across repos and `status` covers the flip. |
| `test-spec` | the feature-spec's Acceptance criteria section; the tests themselves are files in the repo. |
| `roadmap`, `glossary` | prd sections. |
| `erd` | the arc42 Crosscutting Concepts section. |

`repo` is not cut: it was a plain text attribute on `component` through preset 0.1.0, and became a type at 0.2.0 because the registry is what lets `check` hold every component's codebase to a row that exists, and a repo carries prose of its own.

## Add-back ladder

Order matters more than the list — it is what makes lite an on-ramp rather than a dead end. Each step is a schema edit in your workspace's `.khub/ontology.yaml` (plus a `storage.yaml` entry for where the type lands); no surface code changes.

1. **`boundary` + `quality-attribute`** — the moment an invariant needs a named enforcement mechanism.
2. **`contract`** — a second repo consuming an interface. It arrives alone: `contract.provider` targets `component`, and a vendor-provided contract names a `kind: external` component as its provider, exactly as in build-hub 0.3.0. Do **not** reintroduce `external-system` — build-hub no longer has it, and adding it here would produce a schema build-hub cannot absorb.
3. **`work-package`** — multi-repo delivery, or a tracker sync. `repo` is already here, so a work package's `repo` routing edge has its target.
4. **`domain` + `entity`** — when component count passes ~15 and ownership stops being obvious.
5. The rest — or switch to `build-hub`.

## Narrative roots and the orphan sweep

`prd` and `arc42` declare `orphan: true`. Every stored edge points *up* the durability ladder (`feature-spec → requirement`) and both documents sit above its top, so nothing points at them and they point at nothing. Without the flag they would be permanent orphan findings that no authoring could ever close, and `check --strict` could never go green on a correct workspace. The flag is per type, never inferred from `layout: singleton` — a singleton that *does* carry relations is still swept. See [`schema.md`](schema.md#type-level-gates).

## Design capture

The `ontology.yaml` header is the primary source: it carries every authoring decision, the full cut list with rationale, and the add-back ladder. This page is the reading copy.
