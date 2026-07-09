---
id: FS-006
name: Claude Code Skill
priority: High
dependencies: [FS-002, FS-003]
updated: 2026-06-21
---

# Claude Code Skill

## Overview

The agent's surface onto khub. The agent is khub's primary consumer, so the CLI and the skill ship together in v1. The skill is a thin `SKILL.md` over the same commands: it maps agent intent onto the read and write verbs the CLI already exposes, hardcodes no per-type knowledge, and reads structured output via `--format json`. It adds no logic — the core library remains the only place logic lives. The MCP server (post-v1) exposes the same verbs as tools.

**Primary Actor:** Agent

**Depends on:** FS-002: Authoring, FS-003: Query & Graph

> **Update (2026-07-08):** The skill ships from the `khub` repo as its own single-plugin Claude Code marketplace, installable via `claude plugins add` then `/plugin install khub@khub` — not through the atelier marketplace named in STORY-SKL-001 AC-002. `search` shipped in 0.2.0, so it belongs to the retrieval surface (STORY-SKL-002's fast-follow note no longer applies). A companion `/khub:setup` skill installs the CLI and runs the new `khub wire` command, which links the schema into `CLAUDE.md` so an agent reasons in the ontology even without the CLI.
>
> **Update (0.4.0, 2026-07-08):** The skill is no longer Claude-only. It adopts the open Agent Skills convention: `.claude-plugin/marketplace.json` declares the `khub` and `setup` skills, and `npx skills` (Vercel's skills.sh CLI, ~70 agents) installs them from the private repo over SSH. `khub init` installs the skill this way as a best-effort tail of scaffolding, so a fresh workspace is agent-ready in one command.

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-SKL-001 | Ship the Skill Over the CLI | Operator |
| STORY-SKL-002 | Map Agent Retrieval Intent to Reads | Agent |
| STORY-SKL-003 | Map Agent Write Intent to Authoring | Agent |
| STORY-SKL-004 | Consume Structured Output | Agent |

---

## Stories

---

### STORY-SKL-001: Ship the Skill Over the CLI

**As an** Operator
**I want to** distribute a `SKILL.md` that drives the `khub` CLI
**So that** an agent operates khub without bespoke integration code

#### Preconditions

- [ ] PRE-001: The `khub` CLI is installed and resolves on PATH
- [ ] PRE-002: A `.khub/` workspace resolves above the working directory

#### Acceptance Criteria

##### AC-001: The Skill Wraps the CLI, Not the Library

**Given** the skill is installed
**When** the agent invokes it
**Then** the system shall:
- [ ] Drive khub through the documented CLI verbs only
- [ ] Hold no per-type knowledge; introspect the schema via `khub schema`
- [ ] Add no logic beyond intent-to-command mapping

##### AC-002: Ship Through the Plugin Marketplace

**Given** the skill is a v1 deliverable
**When** it is released
**Then** the system shall:
- [ ] Ship through the acme/marketplace plugin marketplace, separate from the CLI package
- [ ] Declare the `khub` CLI as its runtime prerequisite

##### AC-003: No Workspace Resolves

**Given** the working directory has no `.khub/` above it
**When** the agent invokes a khub skill action
**Then** the system shall:
- [ ] Surface the CLI's workspace-resolution error
- [ ] Suggest `khub init <preset>`

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-SKL001-01 | EARS-U | The skill shall drive khub through the CLI verbs and hold no per-type knowledge |
| REQ-SKL001-02 | EARS-U | The skill shall ship through the plugin marketplace, separate from the CLI package |
| REQ-SKL001-03 | EARS-W | If no workspace resolves, then the skill shall surface the resolution error and suggest `init` |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| SKL-001 | The skill is a thin adapter; the core library is the only place logic lives | Constraint |
| SKL-002 | The skill introspects the schema at runtime; it hardcodes no types | Constraint |
| SKL-003 | The skill ships separately from the CLI, alongside facet and forge | Constraint |

#### Technical Notes

- **Artifact:** `SKILL.md` over the `khub` CLI
- **Distribution:** acme/marketplace plugin marketplace (see FS-007 for the CLI package)
- **Invariant upheld:** the schema is the contract; surfaces introspect, never hardcode
- **Output:** agent actions resolve to `khub` invocations

#### Test Hints

- **Unit:** intent-to-command mapping table
- **Integration:** skill action against a seeded workspace
- **E2E:** an agent installs the skill and round-trips a `get` after an `add`

---

### STORY-SKL-002: Map Agent Retrieval Intent to Reads

**As an** Agent
**I want to** turn a retrieval need into the right read command
**So that** I pull typed context instead of grepping files

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The skill is installed

#### Acceptance Criteria

##### AC-001: The v1 Retrieval Surface

**Given** an agent needs typed context
**When** it maps intent to a command
**Then** the system shall expose:
- [ ] `query` for frontmatter filters, including `--missing` for gaps
- [ ] `get` for one entity, with `--edges` for derived inverses
- [ ] `neighbors`, `impact`, and `history` for the graph walks
- [ ] Drafts in scope and `orphan`/`stale` flags by default on every read

##### AC-002: Search Defers to the Fast-Follow

**Given** the agent wants full-text search
**When** it looks for `search`
**Then** the system shall:
- [ ] Note `search` is a fast-follow (no FTS in v1)
- [ ] Fall back to `query` (frontmatter) and ripgrep in the interim

##### AC-003: Prefer Structured Output

**Given** an agent consumes a read result
**When** it issues the command
**Then** the system shall:
- [ ] Request `--format json` so the result parses without scraping a table

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-SKL002-01 | EARS-U | The skill shall map retrieval intent onto `query`, `get`, `neighbors`, `impact`, and `history` |
| REQ-SKL002-02 | EARS-U | The skill shall present `search` as a fast-follow, with `query` and ripgrep as the interim |
| REQ-SKL002-03 | EARS-O | Where a result feeds the agent, the skill shall request `--format json` |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| SKL-004 | The v1 retrieval surface is `query`/`get`/`neighbors`/`impact`/`history` | Constraint |
| SKL-005 | Reads carry drafts in scope and `orphan`/`stale` flags by default | Constraint |

#### Technical Notes

- **Commands:** `query`, `get`, `neighbors`, `impact`, `history` (FS-003); `search` deferred
- **Invariant upheld:** the graph is a derived projection; reads reflect the live tree
- **Output:** JSON for the agent

#### Test Hints

- **Unit:** intent → read-command resolution
- **Integration:** `--format json` parse of each read
- **E2E:** an agent answers a question via `neighbors` then `impact`

---

### STORY-SKL-003: Map Agent Write Intent to Authoring

**As an** Agent
**I want to** turn a capture or change into the right write command
**So that** my results land as typed entities in the same graph a human writes

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The skill is installed

#### Acceptance Criteria

##### AC-001: The Write Surface

**Given** an agent has a result to record
**When** it maps intent to a command
**Then** the system shall expose:
- [ ] `add` to mint an entity (active by default; `--draft` to capture as unpublished)
- [ ] `edit` to change fields and publish/unpublish (set the `draft` flag)
- [ ] `link` / `unlink` for relations
- [ ] `remove` for deletion, guarded by inbound edges

##### AC-002: Symmetric, Ungated Writes

**Given** agent and human write the same graph
**When** the agent writes
**Then** the system shall:
- [ ] Apply the same schema and referential-integrity gates as a human write
- [ ] Add no propose-then-approve step; the gate is the schema and git

##### AC-003: Capture Is Never Blocked

**Given** an agent has incomplete information
**When** it runs `add` without a required field
**Then** the system shall:
- [ ] Save the entity rather than reject — capture is never blocked
- [ ] Write it active by default (the agent passes `--draft` to mark it unpublished); `check` surfaces the gap as active-but-incomplete

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-SKL003-01 | EARS-U | The skill shall map write intent onto `add`, `edit`, `link`, `unlink`, and `remove` |
| REQ-SKL003-02 | EARS-U | The skill shall write through the same gates as a human, with no approval step |
| REQ-SKL003-03 | EARS-E | When required fields are missing, the skill's `add` shall still save the entity (capture is never blocked), active by default; `--draft` marks it unpublished |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| SKL-006 | Agent and human are symmetric writers; the gate is the schema and git | Constraint |
| SKL-007 | The skill never adds a write path the CLI does not already have | Constraint |

#### Technical Notes

- **Commands:** `add`, `edit`, `link`, `unlink`, `remove` (FS-002)
- **Invariant upheld:** structural integrity is guaranteed, not semantic truth
- **Output:** the new or updated id

#### Test Hints

- **Unit:** intent → write-command resolution
- **Integration:** capture-never-blocked via the skill (incomplete `add` still saves, active by default; `--draft` marks unpublished)
- **E2E:** an agent captures a fragment, then links it

---

### STORY-SKL-004: Consume Structured Output

**As an** Agent
**I want to** receive machine-readable results
**So that** I act on typed data without scraping human tables

#### Preconditions

- [ ] PRE-001: A `.khub/` workspace resolves
- [ ] PRE-002: The skill is installed

#### Acceptance Criteria

##### AC-001: JSON by Default for the Agent

**Given** a non-TTY agent context
**When** a read runs
**Then** the system shall:
- [ ] Default to JSON (the CLI emits JSON off a TTY)
- [ ] Carry each entity's `orphan` and `stale` flags

##### AC-002: Schema-Derived Shapes

**Given** the agent needs to know a type's shape
**When** it calls `schema show <type>`
**Then** the system shall:
- [ ] Return fields, enums, required flags, and relations as JSON
- [ ] Let the agent build a valid `add`/`edit` from the shape

##### AC-003: Errors Are Legible

**Given** a command fails
**When** the agent reads the result
**Then** the system shall:
- [ ] Surface the CLI's located error (field, reason, or unresolved target)

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-SKL004-01 | EARS-O | Where the context is non-TTY, the skill shall consume JSON output |
| REQ-SKL004-02 | EARS-U | The skill shall build writes from `schema show`, never from hardcoded shapes |
| REQ-SKL004-03 | EARS-W | If a command fails, then the skill shall surface the located CLI error |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| SKL-008 | The agent consumes JSON; tables are for humans | Constraint |
| SKL-009 | Write shapes derive from `schema show`, never hardcoded | Constraint |

#### Technical Notes

- **Commands:** every read with `--format json`; `schema show`
- **Invariant upheld:** the schema is the contract
- **Output:** JSON records and located errors

#### Test Hints

- **Unit:** JSON shape consumption
- **Integration:** build an `add` from `schema show` output
- **E2E:** an agent recovers from a referential-integrity error and retries

---

## Shared Context

### Entities

The skill operates over the same graph the CLI exposes; it introduces no entities of its own.

| Entity | Description |
|--------|-------------|
| Skill | The `SKILL.md` adapter mapping agent intent to `khub` commands |
| Intent Map | The table from agent need to CLI verb |
| Command Invocation | One resolved `khub` call with `--format json` |

#### Command Invocation

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Verb | Text | Yes | The khub command (`add`/`get`/`query`/…) |
| Intent | Text | Yes | The agent need the call serves |
| Format | Type | No | Output shape (`json` for the agent) |
| Workspace | Reference | Yes | The `.khub` workspace the call runs against |

### Surface Map *(named enumeration)*

| Value | Description |
|-------|-------------|
| retrieval | `query`, `get`, `neighbors`, `impact`, `history` (v1) |
| write | `add`, `edit`, `link`, `unlink`, `remove` (v1) |
| introspection | `schema`, `schema show`, `schema edges`, `status` (v1) |
| search | `search` (fast-follow; `query` + ripgrep interim) |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| A new CLI verb ships | The skill maps it without new logic | The skill is a thin adapter |
| The schema changes | The skill reflects it via `schema show` | No hardcoded types |
| The MCP server lands (post-v1) | The same verbs become tools | One verb set across surfaces |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| SKL-SHARED-001 | The skill adds no logic; the core library is the only place logic lives | STORY-SKL-001, STORY-SKL-003 |
| SKL-SHARED-002 | Every surface introspects the schema; no per-type knowledge is hardcoded | STORY-SKL-002, STORY-SKL-004 |

### Cross-Feature Dependencies

| Entity | Source Feature | Usage |
|--------|---------------|-------|
| Read commands | FS-003: Query & Graph | The retrieval surface the skill maps onto |
| Write commands | FS-002: Authoring | The write surface the skill maps onto |
| Schema introspection | FS-001: Workspace & Schema | `schema show` drives write shapes |
| CLI package | FS-007: Distribution | The runtime prerequisite the skill declares |

### Cross-Story Dependencies

```
STORY-SKL-001 (Ship the Skill)
    ├── STORY-SKL-002 (Map Retrieval)
    ├── STORY-SKL-003 (Map Writes)
    └── STORY-SKL-004 (Consume Structured Output)
```

The skill ships first; the intent maps and structured-output handling all ride on that one adapter.

## Scope Changes

### Deferred Stories

| Story | Moved To | Reason | Date |
|-------|----------|--------|------|
| MCP server (same verbs as tools) | Post-v1 | Tools generated from the LinkML/Pydantic models, after v1 | 2026-06-21 |
| Search intent mapping (`search`) | Fast-follow | Needs the SQLite/FTS projection | 2026-06-21 |
