# The Claude Code plugin — the skills, and a small mod that is a shell over a pure core

`plugin/` is khub's plugin for Claude Code. It carries the two agent skills
(`skills/khub`, `skills/setup`) and the mod. The mod is TypeScript function hooks
that draw two things in a session. A band above the prompt shows the workspace's
counts and what the session changed. A compact row stands for each khub call the
agent makes through Bash. The agent keeps using the CLI, and the mod never
changes what the model reads.

## How it ships

- The repo is a Claude Code plugin marketplace. `.claude-plugin/marketplace.json`
  at the root lists one plugin, `khub`, with `source: ./plugin`. Claude Code
  users run `/plugin marketplace add endgame-build/khub`, then
  `/plugin install khub@khub`.
- Other agents get the skills with `npx skills add endgame-build/khub`, which
  finds them through the marketplace manifest. They get no mod.
- The binary carries no skills and installs none.
- The plugin updates apart from the binary. `MIN_KHUB` in `hooks/workspace.ts` is
  the oldest khub whose output the mod reads. An older binary gets one status
  line and every hook passes through. Raise it in the commit that starts reading
  a newer field.
- The plugin's version lives in `plugin/.claude-plugin/plugin.json` only, never
  in the marketplace entry.
- The one setting is `binary`, the command that runs khub. Empty resolves
  `node_modules/.bin/khub` under the workspace root, then `khub` on the `PATH`.

The mod API is early access and moves between Claude Code releases. Everything
below under "Engine rules" was observed on Claude Code 2.1.288 and 2.1.291.

## What the mod may not do

- **No model-callable tools.** `$.tool.register` lists a tool as `mcp__khub__*`.
  That is the second contract `cli.md` rejects under "No MCP server".
- **No change to what the model reads.** A `tool.call` hook returns what `next`
  answered, with the command as the agent wrote it. The mod adds no note and
  rewrites no result.
- **Zero per-type code.** The file-to-entity mapping reads layouts and paths from
  `khub schema --format json`. A type name, a field name or a predicate in the
  mod's logic is the same bug it is in `internal/cli`.
- **Workspace text is drawn, never trusted.** Titles, reasons, the preset's name
  and version, and the text of the agent's own command reach the terminal with
  control characters stripped (`clean` in `hooks/cli.ts`).
- **No network calls.** khub sends nothing outward, and neither does the mod.
- **Refresh on events, never on a timer.** Every khub call rescans the
  workspace. A session start, a turn start, a khub call that writes, and an Edit
  or Write of an entity file or a schema file are the triggers. Only the session
  start waits for its refresh, so no tool result is held. A refresh asked for
  while one waits to start joins it.
- **Logic stays in the binary.** The mod calls khub and renders what comes back.
  A rule that needs judgment about the graph belongs in `internal/`. One list
  mirrors the binary. `INFORMATIONAL` in `hooks/cli.ts` names the `check`
  buckets that never fail the gate, because the JSON does not say which buckets
  gate. It follows `CheckReport.Passed` in `internal/integrity/check.go`, and
  the count it feeds shows only while `passed` is false.

## What the band counts

- A refresh runs `khub check --format json` and `khub query --format json`. The
  total and the draft count come from the query rows, and the session's ids are
  their qualified `id` fields.
- The first refresh of a session fixes the baseline. `+n` and `−n` are the ids
  that joined and left it, so they include changes made outside Claude.
- `~n` counts baseline entities the session edited through its tools: a khub
  `edit`, `link` or `unlink` that changed something, and an Edit or Write on a
  file that holds one entity. A change made outside those tools does not raise it.
- A cut document, or a call that did not answer, keeps what the band shows.

## Engine rules that shape the code

- **`$` never crosses an import.** The engine follows the engine handle only
  into functions declared in the hooks module itself, and refuses a module that
  passes `$` to an imported function. So `hooks/register.tsx` is the shell and
  holds every call on `$`. Every other file under `hooks/` is pure: data in,
  data or a tree out. A view takes the element table (`El`) and plain data.
- **State atoms are top-level consts of the hooks module.** The engine reads
  which state a module touches from `atom(...)` consts in that file. A family
  member is `memberOf(...)` written in a const or at the call. A member chosen
  by a conditional fails validation.
- **The state contract is self-contained.** `types/index.d.ts` may not import.
  It exports its value types and declares `PluginState` inside
  `declare module 'claude-code'`, where `StateFamily` is in scope.
- **One literal matcher per registration.** `on('tool.call', { tool: 'Edit' }, …)`
  and the same for `Write` are two registrations.
- **A Bash result has no exit code.** A failed gate or a refusal arrives as
  `isError: true` with khub's output in `text`. The shell reads the outcome
  from the JSON document, where an `error.code` is a refusal.
