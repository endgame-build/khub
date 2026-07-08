---
id: FS-007
name: Distribution
priority: High
dependencies: [FS-000, FS-001]
updated: 2026-06-21
---

# Distribution

## Overview

How khub ships and runs. khub is a Python package (3.11+) exposed as a `khub` console script, installed and run through `uv`. The zero-install path mirrors `npx`, straight from the private repo over git. Everything stays private for now: `uvx` and `uv tool install` authenticate through git (SSH key or HTTPS token); a public PyPI release waits until there is a reason to open the engine. The engine and the canonical presets are collocated in one private repo for v1, with `--preset-source` reserved for a later split.

**Primary Actor:** Operator

**Depends on:** FS-000: Schema & Compiler, FS-001: Workspace & Schema

> **Update (2026-07-08):** Distribution stays on the pinned git install (`uv tool install git+ssh://…@v0.3.0`, `uvx --from …@v0.3.0`). No private PyPI exists and khub stays private, so the bare `uvx khub` shorthand remains deferred (STORY-DST-002 AC-002). `RELEASING.md` records the tag-based release flow.

---

## Story Summary

| ID | Name | Actor |
|----|------|-------|
| STORY-DST-001 | Install the CLI | Operator |
| STORY-DST-002 | Run Zero-Install via uvx | Operator |
| STORY-DST-003 | Authenticate Against the Private Repo | Operator |
| STORY-DST-004 | Resolve the Preset Source | Operator |

---

## Stories

---

### STORY-DST-001: Install the CLI

**As an** Operator
**I want to** install khub once as a `khub` console script
**So that** the command resolves on PATH for repeated use and for the skill

#### Preconditions

- [ ] PRE-001: `uv` is installed
- [ ] PRE-002: Python 3.11+ is available to `uv`
- [ ] PRE-003: Git access to the private repo resolves (see STORY-DST-003)

#### Acceptance Criteria

##### AC-001: Install Once, Reuse

**Given** an operator with `uv` and repo access
**When** they run `uv tool install git+ssh://git@github.com/endgame-build/knowledge-hub`
**Then** the system shall:
- [ ] Install the `khub` console script on PATH
- [ ] Bundle the engine and the canonical presets
- [ ] Resolve `khub --version` and `khub --help`

##### AC-002: Console-Script Entry Point

**Given** the package is installed
**When** the operator runs `khub`
**Then** the system shall:
- [ ] Dispatch to the Typer CLI (FS-000…FS-005 verbs)
- [ ] Require no runtime tie to the hub repo after install

##### AC-003: Missing uv or Python

**Given** `uv` or a compatible Python is absent
**When** the operator attempts to install
**Then** the system shall:
- [ ] Fail with a prerequisite error naming `uv` / Python 3.11+
- [ ] Install nothing partial

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-DST001-01 | EARS-E | When installed via `uv tool install`, the system shall expose the `khub` console script on PATH |
| REQ-DST001-02 | EARS-U | The installed package shall carry no runtime dependency on the hub repo |
| REQ-DST001-03 | EARS-W | If `uv` or Python 3.11+ is absent, then the system shall fail with a prerequisite error |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| DST-001 | khub ships as a `khub` console script, installed through `uv` | Constraint |
| DST-002 | An installed workspace carries no runtime tie to the hub | Constraint |
| DST-003 | Python 3.11+ is the floor | Validation |

#### Technical Notes

- **Command:** `uv tool install git+ssh://git@github.com/endgame-build/knowledge-hub`
- **Entry point:** `khub` console script over the Typer CLI
- **Invariant upheld:** the workspace is self-contained after init (FS-001)
- **Output:** a `khub` binary on PATH

#### Test Hints

- **Unit:** entry-point registration
- **Integration:** `uv tool install` from a git ref, then `khub --version`
- **E2E:** install, then `khub init firm-ops` succeeds with no hub checkout

---

### STORY-DST-002: Run Zero-Install via uvx

**As an** Operator
**I want to** run khub without installing it first
**So that** I can scaffold a workspace in one command, npx-style

#### Preconditions

- [ ] PRE-001: `uv` is installed
- [ ] PRE-002: Git access to the private repo resolves

#### Acceptance Criteria

##### AC-001: Scaffold in One Command

**Given** an operator with `uv` and repo access
**When** they run `uvx --from git+ssh://git@github.com/endgame-build/knowledge-hub khub init firm-ops ./my-hub`
**Then** the system shall:
- [ ] Fetch and run khub without a prior install
- [ ] Execute the `init` exactly as the installed CLI would

