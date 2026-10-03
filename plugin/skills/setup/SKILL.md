---
name: setup
description: Install the khub CLI and set up the current project for agent use. Use when khub commands are unavailable (command not found), or to set khub up in a repository.
user-invocable: true
allowed-tools: Bash
---

# khub: install and set up

Install the khub CLI on this machine, then set up the current project so any agent reasons in its ontology.

## 1. Install the CLI

khub is a single static binary distributed through npm as
`@endgame-build/khub`. In a repo, install it as a pinned dev dependency — one
khub version per repo, reviewed in git:

```bash
npm install -D @endgame-build/khub
npx @endgame-build/khub --version
```

If the project already pins khub in `package.json`, just `npm ci` (or
`npm install`) and use `npx @endgame-build/khub`. Always write the full
scoped name: the unscoped npm name `khub` is an unrelated package, and npx
downloads and runs it wherever the local install is missing. For a
machine-global install instead:

```bash
npm install -g @endgame-build/khub
```

npm is the only install channel; khub needs Node on the machine (the binary
itself has no runtime dependency once installed).

If the install fails, report the error and stop; install nothing partial, and
never write registry or credential configuration yourself — that is the
human's call.

## 2. Set up the project

**No workspace yet** (no `.khub/` at or above the working directory)? Scaffold one. `khub init` seeds the tree and wires the schema into your agent files (`CLAUDE.md` + `AGENTS.md`):

```bash
khub init firm-ops ./my-hub && cd my-hub
```

The agent skills ship apart from the CLI. You are reading one, so this agent has them. For the other agents on the project:

```bash
npx skills add endgame-build/khub
```

That copies the `khub` and `setup` skills into the skill folders of the agents it finds (Codex, opencode, Cursor and others). `-a <agent>` narrows it; `-g` installs for the machine instead of the project.

In Claude Code the skills come with the khub plugin, which also adds a pane over the graph and integrity warnings on writes:

```text
/plugin marketplace add endgame-build/khub
/plugin install khub@khub
```

**Workspace already present** (a cloned engagement repo)? Do not re-scaffold; just wire it:

```bash
khub wire
```

`wire` injects a managed block into the workspace's agent files (`CLAUDE.md` imports whichever layer files the workspace has — `@.khub/ontology.yaml`, `@.khub/policy.yaml`, `@.khub/storage.yaml`; `AGENTS.md` points at them instead) plus the command surface, so an agent reasons in the ontology even without running khub. Bare `wire` updates whichever files exist; `khub wire --target claude|agents|both` creates a specific one. Re-run after the schema changes; the block updates in place.

**After bumping the khub version** (a new `@endgame-build/khub` pin, or a
new global install), preview with `khub upgrade --dry-run`, then run
`khub upgrade` in each workspace. The preview validates a complete candidate
without creating workspace files or a lock. The real upgrade replaces
`.khub/` from the shipped preset (copying an edited file to `<name>.bak`
first), scaffolds what the ontology gained, removes the skill copies earlier khub
versions installed into the workspace (run `npx skills add` after it, not
before), and re-wires the agent files. Core
publication is transactional and writes the version last; wire and index
failures are reported after it. `khub upgrade --no-schema`
keeps the workspace's own schema and reports what the shipped one has that it
does not. An `upgrade_recovery_failed` error names a retained journal under
`.khub/generated/`; inspect it manually because khub does not auto-recover after a crash.

**opencode** asks before every shell command unless told otherwise. Allow khub
outright — the catch-all goes FIRST, opencode takes the last match:

```json
{ "permission": { "bash": { "*": "ask", "khub *": "allow" } } }
```

## Next

The `khub` skill maps read and write intent onto the CLI. Use it for typed context from here on.
