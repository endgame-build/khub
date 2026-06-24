---
id: WPK-002-1
name: Create and Read Entities
feature-spec: plan/specs/FS-002-authoring.md
test-spec: plan/tests/TS-002-authoring.md
stories: [STORY-ENT-001, STORY-ENT-002]
test-scenarios: [TS-ENT-001-01, TS-ENT-001-02, TS-ENT-001-03, TS-ENT-001-04, TS-ENT-001-05, TS-ENT-001-06, TS-ENT-001-U01, TS-ENT-001-U02, TS-ENT-001-U03, TS-ENT-001-U04, TS-ENT-001-U05, TS-ENT-001-U06, TS-ENT-001-U07, TS-ENT-002-01, TS-ENT-002-02, TS-ENT-002-03, TS-ENT-002-04, TS-ENT-002-U01, TS-ENT-002-U02, TS-ENT-002-U03, TS-ENT-002-U04, TS-ENT-002-U05, TS-ENT-002-U06]
depends-on: [WPK-001-1, WPK-001-2]
updated: 2026-06-24
---

# Create and Read Entities

## Objective

Delivers the two foundational authoring verbs — `khub add <type>` (`core.create`) and `khub get <id>` (`core.get`) — with the compiled schema and git as the only gates. `add` mints a typed entity as one Markdown file at its layout path; `get` reads it back, optionally with derived edges. Together they establish slug minting and collision suffixing, layout resolution (folder `_index.md` vs flat file), field/enum/pattern validation, the draft-vs-active completeness gate, the referential-integrity hard-fail that writes no file, id resolution with `type/slug` ambiguity handling, and read-time inverse-edge derivation that is never stored.

---

## Acceptance Criteria

| Story | AC | Criterion | Test Scenario |
|-------|----|-----------|---------------|
| STORY-ENT-001 | AC-001 | Create a well-formed active entity: validate each field against type/enum/pattern, resolve `client`/`owner`, mint a unique bare slug, write `opportunities/{slug}/_index.md`, set `created`+`updated` to today, set `draft: false`, print the id and path, display `Created opportunity '{slug}' (active)` | TS-ENT-001-01 |
| STORY-ENT-001 | AC-002 | Missing required field saves as draft: `draft: true`, preserve the supplied fields, never block capture, display `Created opportunity '{slug}' (draft: missing required client, owner)` | TS-ENT-001-02 |
| STORY-ENT-001 | AC-003 | Relation to a non-existent target is rejected (referential-integrity hard-fail): display `No client 'ghost-co' to satisfy relation 'client'`, write no file | TS-ENT-001-03 |
| STORY-ENT-001 | AC-004 | Unknown field under `--strict`: reject, display `Unknown field 'vibe' rejected under --strict`; without `--strict`, accept and preserve `vibe` as a free extension | TS-ENT-001-04 |
| STORY-ENT-001 | AC-005 | Create a meeting with an explicit engagement edge: write flat at `meetings/{slug}.md`, mint a bare slug, store `engagement` as an explicit edge resolving to its union target (opportunity \| project \| partnership) | TS-ENT-001-05 |
| STORY-ENT-001 | AC-006 | Explicit id and collision suffix: use `acme` when unique within the type, append a deterministic suffix (`acme-2`) on a within-type collision, mint a slug from `--id` else name/type when omitted | TS-ENT-001-06 |
| STORY-ENT-002 | AC-001 | Print an entity: print frontmatter and body, resolve the id by slug (qualified `type/slug` only on ambiguity), Rich view on a TTY / JSON under `--format json`, raw file content unchanged under `--format raw` | TS-ENT-002-01 |
| STORY-ENT-002 | AC-002 | Include derived edges: include a stored forward edge (`supersedes`) and its derived inverse (`superseded_by`), mark which edges are stored versus derived — firm-ops declares no inverse, so this runs on a generic fixture | TS-ENT-002-02 |
| STORY-ENT-002 | AC-003 | Unknown id: return a lookup error, display `No entity 'nope' found` | TS-ENT-002-03 |
| STORY-ENT-002 | AC-004 | Ambiguous slug: return an ambiguity error, display `Slug 'acme' is ambiguous: client/acme, partnership/acme. Qualify as type/slug` | TS-ENT-002-04 |

---

## Requirements