- **A hook that throws is skipped whole.** The shell's khub runner never rejects,
  so a call that could not run answers exit -1 with the reason. Each `tool.call`
  hook has a `.catch` that passes the call on, so a failure in the mod never
  blocks a tool. In a `.catch`, `next(e)` replays what the call settled to and
  runs nothing twice. The Bash hook's handler also clears the call's row, so a
  row never stays running.
- **A verbose session draws each call as a `ToolUse` row with the result
  inline, and raises no `ToolResult` event.** The compact row is one tree drawn
  by the `ToolUse` hook, holding the head line, the result line and the list
  rows. Its `json` button hands the row back to the engine.
- **With verbose off, Bash calls fold into a `ToolGroup` count line** such as
  `Ran 3 shell commands`, khub writes included, and no row event fires. The mod
  leaves that alone, so compact rows show in verbose sessions only.
- **A command typed with `!` raises only `session.append` rows.** No `tool.call`
  and no row event fires, so the mod draws nothing for it. It starts a turn, and
  the turn start refreshes the band.
- **A row's duration runs from the call to its result.** A wait at the approval
  dialog is inside it.
- **Output the engine moved to disk may be cut in the result.** A document that
  does not parse while `persistedOutputPath` is set leaves the row to the engine.
- **The engine draws a plugin's status line as a warning.** `$.ui.status(text)`
  takes text alone, and the engine shows it as `⚠ khub: <text>` in yellow. So the
  status line carries trouble only, its text never starts with the plugin's
  name, and the everyday summary is the band. While khub cannot be asked, the
  mod looks for it again at each turn start, so the text clears once its cause
  is gone.
- **Hooks load from a marketplace install, `--plugin-dir` and the user-level
  skills folder.** A plugin under a project's `.claude/skills/<name>/` loads its
  `SKILL.md` and nothing else.
- **Generated type files exist only under `--plugin-dir`.** A marketplace
  install lays no `.claude-plugin/types/`, so `tsc` runs against a checkout
  loaded with `--plugin-dir`.
- **A `--plugin-dir` plugin reads its settings from `pluginConfigs`**, keyed by
  the plugin's name or `<name>@inline`, as `{"options": {"binary": "<command>"}}`.
- **`--strict` validation needs an `author`** in `plugin.json`.

## Gates

From the repo root, `claude plugin validate --strict .` checks the marketplace
manifest. From `plugin/`:

```bash
claude plugin validate --strict .   # manifest, contract, and what the module hooks and calls
claude plugin test .                # every tests/*.test.ts(x)
tsc -p .                            # after Claude Code has laid .claude-plugin/types/
bash tools/record-fixtures.sh       # rebuild tests/fixtures.ts from the built binary
```

`validate` and `test` need no credentials and no network. `tools/try.sh` builds
khub and a scratch workspace, then prints the command that opens a session there
with the plugin loaded.

## Tests read real khub output

`tests/fixtures.ts` is recorded from the binary built from this checkout, with
the clock pinned by `KHUB_PARITY_NOW`. It is the tripwire for the CLI contract.
When a JSON shape the mod reads moves, re-recording changes the fixture and the
tests that parse it fail. Never edit the file by hand.

`tests/helpers.ts` holds `fakeHost`, which stands in for the host in a shell
test. It gives a workspace at a fixed root and khub answering from the fixtures.

## The CLI contract the mod reads

| Command | Fields |
|---|---|
| any refusal | `error.code`, `error.message` on stdout |
| `--version` | the version number in the printed text |
| `schema` | `provenance.{preset, version}`, `types[].{name, layout, format, path}` |
| `check` | `passed`, `strict`, every array-valued bucket, and `id`, `path` or `alias` of a finding |
| `query`, `stale` | per row `id`, `title`, `draft`, `orphan`, `stale` |
| `add`, `edit`, `remove` | `id`, `draft` |
| `link`, `unlink` | `id`, `slug`, `predicate`, `target`, `changed` |
| `get` | `id`, `title` or `name` at the top or in `frontmatter`, `edges` |
| `search` | per row `id`, `title`, the flags, and the stderr `note:` line |
| `neighbors` | per row `id`, `predicate`, `direction` |
| `impact` | per row `depth` |
| `history`, `schema types`, `schema edges` | the number of rows |
| `status` | `total`, `draft`, `orphan`, `stale` |
| `validate` | `count`, `errors[].{id, field, reason}`, `gaps` |
| `schema show`, `schema diff`, `schema snapshot` | `name`, `fields`, `relations`, `pending`, `changes`, `types` |

`khub query --format ids` prints bare slugs, which two types may share. The mod
reads `query --format json` for qualified ids.
