# build-lite, standalone

> **Baseline note, 2026-08-16.** This was written while khub was a Python
> package, and several arguments below weigh khub's dependency count and
> install cost against a standalone script. khub is a single static binary
> now, installed by one `curl`, so that side of the comparison is much
> lighter than the tables here assume. The reasoning about what build-lite
> actually needs is unaffected.

> **Baseline note, 2026-09-02.** The id scheme, exit codes, body templates and
> `upgrade` described below changed in the kb 0.14.0 port (see CHANGELOG);
> `cmp-001-api`-style ids, the `-N` suffix and by-kind `fr-/cst-/br-` prefixes
> no longer exist in either tool.

**A separate, tiny implementation of the build-lite preset for one build project
driven by opencode: a drop-in directory holding a skill and one stdlib Python
script. ~850 lines of code where khub is 7,000, no dependencies where khub has
six, and byte-compatible output so a corpus that outgrows it graduates by running
`khub init build-lite` over the same files.**

The working implementation now lives in its own repository, `paved-kb`. This
page is why it has the shape it has, and what was considered instead — the
reasoning is khub's to keep even though the code left.

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
   `adr.affects: [cmp-api]` lives on the adr. Nothing in `cmp-api.md`
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
4. **Deterministic ids.** An id is a pure function of the title (plus the type's
   prefix and date), so two sessions that mint the same title mint the same id
   and the second collides loudly instead of racing for the next free ordinal.

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
contract.** `kb new` scaffolds, `kb link` maintains edges (the one edit where a
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
| **Full khub + build-lite preset** (status quo) | 7,000 lines, one static binary, `curl … | sh`, search/viz/backfill/collections/presets unused | The graduation target, not the daily driver. Its generality is real value at a firm's scale and dead weight at one project's. |

The recommendation is the second row, and the deliverable is **one directory you
drop into a project**, holding the two things that install to different places:

```
kb/
  skills/kb/SKILL.md    what the agent reads: the ontology, the routing
                        rules, the loop                          (95 lines)
  scripts/
    kb.py               scaffold + check + walk, stdlib only     (~850 lines)
    build.schema.yaml   khub's core base + the six types, combined
    templates/*.md      4 body templates; their `##` headings are the
                        body contract
  install.sh            wires the skill into .opencode/skills
  test_kb.py            17 tests
```

Only the skill has to move, because `.opencode/skills`, `.claude/skills` and
`.agents/skills` are the only places a host looks. `scripts/` stays where it
lands — `kb.py` resolves its schema and templates relative to itself. And there
is a zero-copy path: opencode's `skills.paths` config key takes directories
(relative entries resolve against the project root, scanned for `**/SKILL.md`),
so `{"skills": {"paths": ["kb/skills"]}}` loads the skill in place with
no install step at all. That key is implemented but undocumented, hence the
install script as the guaranteed route.

### The API is khub's

Every verb and flag name is khub's, and kb implements a strict subset: `init`,
`schema`, `status`, `add`, `get`, `edit`, `remove`, `link`, `unlink`, `query`,
`neighbors`, `impact`, `history`, `validate`, `check`, `install-skills`. Absent,
and deliberately: `search`, `stale`, `reindex`, `viz`, `backfill`, `wire`.

An earlier draft optimised each verb locally and produced a surface that read
differently — `new`, `ls`, and one merged `links` walk. It was a smaller surface
and it was wrong: the entire value of this thing is that a corpus graduates to
khub, and a corpus graduates far more easily than a habit does. Names that
transfer are worth more than names that are individually shorter.

### Why one script, not one script per verb

`add.py` / `link.py` / `validate.py` as separate files is the obvious
decomposition. One argument against it used to be decisive and no longer is; the
other still holds.

**The one that lapsed.** When a skill loads, opencode lists its files for the
model — `ripgrep.find({ pattern: "!**/SKILL.md", limit: 10 })`, followed by
*"Note: file list is sampled."* While the tool lived inside the skill directory,
eight verb scripts plus a shared core, the schema and four templates would have
been fourteen files competing for ten slots, with no say in which four vanished.
Moving `scripts/` out of the skill directory removed that constraint entirely:
the skill now holds exactly one file. Splitting is no longer *blocked*.

**The one that stands.** The verbs are not independent programs. Roughly 400 of
the 850 lines are the frontmatter profile, the schema reader, the corpus scan and
the edge computation, and every verb needs most of it — `add` resolves edges,
which needs the index; `link` validates a target, which needs the index and the
schema; `check` needs all of it. Splitting therefore means a shared `_core.py`
plus seven ~30-line wrappers: eight files and slightly more code for exactly the
same logic. The alternative — each script parsing frontmatter its own way — is
how two readers start disagreeing about the same file.

What the decomposition is actually reaching for is that the verbs should be
legible. They are: `kb` with no arguments prints all eight with one-line
descriptions, and `SKILL.md` leads with the same list. If per-verb entry points
are ever wanted for a human's fingers, symlinks cost nothing and change no code.

### opencode specifics that shaped it

Verified against the opencode source at v1.18.5 (its docs site 403s from here, so
the doc sources in `packages/web/src/content/docs` and the implementing
TypeScript were read instead):

- **Skills are first-class and can bundle scripts.** Loading a skill hands the
  model the skill's absolute base directory plus a file listing **capped at 10
  entries and explicitly described to the model as sampled**. Bundling was the
  first design; keeping the tool in a sibling `scripts/` instead means the skill
  ships one file and the cap stops mattering, at the cost of `SKILL.md` having to
  name the script's path.
- **`skills.paths` loads a skill from anywhere.** Relative entries resolve
  against the project root and are scanned for `**/SKILL.md`, which is what makes
  a genuine zero-copy drop-in possible. Implemented in `skill/index.ts`, absent
  from every doc page — so it is offered as the fast path, not the only one.
- **No `allowed-tools`.** opencode recognizes only `name`, `description`,
  `license`, `compatibility`, `metadata`. `name` must match the directory name.
- **Permissions match per sub-command, last rule wins.** The shell tool
  tree-sitter-parses the command line and asks about each sub-command separately,
  so `kb check | head` needs `head *` allowed too. Put `"*": "ask"` **first** and
  `"kb *": "allow"` after it, and prefer not to pipe.
- **There are no PostToolUse hooks.** The analogue is a plugin's
  `tool.execute.after`, which can append to the tool's own output — ~30 lines,
  in the `paved-kb` repository's README,
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
| `query`, `get`, `neighbors`, `impact`, `history` | **kept, under khub's names.** One BFS backs all three walks — they differ only in defaults — but the verbs stay separate so what an agent learns on khub transfers unchanged. |
| `search` (FTS5, BM25) | **cut.** ripgrep is better at this scale and the agent already has it. |
| `add --body/--body-file`, `edit` | **kept, and rarely the right call.** The agent writes prose into the file; `add` scaffolds, `edit` is for one attribute, `link`/`unlink` keep edges honest. |
| `validate` + `check` as two gates | **both kept**, splitting one sweep by finding code: `validate` reports the per-entity subset, `check` adds the graph-wide gates. Two severities cut across them — `error` (broken) and `gap` (unfinished) — and only errors set the exit code, so capture is never blocked. |
| `draft` flag, active-subgraph logic | **the behaviour is cut, the attribute is not.** `adr.status` and `feature-spec.status` already say what draft would, and no relation in build-lite is required, so the flag gates nothing here. It is still declared, because deleting an attribute from a *closed* schema turns every entity khub writes into an `unknown_field` error — which is what the drift test caught. Same for `author`, `sources` and `references`: declared, unread. |
| `stale`, `backfill`, `log`, git integration | **cut.** git is the freshness record; `git log -- <path>` answers it without a projection. |
| `reindex`, `viz`, OKF export | **cut.** No index to keep current, no dashboard consumer. |
| collections, `json`/`yaml`/`jsonl` entities, locks, atomic replace | **cut.** Markdown only. build-lite has no homogeneous registry left. |
| presets, `init <preset>`, schema flattening, `wire` | **cut.** One schema, shipped pre-flattened as `build.schema.yaml`. `install-skills` is kept, under khub's name. |
| networkx, pydantic, typer, rich, ruamel, python-frontmatter | **cut.** BFS over a dict is 20 lines; validation is the checker; argparse is stdlib. Two hand-written readers replace the YAML dependency: a flat profile for frontmatter (~90 lines, which is also what pins the corpus to one shape) and a nested subset reader for the schema (~140 lines, tested against `ruamel.yaml`'s parse of the shipped file). |
| `type/slug` qualification, ambiguity resolution | **cut by specialization.** Ids carry a type prefix (`ad-`, `cmp-`, `fs-`, `fr-`/`cst-`/`br-`), so every slug is globally unambiguous. |

### And what specialization buys back

Cutting is only half of it. A fixed schema affords checks a general engine cannot:

- **The id scheme moved *into* khub.** Minting `ad-2026-07-28-use-postgres` began as a kb-only trick, which made it a divergence and a second thing to learn. It is now `id_prefix` and `id_date` in the schema, so khub mints the same id from the same input and the presets' long-documented `ad-`/`req-`/`cmp-` conventions are finally declared rather than typed by hand into `--id`.
- **The schema is closed by default.** In khub, unknown keys are legal (it is an open format serving many domains) and `--strict` closes it. Here, an unknown field is an *error* — because the realistic failure is `realised_in` for `realized_in`, a typo that silently produces no edge at all.
- **Body templates are Markdown files.** Their own `##` headings are the contract, so one file both seeds a new document and validates every existing one. khub needs a YAML section list because it must express more.

## Behavioural parity, not a variant

An earlier draft of this tool diverged in four ways that all looked like
improvements in isolation: one merged gate instead of `validate` and `check`, a
closed schema, an id-shape gate khub does not have, and its own JSON record
shape. Every one of them broke the premise. A corpus that graduates is worth
little if the *commands* do not, and a divergence discovered at graduation time
is a divergence discovered too late.

So kb is now a strict behavioural subset: same verb names, same flags, same
payload keys, same exit codes, same findings on the same corpus.
`tests/test_kb_khub_parity.py` builds one workspace, runs both tools over it, and
diffs the results — `validate` and `check` payloads on clean and broken corpora,
the open-schema/`--strict` pair, `query` identity, `status` counts, `search`
ranking scores, and the bytes of `index.md`.

Matching khub cost the checks kb had invented. One of them was worth keeping — a
`fr-` file whose `kind` says `constraint` — so it moved into khub's `validate`
rather than being dropped, and both tools now have it.

The one invariant that was never negotiable: **the graph is derived, never
stored**, which is why the walks compute inverses on every call.

What still differs is capability, not behaviour: `viz` and `backfill` are absent,
and `stale` reads the dates the files carry rather than backfilling them from
`git log`.

The invariant that is *strengthened*: schema-genericity. `kb.py` contains no type
name, no field name, and no predicate. Every one comes from `build.schema.yaml`,
which is why adding a field is a data edit — a project can even override the
shipped copy at `.kb/build.schema.yaml` without touching the drop-in —
and why the add-back ladder in
[`build-lite-preset.md`](build-lite-preset.md) still works here.

## Size

| | build-lite standalone | khub |
|---|---|---|
| implementation | 1,013 lines, 1 file (847 non-blank) | 6,990 lines, 40 modules |
| tests | 339 lines, 17 tests | 8,135 lines |
| runtime dependencies | 0 | 6 |
| install | `cp -r kb/` into the project | `npm install -D @endgame-build/khub` |
| commands | 8 | 30 |

The tests are shaped around the fact that **the checker is the product**: one
seeded corpus broken in every way the schema permits, asserted against the set of
finding codes it produces, plus a round-trip of the frontmatter profile and a
differential check of the schema reader against `ruamel.yaml`.

## Risks worth watching

- **The two hand-written YAML readers** are where a bug would be quiet rather
  than loud. The frontmatter profile is confined to `parse_front`/`emit_scalar`
  and round-trip tested over hostile scalars (`"Use: Postgres"`, leading dashes,
  `true`, empty, padded). The schema reader is nested and therefore riskier, so
  it is tested differentially: its parse of the shipped `build.schema.yaml` must
  equal `ruamel.yaml`'s, byte for byte in structure. If either ever misreads real
  data, swapping in `ruamel` is a small, local change — at the cost of the
  zero-dependency install, which is most of why this is easy to adopt.
- **Surgical frontmatter edits** in `link`/`unlink` splice one key's lines and
  leave the rest byte-identical, so a hand-written file keeps its comments and
  quoting. Tested, but it is the subtlest code in the file.
- ~~**Drift from the canonical preset.**~~ Closed. See "What can be built from
  khub" below: `tests/test_build_lite_standalone.py` diffs the shipped schema
  against a live `khub init build-lite` scaffold on every CI run, and the body
  templates are generated from the preset's own renderer.
- **Body-shape gaps are noisy at first.** A freshly scaffolded `prd.md` satisfies
  its template because `init` seeds it; a hand-created document will not. That is
  the intended nudge, but it is the finding most likely to be ignored.

## What can be built from khub, and what cannot

The tool is hand-written, which invites the question of how much of it could be
*derived* from khub instead. The line falls between code and contract.

**The contract is derivable, and now verified as such.**
`tests/test_build_lite_standalone.py` scaffolds a live `khub init build-lite`
workspace on every CI run and diffs it against the shipped drop-in: the base
block must be khub's verbatim, every entity must match the preset key for key,
and the only permitted addition is `id_prefix`. The four body templates are not
compared but *generated* — khub's `BodyTemplate.render()` emits exactly the
`## Heading` + `<!-- hint -->` Markdown the drop-in ships, so they are a build
artifact of the preset. The test also runs both directions of the compatibility
claim: a corpus authored entirely through `kb` survives `khub init --force` byte
for byte and passes khub's `validate` and `check`, and an entity khub writes
passes `kb check` clean.

It earned itself on the first run, catching two genuine drifts: the templates had
hand-typed blank lines the renderer does not produce, and dropping `draft` from a
*closed* schema made every khub-written entity an `unknown_field` error. The
second one is the interesting failure — the deltas were being reasoned about one
side at a time, and a test that runs both tools over one corpus does not let you.

**The code is not derivable.** Three independent reasons:

1. **Dependency floor.** khub's core needs pydantic, ruamel, networkx and
   python-frontmatter. Any derivation carries them, and dependency-free is the
   whole premise of a drop-in.
2. **`kb` is not a subset of khub.** Merged `validate`+`check` with severities, a
   closed schema, kind-agreeing id prefixes, Markdown templates, one `links` verb
   in place of three walks. Specialization is not subtraction, so even a perfect
   tree-shaker would emit khub-minus-features, not this.
3. **Nothing shakes Python source to readable single-file output** anyway.
   `pyinstaller` and `nuitka` emit binaries, not source you can read in a repo.

And the code that *would* be shared is the part that stopped being interesting:
roughly 400 lines of scan, index and edge-walking, all of which is small
precisely *because* the dependencies went. `networkx` becomes a 20-line BFS. The
sharing opportunity evaporated at the moment specialization made it cheap.

The only true merge — making khub's core stdlib-only so the drop-in could vendor
it — refactors the engine to serve its smallest consumer. Named here so it is
visibly rejected rather than overlooked.

### Why not ship khub as a `.pyz`

A zipapp is the obvious "distribute it whole" answer and it fails on all three
counts. `pydantic-core` is a compiled Rust extension, and Python cannot `dlopen`
from inside a zip — so shiv or pex would have to unpack to a cache on first run,
which makes it a self-extracting installer, not a file. That archive is also
built against one platform's wheels, so it stops being droppable into a
teammate's repo. It weighs tens of megabytes against 36 KB. And it still would
not be `kb`: same thirty verbs, none of the specialization.

For `kb` itself a `.pyz` is strictly worse than what it already is. One stdlib
file is readable, greppable, diffable in git, and editable in place — and
`build.schema.yaml` sitting next to it *is meant to be edited*. Zipping that shut
trades every one of those properties for a packaging problem the tool does not
have. And "without installing it" stopped meaning much once khub became a single
static binary: `npm install -D @endgame-build/khub` pins one reviewed line in
`package.json` and needs no runtime beyond the npm the repo already uses.

## Graduation

Nothing here is a dead end. The corpus is khub-shaped: same paths, same
frontmatter, same predicates, same id conventions. When a constraint needs a
named enforcement mechanism, or a second repo consumes an interface:

```bash
khub init build-lite . --force   # --force only means "the directory isn't empty"
khub validate && khub check
```

Verified on a corpus authored entirely by `kb` — two components, two
requirements, an adr, a feature spec and both narrative documents. `init`
reported *0 entity files modified*, `validate` returned 8 entities and no
errors, `check` passed with no findings of any kind, and `status` reported the
workspace OKF-conformant. Then follow the add-back ladder in
[`build-lite-preset.md`](build-lite-preset.md#add-back-ladder). The only thing
lost in the move is the parts of this tool khub does better anyway.
