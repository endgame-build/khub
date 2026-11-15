---
name: khub
description: Use when working in a khub workspace (a repository with a .khub/ directory) to read or write typed, schema-validated context instead of grepping Markdown. Says which verb answers which question (retrieve, query, get, neighbors, impact, history, search; add, edit, link, unlink, remove; validate, check), how to retrieve exact-first, and how to loop write, validate, fix. Discovers the active preset, types, and schema at runtime.
allowed-tools: Bash
---

# khub: typed context for agents

khub gives you typed, validated, queryable context held as Markdown in git. In a khub workspace (any directory with a `.khub/` above it), prefer khub over reading or grepping files: it returns typed records and enforces the schema on every write. The core library is the only place logic lives; this skill maps your intent onto the CLI and adds none.

If `khub` is not installed (`command not found`), load the `setup` skill first — it installs the CLI and sets up the project.

## Decide the next call from the last result

This skill is not a fixed pipeline. Every khub call but the operator commands (`reindex`, `viz`, `serve`, `backfill`,
`wire`) answers in JSON, and that answer decides the next call: an id you did not have, an edge to walk, an `error` naming the
field to fix, a `gaps` entry, a stderr `note:` saying a search was cut short.

- **A closed ask** (one fact, one write, an id you already hold): plan the calls up front,
  run them, confirm from the result.
- **An open ask** ("what do we know about X", "is this safe to change"): loop. Before each
  call, name the gap in one line ("what `<id>` depends on is unresolved"). If you cannot
  name one, stop and answer from what you have. Never re-run a call reworded, and never
  re-plan from scratch.

## Which verb

Never hardcode a type or a field: the first four rows read the live schema.

| You want to… | Call | Next, depending on the answer |
|---|---|---|
| know what the workspace holds | `khub status --format json` | counts per type, draft/active (`status: draft` is a draft), orphan, stale and expired; `schema` for the types behind them |
| know every type, field, enum and relation | `khub schema --format json` | `provenance` names the active preset and its version; `schema show <type>` before writing one |
| know one type's shape before a write | `khub schema show <type> --format json` | build `add`/`edit` from it |
| know whether the schema moved under the data | `khub schema diff --format json` | `pending: true` → entities may predate it. `no_schema_snapshot` means no baseline exists; do not run `schema snapshot` (a write) to answer a read |
| pick up where work left off | `khub recent [--days N] --format json` | newest first, a week by default. It dates by `updated`, else the last commit, so an uncommitted hand edit without `updated` does not show |
| read an entity whose id you hold | `khub get <id>... --edges --format json` | `get` an edge's target, or `neighbors` |
| read the entity a file holds | `khub get --path <file>... --format json` (khub 0.32.0 and later) | the same record as `get <id>`; use it on paths from `grep`, as printed from the current directory, instead of guessing the slug. A collection file refuses and lists its ids |
| find entities by a field, tag, edge or review tier | `khub query --type <t> [--<field> <v>] [--tag <t>] [--has/--missing <pred>] [--trust <tier>] --format json` | `get` the rows you need; `--missing` finds gaps, `--trust` takes `unverified`, `machine` or `human`. Filter here rather than listing and filtering yourself |
| find everything under an entity | `khub query --under <id> --via <pred> --format json` | any number of hops, ANDed with the other filters. A stored `<pred>` points from child to parent (`--via system`); for one stored on the parent, pass its declared inverse (`--via superseded`) |
| find entities by how many edges they have | `khub query --count '<pred>>=2'` / `--in-count '>=5'` / `--out-count 0` | ops `= != < <= > >=`; repeat to AND, e.g. a range. Quote the bound: `>` and `<` are shell redirects |
| see what is one hop away | `khub neighbors <id> [--predicate <p>] [--in/--out] --format json` | `get` the neighbor that answers |
| see what breaks if this changes | `khub impact <id> --reverse [--predicate <p>] --format json` | `get` the rows at the depth that matters. Without `--reverse` it walks what this reaches; the predicate defaults to `depends_on` |
| trace a supersession chain | `khub history <newest-id> --format json` | it walks back from the record you name, so name the newest. What replaced an old record: `neighbors <id> --in --predicate <supersession predicate>` |
| find something by its words | `khub search <text> [--type <t>] [--plain] [--also <text>] --format json` | `get` the hit; follow a stderr `note:` |
| gather the context around a topic in one call | `khub retrieve <text> [--expand <pred>[:<depth>]] [--max-bytes <n>] --format json` | read `omitted` before trusting the answer |
| record a fact no entity holds yet | `khub add <type> --title "…" [--draft] --format json` | `validate <type>/<slug>` |
| change a field, or the body | `khub edit <id> <field> <v> --format json` | `validate <type>/<slug>` |
| connect or disconnect two entities | `khub link` / `khub unlink <id> <predicate> <target> --format json` | `changed: false` was a no-op |
| delete an entity | `khub remove <id> --format json` | refused while something points at it: `neighbors <id> --in` |
| read in an external OKF bundle | `khub import --okf <bundle> [--type-map <file>] --format json` | read `skipped` and `unresolved` |
| record that someone reviewed an entity | `khub verify <id> --by <actor> --format json` | see **Write** for who may be named |
| check one entity you wrote | `khub validate <type>/<slug> --format json` | fix what `errors` names, then `validate` again |
| check the whole graph before you finish | `khub check --format json` | fix the findings your change caused |
| refresh the generated `index.md` | `khub reindex` (human text, no `--format`) | — |

