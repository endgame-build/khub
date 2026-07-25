# build-lite

**A drop-in typed doc corpus for one build project.** Six kinds of document held
as Markdown in git next to the code, a skill that teaches an agent to use them,
and one script that scaffolds, checks and walks them.

Self-contained: copy this directory into a project and it works. Stdlib Python
3.11+, no packages to install, no dependency on khub. The rationale — what was
cut and why — is in [`docs/build-lite-standalone.md`](../docs/build-lite-standalone.md).

```
build-lite/
  skills/build-lite/SKILL.md   what the agent reads
  scripts/
    bl.py                      scaffold + check + walk
    build.schema.yaml          the whole contract: khub's core base + the six types
    templates/*.md             body templates; their ## headings are the body contract
  install.sh                   wires the skill into .opencode/skills (or copy it yourself)
  test_bl.py                   17 tests, no pytest required
```

## Drop it in

```bash
cp -r build-lite /path/to/your/project/
cd /path/to/your/project
build-lite/install.sh --bin ~/.local/bin
bl init && bl check
```

If your opencode config can point at the skill directly, skip the install
entirely — this is the zero-copy path:

```json
{ "skills": { "paths": ["build-lite/skills"] } }
```

Then allow the command. The catch-all goes **first**, because opencode's last
matching rule wins, and it matches each sub-command of a shell line separately —
so `bl check | head` would also need `head *`:

```json
{ "permission": { "bash": { "*": "ask", "bl *": "allow", "python3 *bl.py *": "allow" } } }
```

## Use

```bash
bl                        # the verb list
bl schema                 # types, fields, enums, edges — the whole vocabulary
bl new adr "Use Postgres for the primary store" --set status=proposed
bl link ad-004-use-postgres affects cmp-001-api
bl links cmp-001-api --depth 3        # edges in and out, including derived inverses
bl ls requirement --missing realized_in
bl check [--strict] [--json]
```

`new` mints the id (`ad-004-…`, prefix from the type and its `kind`), writes the
frontmatter, and seeds the body from `templates/<type>.md`. You then write the
prose in the file directly — that is the intended path, not a `--body` flag.
`check` is the gate: **errors** mean the corpus is broken, **gaps** mean legal but
unfinished, and only errors fail the exit code (`--strict` fails on both).

```
knowledge/prd.md          the product          (one document, required)
knowledge/arc42.md        the architecture     (one document)
knowledge/requirements/   fr- / cst- / br-NNN  what must hold
knowledge/decisions/      ad-NNN               why it is so
knowledge/components/     cmp-NNN              what exists, what depends on what
specs/                    fs-NNN               what is being built
```

## The contract

`scripts/build.schema.yaml` is khub's `core.yaml` base block and its build-lite
preset combined into one file, in khub's own vocabulary. It is the only thing to
edit when a project needs a field — `bl.py` names no type, field or predicate.
A project can override the shipped copy at `.build-lite/build.schema.yaml` (and
`.build-lite/templates/<type>.md`) without touching this directory.

Two deliberate deltas from what `khub init build-lite` generates, both commented
in the file: `id_prefix` is added per type (khub mints slugs from titles; `bl`
mints enumerated ids and checks the prefix against the entity's `kind`), and the
base `draft` flag is dropped (nothing in build-lite gates on it, and absent reads
as khub's default `false`).

Frontmatter is a deliberately small YAML subset — flat `key: value`, `[a, b]`, or
`- item` lines; no nesting, no multi-line scalars, no trailing comments. That is
why there is no YAML dependency, and anything outside the profile is reported as
`malformed` rather than silently misread. The schema file itself is nested and is
read by a separate subset parser, checked in the tests against `ruamel.yaml`.

Every file this writes is a valid khub `build-lite` entity, so a corpus that
outgrows six types graduates with `khub init build-lite . --force` — nothing is
rewritten.

## Optional: make the gate ambient

opencode has no PostToolUse hook, but a plugin's `tool.execute.after` can append
to a tool's own output, so the agent sees breakage it just caused in the same
turn. Save as `.opencode/plugins/build-lite-check.ts`:

```ts
import type { Plugin } from "@opencode-ai/plugin"

const WRITERS = ["write", "edit", "patch"]
const CORPUS = /(knowledge|specs)\//

export const BuildLiteCheck: Plugin = async ({ $, worktree }) => ({
  "tool.execute.after": async (input, output) => {
    if (!WRITERS.includes(input.tool)) return
    if (!CORPUS.test(JSON.stringify(input ?? {}) + JSON.stringify(output?.metadata ?? {}))) return
    const raw = await $`python3 ${worktree}/build-lite/scripts/bl.py check --json`
      .cwd(worktree).nothrow().quiet().text()
    let errors: { code: string; where: string; message: string }[] = []
    try { errors = JSON.parse(raw).errors ?? [] } catch { return }
    if (!errors.length) return
    output.output += `\n\nbuild-lite check — ${errors.length} error(s), fix before finishing:\n` +
      errors.map((e) => `  ${e.code}  ${e.where}  ${e.message}`).join("\n")
  },
})
```

Gaps are deliberately not surfaced there — they are normal mid-change state, and
a gate that cries on every edit gets ignored. A `/corpus` command is the lighter
alternative, as `.opencode/commands/corpus.md`:

```md
---
description: Show the build-lite corpus state and fix what is broken
---
The doc corpus right now:

!`python3 build-lite/scripts/bl.py check`

Fix every error. Then fix the gaps caused by recent work; report what you left.
```

## Test

```bash
python3 build-lite/test_bl.py     # no pytest required
uv run pytest build-lite          # under the repo's runner; also runs the
                                  # differential check against ruamel.yaml
```
