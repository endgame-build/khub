# Ingestion: schema-constrained extraction

khub turns a source document into entities in two halves, with the extractor
between them outside khub. khub ships the contract and the checker; it never
calls a model, sends nothing outward, and makes no network call. The
extractor is whoever reads the source: an agent given the contract as its
prompt, a script, a person.

```
khub schema scaffold ──▶ extractor (outside khub) ──▶ khub schema conform ──▶ khub add
   the contract            fills the envelope          scores, writes nothing    one draft per entry
```

This is the shipped half of ingestion. The two design notes beside it,
[ingestion-conflicts.md](ingestion-conflicts.md) (what a re-run does when the
source and the workspace disagree) and
[ingestion-alignment.md](ingestion-alignment.md) (how a reference a source
makes lands on an entity), describe the importer that does not exist yet.
Nothing here contradicts them: identity is never guessed, an unresolved
target is reported, a gap never blocks capture, and an ingested entity is a
draft with `author` naming the source. The command contract — flags, payloads,
error codes — is in [`cli.md`](cli.md#workspace) and its
[payload shapes](cli.md#payload-shapes); this page is the walk through it.

## 1. The contract — `khub schema scaffold`

The resolved schema, rendered as what an extractor needs and nothing else:
each type's `when` cue and id shape; its fields with kind, enum, pattern,
default and required flag; its stored relations with their target types and
cardinality (no derived inverse — an extractor fills stored edges); and, for a
templated type, the body sections with their hints. The base fields `add`
writes itself (`type`, `created`, `updated`, and the OKF `generated` stamp)
are listed as `supplied` rather than asked for, and the base fields that hold
objects (`generated`, `verified`, `sources`, `usage_window`) are left out:
`add` has no spelling for them, and a review event or a provenance record is
khub's to write (`verify`, `import`), never an extractor's to fill. Every row
is walked from `ResolvedSchema`; no type or field is named in the code, so a
schema edit changes the contract with no surface change.

`--format json` is the machine form; `--format markdown` prints the same
contract as a Markdown prompt — the rules, the envelope, one section per
type with its tables and body sections — on any stream, for an agent to
paste in front of the source (on a TTY the default `text` prints the same
prompt). `--type` narrows it to the types a source could plausibly hold. A
relation that may name a workspace file (policy `path_refs`) says so, and
each body section carries the rules `validate` would otherwise report as a
gap after the write (word counts, required and forbidden text, code
blocks), so the extractor can satisfy them before it.

The rules the prompt carries are fixed and schema-generic: use only the
listed types; fill a field only when the source states it and leave it out
rather than guess; an enum takes one listed value; a relation target is the
id or alias of an entity the workspace holds, or the name the source uses,
and khub resolves it — never invent an id; quote the passage in `source` and
give `confidence` when unsure.

## 2. The envelope

What the extractor returns, pinned in [`cli.md`](cli.md#payload-shapes):

```json
{"entities": [
  {"type": "project",
   "fields": {"title": "Initech PoV", "active": true},
   "relations": {"client": ["Initech"], "owner": "noor"},
   "body": "## Context\n…",
   "source": "Agreed on the kickoff call, p. 2",
   "confidence": 0.9}
]}
```

`fields` holds attribute values as JSON gave them, judged as the kinds they
are (a bool is `true`, not `"yes"`); a declared predicate among them is read
as a relation, the same statement `--<predicate>` makes on `add`. A supplied
key among them is an error, as is the retired `draft` (`status: draft` is the
publish flag now) and an object-valued attribute — each the refusal `add`
would give. `relations` maps a predicate to one target or a list. `body`,
`source` and `confidence` (a number from 0 to 1) are optional. Anything else
is a shape fault, refused as `invalid_envelope` with the entity's index: the
extractor's output is the call, and a call that cannot be read is corrected,
not scored.

## 3. The check — `khub schema conform`

Each entry is judged as `add` then `validate` would judge the written file,
through the same code: the per-field validator (`integrity.FieldFindings` —
kind, enum, pattern, cardinality), the write path's relation resolution
(`entity.ResolveWriteValue` — exact slug, then alias, qualified when a bare
slug would be ambiguous; the first two rungs of the
[reference ladder](ingestion-alignment.md#the-reference-ladder), and nothing
past them), the id the write path would mint with every refusal it raises
before writing (`entity.PlanSlug` — no slug source, an over-long, reserved or
navigation-name slug, a slug or alias the workspace holds, an existing
singleton), and the body-shape finding against the type's template. There
is no second validator, and where `add` is lenient conform mirrors it: a
blank `""` attribute reads as `null`, a blank body is no body.

The report keeps three things apart, the way `validate` and `check` keep them
apart:

- **`errors`** — the record is malformed: an off-enum value, a pattern miss, a
  wrong kind, a surplus target on a single-valued relation, an undeclared
  predicate, a supplied key, a body missing a required heading — or its id
  cannot be written (field `id`): nothing to mint from, an over-long or
  reserved slug, a slug or alias the workspace already holds, a singleton
  that exists, or a second entry in the same envelope minting an id an
  earlier one claimed. `add` would refuse or `validate` would fail.
- **`unresolved`** — a relation target the workspace holds no entity for,
  with the refusal `add` would raise. Reported, never guessed: the stub and
  likeness rungs that come after the exact ones are the
  [alignment ladder](ingestion-alignment.md), and it never auto-merges.
  A target naming an earlier entry of the same envelope resolves when that
  entry conforms, since the `add`s run in envelope order; a later entry is
  never a target ([`cli.md`](cli.md#payload-shapes) has the rule).
- **`gaps`** — the required fields and relations the written record would
  leave empty. Capture is never blocked, so a gap fails nothing; the entry
  lands as active-but-incomplete for `check` to report, or as a draft. The
  one exception is the type's slug source (`name`, then `title`): `add`
  cannot mint an id without it, so its absence is an `id` error.

`ok` is false on `errors` or `unresolved`; `id` is the qualified id `add`
would write (null when it cannot); `confidence` echoes the entry's number;
the summary's `score` is conformant over entities. Exit 1 when any entry
fails, 2 for an unreadable envelope or an undeclared type. Nothing is
written and no lock is taken. What `validate` reports as gaps after the
write — body rules, dead links, actor and footnote gaps — is not evaluated
here.

## 4. Capture — `khub add`

Each conformant entry becomes one `khub add <type> --<field> <value> …
--body-file <file>` call, in envelope order so an entry a later one names
exists first, and `--draft` when its gaps are non-empty or its confidence is
low. Provenance follows what
[ingestion-conflicts.md](ingestion-conflicts.md#fields-ingestion-owns)
writes on a created record: `--author` naming the source, and the entity a
draft until a person publishes it. The quoted `source` passage and the
confidence go in the body (the convention: quote `source` at the top of the
body, under the first heading or as its own paragraph) or in an extension
field of your own. `sources`, the OKF provenance list, has no `add` spelling
today. A draft is the containment for injected content — `check` is the
human review point before it is published. A minted id that collides with an
entity the workspace already holds is reported by conform (`slug_taken`,
`alias_taken`); whether that entity is the same thing is the
[slug-collision rung](ingestion-conflicts.md#identity) of the identity
ladder, not conform's question.

## Not shipped here

- **The importer** of the two design notes: the alignment ladder past its
  exact rungs (stubs, likeness candidates with evidence; #62) and the
  conflict policy for a re-run (strategies, the conflict report; #29).
  conform stops at "unresolved" and judges one envelope against the
  workspace as it is.
- **OKF bundle ingest** (#9): `import --okf` is the consume path for an
  external bundle. conform is strict by design: it judges against the
  workspace's own schema.
- A `--draft-plan` that prints the `add` commands: shell quoting of bodies
  and values makes it bigger than it looks; left for a later issue.
