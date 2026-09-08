---
name: khub
description: Use when working in a khub workspace (a repository with a .khub/ directory) to read or write typed, schema-validated context instead of grepping Markdown. Covers retrieval (query, get, neighbors, impact, history, search) and authoring (add, edit, link, unlink, remove). Discovers the active preset, types, and schema at runtime.
allowed-tools: Bash
---

# khub: typed context for agents

khub gives you typed, validated, queryable context held as Markdown in git. In a khub workspace (any directory with a `.khub/` above it), prefer khub over reading or grepping files: it returns typed records and enforces the schema on every write. The core library is the only place logic lives; this skill maps your intent onto the CLI and adds none.

If `khub` is not installed (`command not found`), load the `setup` skill first — it installs the CLI and sets up the project.

## Discover the ontology first

Never hardcode a type or a field. Read the live schema:

- `khub status --format json` — counts per type, draft/active, and the active preset.
- `khub schema --format json` — every type with its fields, enums, required flags, and relations.
- `khub schema show <type> --format json` — one type's shape; build an `add`/`edit` from it.

The schema lives at `.khub/ontology.yaml` (the domain model) with `.khub/policy.yaml` and `.khub/storage.yaml` beside it; the active preset (`firm-ops` or `build-hub`; `build-lite` is an alias that `init` resolves and records as `build-hub`) is recorded in `.khub/config.yaml`. `khub wire` links the schema into the agent context files (`CLAUDE.md`, `AGENTS.md`), so the ontology may already be in your context.

## Retrieve

- `khub query --type <t> [--<field> <v>] [--tag <t>] [--has <pred>] [--missing <pred>] --format json` — filter by frontmatter; `--missing` surfaces gaps.
- `khub get <id>... --edges --format json` — one ID returns one object; multiple IDs return an all-or-nothing array in argument order, duplicates included. Raw output accepts one ID only.
- `khub neighbors <id> --format json` — one-hop adjacency.
- `khub impact <id> --format json` — transitive closure over a predicate (blast radius).
- `khub history <id> --format json` — a supersession chain.
- `khub search <text> --format json` — BM25 full-text over titles, bodies, and fields.

## Know when to write

Most missed capture is not a wrong command — it is no command, because a terse ask reads as
conversation. "Policy: credentials must never appear in output. Note it." is a `requirement`,
not a reply.

Each type declares `when` — the moment to capture it, in the domain's own language. Read them
from `khub schema --format json` (or the wired block, which lists them), and treat them as
triggers: when the moment occurs, record it and say you did. Propose the record in place rather
than asking permission for each one — capture is never blocked, and a `--draft` entity is the
right answer when you are unsure it belongs.

A stated fact about the system is a write, whatever the wording. "note it", "write it down",
"log it", "FYI", "heads up", "for the record" — and a bare statement with no instruction at all
— all mean record it. Do that, then say what you recorded and its id. Answering "Noted." without
a record does not complete the task, and neither does asking which file to write to: entities are
written with `khub add`, never by choosing a path. A question about the system is a query.

## Write

Agent and human write the same graph through the same gates; there is no approval step.
Every write goes through the CLI — the only path that validates and resolves relations. If an
entity file gets hand-edited anyway, run `khub validate` on it immediately: an unvalidated
hand-edit is how a workspace acquires a field no default gate will report.

- `khub add <type> --<field> <v> [--body <text>] [--draft] --format json` — mint an entity. Set relation fields inline (`--client acme-corp`). A relation to a missing target is rejected; a missing required field is captured anyway and reported later by `khub check`.
- `khub edit <id> <field> <v> --format json` — change a field; `edit <id> draft false` publishes.
- `khub link <id> <predicate> <target>` / `khub unlink <id> <predicate> <target> --format json` — relations, idempotent. Read `changed` to tell a write from a no-op; both exit 0.
- `khub remove <id> --format json` — delete; refuses while an inbound edge resolves to it unless `--force`.

## The loop

