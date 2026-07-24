---
name: setup
description: Install the khub CLI and set up the current project for agent use. Use when khub commands are unavailable (command not found), or to set khub up in a repository. Invocable as /khub:setup.
user-invocable: true
allowed-tools: Bash
---

# khub: install and set up

Install the khub CLI on this machine, then set up the current project so any agent reasons in its ontology.

## 1. Install the CLI

khub needs `uv` and Python 3.11+. Install the pinned release from the private repo (SSH key, or HTTPS with a token):

```bash
uv tool install git+ssh://git@github.com/endgame-build/khub@v0.8.0
```

Then confirm it resolves:

```bash
khub --version
```

If `uv` or Python 3.11+ is missing, report the prerequisite error and stop; install nothing partial.

## 2. Set up the project

**No workspace yet** (no `.khub/` at or above the working directory)? Scaffold one. `khub init` seeds the tree and wires the schema into your agent files (`CLAUDE.md` + `AGENTS.md`):

```bash
khub init firm-ops ./my-hub && cd my-hub
```

Then install the agent skill into this project (a separate step since 0.9.0 — it needs `npx` and SSH access, which a scaffold must not depend on):

```bash
khub install-skills
```

**Workspace already present** (a cloned engagement repo)? Do not re-scaffold; just wire it:

```bash
khub wire
```

`wire` injects a managed block into the workspace's agent files (`CLAUDE.md` imports the schema via `@.khub/schema.yaml`; `AGENTS.md` points at the schema file) plus the command surface, so an agent reasons in the ontology even without running khub. Bare `wire` updates whichever files exist; `khub wire --target claude|agents|both` creates a specific one. Re-run after the schema changes; the block updates in place.

## Next

The `khub` skill maps read and write intent onto the CLI. Use it for typed context from here on.