| Story | ID | Type | Requirement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-ENT-001 | REQ-ENT001-01 | EARS-E | When all required fields and relations are present, the system shall write the entity as `active` | TS-ENT-001-U03 |
| STORY-ENT-001 | REQ-ENT001-02 | EARS-W | If a required field or relation is missing, then the system shall save the entity as `draft` and preserve the supplied fields | TS-ENT-001-U03 |
| STORY-ENT-001 | REQ-ENT001-03 | EARS-W | If a relation names a non-existent target, then the system shall reject the write and write no file | TS-ENT-001-U04 |
| STORY-ENT-001 | REQ-ENT001-04 | EARS-O | Where `--strict` is set, the system shall reject any undeclared field | TS-ENT-001-U05 |
| STORY-ENT-001 | REQ-ENT001-05 | EARS-E | When a meeting is created, the system shall store `engagement` as an explicit edge and write the entity flat | TS-ENT-001-U07 |
| STORY-ENT-001 | REQ-ENT001-06 | EARS-W | If an explicit `--id` collides within the type, then the system shall append a deterministic suffix | TS-ENT-001-U06 |
| STORY-ENT-002 | REQ-ENT002-01 | EARS-E | When get runs on a resolvable id, the system shall print frontmatter and body | TS-ENT-002-U01 |
| STORY-ENT-002 | REQ-ENT002-02 | EARS-O | Where `--edges` is set, the system shall include derived inverse edges, marked as derived | TS-ENT-002-U03 |
| STORY-ENT-002 | REQ-ENT002-03 | EARS-W | If the id does not resolve, then the system shall return a lookup error | TS-ENT-002-U05 |
| STORY-ENT-002 | REQ-ENT002-04 | EARS-W | If a bare slug is ambiguous across types, then the system shall require a `type/slug` qualifier | TS-ENT-002-U02 |

---

## Business Rules

| Story | ID | Rule | Enforcement | Unit Test |
|-------|----|------|-------------|-----------|
| STORY-ENT-001 | ENT-001 | The id is a bare slug, unique per `(type, slug)` | Constraint | TS-ENT-001-U01 |
| STORY-ENT-001 | ENT-002 | Referential integrity hard-fails on write; a relation must resolve | Validation | TS-ENT-001-U04 |
| STORY-ENT-001 | ENT-003 | A well-formed but incomplete entity is saved as `draft`, never rejected | Validation | TS-ENT-001-U03 |
| STORY-ENT-001 | ENT-004 | Undeclared fields are preserved unless `--strict` closes the schema | Validation | TS-ENT-001-U05 |
| STORY-ENT-002 | ENT-005 | Derived edges are computed at read time, never stored | Constraint | TS-ENT-002-U03 |
| STORY-ENT-002 | ENT-006 | A bare slug resolves only when unique across types | Validation | TS-ENT-002-U02 |
| STORY-ENT-001 | ENT-SHARED-001 | Referential integrity hard-fails on write; every relation must resolve | Validation | TS-ENT-001-U04 |
| STORY-ENT-001 | ENT-SHARED-002 | A well-formed but incomplete entity saves as `draft`; capture is never blocked | Validation | TS-ENT-001-U03 |
| STORY-ENT-002 | ENT-SHARED-003 | Forward edges store single-sided; inverse edges are derived | Constraint | TS-ENT-002-U03 |
| STORY-ENT-001 | ENT-SHARED-004 | Structural integrity is guaranteed; semantic truth is not (a schema-legal but false write validates) | Constraint | TS-ENT-001-01 |

---

## Entities

### opportunity

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `opportunity` |
| Client | Reference | Yes | Edge → client |
| Stage | Status | Yes | Pipeline stage enum |
| Owner | Reference | Yes | Edge → person |
| Created | Date | Yes | Mint date |
| Updated | Date | Yes | Last edit date |
| Source | Type | No | Lead source enum |
| Partner | Reference | No | Edge → partnership |
| CRM Id | Text | No | CRM deal id (confidence lives in CRM, not duplicated here) |

### person

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `person` |
| Name | Text | Yes | Full name |
| Role | Status | Yes | Role enum |
| Department | Type | No | Department enum |
| Created | Date | Yes | Mint date |

### meeting

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `meeting` |
| Date | Date/Time | Yes | Event date (HQ stores an ISO datetime) |
| Engagement | Reference | Yes | Edge → opportunity \| project \| partnership (explicit union edge) |
| Call Type | Type | Yes | client, sales, partner, internal |
| Source | Type | Yes | recording, manual |
| Transcript | Reference | No | Edge → transcript |
| Owner | Reference | No | Edge → person |

