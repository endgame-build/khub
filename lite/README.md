# build-lite (standalone)

**khub's build-lite preset, reimplemented as one skill and one script.** Six document
types for a small build project, held as Markdown in git next to the code. No
dependency on khub, no dependency on anything: `bl.py` is stdlib Python 3.11+.

The rationale — what was cut, why, and what the alternatives were — is in
[`docs/build-lite-standalone.md`](../docs/build-lite-standalone.md). This page is
how to run it.

## Install

The whole product is `skill/`. Copy it in:

```bash
mkdir -p .opencode/skills
cp -r lite/skill .opencode/skills/build-lite
ln -s "$PWD/.opencode/skills/build-lite/bl.py" ~/.local/bin/bl   # optional, but nicer
bl init && bl check
```

`.claude/skills/` and `.agents/skills/` work identically — opencode reads all
three, and the skill is plain SKILL.md with no host-specific frontmatter.

Then allow the command in `opencode.json` (see `opencode/opencode.json.example`;
the catch-all must come **first**, because last match wins):

```json
{ "permission": { "bash": { "*": "ask", "bl *": "allow", "python3 *bl.py *": "allow" } } }
```

Two optional extras in `opencode/`: `plugin-check.ts` runs the sweep after every
edit to the corpus and appends the errors to that tool's output (opencode has no
PostToolUse hook; `tool.execute.after` is the analogue), and `command-corpus.md`
is a `/corpus` slash command.

## Use

```bash
bl schema                 # the whole vocabulary: types, fields, enums, edges
bl new adr "Use Postgres for the primary store" --set status=proposed
bl link ad-004-use-postgres affects cmp-001-api
bl links cmp-001-api --depth 3        # edges in and out, including derived inverses
bl ls requirement --missing realized_in
bl check [--strict] [--json]
```

`new` mints the id (`ad-004-…`, prefix from the type and its `kind`), writes the
frontmatter, and seeds the body from `templates/<type>.md`. You then write the
prose in the file directly — that is the intended path, not a `--body` flag.
`check` is the gate: **errors** mean the corpus is broken, **gaps** mean legal
but unfinished, and only errors fail the exit code (`--strict` fails on both).

```
knowledge/prd.md          the product          (one document, required)
knowledge/arc42.md        the architecture     (one document)
knowledge/requirements/   fr- / cst- / br-NNN  what must hold
knowledge/decisions/      ad-NNN               why it is so
knowledge/components/     cmp-NNN              what exists, what depends on what
specs/                    fs-NNN               what is being built
```

## The contract

`schema.json` is the whole ontology and the only thing to edit when a project
needs a field. A workspace can override the shipped copy by putting its own at
`.build-lite/schema.json` (and `.build-lite/templates/<type>.md`).

Frontmatter is a deliberately small YAML subset — flat `key: value`, `[a, b]`, or
`- item` lines; no nesting, no multi-line scalars, no anchors, no trailing
comments. That is why there is no YAML dependency, and anything outside the
profile is reported as `malformed` rather than silently misread.

Every file this writes is a valid khub `build-lite` entity, so a corpus that
outgrows six types graduates by running `khub init build-lite` over it — nothing
is rewritten. It writes no `draft` key (absent reads as khub's default, `false`).

## Test

```bash
python3 lite/test_bl.py     # no pytest required
uv run pytest lite          # or under the repo's runner
```