##### AC-002: Private Shorthand Only

**Given** the engine stays private
**When** the operator looks for the bare `uvx khub` shorthand
**Then** the system shall:
- [ ] Require `--from git+ssh://…` (no PyPI shorthand while private)
- [ ] Reserve the bare `uvx khub` form for a future public release

##### AC-003: Repo Fetch Fails

**Given** the git ref or network is unavailable
**When** the operator runs the `uvx --from <git>` command
**Then** the system shall:
- [ ] Surface git's fetch error
- [ ] Run nothing and leave no partial workspace

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-DST002-01 | EARS-E | When run via `uvx --from <git>`, the system shall execute khub with no prior install |
| REQ-DST002-02 | EARS-U | The system shall require the `--from <git>` form while the repo is private |
| REQ-DST002-03 | EARS-W | If the repo fetch fails, then the system shall surface git's error and run nothing |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| DST-004 | The zero-install path runs from the private repo over git | Constraint |
| DST-005 | The bare `uvx khub` shorthand waits on a public release | Constraint |

#### Technical Notes

- **Command:** `uvx --from git+ssh://git@github.com/endgame-build/knowledge-hub khub <args>`
- **Invariant upheld:** Markdown is truth; `uvx` owns only execution, never the data
- **Output:** the invoked command's result (e.g. a scaffolded workspace)

#### Test Hints

- **Unit:** `--from` argument handling
- **Integration:** `uvx --from <git> khub --version`
- **E2E:** `uvx … khub init firm-ops ./tmp` scaffolds a workspace

---

### STORY-DST-003: Authenticate Against the Private Repo

**As an** Operator
**I want to** install or run khub from the private repo with my git credentials
**So that** distribution stays private without a separate auth system

#### Preconditions

- [ ] PRE-001: An SSH key or an HTTPS token grants access to the repo

#### Acceptance Criteria

##### AC-001: Auth Delegates to Git

**Given** an operator with valid git credentials
**When** they install or `uvx`-run from the repo
**Then** the system shall:
- [ ] Delegate authentication to git (SSH key or HTTPS token)
- [ ] Add no khub-specific credential store

##### AC-002: Missing Credentials

**Given** the operator lacks repo access
**When** they attempt to install or run
**Then** the system shall:
- [ ] Surface git's authentication failure
- [ ] Install or run nothing

##### AC-003: HTTPS Token Path

**Given** an operator using HTTPS rather than SSH
**When** they install from `git+https://…`
**Then** the system shall:
- [ ] Accept the HTTPS token git provides
- [ ] Behave identically to the SSH path thereafter

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-DST003-01 | EARS-U | The system shall delegate authentication to git, with no separate credential store |
| REQ-DST003-02 | EARS-W | If credentials are missing, then the system shall surface git's auth failure and do nothing |
| REQ-DST003-03 | EARS-U | The system shall support both SSH-key and HTTPS-token git access |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| DST-006 | Auth is git's job; khub adds no credential layer | Constraint |
| DST-007 | Everything stays private until there is a reason to open the engine | Constraint |

#### Technical Notes

- **Mechanism:** `uv` delegates fetch and auth to git (SSH or HTTPS token)
- **Invariant upheld:** distribution stays private with no bespoke auth
- **Output:** an authenticated fetch, or git's failure

#### Test Hints

- **Unit:** git-URL scheme handling (ssh vs https)
- **Integration:** auth failure surfaces cleanly
- **E2E:** install over SSH and over HTTPS both reach `khub --version`

---

### STORY-DST-004: Resolve the Preset Source

**As an** Operator
**I want to** name where presets come from at init
**So that** the engine and presets can split repos later without breaking `init`

#### Preconditions

- [ ] PRE-001: khub resolves (installed or via `uvx`)

#### Acceptance Criteria

##### AC-001: Collocated Presets by Default

**Given** the v1 layout (engine and presets in one repo)
**When** the operator runs `khub init firm-ops` with no `--preset-source`
**Then** the system shall:
- [ ] Resolve the preset from the bundled, collocated presets

##### AC-002: Override the Preset Source

**Given** presets later live in their own private repo
**When** the operator runs `khub init engineering --preset-source <git|path>`
**Then** the system shall:
- [ ] Pull the named preset from that source
- [ ] Flatten `core` + preset into the workspace exactly as the collocated path does (FS-001)

##### AC-003: Unresolvable Preset Source

