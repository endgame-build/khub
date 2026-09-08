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
npx khub --version
```

If the project already pins khub in `package.json`, just `npm ci` (or
`npm install`) and use `npx khub`. For a machine-global install instead:

```bash
npm install -g @endgame-build/khub
```

khub's packages live on GitHub Packages, which is private today, so npm needs
the scope mapped and a GitHub token with `read:packages` — two lines in
`~/.npmrc`:

```
@endgame-build:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=<token>
```

**Do not write those yourself.** If the install fails with a 401 or 404, that
file is missing or its token lacks the `read:packages` scope — report exactly
that, name the scope, and stop. Configuring a credential is the human's call,
not yours.

An SSH key does **not** work for the registry, even though it does work for
`npx skills add git@github.com:endgame-build/khub.git -s setup`. Having just
succeeded at that, SSH is the wrong guess to reach for next.

If the install fails for any other reason, report the error and stop; install
nothing partial.

## 2. Set up the project

**No workspace yet** (no `.khub/` at or above the working directory)? Scaffold one. `khub init` seeds the tree and wires the schema into your agent files (`CLAUDE.md` + `AGENTS.md`):

```bash
khub init firm-ops ./my-hub && cd my-hub
```

Then install khub's agent skills into this project — a file copy out of the installed package, so it needs no network:

```bash
khub install-skills
```

That writes both skills into `.claude/skills/`, `.agents/skills/`, and `.opencode/skills/`, and gitignores them (they are reproducible from the CLI). `--target <name>` narrows it; `--global` installs into your home directories instead, once per machine.

**Workspace already present** (a cloned engagement repo)? Do not re-scaffold; just wire it:

```bash
khub wire
```

`wire` injects a managed block into the workspace's agent files (`CLAUDE.md` imports whichever layer files the workspace has — `@.khub/ontology.yaml`, `@.khub/policy.yaml`, `@.khub/storage.yaml`; `AGENTS.md` points at them instead) plus the command surface, so an agent reasons in the ontology even without running khub. Bare `wire` updates whichever files exist; `khub wire --target claude|agents|both` creates a specific one. Re-run after the schema changes; the block updates in place.

Run `khub install-skills` here too, so this workspace carries the skills.

**After bumping the khub version** (a new `@endgame-build/khub` pin, or a
new global install), preview with `khub upgrade --dry-run`, then run
`khub upgrade` in each workspace. The preview validates a complete candidate
without creating workspace files or a lock. The real upgrade replaces
`.khub/` from the shipped preset (copying an edited file to `<name>.bak`
first), scaffolds what the ontology gained, re-installs the skills and re-wires
the agent files. Core publication is transactional and writes the version last;
skill, wire, and index failures are reported after it. `khub upgrade --no-schema`
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
