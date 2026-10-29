---
name: setup
description: Install the khub CLI and set up the current project for agent use. Use when khub commands are unavailable (command not found), or to set khub up in a repository.
user-invocable: true
allowed-tools: Bash
---

# khub: install and set up

Install the khub CLI on this machine, then set up the current project so any agent reasons in its ontology.

**Done when** khub answers from the workspace: `khub status --format json`, run inside it, prints its counts, and `khub check` runs to a verdict. A `khub init` that prints success is not the end; step 3 is.

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
scoped name (see the never-do list below). For a machine-global install
instead:

```bash
npm install -g @endgame-build/khub
```

npm is the only install channel; khub needs Node on the machine (the binary
itself has no runtime dependency once installed).

If the install fails, report the error and stop.

The rest of this skill writes `khub …`. With the per-repo install, run that as
`npx @endgame-build/khub …`: a dev dependency puts no `khub` on the `PATH`, so a
bare `khub` fails there. With a global install, run it as `khub …`.

## 2. Set up the project

**No workspace yet** (no `.khub/` at or above the working directory)? Scaffold one. `khub init` seeds the tree and wires the schema into your agent files (`CLAUDE.md` + `AGENTS.md`):

```bash
khub init firm-ops ./my-hub && cd my-hub
# per-repo install: npx @endgame-build/khub init firm-ops ./my-hub && cd my-hub
```

The agent skills ship apart from the CLI. You are reading one, so this agent has them. For the other agents on the project:

```bash
npx skills add endgame-build/khub
```

That copies the `khub` and `setup` skills into the skill folders of the agents it finds (Codex, opencode, Cursor and others). `-a <agent>` narrows it; `-g` installs for the machine instead of the project.

In Claude Code the skills come with the khub plugin, which also draws a band above the prompt and compact rows for khub calls:

```text
/plugin marketplace add endgame-build/khub
/plugin install khub@khub
```

The user types those two at the Claude Code prompt. To update the plugin later, the user runs these from a shell, then starts a new session:

```bash
claude plugin marketplace update khub
claude plugin update khub@khub
```

**Workspace already present** (a cloned engagement repo)? Do not re-scaffold; wire it:

```bash
khub wire
```

`wire` injects a managed block into the workspace's agent files (`CLAUDE.md` imports whichever layer files the workspace has — `@.khub/ontology.yaml`, `@.khub/policy.yaml`, `@.khub/storage.yaml`; `AGENTS.md` points at them instead) plus the command surface, so an agent reasons in the ontology even without running khub. Bare `wire` updates whichever files exist; `khub wire --target claude|agents|both` creates a specific one. Re-run after the schema changes; the block updates in place.

**After bumping the khub version** (a new `@endgame-build/khub` pin, or a
new global install), preview with `khub upgrade --dry-run`, then run
`khub upgrade` in each workspace. The preview validates a complete candidate
without creating workspace files or a lock. The real upgrade replaces
`.khub/` from the shipped preset (copying an edited file to `<name>.bak`
first), scaffolds what the ontology gained, and re-wires the agent files. It
leaves the skill copies khub 0.27.0 and earlier wrote under `.claude/skills/`,
`.agents/skills/` and `.opencode/skills/`; delete those folders and their
`.gitignore` lines by hand. Core
publication is transactional and writes the version last; wire and index
failures are reported after it. `khub upgrade --no-schema`
keeps the workspace's own schema and reports what the shipped one has that it
does not. An `upgrade_recovery_failed` error names a retained journal under
`.khub/generated/`; inspect it manually because khub does not auto-recover after a crash.

**opencode** asks before every shell command unless told otherwise. Allow khub
outright — the catch-all goes FIRST, opencode takes the last match:

```json
{ "permission": { "bash": { "*": "ask", "khub *": "allow", "npx @endgame-build/khub *": "allow" } } }
```

## 3. Prove it works

From inside the workspace:

```bash
khub status --format json   # counts per type; a fresh preset shows its seeded singletons, or zero
khub check                  # exits 0 (passed) or 1 (findings); either is a working setup
```

Exit 2, an `error.code` in the JSON, or `command not found` means the setup is not done: fix the cause (wrong directory, missing install, bare `khub` after a dev-dependency install) and run both again.

The khub plugin's band above the prompt is for the user to see; you cannot, so do not wait on it. It shows the workspace's total only when two things hold:

- Claude Code was started at the workspace or inside it. The plugin finds `.khub/` from the directory the session started in and does not follow a `cd`, so a workspace that `init` created in a subdirectory gets no band until a session starts there.
- khub is reachable from the workspace root: installed under `<workspace>/node_modules`, installed globally on the `PATH`, or named by the plugin's `binary` setting. A dev dependency installed in a parent directory is none of these.

A plugin installed or updated during an open session loads after `/reload-plugins` or in a new session. Say in the report whether the user should expect the band.

## 4. Report back

End with one message to the user in this shape:

```text
khub is set up.
- Workspace: <absolute path of the directory holding .khub/>
- Preset: <name and version from `khub schema --format json` provenance>
- khub: <version>, installed <per-repo dev dependency | globally>; run it as <`npx @endgame-build/khub` | `khub`>
- Verified: `khub status` total <n>; `khub check` <passed | n findings>
- Claude Code band: <expect it with total <n> when Claude Code starts in <workspace> | not expected, because <reason>; to get it, <fix>>
- Other agents: `npx skills add endgame-build/khub` (Claude Code: `/plugin marketplace add endgame-build/khub`, then `/plugin install khub@khub`)
- Try next: `khub query`, then `khub add <type> …` for a first entity
- Unusual: <anything that surprised you — a warning, a skipped step, an old skill folder left to delete — or "nothing">
```

## Never do

- Never re-run `khub init` in a directory that already has `.khub/` at or above it. Run `khub wire`, or `khub upgrade` after a version bump.
- Never install or run the unscoped npm name `khub`. It is an unrelated package, and npx downloads and runs it wherever the local install is missing. Always write `@endgame-build/khub`.
- Never write registry, token or credential configuration (`.npmrc`, registry or token environment variables) yourself. That is the human's call.
- Never leave a partial install behind. If a step fails, report the error and stop.
- Never report the setup done before the commands in step 3 answer.
- Never edit the managed block `wire` writes into `CLAUDE.md` or `AGENTS.md`. Re-run `khub wire`; it rewrites the block in place.

## Next

The `khub` skill maps read and write intent onto the CLI. Use it for typed context from here on.
