# The Claude Code plugin — the skills, and a mod that is a shell over a pure core

`plugin/` is khub's plugin for Claude Code. It carries the two agent skills
(`skills/khub`, `skills/setup`) and the mod: TypeScript function hooks that draw
in the session and wrap the agent's tool calls. The agent keeps using the CLI
through Bash. The mod is the human's view of the graph and the harness side of
the integrity loop the `khub` skill asks for.

## How it ships

- The repo is a Claude Code plugin marketplace: `.claude-plugin/marketplace.json`
  at the root lists one plugin, `khub`, with `source: ./plugin`. Claude Code
  users run `/plugin marketplace add endgame-build/khub`, then
  `/plugin install khub@khub`.
- Other agents get the skills with `npx skills add endgame-build/khub`, which
  finds them through the marketplace manifest. They get no mod.
- The binary carries no skills and installs none. A skill-copying command in Go
  would be a second channel to keep in step with this one.
- The plugin updates apart from the binary. `MIN_KHUB` in `hooks/workspace.ts` is
  the oldest khub whose output the mod reads. An older binary gets one status
  line and every hook passes through. Raise it in the commit that starts reading
  a newer field.
- The plugin's version lives in `plugin/.claude-plugin/plugin.json` only, never
  in the marketplace entry.

The mod API is early access and moves between Claude Code releases. Everything
below under "Engine rules" was observed on Claude Code 2.1.288.

## What the mod may not do

These follow from khub's own rules, and each has a reason.

- **No model-callable tools.** `$.tool.register` lists a tool as `mcp__khub__*`.
  That is the second contract `cli.md` rejects under "No MCP server".
- **Zero per-type code.** Tabs, forms, guards and summaries read
  `khub schema --format json`. A type name, a field name or a predicate in the
  mod's logic is the same bug it is in `internal/cli`.
- **Ids in notes, never bodies.** A note the mod adds for the model carries ids,
  bucket names and khub's own finding lines. Entity bodies are untrusted
  content, so an edited body is named `body` and never quoted. A `/khub`
  passthrough prints what khub printed, as the same call through Bash would.
- **No network calls.** khub sends nothing outward, and neither does the mod.
- **Refresh on events, never on a timer.** Every khub call rescans the
  workspace. A write, a turn start and a schema edit are the triggers.
- **Logic stays in the binary.** The mod calls khub and renders what comes back.
  A rule that needs judgment about the graph belongs in `internal/`.

## Engine rules that shape the code

- **`$` never crosses an import.** The engine follows the engine handle only
  into functions declared in the hooks module itself, and refuses a module that
  passes `$` to an imported function. So `hooks/register.tsx` is the shell and
  holds every call on `$`. Every other file under `hooks/` is pure: data in,
  data or a tree out. A view takes the element table (`El`), plain data and
  action closures the shell built.
- **State atoms are top-level consts of the hooks module.** The engine reads
  which state a module touches from `atom(...)` consts in that file. An atom
  imported from another file, or held in an object, fails validation.
- **The state contract is self-contained.** `types/index.d.ts` may not import.
  It exports its value types and declares `PluginState` inside
  `declare module 'claude-code'`, where `StateFamily` is in scope.
- **One literal matcher per registration.** `on('tool.call', { tool: 'Edit' }, …)`
  and the same for `Write` are two registrations that share one function.
- **A Bash result has no exit code.** A failed gate or a refusal arrives as
  `isError: true` with khub's output in `text`. The shell reads the outcome
  from the JSON document: an `error.code` is a refusal.
- **Tool rows have no expanded flag.** `ToolUse` and `ToolResult` cannot tell
  whether the full transcript view is open. Each compact row carries a `json`
  button that flips that row to the engine's own drawing.
- **A skill owns its slash command, and a hook can still answer it.** The
  plugin's own `khub` skill takes `/khub`, so registering the name throws, and
  the command arrives as `khub:khub`. A skill copied into a project takes it as
  `khub`. A `command.run` hook on either name runs first, so the mod registers
  both and shares the command: the bare name opens the pane,
  `/khub <khub command>` runs khub, and anything else passes through to the
  skill.