### client

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Type | Type | Yes | Const `client` |
| Name | Text | Yes | Organization name |
| Created | Date | Yes | Mint date |
| Updated | Date | Yes | Last edit date |

### Relations exercised *(read-side derivation)*

| Predicate | From | To | Stored / Derived | Description |
|-----------|------|----|------------------|-------------|
| engagement | meeting | opportunity \| project \| partnership | Stored | Explicit union edge written at create time |
| supersedes | generic fixture | generic fixture | Stored | Generic-fixture forward edge — firm-ops declares no inverse |
| superseded_by | generic fixture | generic fixture | Derived | Computed inverse of `supersedes` at read time; never persisted |

### Lifecycle Flag *(`draft` boolean)*

| Value | Description |
|-------|-------------|
| `draft: true` | Well-formed but incomplete; does not satisfy another entity's required relation |
| `draft: false` | All required fields and relations present (default); counts toward `check` completeness |

### Opportunity Stage *(named enumeration)*

> The CRM "Sales" pipeline stages — the deal system of record.

| Value | Description |
|-------|-------------|
| prospect | Early-stage lead in the pipeline |
| proposal-sent | Proposal issued |
| won | Deal won |
| signed | Contract signed |
| lost | Did not convert |

> **Standard Data Types:** Identifier, Reference, Text, Number, Currency, Date, Date/Time, Yes/No, Status, Type, Collection

---

## State Transitions

```
┌─────────────┐
│   (create)  │
└──────┬──────┘
       │ required fields + relations present?
       ├── yes ──► active
       └── no  ──► draft
                    │ edit fills the gap (WPK-002-2)
                    └──────────► active
```

| From | Action | To | Conditions |
|------|--------|----|------------|
| (create) | `add` with all required present | active | every required field and relation resolves |
| (create) | `add` with a gap | draft | a required field or relation is missing |
| draft | edit fills the missing field/relation | active | completeness reached (realized by `khub edit`, WPK-002-2) |

---

## API Contracts

> CLI command contract (this is a CLI, not an HTTP API). Each story maps to one `khub` subcommand.

### STORY-ENT-001: Create an Entity (`khub add`)

- **Library verb:** `core.create(type, fields, parent)`
- **Command:** `khub add <type>`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| type | string (positional) | Yes | Schema type to mint (e.g. `opportunity`, `meeting`, `client`) |
| --\<field\> \<value\> | repeatable | No | Field/relation values (e.g. `--client initech`, `--stage prospect`) |
| --id | string | No | Explicit slug; deterministic suffix on within-type collision |
| --body-file | path (`-` for stdin) | No | Body authored in the file/piped, not argv (inline `--body` omitted) |
| --strict | flag | No | Reject any undeclared field |
| --format | enum(text, json) | No | `text` confirmation (default); `json` emits the written record |

- **Writes:** one file at the type's layout path (`opportunities/{slug}/_index.md` folder, `meetings/{slug}.md` flat); frontmatter with an empty body; `created`+`updated` set to today.
- **Output (success):** the new id and file path; `Created <type> '{slug}' (active)` or `Created <type> '{slug}' (draft: missing required <fields>)`.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | referential_integrity | Relation target does not resolve — `No client 'ghost-co' to satisfy relation 'client'`; no file written |
| non-zero | strict_unknown_field | Undeclared field under `--strict` — `Unknown field 'vibe' rejected under --strict` |

### STORY-ENT-002: Read an Entity (`khub get`)

- **Library verb:** `core.get(id, with_edges)`
- **Command:** `khub get <id>`
- **Arguments / Flags:**

| Arg/Flag | Type | Required | Description |
|----------|------|----------|-------------|
| id | string (positional) | Yes | Bare slug, or `type/slug` to disambiguate |
| --edges | flag | No | Include derived inverse edges, marked as derived |
| --format | enum(json, table, raw) | No | Rich table on a TTY (default); `json` machine-readable; `raw` byte-unchanged file content |

- **Reads:** the resolved entity file; the graph projection for derived inverses.
- **Output (success):** frontmatter and body — Rich view on a TTY, JSON under `--format json`, raw file content under `--format raw`. With `--edges`, stored forward edges plus derived inverses, each marked stored vs derived.
- **Errors:**