**Given** a `--preset-source` that does not resolve
**When** the operator runs `init`
**Then** the system shall:
- [ ] Return a configuration error naming the source
- [ ] Write nothing

#### Requirements (EARS)

| ID | Type | Requirement |
|----|------|-------------|
| REQ-DST004-01 | EARS-U | The system shall resolve presets from the collocated bundle by default |
| REQ-DST004-02 | EARS-O | Where `--preset-source <git\|path>` is set, the system shall pull presets from that source |
| REQ-DST004-03 | EARS-W | If the preset source does not resolve, then the system shall fail init and write nothing |

#### Business Rules

| ID | Rule | Enforcement |
|----|------|-------------|
| DST-008 | v1 collocates the engine and presets; the presets are the IP | Constraint |
| DST-009 | `--preset-source` keeps `init` stable across a future repo split | Constraint |

#### Technical Notes

- **Command:** `khub init <preset> [path] --preset-source <git\|path>` (FS-001)
- **Invariant upheld:** the workspace is self-contained after init
- **Output:** a scaffolded workspace from the resolved preset source

#### Test Hints

- **Unit:** preset-source resolution (collocated vs override)
- **Integration:** `--preset-source <path>` against a local preset checkout
- **E2E:** `init` with and without `--preset-source` yield the same flattened schema

---

## Shared Context

### Entities

Distribution concerns packaging and fetch, not graph entities.

| Entity | Description |
|--------|-------------|
| Package | The Python distribution exposing the `khub` console script |
| Preset Source | The git URL or path `init` pulls presets from |
| Credential | The SSH key or HTTPS token git uses to fetch the private repo |

#### Package

| Attribute | Type | Required | Description |
|-----------|------|----------|-------------|
| Name | Text | Yes | The distribution name (`khub`) |
| Entry Point | Text | Yes | The `khub` console script over the Typer CLI |
| Python | Text | Yes | Floor version (3.11+) |
| Channel | Type | Yes | `uvx` or `uv-tool` (PyPI deferred) |
| Preset Source | Reference | No | Where `init` pulls presets from (collocated by default) |

### Distribution Channel *(named enumeration)*

| Value | Description |
|-------|-------------|
| uvx | Zero-install run from the private repo (`uvx --from <git> khub …`) |
| uv-tool | Install once, reuse (`uv tool install <git>`) |
| pypi | Public `uvx khub` shorthand (deferred until the engine opens) |
| marketplace | The Claude Code skill, shipped separately (FS-006) |

### Cascade Behaviors

| Relationship | Behavior | Rationale |
|--------------|----------|-----------|
| The presets split into their own repo | `init --preset-source` points at it | `init` stays stable across the split |
| The engine opens to the public | The bare `uvx khub` shorthand becomes available | Private-only is a current constraint, not a design limit |
| A new khub version ships | `uv tool install` / `uvx` fetch the new git ref | No bespoke update channel |

### Shared Business Rules

| ID | Rule | Applies To |
|----|------|------------|
| DST-SHARED-001 | khub runs through `uv`; auth and fetch are git's job | STORY-DST-001, STORY-DST-002, STORY-DST-003 |
| DST-SHARED-002 | The workspace is self-contained after init; distribution owns only fetch and execution | STORY-DST-001, STORY-DST-004 |

### Cross-Feature Dependencies

| Entity | Source Feature | Usage |
|--------|---------------|-------|
| Engine package | FS-000: Schema & Compiler | The compiled engine the package bundles |
| `init` | FS-001: Workspace & Schema | The command `uvx`/install most often runs, and the `--preset-source` consumer |
| Skill | FS-006: Claude Code Skill | Ships separately through the marketplace, declaring this CLI as prerequisite |

### Cross-Story Dependencies

```
STORY-DST-003 (Authenticate)
    ├── STORY-DST-001 (Install the CLI)
    │       └── STORY-DST-004 (Resolve the Preset Source)
    └── STORY-DST-002 (Run Zero-Install via uvx)
```

Auth underpins both the install and the zero-install paths; preset-source resolution rides on a working khub.

## Scope Changes

### Deferred Stories

| Story | Moved To | Reason | Date |
|-------|----------|--------|------|
| Public PyPI release and bare `uvx khub` shorthand | Deferred | Stays private until there is a reason to open the engine | 2026-06-21 |
| Presets split into their own private repo | Deferred | v1 collocates engine and presets; `--preset-source` reserves the seam | 2026-06-21 |
| Open-core (public engine, private presets) | Deferred | A later option, not a v1 commitment | 2026-06-21 |