1. `khub add <type> --title "..." …` — it mints the id (`khub schema show <type>` names the
   shape: the type's prefix, a date where the type declares one, then the slugified title),
   writes the frontmatter, and seeds the body from the type's template with a hint comment
   under each heading. The id is the handle from here on; there is no shorter form of it.
2. Open the file and write the prose. Replace the hint comments; keep the `##` headings and
   their order. A heading you have nothing to say under may be deleted if the template
   declares it `optional` — an empty one helps nobody.
3. `khub validate <type>/<slug> --format json` — the file you just wrote, on its own. Cheap,
   and it names the one thing that is wrong. Read all four keys: `errors` (broken), `gaps`
   (unfinished), `body` (word counts per section) and `lenses` (the review questions the
   type declares). See **Reviewing a body**.
4. `khub link` every relation. Edit frontmatter by hand only for plain attributes — and
   `validate` again if you did.
5. `khub check`. **Errors** mean the workspace is broken — fix them before you finish.
   **Gaps** (`incomplete`, orphans, `thin`) mean legal but unfinished; fix the ones your
   change caused.
6. `khub reindex` if you added, removed or re-linked anything. `index.md` is generated, and
   a stale one sends the next reader to a file that moved.

`validate` is the per-entity subset: well-formedness, the schema, whether the ids it names
resolve, and the body against its template. `check` adds what only exists across the whole
graph — orphans, cycles, strays, a missing required singleton. `--strict` means two
different things: on `check` it fails on orphans too; on `validate` and `add`/`edit` it
closes the schema so a typo like `realised_in` is rejected instead of silently producing no
edge.

## Reviewing a body

`validate` reports three things about the prose, and they are not the same kind of thing:

- **`errors`** with `field: body` — broken. A missing or out-of-order `##` heading
  (`body_shape`), or a template that does not parse (`field: template`). Fix before you
  finish.
- **`gaps`** with `field: body` — `body_rule`. A section rule is unmet: thin prose, a
  forbidden phrase, a missing code block. Legal, and it fails no gate. Fix the ones your
  change caused; leave the ones that were already there.
- **`body` and `lenses`** — no finding at all. Word counts per section, and the review
  questions the type declares, filtered to this entity's frontmatter (a lens may apply only
  to one `kind` or `status`). khub computes and asks; the judgement is yours.

**Answer the lenses out loud** when you have just written or substantially changed a
document. One line each, naming what you actually looked at:

> `single`: one statement — "the API returns 401 on an expired token". No "and".
> `measurable`: **fails.** Body says "responds quickly". No number, no measurement point.
> Rewriting as "p99 under 200 ms at the API edge".

**Evidence discipline.** Never assert what you did not verify. If a lens asks something you
cannot check from what is in front of you — whether a component really depends on a
vendor, whether a metric has a dashboard — say you could not check it rather than
guessing. A confident wrong answer in a review is worse than no review, because it closes
the question.

### What good looks like

The shape of a good body follows from what the type is for. Two kinds recur in every
preset: a record that states a rule, and a record that explains a choice.

A rule-stating body (e.g. a `requirement`) is one statement, present tense, with the number
and the measurement in it:

> When an access token has expired, the API rejects the request with 401 and does not
> touch the database. Enforced in the auth middleware ahead of routing; covered by
> `test_expired_token_short_circuits`.

Thin, and why: *"Expired tokens should be handled gracefully."* — "gracefully" is not a
bound, "should" is not a rule, and nothing here can be observed from outside.

A choice-explaining body (e.g. an `adr`) names a real alternative and a real cost:

> **Decision.** We store the corpus as Markdown files in the project's own git repository,
> one file per entity.
>
> **Alternatives.** A SQLite database beside the code — queryable, but invisible in a diff
> and unmergeable, and review is the point. A hosted wiki — searchable, but it drifts from
> the commit that changed the code.
>
> **Consequences.** Review, blame and merge come free. The cost is that every query is a
> full scan, so this stops being cheap somewhere in the low thousands of entities, and
> each command publishes one entity atomically; commands do not form a multi-command transaction.

Thin, and why: *"We decided to use Markdown because it is simple and flexible."* — no
alternative was on the table, and consequences that are all upside mean the tradeoff has
not been found yet.

## Rules

