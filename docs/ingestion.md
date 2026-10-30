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

## 1. The contract — `khub schema scaffold`

The resolved schema, rendered as what an extractor needs and nothing else:
each type's `when` cue and id shape; its fields with kind, enum, pattern,
default and required flag; its stored relations with their target types and
cardinality (no derived inverse — an extractor fills stored edges); and, for a
templated type, the body sections with their hints. The base fields `add`
writes itself (`type`, `created`, `updated`, `draft`) are listed as `supplied`
rather than asked for. Every row is walked from `ResolvedSchema`; no type or
field is named in the code, so a schema edit changes the contract with no
surface change.

`--format json` is the machine form; on a TTY (or under `TTY_COMPATIBLE=1`)
the same contract prints as a Markdown prompt — the rules, the envelope, one
section per type — that an agent pastes in front of the source. `--type`
narrows it to the types a source could plausibly hold.

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

`fields` holds attribute values as JSON gave them; a declared predicate among
them is read as a relation, the same statement `--<predicate>` makes on `add`.
`relations` maps a predicate to one target or a list. `body`, `source` and
`confidence` are optional. Anything else is a shape fault, refused as
`invalid_envelope` with the entity's index: the extractor's output is the
call, and a call that cannot be read is corrected, not scored.

## 3. The check — `khub schema conform`

Each entry is judged exactly as `add` then `validate` would judge the written
file, through the same code: the per-field validator (`integrity.FieldFindings`
— kind, enum, pattern, empty string, cardinality), the write path's relation
resolution (`entity.ResolveWriteValue` — exact slug, then alias, qualified
when a bare slug would be ambiguous), and the body-shape finding against the
type's template. There is no second validator.

The report keeps three things apart, the way `validate` and `check` keep them
apart:

- **`errors`** — the record is malformed: an off-enum value, a pattern miss, a
  wrong kind, a surplus target on a single-valued relation, an undeclared
  predicate, a body missing a required heading. `add` would refuse or
  `validate` would fail.
- **`unresolved`** — a relation target the workspace holds no entity for,
  with the refusal `add` would raise. Reported, never guessed: the alignment
  ladder that proposes a match with evidence is [#62](https://github.com/endgame-build/khub/issues/62),
  and it never auto-merges.
- **`gaps`** — the required fields and relations the written record would
  leave empty. Capture is never blocked, so a gap fails nothing; the entry
  lands as active-but-incomplete for `check` to report, or as a draft.

`ok` is false on `errors` or `unresolved`; the summary's `score` is
conformant over entities. Exit 1 when any entry fails, 2 for an unreadable
envelope or an undeclared type. Nothing is written and no lock is taken.

## 4. Capture — `khub add`

Each conformant entry becomes one `khub add <type> --<field> <value> …
--body-file <file>` call, `--draft` when its gaps are non-empty or its
confidence is low; `source` goes in `sources` or the body as provenance. A
draft is the containment for injected content — `check` is the human review
point before it is published. The slug is minted from the entry's name or
title, so a collision with an existing entity refuses (`slug_taken`): that is
a same-entity candidate, and deciding it is the alignment ladder's job, not
conform's.

## Not shipped here

- **Alignment** (#62): resolving a mention down the ladder — slug, alias,
  `source_id`, then FTS5 candidates proposed with evidence — and minting the
  rest as drafts with provenance. conform stops at "unresolved".
- **Conflict policy** (#29): never silently overwriting a non-draft value;
  the conflict report and its resolution menu.
- **OKF bundle ingest** (#9): the permissive consume path for external
  bundles. conform is strict by design: it judges against the workspace's
  own schema.
- A `--draft-plan` that prints the `add` commands: shell quoting of bodies
  and values makes it bigger than it looks; left for a later issue.