- **A hook that throws is skipped whole.** Work started before the throw stays
  done and the rest never runs, so the shell's khub runner never rejects: a
  call that could not run answers exit -1 with the reason.
- **Hooks load from a marketplace install, `--plugin-dir` and the user-level
  skills folder.** A plugin under a project's `.claude/skills/<name>/` loads its
  `SKILL.md` and nothing else. A project's `.claude/settings.json` that names
  the marketplace does not load the plugin in a headless session, since the
  marketplace registers only after the trust dialog.
- **Generated type files exist only under `--plugin-dir`.** A marketplace
  install lays no `.claude-plugin/types/`, so `tsc` runs against a checkout
  loaded with `--plugin-dir`.
- **`--strict` validation needs an `author`** in `plugin.json`.

## Rules for calling khub from the shell

- **A user-typed value is one argument that khub cannot read as a flag.** A form
  value that starts with a dash goes out as `--name=value`, and a search text
  follows `--`.
- **Any nonzero exit is a failure.** A usage error prints no JSON document, so a
  write that only looked for a refusal envelope would report success.
- **The agent's author stamp follows the type.** khub reads the word after `add`
  as the type, and the end of the command may be a comment or a heredoc.
- **An edit goes before its unlinks**, so a refused edit removes no edge.
- **Only the latest read lands.** Browse and Search count their reads and drop an
  answer that arrives after a newer one was asked.
- **The stats tally in `$.store` is added to, never replaced.** Each turn adds
  what it did to what the store holds at that moment, since two sessions share it.

## Gates

From the repo root, `claude plugin validate --strict .` checks the marketplace
manifest. From `plugin/`:

```bash
claude plugin validate --strict .   # manifest, contract, and what the module hooks and calls
claude plugin test .                # every tests/*.test.ts(x)
tsc -p .                            # after Claude Code has laid .claude-plugin/types/
bash tools/record-fixtures.sh       # rebuild tests/fixtures.ts from the built binary
```

`validate` and `test` need no credentials and no network.

## Tests read real khub output

`tests/fixtures.ts` is recorded from the binary built from this checkout, with
the clock pinned by `KHUB_PARITY_NOW`. It is the tripwire for the CLI contract:
when a JSON shape the mod reads moves, re-recording changes the fixture and the
tests that parse it fail. Never edit the file by hand.

`tests/helpers.ts` holds `fakeHost`, which stands in for the host in a shell
test: a workspace at a fixed root and khub answering from the fixtures.

## The CLI contract the mod reads

| Command | Fields |
|---|---|
| any refusal | `error.code`, `error.message` on stdout |
| `schema` | `provenance.{preset, version}`, `types[].{name, layout, format, path}` |
| `schema show` | `name`, `fields[].{name, type, required, enum}`, `relations[].{predicate, to, many, required}` |
| `status` | `counts`, `total`, `draft`, `orphan`, `stale` |
| `check` | `passed`, `strict`, every array-valued bucket |
| `validate <type>/<slug>` | `count`, `errors[].{id, field, reason}`, `gaps[].reason`, `lenses[].{code, name}` |
| `add`, `edit` | `id`, `type`, `slug`, `draft` |
| `link`, `unlink` | `slug`, `predicate`, `target`, `changed` |
| `remove` | `id`, `removed` |
| `get` | `id`, `frontmatter`, `edges[].{predicate, target, derived}` |
| `query`, `stale` | per row `id`, `title`, `draft`, `orphan`, `stale` |
| `search` | per row `id`, and the stderr `note:` line |
| `neighbors` | per row `id`, `predicate`, `direction` |
| `impact` | per row `id`, `depth` |
| `reindex --dry-run` | a unified diff on stdout |
| `schema diff` | `pending`, `changes[].{op, path, from, to}` |
| `upgrade --dry-run` | `preset`, `version_from`, `version_to`, `schema_drift` |

`khub edit` replaces a many-valued relation with the list it is given and refuses
an empty one (`empty_relation_value`). An edit form that empties a relation
sends one `unlink` per target instead.
