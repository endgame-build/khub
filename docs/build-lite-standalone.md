# build-lite, standalone

**A separate, tiny implementation of the build-lite preset for one build project
driven by opencode: one skill directory containing one stdlib Python script.
~700 lines of code where khub is 7,000, no dependencies where khub has six, and
byte-compatible output so a corpus that outgrows it graduates by running
`khub init build-lite` over the same files.**

The working implementation is in [`lite/`](../lite/README.md). This page is why
it has the shape it has, and what was considered instead.

## The question this answers

khub is a general engine: any schema, any domain, five layers, every surface a
thin adapter. Its build-lite preset already cuts the *ontology* to six types.
This asks the next question — what does the *engine* need to be when there is
exactly one schema, exactly one project, and exactly one caller, an agent in
opencode?

Not "khub with flags off." A separate implementation, free to make different
trades, held to one compatibility constraint: **the files it writes are khub
files.** That constraint is what keeps "tiny" from being a dead end.

## What earns code

The honest starting point is that an agent in a coding harness already has
excellent tools. It reads files, greps, writes Markdown, and follows a link by
opening the next file. Most of a knowledge CLI is a worse version of tools the
agent already holds.

So the test for every khub verb was: **does this beat the agent's own tools?**

Four things pass:

1. **The reverse edge is not in the file.** Edges are stored single-sided —
   `adr.affects: [cmp-001-api]` lives on the adr. Nothing in `cmp-001-api.md`
   says an adr constrains it. "What points at this?" is not a grep, it is a scan
   of the whole corpus, and an agent that greps for a slug finds prose mentions
   and misses nothing reliably.
2. **A break is global, boring, and exhaustive.** One renamed file leaves a
   dangling reference three directories away. Catching that is a machine's job,
   and it is the only thing standing between a typed corpus and a folder of
   Markdown that merely looks typed.
3. **Uniformity across sessions.** Session 40 must produce the same shape as
   session 1: the same id scheme, the same field names, the same section
   headings. A schema the tool enforces does this; a convention in a prompt
   decays.
4. **Deterministic ids.** `ad-007-…` requires knowing what 001–006 are. Cheap for
   a machine, a race for an agent that glanced at the directory.

Everything else fails the test. Full-text search loses to ripgrep on 50 files.
`get` loses to reading the file. A `--body` flag for writing prose loses to the
agent's own editor — badly, because it forces multi-paragraph Markdown through
shell quoting. `viz`, `reindex`, `backfill`, `stale`: no consumer here.

That is the whole design. The tool is a **scaffolder, a checker, and a graph
walker.** Prose is written by the agent, straight into the file.

### The consequence: files are the write API

khub's write verbs exist so that validation happens at the moment of writing.
But an agent with an Edit tool will edit the file anyway — it is right there and
it costs one call instead of a CLI round-trip. Two write paths means one of them
is unpoliced.

So build-lite standalone has one: **the file is the API, and `check` is the
contract.** `bl new` scaffolds, `bl link` maintains edges (the one edit where a
typo is invisible until something breaks), and everything else is the agent
editing Markdown. Referential integrity moves from *hard-fail on write* to
*caught by the next sweep* — the loop still closes inside the same turn, because
the skill's rule is to run `check` before finishing, and the optional opencode
plugin runs it automatically after every corpus edit.

## The delivery vehicle

| Option | What it costs | Verdict |
|---|---|---|
| **Skill only, no code** — conventions in `SKILL.md`, integrity by asking nicely | nothing | The right answer under ~20 documents. Past that, drift is invisible: the agent's "check" is neither exhaustive nor repeatable, and nothing runs in CI. |
| **Skill + one CLI script** ✅ | one file, one language runtime | **Recommended.** Works in opencode, Claude Code, a bare terminal, a pre-commit hook, and CI — the same four commands. Nothing about it is opencode-specific, which is what makes it survive the next tool change. |
| **opencode custom tools** (`.opencode/tools/*.ts`) | TypeScript + a yaml dep; opencode-only; every tool's schema sits in context permanently | A wrapper worth adding *if* bash approval friction bites. Not the base: it cannot run in CI, and typed args stop mattering once the verbs take two arguments each. |
| **MCP server** | a process, a protocol, always-on tool schemas | Over-serving. Its advantage is cross-agent reach, and a CLI already has that more cheaply. Revisit only if the corpus is driven from a hosted agent with no shell. |
| **Full khub + build-lite preset** (status quo) | 7,000 lines, 6 deps, `uv tool install`, search/viz/backfill/collections/presets unused | The graduation target, not the daily driver. Its generality is real value at a firm's scale and dead weight at one project's. |

The recommendation is the second row, and the packaging follows from how opencode
loads skills: **the deliverable is a single directory** that is simultaneously
the skill, the schema, the templates, and the tool.