| Exit | Code | Condition |
|------|------|-----------|
| non-zero | lookup_error | Id resolves to nothing — `No entity 'nope' found` |
| non-zero | ambiguity_error | Bare slug shared across types — `Slug 'acme' is ambiguous: client/acme, partnership/acme. Qualify as type/slug` |

---

## Test Data

### opportunity

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | resolvable `client`/`owner`, `stage` in enum | Happy-path active create (TS-ENT-001-01) |
| Invalid | required `client`/`owner` omitted | Draft degradation (TS-ENT-001-02) |
| Invalid | `client` names a non-existent `ghost-co` | Referential-integrity hard-fail, no file (TS-ENT-001-03) |
| Boundary | extra undeclared field `vibe`, with/without `--strict` | Strict-mode rejection vs free extension (TS-ENT-001-04) |

### meeting

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `engagement → initech-pov`, `call-type client`, `source recording`, `date 2026-06-19` | Explicit union edge, flat layout (TS-ENT-001-05) |

### client

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `--id acme`, `--name "Acme Corp"` into an empty type | Explicit-id use (TS-ENT-001-06) |
| Boundary | `acme` already present within the `client` type | Deterministic `acme-2` suffix (TS-ENT-001-06) |
| Ambiguous | both `client/acme` and `partnership/acme` exist | Bare-slug ambiguity error on `get` (TS-ENT-002-04) |

### project (read target)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | `initech-pov` carrying frontmatter and a body | `get` round-trips across default/json/raw branches (TS-ENT-002-01) |

### generic fixture (derived inverse)

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Valid | synthetic type declaring `supersedes` with a derived `superseded_by`; `node-a` stores `supersedes → node-b` | Derived inverse at read time — firm-ops declares none (TS-ENT-002-02) |

### Missing lookups

| Variant | Key Attributes | Purpose |
|---------|---------------|---------|
| Missing | id `nope` resolves to nothing | Lookup error on `get` (TS-ENT-002-03) |

---

## Implementation Notes

- Both commands are thin adapters over a core verb (`core.create`, `core.get`); the compiled schema and git are the only gates. The schema is introspected at runtime (WPK-001-2 layer) for fields, enums, patterns, and legal relations — never hardcoded.
- `add` order of operations: validate each field (type/enum/pattern) → resolve relations (referential-integrity hard-fail **before any byte is written**, no file) → completeness gate (all required present → `draft: false`/active; a missing required field/relation → `draft: true`, preserve supplied fields, never block capture) → mint the slug (from `--id`, else name/type; unique per `(type, slug)`; deterministic suffix on collision pinned to a within-type count, not directory scan order) → resolve the layout path (folder `_index.md` vs flat file) → write frontmatter with an empty body, `created`+`updated` to today, print id + path.
- Body is authored in the file, not argv: `add` writes an empty body; `--body-file`/stdin (`-`) covers the agent piping generated prose. Inline `--body <string>` is omitted (quoting-hostile).
- The hard-fail "writes no file" contract is the only guard against a half-written entity — verify it by snapshotting the full directory listing before the call and asserting byte-for-byte identity after, independent of exit code.
- `get` resolves the id by bare slug, requiring a `type/slug` qualifier only on cross-type ambiguity. Inverse edges (`superseded_by`) are derived at read time, marked derived, and never persisted — assert the inverse appears in `get --edges` output **and** is absent from the target entity's stored frontmatter on disk.
- Output branches: Rich on a TTY, JSON under `--format json` (with field parity to the Rich view), byte-unchanged file content under `--format raw`. Drive both branches deterministically via `CliRunner` / Rich `force_terminal`.

---

## Done Criteria

- [ ] All 10 acceptance criteria pass (ENT-001 AC-001 through AC-006; ENT-002 AC-001 through AC-004)
- [ ] All 10 EARS requirements tested (REQ-ENT001-01 through -06; REQ-ENT002-01 through -04)
- [ ] All 6 business rules enforced (ENT-001 through ENT-006), plus shared ENT-SHARED-001..004 where they apply
- [ ] All 23 test scenarios pass (TS-ENT-001-01 through -06 + U01–U07; TS-ENT-002-01 through -04 + U01–U06)
- [ ] Referential-integrity hard-fail leaves the directory listing byte-identical (snapshot before == after)
- [ ] Derived `superseded_by` shows in `get --edges` yet is absent from the target's stored frontmatter on disk
- [ ] `add opportunity` → `get` round-trips frontmatter and body (E2E)
- [ ] No regressions in existing tests