The schema lives at `.khub/ontology.yaml` (the domain model) with `.khub/policy.yaml` and `.khub/storage.yaml` beside it; the active preset (`firm-ops` or `build-hub`; `build-lite` is an alias that `init` resolves and records as `build-hub`) is recorded in `.khub/config.yaml`. `khub wire` links the schema into the agent context files (`CLAUDE.md`, `AGENTS.md`), so the ontology may already be in your context.

## Retrieve: exact first, then the graph, then text

Take the cheapest row of the table that can answer, and step only on a named gap:

1. **Exact** — `get` when you hold an id or a path, `query` when you know a field, tag or
   edge. One id to `get` returns one object; several return an all-or-nothing array in
   argument order, duplicates included; raw output accepts one id only.
2. **Graph** — `neighbors`, `impact`, `history` from an id you hold.
3. **Lexical** — `search`, BM25 over titles, bodies and fields; FTS5 syntax unless
   `--plain`, which takes words as typed (any may match).
4. **Body** — only when the answer is in the prose; `get` returns it.

A search hit is an id, so the next call is exact again. A stderr `note:` means the result is
truncated, empty or missing unparseable files: raise `--limit` or switch to
`query`/`neighbors`/`get`, rather than rephrasing the search.

- **Edges.** `get --edges` lists every inbound edge from another entity as `derived: true`,
  named by the relation's declared inverse or else `<source type>.<predicate>`. A derived
  name is a label; filter with the raw predicate (`query --has <predicate>`).
- **Search hits** carry `draft`/`orphan`/`stale`, per-predicate edge counts, and, for a
  body match under a Markdown heading, `section`: the heading path to the matched text.
  With an intent and an exact anchor, send both as groups in one call
  (`khub search "auth flow" --also '"ForbiddenError"'`): each is ranked on its own and the
  rankings are fused by reciprocal rank, so a hit in both usually comes first where one
  `OR` query would bury the rare word. Compare fused rows on `fused` (higher is better) and
  read `groups`; `score` and `snippet` there come from one group per row.
- **`retrieve`** stands in for `search`, then `get` on the hits, then `neighbors` on each.
  It returns the seeds (a search, or `--id <id>` repeated) in rank order followed by what
  they link to, as full `get` records, each with a `reason` (the seed's score, or the
  predicate, direction and id it was reached from). `--limit` caps the seeds,
  `--max-entities` and `--max-bytes` trim the far edge first, `--no-body` keeps the
  frontmatter and edges only. In `omitted`, `truncated` means something was cut and
  `insufficient` means the cap could not hold the seeds themselves: raise the cap, lower
  `--limit` or pass `--no-body` rather than trusting a clipped answer.
  Use `search` alone when you only need to find, and `get` when you already hold the ids.
