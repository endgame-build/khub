# The khub plugin for Claude Code

The plugin puts khub into a Claude Code session. It carries the two agent
skills and a mod, a set of hooks that draw in the session and wrap the agent's
tool calls. The agent keeps using the khub CLI through Bash; the plugin adds no
tool the model can call.

Other agents (opencode, Cursor, Codex, Gemini CLI) get the skills alone, with
`npx skills add endgame-build/khub`.

## Install

```text
/plugin marketplace add endgame-build/khub
/plugin install khub@khub
```

From a shell the same two steps are `claude plugin marketplace add
endgame-build/khub` and `claude plugin install khub@khub`. Both use your own git
credentials.

The plugin needs Claude Code 2.1.288 or later and the khub CLI, 0.27.0 or
later. It looks for khub in this order: the `binary` setting,
`node_modules/.bin/khub` under the workspace, then `khub` on the path. With an
older khub the status line reads `khub: needs khub 0.27.0 or newer` and the
plugin does nothing else.

Outside a khub workspace (no `.khub/` at or above the working directory) the
plugin does nothing.

## What it does

| Where | What you see |
|---|---|
| Tool rows | A khub call the agent makes draws as one line: `● khub add adr`, then `+ adr/ad-…`. A `json` button on the row shows the raw output. |
| After a write | khub `validate` and `check` run, and the agent is told about findings its write introduced, as `<id> › <rule>: <message>`. This holds for `khub` calls and for hand edits of entity files. |
| Band above the prompt | One line. Normally a dim summary: `khub  ✓ · 146 entities · 3 draft · p50 38 ms`. When something needs attention it says so instead, with buttons: what the turn wrote, new errors, a pending schema change, a preset upgrade. |
| Status line | Problems only, since Claude Code draws a plugin's status line as a warning: `khub: ✗ 1 error · …` while `check` fails, a schema that does not resolve, a khub that is missing or too old. |
| Pane | `/khub` opens it. Five tabs: Session, Health, Browse, Search, Stats. |

Guards on the agent's tool calls:

- A Write that creates a file inside a type's storage path is denied, with a
  pointer to `khub add <type>`.
- A command that removes or moves the workspace lock is denied.
- `remove --force`, `init --force` and a real `upgrade` ask first.
- A `khub add` that names no author is stamped with the `agent_author` setting.

After a Read of an entity file the agent is told which entities point at it,
since the inbound edge is not in the file.

## The pane

| Tab | Holds |
|---|---|
| Session | What this session added, edited, linked and removed, each with its validate result, and the lenses to answer for entities whose body changed. |
| Health | The check's verdict and findings, marked when new this session. Drafts, with Publish and Remove. The index preview and Reindex. A pending preset upgrade. |
| Browse | Types, then a type's entities, then one entity with its fields, its edges both ways and its body. Edit, Link, Unlink, Publish, Remove, and an impact tree per inbound predicate. |
| Search | Plain-word search over the corpus. A hit opens in Browse. |
| Stats | khub calls by verb, reads against writes, refusals by code, search outcomes, p50 and p95, for this session or all sessions. |

Forms are built from `khub schema`, so a type you add to the ontology gets its
form with no change to the plugin.

## The command

`/khub` is shared with the `khub` skill:

| You type | What runs |
|---|---|
| `/khub` | The pane opens. |
| `/khub <khub command>` | khub runs with those arguments and its answer prints in the transcript, for example `/khub get cmp-search`. Writes go through the same integrity check. |
| `/khub doctor` | The workspace is examined again: a pending preset upgrade and schema drift. |
| `/khub <anything else>` | The skill answers. |

## Settings

Set them with `claude plugin install khub@khub --config key=value`, or `claude plugin configure khub@khub` afterwards.

| Setting | Values | Default | Effect |
|---|---|---|---|
| `binary` | text | empty | The command that runs khub. |
| `sidebar` | `on-request`, `wide` | `on-request` | `wide` also opens the pane at session start on wide terminals. |
| `gate` | `advisory`, `block` | `advisory` | `block` keeps a turn from finishing while errors the session introduced remain. |
| `agent_author` | text | `claude-code` | The author stamped on the agent's adds. Empty turns the stamp off. |
| `guard_new_files` | boolean | true | Deny a Write that creates an entity file by hand. |

## Working on the plugin

The sources are under `plugin/`. `hooks/register.tsx` holds every call on the
engine; the files beside it are pure. `.claude/rules/plugin.md` has the rules.

```bash
claude plugin validate --strict .            # from the repo root: the marketplace manifest
cd plugin
claude plugin validate --strict .            # the plugin manifest and what the hooks call
claude plugin test .                         # the tests
tsc -p .                                     # types, in a checkout loaded with --plugin-dir
bash tools/record-fixtures.sh                # re-record khub's output into tests/fixtures.ts
bash tools/try.sh                            # a throwaway workspace and the command to open it
```

`tests/fixtures.ts` is recorded from the khub built from the checkout, so a
change to a JSON shape the plugin reads fails its tests.