```
.opencode/skills/build-lite/
  SKILL.md          the ontology, the routing rules, the loop      (92 lines)
  bl.py             scaffold + check + walk, stdlib only          (~700 lines)
  schema.json       the contract — the only file to edit for a new field
  templates/*.md    4 body templates; their `##` headings are the body contract
```

`lite/install.sh` (or a bare `cp -r`) installs it. The same directory works
verbatim in Claude Code (`.claude/skills/`) — opencode reads Claude's skill
locations too, and the frontmatter uses only `name` and `description`, which both
hosts accept.

### Why one script, not one script per verb

`add.py` / `link.py` / `validate.py` as separate files is the obvious
decomposition, and it loses on two counts.

**It breaks the mechanism it is meant to serve.** When a skill loads, opencode
lists its files for the model — `ripgrep.find({ pattern: "!**/SKILL.md", limit:
10 })`, followed by *"Note: file list is sampled."* Ten. The install directory
holds six files, so all six are always visible. Eight verb scripts plus a shared
core, `schema.json` and four templates is fourteen, and which four vanish is
whatever ripgrep happened not to reach. Verb names would become *less* reliably
discoverable, not more, and `SKILL.md` already lists all eight in six lines.

**The verbs are not independent programs.** Roughly 310 of the 700 lines are the
frontmatter profile, the schema, the corpus scan, and the edge computation, and
every verb needs most of it. Splitting therefore means a shared `_core.py` plus
seven ~30-line wrappers: eight files and slightly more code for exactly the same
logic. The alternative — each script parsing frontmatter its own way — is how two
readers start disagreeing about the same file.

What the decomposition is actually reaching for is that the verbs should be
legible. They are: `bl` with no arguments prints all eight with one-line
descriptions, and `SKILL.md` leads with the same list. If per-verb entry points
are ever wanted for a human's fingers, symlinks cost nothing and change no code.

### opencode specifics that shaped it

Verified against the opencode source at v1.18.5 (its docs site 403s from here, so
the doc sources in `packages/web/src/content/docs` and the implementing
TypeScript were read instead):

- **Skills are first-class and bundle scripts.** Loading a skill hands the model
  the skill's absolute base directory plus a file listing that is **capped at 10
  entries and explicitly described to the model as sampled**. The install
  directory holds 6 files besides `SKILL.md` — deliberate headroom, and the
  reason `README.md`, the tests, and the opencode extras live outside it.
- **No `allowed-tools`.** opencode recognizes only `name`, `description`,
  `license`, `compatibility`, `metadata`. `name` must match the directory name.
- **Permissions match per sub-command, last rule wins.** The shell tool
  tree-sitter-parses the command line and asks about each sub-command separately,
  so `bl check | head` needs `head *` allowed too. Put `"*": "ask"` **first** and
  `"bl *": "allow"` after it, and prefer not to pipe.
- **There are no PostToolUse hooks.** The analogue is a plugin's
  `tool.execute.after`, which can append to the tool's own output — that is
  [`lite/opencode/plugin-check.ts`](../lite/opencode/plugin-check.ts), ~30 lines,
  turning the gate from something the agent must remember into something ambient.
  (The `experimental.hook.file_edited` key visible in opencode's SDK types is a
  stale artifact — no runtime code reads it. Formatters can run a command on
  write, but nothing documents what happens to their output, so they are not a
  reporting channel.)
- **Nested `AGENTS.md` files load lazily**, pulled in when the agent touches a
  file in that directory. A short `knowledge/AGENTS.md` is therefore free until
  the corpus is actually opened — a better place for standing rules than the
  root file.
- Undocumented but implemented: `skills.paths` and `skills.urls` in
  `opencode.json` distribute skills from a shared directory or a remote registry.
  Real, and unstable; worth knowing when this needs to reach more than one repo.

## What was cut

| khub surface | In build-lite standalone |
|---|---|
| `query`, `get`, `neighbors`, `impact`, `history` | folded into **`ls`** and **`links`**. One walk verb with `--depth`, `--predicate` and `--direction` covers adjacency, blast radius, and the supersession chain — they were never three algorithms. |
| `search` (FTS5, BM25) | **cut.** ripgrep is better at this scale and the agent already has it. |
| `add --body/--body-file`, `edit` | **cut.** The agent writes the file. `new` scaffolds, `link`/`unlink` keep edges honest. |
| `validate` + `check` as two gates | **one `check`** with two severities: `error` (broken) and `gap` (unfinished). The invariant the split protects — *capture is never blocked* — survives as the exit code, which only errors set. |
| `draft` flag, active-subgraph logic | **cut.** `adr.status` and `feature-spec.status` already say what draft would. Absent reads as khub's default `false`, so files stay compatible. |
| `stale`, `backfill`, `log`, git integration | **cut.** git is the freshness record; `git log -- <path>` answers it without a projection. |
| `reindex`, `viz`, OKF export | **cut.** No index to keep current, no dashboard consumer. |
| collections, `json`/`yaml`/`jsonl` entities, locks, atomic replace | **cut.** Markdown only. build-lite has no homogeneous registry left. |
| presets, `init <preset>`, schema flattening, `wire`, `install-skills` | **cut.** One schema, shipped as `schema.json`. Installation is `cp -r`. |
| networkx, pydantic, typer, rich, ruamel, python-frontmatter | **cut.** BFS over a dict is 20 lines; validation is the checker; argparse is stdlib. Frontmatter is a documented flat-YAML profile with a hand-written reader (~90 lines), which is also what pins the corpus to one shape. |
| `type/slug` qualification, ambiguity resolution | **cut by specialization.** Ids carry a type prefix (`ad-`, `cmp-`, `fs-`, `fr-`/`cst-`/`br-`), so every slug is globally unambiguous. |

### And what specialization buys back

Cutting is only half of it. A fixed schema affords checks a general engine cannot:

- **`bl new` mints the id**, including the kind-dependent prefix — `--set kind=constraint` yields `cst-004-…`, and a `fr-` file whose `kind` says `constraint` is a `check` error. khub cannot do this; its slugs come from titles.
- **The schema is closed by default.** In khub, unknown keys are legal (it is an open format serving many domains) and `--strict` closes it. Here, an unknown field is an *error* — because the realistic failure is `realised_in` for `realized_in`, a typo that silently produces no edge at all.
- **Body templates are Markdown files.** Their own `##` headings are the contract, so one file both seeds a new document and validates every existing one. khub needs a YAML section list because it must express more.