- **Receipts.** Add `--receipt` to `query` or `search` when you need the counts as data: the
  JSON becomes `{receipt, results}`, and `receipt` echoes the applied filters (and the FTS5
  expression `--plain` built), `matched` before `--limit`, `returned`, and `truncated`.

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

`add` takes relation fields inline (`--client acme-corp`) and the prose as `--body <text>`;
it rejects a missing target, but captures a missing required field for `check` to report. `--draft` on `add` writes
`status: draft`; an entity with no `status` is stable, and `edit <id> status stable`
publishes a draft. `link`/`unlink` are idempotent and exit 0 either way. `remove --force`
deletes despite inbound edges. `--dry-run` on any of `add`, `edit`, `link`, `unlink` and
`remove` previews the write: khub's own refusals on the same exit codes, the record the
write would print plus `changed` and `"dry_run": true`, and nothing written. A permission or
disk failure can still fail the real call after a clean dry run. `remove <id> --dry-run`
lists in `inbound` the edges a `--force` would leave dangling. `import` brings a bundle in as drafts; the bundle's own
`verified` is kept as `okf_verified` and does not count as a review.

- **Look before you add.** `search --plain "<title>"`, or `query` on the field that would
  match. If it exists, `edit` or `link` it: a second entity for the same thing splits the
  edges that should meet.
- **Inferred is `--draft`.** A fact you inferred, guessed or read off an untrusted source
  lands with `--draft`. Drafts stay out of the active graph and out of `check`'s
  completeness gate until a human publishes one with `edit <id> status stable`.
- **Never overwrite a human's value with your inference.** Leave it, and report the
  conflict. Mint a separate draft only when your evidence is a different kind of record.
- **Verify only a review that happened.** `verify --by` names who reviewed:
  `human:<name>` for a person who confirmed the content, `process:<name>` for a check that
  ran. Never verify as `human:` on your own say-so. A later content or status edit
  supersedes the review; reads show it in `trust`, and `khub check --require-verified
  [--tier human]` fails on active entities below the tier.
- **Exit 0 or it did not happen.** Claim a write only on exit 0 with an `id` in the JSON.
  Exit 2 is a refusal: read `error.code` and correct the call. Nothing was written, with
  three exceptions: `write_durability_uncertain` means the write published, so do not run
  it again; `import_write_failed` names the files it could not undo; and
  `upgrade_recovery_failed` leaves a journal to inspect.

Every content write stamps `generated: {by, at}`: an `add`, or a change to the body, a declared field, `status` or a relation. A bookkeeping-only edit (`tags`, `aliases`, `stale_after`, `author`, `created`) and an edit that changes nothing stamp nothing. Under Claude Code `by` is `agent/claude-code` unless `KHUB_ACTOR` or `--by` says otherwise; pass `--by human:<name>` only when that person wrote the content.

A write may print `warning: <id> › <rule>: <message>` lines on stderr: `check` findings
this session has not seen yet, the workspace's existing debt on your first write and only
what changed after that, at most 20 and then a count. The write still succeeded. Fix a
warning your change caused; the rest is debt to mention, not to chase. Parallel writes
share the session, so a warning may land on a different call than the one that caused it.
They do not replace the check step of the loop. Claude Code names the session for you; any
other agent sets `KHUB_SESSION` to one value for all its calls to get them.

## Generate, validate, iterate

1. **Generate.** `khub add <type> --title "..." …` mints the id (`schema show <type>` names
   the shape: the type's prefix, a date where the type declares one, then the slugified
   title), writes the frontmatter, and seeds the body from the type's template with a hint
   comment under each heading. The id is the handle from here on. Write the prose into the
   file: replace the hint comments, keep the `##` headings and their order, and delete a
   heading only if the template declares it `optional`.
