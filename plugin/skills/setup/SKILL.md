---
name: setup
description: Install the khub CLI and wire the current project into agent context. Use when khub commands are unavailable (command not found), or to set khub up in a repository. Invocable as /khub:setup.
user-invocable: true
allowed-tools: Bash
---

# khub: install and wire

Set up the khub CLI on this machine and wire the current project so any agent reasons in its ontology.

## 1. Install the CLI

khub needs `uv` and Python 3.11+. Install the pinned release from the private repo (SSH key, or HTTPS with a token):

```bash
uv tool install git+ssh://git@github.com/endgame-build/knowledge-hub@v0.3.0
```

Then confirm it resolves:

```bash
khub --version
```

If `uv` or Python 3.11+ is missing, report the prerequisite error and stop; install nothing partial.

## 2. Wire the current project

`khub wire` needs a workspace. If a `.khub/` directory resolves at or above the working directory, wire it:

```bash
khub wire
```

This injects a managed block into `CLAUDE.md` that imports the schema (`@.khub/schema.yaml`) and lists the command surface, so an agent reasons in the ontology even without running khub. Re-run it after the schema changes; the block updates in place. Add `--agents` to mirror it into `AGENTS.md`.

If no workspace resolves, seed one first, then wire:

```bash
khub init firm-ops ./my-hub && cd my-hub && khub wire
```

## Next

The `khub` skill maps read and write intent onto the CLI. Use it for typed context from here on.
