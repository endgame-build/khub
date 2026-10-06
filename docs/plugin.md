# The khub plugin for Claude Code

The plugin puts khub into a Claude Code session. It carries the two agent
skills (`khub` and `setup`) and a small mod, a set of hooks that draw in the
session. The agent keeps using the khub CLI through Bash.

Other agents (opencode, Cursor, Codex, Gemini CLI) get the skills alone, with
`npx skills add endgame-build/khub`.

The mod is active in a session whose working directory is inside a khub
workspace, which is a directory with a `.khub/` folder at or above it. In any
other session it draws nothing.

## What it shows

```text
khub  build-hub 0.6.0 · 146 entities [+2 −1 ~3] · 3 draft · check ✓

● khub query adr 38 ms json
  ⎿ 12 entities
    adr/ad-2026-01-15-use-re2-patterns     Use RE2 patterns        draft
    adr/ad-2026-02-03-pin-khub-per-repo    Pin khub per repo
    adr/ad-2026-02-20-one-workspace-lock   One workspace lock
    adr/ad-2026-03-11-single-sided-edges   Single-sided edges      stale
    adr/ad-2026-04-02-search-in-memory     Search in memory
    … 7 more
```

### The band

The band is one line above the prompt.

| Part | Meaning |
|---|---|
| `build-hub 0.6.0` | The workspace's preset and its version. |
| `146 entities` | Every entity in the workspace, drafts included. |
| `[+2 −1 ~3]` | What the session changed: 2 entities added, 1 removed, 3 edited. A count of zero is left out, and the bracket is absent while the session has changed nothing. |
| `3 draft` | Entities with `draft: true`. A count of zero is left out. |
| `check ✓` | `khub check` passes. |

A failing `khub check` reads `check ✗ 1 error` or `check ✗ <n> errors` and turns
the whole line red.

### The rows

In a verbose session, a khub call Claude makes through Bash draws as a compact
row in place of Claude Code's raw drawing.

| Line | Holds |
|---|---|
| Head | The call, how long it took, and a `json` button: `● khub query adr 38 ms json`. The time includes any wait at the approval dialog. |
| Result | What came back, in one line: `⎿ 12 entities`, or `⎿ + adr/ad-… · draft` after an `add`. |
| List | Up to five lines of a list result, then `… n more`. |

- A list follows the result line for `query`, `stale`, `search`, `neighbors`, a
  `get` with several ids, a failed `check` and a `validate` with errors.
- A list line for an entity holds its id, its title and its flags (`draft`,
  `orphan`, `stale`). A `neighbors` line holds the id, the direction and the
  predicate. A `check` line holds the bucket and the id or path. A `validate`
  line holds the id, the field and the reason.
- The `json` button returns that compact row to Claude Code's raw drawing.
- A command made only of khub calls joined by `;` or `&&` draws one compact row
  per call, with the time and the `json` button on the first. A leading
  `cd <dir>` is allowed. The row keeps the raw drawing when the output does not
  hold one JSON document per call. Its rows leave out khub's stderr notes, such
  as a search's thin-result note.
- A command that pipes khub's output, redirects it, runs in a subshell or runs
  another program keeps the raw drawing, because its output is not khub's alone.
- In the ctrl+o detailed transcript the compact row stands for the whole call.
  Claude Code's raw result block is hidden there, along with the notes it
  draws under that block, such as the auto-mode line. The `json` button shows
  them again.
- A command the user types with `!` is not redrawn.
- With verbose off, Claude Code folds shell commands into one count line, so
  the compact rows do not show. The band still does.

## The session diff

The baseline is the set of entity ids khub reports when the session starts.

| Count | Source | Includes |
|---|---|---|
| `+` added | khub's current ids against the baseline | Every new entity, whoever made it: Claude, a hand edit or a shell command. |
| `−` removed | khub's current ids against the baseline | Every entity that is gone, whoever removed it. |
| `~` edited | Claude's own tool calls in the session | A khub `edit`, `link` or `unlink`, and an Edit or Write on an entity file. |

- An edit made outside Claude's tools does not raise `~`. An entity changed in
  an editor or by a shell script stays out of the edited count.
- An entity added and then edited counts under `+` only.

## The setting

| Setting | Default | Effect |
|---|---|---|
| `binary` | empty | The command that runs khub. Empty resolves `<workspace>/node_modules/.bin/khub`, then `khub` on the `PATH`. |

Set it at install with `claude plugin install khub@khub --config binary=<command>`,
or later with `claude plugin configure khub@khub`.

## Install

```text
/plugin marketplace add endgame-build/khub
/plugin install khub@khub
```

From a shell the same two steps are `claude plugin marketplace add
endgame-build/khub` and `claude plugin install khub@khub`. Both use the user's
own git credentials.

The mod needs the khub CLI, 0.27.0 or later. Trouble shows on the plugin's
status line under the prompt, which Claude Code draws as a warning.

| Status text | Cause | What the mod does |
|---|---|---|
| `not installed` | No khub at the `binary` setting, in `node_modules/.bin` or on the `PATH`. | Stands aside. |
| `needs khub 0.27.0 or newer` | The khub it found is older. | Stands aside. |
| `schema: <message>` | khub could not load the workspace's schema. | Reports khub's message. |

A mod that stands aside draws no band and no rows, and the session runs as it
does without the plugin. It looks for khub again at each turn start, so the
text clears once khub is installed or updated.

## Old skill copies

khub 0.27.0 and earlier releases copied the two skills into the workspace. A
workspace set up with one of them still holds:

- `.claude/skills/khub` and `.claude/skills/setup`
- `.agents/skills/khub` and `.agents/skills/setup`
- `.opencode/skills/khub` and `.opencode/skills/setup`
- one `.gitignore` line per folder, such as `.claude/skills/khub/`

khub leaves them in place. Delete the folders and their `.gitignore` lines by
hand. While `.claude/skills/khub` remains, Claude Code lists the skill twice,
once from the workspace and once from the plugin.

## What it never does

- It never changes what the model reads. The rows and the band are drawn for
  the person at the terminal.
- It registers no tool the model can call.
- It makes no network call.
- It runs no timer. It refreshes at session start, at each turn start, after a
  khub write and after an Edit or Write of an entity file or a schema file.

## Gates

The sources are under `plugin/`, and `.claude/rules/plugin.md` has the rules.
From the repo root:

```bash
claude plugin validate --strict .          # the marketplace manifest
claude plugin validate --strict plugin     # the plugin manifest and what the hooks call
claude plugin test plugin                  # the plugin's tests
bash plugin/tools/record-fixtures.sh       # re-record the khub output the tests read
```

`record-fixtures.sh` builds khub from the checkout and rewrites
`plugin/tests/fixtures.ts`. The diff must be empty, so a JSON shape the mod
reads cannot move unnoticed.

Two local tools sit beside the gates.

- `tsc -p plugin` type-checks the mod. It needs `plugin/.claude-plugin/types/`,
  which Claude Code lays when it loads the plugin with
  `claude --plugin-dir plugin`.
- `bash plugin/tools/try.sh` builds khub, seeds a scratch workspace and prints
  the command that opens a session there with the plugin loaded.