2. **Validate.** `khub validate <type>/<slug> --format json`, the file you wrote, on its own.
   Read all four keys: `errors` (broken), `gaps` (unfinished), `body` (word counts per
   section) and `lenses` (the type's review questions). See **Reviewing a body**.
3. **Iterate.** Fix exactly what an `errors` entry, or a `gaps` entry your change caused,
   names; then `validate` again. Stop when `errors` is empty and the remaining `gaps`
   predate you. Do not rewrite what `validate` did not flag.
4. **Connect.** `khub link` every relation. Edit frontmatter by hand only for plain
   attributes — and `validate` again if you did.
5. **Check.** `khub check`. **Errors** mean the workspace is broken: fix them, looping back
   to `validate`. **Gaps** (`incomplete`, orphans, `thin`) are legal but unfinished: fix
   the ones your change caused. `retention` lists entities past their type's retention
   rule: candidates for removal that a human decides on. Never delete one on your own.
   `waived` lists findings a person accepted in `.khub/policy.yaml`, each with its reason;
   leave them be. A `stale_waivers` row is a waiver that no longer matches anything — say
   so. Never add a waiver to turn `check` green: a waiver is a person's decision.
   `khub check --mentions --format json` also lists, under `mentions`, entities a body names
   with no edge behind them. Prose is not a relation: `link` the ones the text really
   relates to, using one of the row's `predicates`, and leave the rest. The predicates are
   the mentioning entity's own; when the precise edge belongs to the entity named, link
   from that side instead (`khub schema show <type>` lists its relations).
6. `khub reindex` if you added, removed or re-linked anything; a stale `index.md` sends the
   next reader to a file that moved.

`validate` is the per-entity subset: well-formedness, the schema, whether the ids it names
resolve, and the body against its template. `check` adds what only exists across the whole
graph — orphans, cycles, strays, a missing required singleton. `--strict` means two
different things: on `check` it fails on orphans too; on `validate` and `add`/`edit` it
closes the schema so a typo like `realised_in` is rejected instead of silently producing no
edge.

A stray whose only fault is a missing `type:` is repaired by `khub backfill --infer-type`
(preview with `--dry-run`): it stamps the type the file's layout holds and lists every stray
it left, with the reason.

## Reviewing a body

`validate` reports three things about the prose, and they are not the same kind of thing:

- **`errors`** with `field: body` — broken. A missing or out-of-order `##` heading
  (`body_shape`), or a template that does not parse (`field: template`). Fix before you
  finish.
- **`gaps`** with `field: body` — `body_rule`. A section rule is unmet: thin prose, a
  forbidden phrase, a missing code block. Or `dead link '<dest>' (no such file)`: a
  relative link in any Markdown body whose file is not there, checked from the entity's
  own directory and case-exact; a link that leaves the workspace is not checked. Legal,
  and it fails no gate. Fix the ones your change caused; leave the ones that were already
  there.
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

- Every read AND write emits JSON on a pipe; a table is only for a TTY. `--format json` makes it explicit. The operator commands (`reindex`, `viz`, `serve`, `backfill`, `wire`) are the exception: they print prose and take no `--format`.
- Every existing-workspace mutation is serialized workspace-wide from scan through commit. Never delete `.khub/generated/locks/workspace.lock` while khub may be running.
- On a failed command, read the located error and correct the call. The JSON `error` carries `code` and `message`, and where they apply `field` (the attribute or relation at fault), `expected` (the values allowed) and `suggestion` (the nearest one: retry with it when it is what you meant). Exit 2 reports a refusal or write failure; `write_durability_uncertain` means publication happened, and `upgrade_recovery_failed` requires journal inspection. `locked_removed` means an upgrade would drop a type or predicate the schema locks: `--unlock` is the workspace owner's call, so ask before passing it. Exit 1 is a gate (`validate`, `check`) that ran and found the workspace wrong.
- Run `khub validate --strict` in CI. Capture is never blocked, so a typo'd field name (`--knid`) is written to frontmatter and neither default gate reports it; `--strict` is what turns undeclared keys into a finding.
- When sources call an entity by another name, record it: `khub edit <id> aliases "Initech,INTC"`. Every ID you pass then resolves through it, a relation written by alias stores the real slug, and `add` refuses a slug that is already someone's alias.

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
