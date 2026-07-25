# kb

**A drop-in typed doc corpus for one build project.** Six kinds of document held
as Markdown in git next to the code, a skill that teaches an agent to use them,
and one script that scaffolds, checks and walks them.

Self-contained: copy this directory into a project and it works. Stdlib Python
3.11+, no packages to install, no dependency on khub. The ontology is khub's
`build-lite` preset; the rationale — what was cut and why — is in
[`docs/build-lite-standalone.md`](../docs/build-lite-standalone.md).

```
kb/
  skills/kb/SKILL.md      what the agent reads: ontology, routing rules, the loop
  scripts/
    kb.py                 the tool — scaffold + check + walk, stdlib only
    build.schema.yaml     the whole contract: khub's core base + the six types
    templates/*.md        body templates; their ## headings are the body contract
  install.sh              wires the skill into .opencode/skills (or copy it yourself)
  test_kb.py              24 tests, no pytest required
```

## The commands

**The verbs and flags are khub's** — kb implements a subset, so anything learned
on one transfers to the other.

| | |
|---|---|
| `kb init` | scaffold the corpus directories and the two narrative documents |
| `kb schema [types\|show <t>\|edges]` | the ontology. Read before writing frontmatter |
| `kb status` | counts per type, orphans, errors, gaps |
| `kb add <type> --title "..." --<field> <v>` | mint one entity file, print its id |
| `kb get <id> [--edges]` | one entity, plus what points at it |
| `kb edit <id> <field> <value>` | change one attribute, bump `updated` |
| `kb remove <id> [--force]` | delete; refuses while an edge still points at it |
| `kb link` / `kb unlink <id> <pred> <target>` | a schema-checked relation |
| `kb query [--type t] [--<field> v] [--has p] [--missing p]` | filter; `--missing` is the gap query |
| `kb neighbors <id> [--depth n] [--in\|--out]` | adjacency, **including derived inverses** |
| `kb impact <id> [--predicate p] [--reverse]` | blast radius over one predicate |
| `kb history <id>` | the supersession chain |
| `kb validate [target]` | per-entity well-formedness and referential integrity |
| `kb check [--strict]` | graph-wide. Errors break, gaps do not |
| `kb install-skills [--target t] [--global] [--bin DIR]` | wire the skill into an agent |

Bare `kb` prints the list. `-C/--workspace` targets another workspace,
`--format json` is on every read command.

Deliberately absent, and khub-only: `search` (ripgrep wins at this scale),
`stale`, `reindex`, `viz`, `backfill`, `wire`. Reading, grepping and writing
prose are things the agent's own tools do better.

## Ids

`kb add` mints `<prefix>-NNN-<slug>`, or `NNN-<slug>` for a type with no declared
prefix. The number is one past the highest in use and each prefix counts
separately, so `fr-001` and `cst-001` coexist. The prefix follows `kind`, which
puts a mislabelled entity in plain sight and makes `check` able to catch it. This
is khub's rule, declared by `id_prefix` in the schema — both tools mint the same
id for the same input, and a test pins that.

## Drop it in

```bash
cp -r kb /path/to/your/project/
cd /path/to/your/project
kb/install.sh --bin ~/.local/bin
kb init && kb check
```

If your opencode config can point at the skill directly, skip the install
entirely — this is the zero-copy path:

```json
{ "skills": { "paths": ["kb/skills"] } }
```

Then allow the command. The catch-all goes **first**, because opencode's last
matching rule wins, and it matches each sub-command of a shell line separately —
so `kb check | head` would also need `head *`:

```json
{ "permission": { "bash": { "*": "ask", "kb *": "allow", "python3 *kb.py *": "allow" } } }
```

## The corpus

```
knowledge/prd.md          the product          (one document, required)
knowledge/arc42.md        the architecture     (one document)
knowledge/requirements/   fr- / cst- / br-NNN  what must hold
knowledge/decisions/      ad-NNN               why it is so
knowledge/components/     cmp-NNN              what exists, what depends on what
specs/                    fs-NNN               what is being built
```

`add` writes the frontmatter and seeds the body from `templates/<type>.md`. You
then write the prose in the file directly — that is the intended path, not a
`--body` flag. `check` is the gate: **errors** mean the corpus is broken, **gaps**
mean legal but unfinished, and only errors fail the exit code (`--strict` fails on
both).

## The contract

`scripts/build.schema.yaml` is khub's `core.yaml` base block and its build-lite
preset combined into one file, in khub's own vocabulary. It is the only thing to
edit when a project needs a field — `kb.py` names no type, field or predicate.
A project can override the shipped copy at `.kb/build.schema.yaml` (and
`.kb/templates/<type>.md`) without touching this directory.

It is **1:1 with `presets/core.yaml` + `presets/build-lite/schema.yaml`** — a
verbatim concatenation of the two, so it is identical by construction. No
additions, no omissions, nothing reordered. Attributes `kb` never reads (`draft`,
`author`, `sources`, `references`) stay declared: a closed schema and a deleted
attribute do not mix — drop one and every entity khub writes becomes an
`unknown_field` error.

`tests/test_build_lite_standalone.py` in the khub repo enforces that. It diffs
this file against both preset sources key by key and fails on **any** difference,
generates the body templates from khub's own renderer, and checks that kb and khub
mint the same id for the same input.

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
turn. Save as `.opencode/plugins/kb-check.ts`:

```ts
import type { Plugin } from "@opencode-ai/plugin"

const WRITERS = ["write", "edit", "patch"]
const CORPUS = /(knowledge|specs)\//

export const BuildLiteCheck: Plugin = async ({ $, worktree }) => ({
  "tool.execute.after": async (input, output) => {
    if (!WRITERS.includes(input.tool)) return
    if (!CORPUS.test(JSON.stringify(input ?? {}) + JSON.stringify(output?.metadata ?? {}))) return
    const raw = await $`python3 ${worktree}/kb/scripts/kb.py check --json`
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

!`python3 kb/scripts/kb.py check`

Fix every error. Then fix the gaps caused by recent work; report what you left.
```

## Test

```bash
python3 kb/test_kb.py     # no pytest required
uv run pytest build-lite          # under the repo's runner; also runs the
                                  # differential check against ruamel.yaml
```