- Every read AND write emits JSON on a pipe; a table is only for a TTY. `--format json` makes it explicit.
- Every existing-workspace mutation is serialized workspace-wide from scan through commit. Never delete `.khub/generated/locks/workspace.lock` while khub may be running.
- Build every write from `khub schema show`, never from a hardcoded shape.
- On a failed command, read the located error (field, reason, or unresolved target) and correct the call. Exit 2 reports a refusal or write failure; `write_durability_uncertain` means publication happened, and `upgrade_recovery_failed` requires journal inspection. Exit 1 is a gate (`validate`, `check`) that ran and found the workspace wrong.
- Gate before you commit: `khub validate` per entity, `khub check` graph-wide.
- Run `khub validate --strict` in CI. Capture is never blocked, so a typo'd field name (`--knid`) is written to frontmatter and neither default gate reports it; `--strict` is what turns undeclared keys into a finding.
- `khub search` and `khub query --has/--missing` both reach attribute values, so find an entity by what it holds (`search python`, `query --type component --missing repo`) rather than listing and filtering yourself.

## What earns a document

Most things do not. Before creating one, ask where it really belongs:

- Prose a human reads start to finish → a **body** section of a document that already
  exists (usually a narrative singleton — `khub schema` lists them by `layout`).
- A fact stored once and read once → an **attribute** on a document that exists.
- Anything that churns with the code (schemas, configs, test files) → leave it in the
  repo and point at it with `resource`.
- A document earns its slot only if it is **referenced by id from elsewhere, walked as a
  graph, or gated by `check`.**

The types the schema declares are the whole set. If something does not fit, it is a
section of a singleton, or it is an instance of the type whose `when` cue it triggers.

## Routing

The schema, not this skill, says where a statement goes. Read the `when` cues and the
relation vocabulary in `khub schema`, and route on them:

| The question | Where to look in `khub schema` |
|---|---|
| What must always hold? | the type whose `when` names rules — "must", "never", "always"; its `kind` enum separates a behaviour from a measurable target from an invariant, and the number goes in the body |
| Why can't I do X? | the type carrying `supersedes` and an `affects: any` edge — the decision, plus every id it constrains |
| Who is this for? | the type whose `when` names a human role — a customer, an admin, an operator. Title only; the persona is its body. A scheduler or another system is not one: it is a `trigger` value on the flow type |
| What does the user do, end to end? | the type carrying `trigger` and a `served_by` edge to the part-of-the-system type, with `actor` and `capability` edges beside it; `served_by` inverted (`impact <cmp> --reverse --predicate served_by`) is what breaks for users when a component dies |
| What does the system do, at map level? | the title-only type that flows belong to (their `capability` edge) and rules are placed in (`capabilities`, many); what realizes it is two hops — its flows, then their `served_by` |
| What talks to what? | the type carrying `depends_on` and a `kind` that separates ours from a vendor's; `depends_on` carries the wiring, vendors included |
| Which system is this in? | the type the part-of-the-system type names with a single `system` edge; it carries `owner` and no edges of its own — its members are the computed inverse, and a one-system workspace never creates one |
| Who consumes this interface? What breaks if it changes? | the type carrying a required `provider` edge back to the part-of-the-system type plus a `kind` and a `status`; consumers are the inverse of that type's `consumes` (`impact <api> --reverse --predicate consumes`), and an unset `provider` is a `check` finding |
| Who owns this? May I build on it? | `owner`, `lifecycle` and `tier` on the part-of-the-system type (`owner` on the system type too): a slug, `experimental … deprecated`, `tier-1 … tier-3`. Text, not an edge — there is no team type to resolve against yet |
| Where does that code live? | the type whose attribute holds a remote path (`org/name`); the part-of-the-system type points at it with a single edge |
| What is this product? How is it shaped? | sections of the narrative singletons (`layout: singleton`) — edit them, never add a second |
| What am I building right now? | the type whose `status` moves (`planned … done`), if the schema declares one — otherwise **not here**: khub records what must hold and why, not what is in flight |

## When it stops fitting

The declared types are a deliberate floor, not a limit anyone forgot to raise. When a rule
needs a named enforcement mechanism the schema cannot hold, **say so** — that is a schema
change someone has to make deliberately, and `.khub/ontology.yaml` (with `storage.yaml`
for where the type lands) is where it happens; a preset's docs carry the order to add
types back in. Do not improvise the missing type in the meantime: a file naming a type the
schema does not declare is a `stray` inside a layout and invisible outside one — either
way it is not an entity, and `check` says so.
