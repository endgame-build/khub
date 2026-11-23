# Docs — every behaviour has one canonical home

**User-facing behaviour is specified in exactly one place. Rationale links to
that place; it never restates it.**

A restated behaviour is a second copy nobody updates. When the behaviour moves,
the canonical page moves with it (a fixture or a gate forces that), and the
copy in the rationale quietly turns false. The design memo's storage section
listed formats the resolver refuses and called nested storage unsupported a
release after it shipped; its command table lacked every command added since
it was written. Neither was caught, because nothing checks prose against prose.

## Where each thing is canonical

| Behaviour | Canonical home | Behaviour pinned by |
|---|---|---|
| Commands, arguments, `--<field>` options, JSON output shapes, error codes, exit codes, `check` buckets and which fail the gate | [`docs/cli.md`](../../docs/cli.md) | `parity/cases/**/expected/` |
| What khub does with the files: validation and `--strict`, entity formats, collections, the body-template keys and the findings bodies raise | [`docs/cli.md`](../../docs/cli.md) ("Validation and open schema" through "Body templates"); the full row model in [`docs/collections-design.md`](../../docs/collections-design.md) | parity |
| Every command's flags and help text | [`docs/cli-reference.md`](../../docs/cli-reference.md) — generated, never hand-edited | `parity/tools/gen_cli_reference.sh`, drift-checked in CI |
| Authoring the schema: the three layers, the base block, storage layouts and paths, path templates, `.khub/ignore`, ids, writing a template | [`docs/schema.md`](../../docs/schema.md) — it links to `docs/cli.md` for the contract above rather than restating it | `internal/schema` tests, parity |
| A preset's types and relations | the preset's YAML under `presets/`; `docs/<preset>-preset.md` is its reading copy | `khub init` + parity |
| The Claude Code plugin, as a user sees it | [`docs/plugin.md`](../../docs/plugin.md) | `claude plugin test plugin` |
| The plugin's engine rules and the CLI fields it reads | [`.claude/rules/plugin.md`](plugin.md) | `plugin/tests/fixtures.ts` |
| What shipped when | [`CHANGELOG.md`](../../CHANGELOG.md) | the release tags |

The executable spec outranks every page: where `docs/cli.md` and a fixture
disagree, the fixture is the behaviour and the page is the bug.

## What the other docs may say

- **Rationale** — [`docs/design-memo.md`](../../docs/design-memo.md),
  [`docs/concepts.md`](../../docs/concepts.md), the "why" in a rules file —
  says *why* and *what is invariant*. It may name a command, a flag or a field
  to anchor an argument. It does not list a command's flags, a payload's
  fields, a bucket list, an error code's trigger, a default, or a file layout.
  Where the reader needs one, link to the canonical heading.
- **The tutorial** ([`docs/getting-started.md`](../../docs/getting-started.md))
  shows commands and their output as a walk-through, verified against a live
  run. It teaches; it does not specify. A behaviour it demonstrates still has
  its home above.
- **The README** summarizes and links.
- **Agent surfaces** are not docs: an agent reads them without opening the
  canonical page, so they carry what they need. There are two kinds.
  - Shipped to users: the skills (`plugin/skills/*/SKILL.md`) and the block
    `khub wire` writes into a workspace's `CLAUDE.md`/`AGENTS.md`
    (`internal/wire/wire.go`). They carry the commands they tell an agent to
    run.
  - Read by contributors' agents: this repo's `CLAUDE.md`, `AGENTS.md` and
    `.claude/rules/*.md`. They carry the invariant a change must keep (the
    exit-code split in `cli.md`, the layout × format axes in `CLAUDE.md`) and
    point at the canonical page for the detail.

  A change that moves a behaviour an agent surface names updates that surface
  in the same commit. Changing the wired block moves parity fixtures; re-record
  it as a behaviour change.
- **The changelog** records what changed in a release, in words. It is history,
  not a spec, and is never edited to track later behaviour.

## When you change behaviour

1. Update the canonical home in the same commit as the code and the fixtures.
2. Grep for the old behaviour everywhere it may be named —
   `grep -rn '<flag or code>' docs/ README.md CLAUDE.md AGENTS.md .claude/rules plugin/skills internal/wire`.
   In a rationale doc or the README, a restatement is a violation: replace it
   with a link, don't update the copy. In an agent surface, update it.
3. Planned work is design, not behaviour: the memo may describe a verb that
   has not shipped, and `docs/cli.md` names it as planned.