## Divergences from khub's invariants, on purpose

Four of khub's stated invariants are broken here, each with a reason that only
holds at this scale. They are listed so the next reader does not "fix" them:

1. **`validate` and `check` are collapsed** into one command with two severities.
   One pass over ~100 files is milliseconds; two gates is one more thing for the
   agent to remember. The distinction survives where it matters — in the exit code.
2. **Referential integrity is not enforced at write time** for hand-edited files,
   only for `new` and `link`. See "files are the write API."
3. **The schema is closed**, not open.
4. **The graph is still derived, never stored** — that one is not negotiable, and
   it is why `links` computes inverses on every call.

The invariant that is *strengthened*: schema-genericity. `bl.py` contains no type
name, no field name, and no predicate. Every one comes from `schema.json`, which
is why adding a field is a data edit, and why the add-back ladder in
[`build-lite-preset.md`](build-lite-preset.md) still works here.

## Size

| | build-lite standalone | khub |
|---|---|---|
| implementation | 841 lines, 1 file (~700 non-blank) | 6,990 lines, 40 modules |
| tests | 269 lines, 13 tests | 8,135 lines |
| runtime dependencies | 0 | 6 |
| install | `cp -r skill/ .opencode/skills/build-lite` | `uv tool install git+ssh://…` |
| commands | 8 | 30 |

The tests are shaped around the fact that **the checker is the product**: one
seeded corpus broken in every way the schema permits, asserted against the set of
finding codes it produces, plus a round-trip of the frontmatter profile.

## Risks worth watching

- **The hand-written YAML profile** is the one place a bug would be quiet rather
  than loud. It is confined to two functions (`parse_front` / `emit_scalar`) and
  covered by a round-trip test over the hostile scalars (`"Use: Postgres"`,
  leading dashes, `true`, empty, padded). If it ever misreads real data, swapping
  in `ruamel.yaml` is a two-function change — at the cost of the zero-dependency
  install, which is most of why this is easy to adopt.
- **Surgical frontmatter edits** in `link`/`unlink` splice one key's lines and
  leave the rest byte-identical, so a hand-written file keeps its comments and
  quoting. Tested, but it is the subtlest code in the file.
- **Drift from the canonical preset.** `schema.json` is a hand-maintained
  projection of `presets/build-lite/schema.yaml`. If the two are meant to stay in
  step, generate it (`khub schema --format json` emits the same shape) and diff it
  in CI. Left manual for now — the preset is stable and the projection is 73 lines.
- **Body-shape gaps are noisy at first.** A freshly scaffolded `prd.md` satisfies
  its template because `init` seeds it; a hand-created document will not. That is
  the intended nudge, but it is the finding most likely to be ignored.

## Graduation

Nothing here is a dead end. The corpus is khub-shaped: same paths, same
frontmatter, same predicates, same id conventions. When a constraint needs a
named enforcement mechanism, or a second repo consumes an interface:

```bash
khub init build-lite . --force   # --force only means "the directory isn't empty"
khub validate && khub check
```

Verified on a corpus authored entirely by `bl` — two components, two
requirements, an adr, a feature spec and both narrative documents. `init`
reported *0 entity files modified*, `validate` returned 8 entities and no
errors, `check` passed with no findings of any kind, and `status` reported the
workspace OKF-conformant. Then follow the add-back ladder in
[`build-lite-preset.md`](build-lite-preset.md#add-back-ladder). The only thing
lost in the move is the parts of this tool khub does better anyway.
