# build-lite preset

**build-hub cut to necessity: six entity types for a small build project, and a documented order for growing back into the full preset.**

Preset version **0.1.0**. Six entity types (four graph records + two narrative singletons). Four relation predicates in five declarations beyond the four universal edges from the core base. The preset is a directory: `schema.yaml` + `templates/*.yaml`, flattened to `.khub/templates/` at init. Four of the six md types ship a body template.

```bash
khub init build-lite ./my-hub
```

## What earns a slot

A type is here only if it is **referenced by id from elsewhere, walked as a graph, or gated by `check`**. Everything else has a cheaper home: prose a human reads start to finish is a body, a fact stored once and read once is an attribute, and anything that churns with the code is a file in the repo linked via the base `resource`.

The six answer the five questions an agent asks mid-build:

| Question | Type |
|---|---|
| What am I building? | `feature-spec` |
| What must hold? | `requirement` |
| Why can't I do X? | `adr` (via `affects` — the blast-radius walk) |
| Where does the code live, and what talks to what? | `component` |
| What is this product, and how is it shaped? | `prd`, `arc42` |

## Layout

One knowledge root plus `specs/`. No collections — lite has no homogeneous wiring left to keep in a registry.

```
knowledge/
  prd.md                  # singleton — absorbs build-hub's roadmap + glossary
  arc42.md                # singleton — absorbs build-hub's erd (Data model section)
  requirements/           # fr-NNN-slug · cst-NNN-slug
  decisions/              # ad-NNN-slug
  components/             # cmp-NNN-slug
specs/                    # fs-NNN-slug
```

**Slugs are lowercase.** `add --id` slugifies whatever you pass and id resolution is case-sensitive, so write `cmp-001-api`, never `CMP-001-API`.

## The six entities

- **prd** (`knowledge/prd.md`, singleton, `required: true`) — the product source of truth; `check` fails while it is absent. Its template carries Vision, Target user, Functional requirements, Non-goals, Glossary, Roadmap, Success metrics.
- **arc42** (`knowledge/arc42.md`, singleton) — Context and scope, Solution strategy, Building blocks, Data model (the ERD as a mermaid block), Crosscutting concepts, Decisions, Risks and technical debt.
- **requirement** (`knowledge/requirements/`) — `kind: functional | constraint | business-rule` (required), `realized_in` the components that satisfy it. EARS-friendly prose in the body. `kind: constraint` absorbs build-hub's `boundary` and `quality-attribute`: state the invariant, its measurement, and how it is enforced in the body.
- **adr** (`knowledge/decisions/`) — `status: proposed | accepted | rejected` (required), `supersedes` another adr, `affects: any`. That last edge is what earns the type its slot: it is the blast-radius query.
- **component** (`knowledge/components/`) — a required `kind: service | library | external`, plus `stack` and `repo` as plain text. `kind: external` is a vendor or neighbouring product: no `repo`, `stack` names the vendor, base `resource` carries its API docs. Base `depends_on` carries component → component and component → external, which is what makes *what breaks when this vendor changes* a graph walk rather than a grep.
- **feature-spec** (`specs/`) — `status: planned | active | done | dropped` (required), `requirements` it satisfies, `supersedes` a prior spec. The FS **is** the work record: no separate feature container, no work package, no test spec.

## Relation vocabulary

| Predicate | From → To | Required |
|---|---|---|
| `requirements` | feature-spec → requirement (many) | no |
| `realized_in` | requirement → component (many) | no |
| `supersedes` | adr → adr; feature-spec → feature-spec | no |
| `affects` | adr → any (many) | no |

`superseded` is never stored — it is the computed inverse of the inbound `supersedes` edge. Both `supersedes` declarations are `acyclic: true`, so a supersession loop is a `check` finding.

## What was cut, and what replaces it

Fourteen build-hub types are absent. None of them were dropped without a home. (A fifteenth row is listed for orientation: `external-system` is not a build-hub type to cut — build-hub 0.3.0 removed it too, the same way.)

| Cut | Replaced by |
|---|---|
| `capability` | base `tags` on requirement — it is a grouping axis, and `feature-spec → requirements` already carries placement. Its real job in build-hub is the UCAP-XXX facet-import spine, which lite does not do (no `facet_id` anywhere here). |
| `pdr` | the prd body. Anything that constrains implementation is an `adr`. |
| `boundary`, `quality-attribute` | `requirement.kind: constraint`. **This is the cut with the most real loss** — `enforcement`, `measurement` and `scenario` stop being queryable attributes and become body prose. First one back. |
| `domain`, `entity` | arc42 sections (Building blocks, Data model). At this size the components *are* the map, and field-level detail lives in the code. |
| `repo` | a plain `repo` text attribute on component. The registry's only benefit was integrity-checking repo names against an enumeration. |
| `contract` | the machine spec (OpenAPI/AsyncAPI) is already a file on disk — hang it off the providing component's base `resource`. The entity earns its slot when two repos consume it independently. |
| `external-system` † | `component` with `kind: external` — the same fold build-hub itself made in 0.3.0, so components carry over unchanged on a move up. † not a lite-only cut. |
| `baseline` | CI and dashboards. A git corpus records metrics stale by construction. |
| `work-package` | the feature-spec **is** the work unit; there is nothing to split across repos and `status` covers the flip. |
| `test-spec` | the feature-spec's Acceptance criteria section; the tests themselves are files in the repo. |
| `roadmap`, `glossary` | prd sections. |
| `erd` | the arc42 Data model section. |

## Add-back ladder

Order matters more than the list — it is what makes lite an on-ramp rather than a dead end. Each step is a schema edit in your workspace's `.khub/schema.yaml`; no surface code changes.

1. **`boundary` + `quality-attribute`** — the moment an invariant needs a named enforcement mechanism.
2. **`contract`** — a second repo consuming an interface. It arrives alone: `contract.provider` targets `component`, and a vendor-provided contract names a `kind: external` component as its provider, exactly as in build-hub 0.3.0. Do **not** reintroduce `external-system` — build-hub no longer has it, and adding it here would produce a schema build-hub cannot absorb.
3. **`repo` + `work-package`** — multi-repo delivery, or a tracker sync.
4. **`domain` + `entity`** — when component count passes ~15 and ownership stops being obvious.
5. The rest — or switch to `build-hub`.

## Narrative roots and the orphan sweep

`prd` and `arc42` declare `orphan: true`. Every stored edge points *up* the durability ladder (`feature-spec → requirement`) and both documents sit above its top, so nothing points at them and they point at nothing. Without the flag they would be permanent orphan findings that no authoring could ever close, and `check --strict` could never go green on a correct workspace. The flag is per type, never inferred from `layout: singleton` — a singleton that *does* carry relations is still swept. See [`schema.md`](schema.md#type-level-gates).

## Design capture

The `schema.yaml` header is the primary source: it carries every authoring decision, the full cut list with rationale, and the add-back ladder. This page is the reading copy.
