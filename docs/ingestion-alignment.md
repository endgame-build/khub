# Ingestion alignment: resolving what a source refers to

**Status: design. Nothing on this page ships yet.** khub has no ingestion verb.
This note is the companion of [ingestion-conflicts.md](ingestion-conflicts.md),
which sets the policy an importer follows: never silently overwrite a
published value, never merge two entities on its own, and match an incoming
record to an entity down a three-rung ladder. That note leaves two things to
this one, and this note adds nothing that contradicts it:

- **References.** A record names other things: the capability a use case
  belongs to, the component a requirement is realized in. Each such reference
  has to land on an entity before the edge can be written, and an unaligned
  reference is how an importer mints duplicates.
- **Evidence.** What counts as a likeness on rung 3, how candidates are ranked,
  and how the ladder is measured.

KAG's knowledge builder makes the same split: extraction produces mentions,
and a separate alignment pass links them to existing nodes before anything is
written. khub's version of that pass is deterministic, reads only the
workspace and the source, and never decides identity by resemblance.

## Terms

- **Record**: one item the source maps to an entity. Its identity is matched
  by the ladder in ingestion-conflicts.md, unchanged here.
- **Reference**: a value in a record that the source maps to a relation. It
  takes one of three forms, and the importer's source mapping says which form
  each field carries:
  - a **source id**, the id of another record of the same system, written as
    `<system>:<id>` (`facet:FUNC-007`), the same `source_id` alias a record
    carries;
  - a **khub id**, a slug or `type/slug`, when the source speaks khub ids (an
    OKF bundle exported from a khub workspace, a hand-written import file);
  - a **mention**, a name with no id behind it (`"Billing"`).
- **Target types**: the types the relation allows. A typed relation has one or
  a few; an `any` relation has all of them.

A minted slug is lowercase `a-z`, `0-9` and hyphens, so it never equals a
`<system>:<id>`. A hand-written file stem can, so a source id is never looked
up as a slug: it resolves only through `aliases`.

## The reference ladder

Each reference is resolved in pass two of a run, after every record has been
written or refused (ingestion-conflicts.md, Field values). So a reference to a
record of the same run that was written resolves on rung 1.

1. **Source id.** The alias is looked up across every type, case-insensitively,
   as lookup resolves aliases.
2. **khub id.** The ID lookup every khub verb uses: exact slug, slug
   case-insensitively, then aliases.
3. **Stub.** A source id that resolves nowhere gets a draft minted for it, so
   the edge can be written and capture is not blocked.
4. **Likeness.** Candidates scored from the workspace (Evidence, below),
   proposed and never linked.

Only rungs 1 and 2 write an edge to an entity that already exists, and both
are exact. The issue that asked for this note listed the lookups as
`(type, slug)`, then alias, then `source_id`. In khub the source id is an alias
with a namespace, and the ID lookup already tries the slug before the aliases,
so the three exact lookups are rungs 1 and 2 by form.

### Outcomes

One reference yields at most one `refused` row and at most one `identity` row.
A `refused` row carries the code, as any value that fails validation does
(ingestion-conflicts.md, Refused). An `identity` row is a proposal. Its `rung`
names where it came from: `source_id` is rung 1, `slug_collision` and
`likeness` come from a stub (rungs 3 and 4), and `unresolved` is a mention.

| Rung | Outcome | Edge | `refused` code | `identity` rung |
|---|---|---|---|---|
| 1 | one entity of a target type holds the alias | written | | |
| 1 | the alias is held by an entity of another type | no | `referential_integrity` | `source_id`, evidence `type_mismatch` |
| 1 | two entities hold the alias (`check` already fails it as `alias_conflicts`) | no | `ambiguity_error` | |
| 1 | no entity holds the alias | rung 3 | | |
| 2 | one entity of a target type | written, stored as the real slug, as `link` stores it | | |
| 2 | no entity, or one outside the target types | no | `referential_integrity` | |
| 2 | two entities (a shared alias, or a slug shared across types on an `any` relation) | no | `ambiguity_error` | |
| 3 | the source id is a record this run read and did not write | no | `record_not_written` | |
| 3 | the stub is minted | written, to the stub | | `likeness` when rung 4 finds candidates |
| 3 | the relation has more than one target type | no | `stub_type_undecided` | |
| 3 | the target type cannot mint (`id_prefix_undecided`, `path_field_missing`, `invalid_slug`) | no | that code | |
| 3 | the minted slug is taken, or is another entity's alias | no | `slug_taken` or `alias_taken` | `slug_collision` |
| 4 | a mention | no | | `unresolved`, with any candidates |

**A refused record is never stubbed.** When a record of this run was not
written (its slug collided, it mapped to no type, its id collided with
another's except for case), a reference to its source id is refused as
`record_not_written`, and the row names that record's source id. A stub there
would carry the alias the refused record should have had, and on the next run
the record would match the stub on rung 1: the duplicate the refusal stopped
would be in place, and the record's own proposal would never come back.

### The stub

- Its type is the relation's target type.
- `draft: true`, `author` naming the source, and `aliases: [<system>:<id>]`, as
  ingestion-conflicts.md writes a created record.
- Its name is the mention text the source puts beside the id, when it puts
  one, else the whole source id with its system. The name goes into the field
  `add` mints a slug from: `name` when the type declares it, else `title`.
  `facet:FUNC-008` in a type with the prefix `cap` mints `cap-facet-func-008`.
  The system stays in, so `facet:FUNC-008` and `okf:FUNC-008` never collide.
- Stubs are minted in a fixed order: records in source order, a record's
  references in relation declaration order, and a many relation's values in
  their order. When two stubs would mint the same slug, the first is written
  and the second takes the `slug_collision` row.

When the record itself arrives in a later run, it matches the stub on rung 1
of the record ladder. The stub is still a draft, so the source owns it: an
empty field is filled, and a name it was given from the id or a mention is
updated. The slug stays as minted, since khub does not rename. When the record
maps to a type other than the stub's, it is not written and gets a
`type_mismatch` proposal on every run, until the mapping or the stub is fixed.

**Where a stub shows.** A stub has an inbound edge, so it is no orphan, and it
is a draft, so `check` does not report it as incomplete. The gate does not see
it. It shows in the run's `created` rows with `"stub": true`, in the draft
counts of `khub status` and `khub query --draft`, and in the identity rows
when it has look-alikes. While its record never arrives, a run over the whole
source lists it under ingestion-conflicts.md's `absent`, with `"stub": true`:
a stub was never in the source, so it has not left it.

**Provenance** is what the stub carries and nothing more: `author` names the
source, the alias names the record, and the commit that wrote it is in git. No
base field is added for extraction provenance, because the path and the run
are in the report and the history, and a field on the entity would go stale
the first time a person edits it.

**Mentions are not minted.** A mention has no id the source will repeat, so a
stub minted for it has no key a re-run can match: the next run would mint the
same slug and collide with its own stub, or mint another. A mention is
reported as `unresolved` with its likeness candidates, and no edge is written.
The fix is in the extractor, which should emit ids, or in the workspace, where
`khub link` writes the edge a person chose.

## Evidence

**Subjects.** Likeness runs for every entity of this source the run created or
matched that is still a draft (a record created on rung 3 of the record
ladder, a record matched to its draft on rung 1, a stub minted or referenced
again), and for every mention. A subject's name is its `title`, else its
`name`, or the mention text. Before it is compared or searched, the name is
trimmed and its whitespace runs collapsed.

**Candidates.** Entities of the target types (the mapped type, for a record)
in the workspace as the run found it. An entity this run created is never a
candidate, and the subject is never its own. Two look-alikes created in one
run are each other's candidates on the next run, because both are drafts in
the workspace then and both are subjects again, as ingestion-conflicts.md
recomputes a draft's proposals on every run.

Each candidate carries the evidence that put it there, as stable codes, in
order of strength:

| Code | Meaning |
|---|---|
| `title_equal` | the candidate's `title`, else its `name`, equals the subject's name, case folded |
| `alias_equal` | one of the candidate's aliases equals the subject's name, case folded |
| `title_match` | a `--plain` search with the subject's name as the query gives the candidate a `title_match` of at least 0.8 |
| `text_match` | a `--plain` search with the name and the subject's `description` as the query gives a `match` of at least 0.8 |
| `id_equal` | one of the candidate's aliases carries the subject's source id under another system (`okf:FUNC-007` beside `facet:FUNC-007`) |

`id_equal` is the weakest because short ids (`1`, `001`) are shared by
unrelated records across systems.

**What the search scores.** The search is the one `khub search --plain` runs
today. Read its two numbers for what they are:

- They measure containment, not similarity: the share of the query's words,
  each weighted by inverse document frequency, that the candidate holds.
  `Billing` scores a `title_match` of 1.0 against `Billing svc 2`. A longer
  mention scores lower: `the billing capability` gives `Billing` 0.218.
- Every word of three or more characters matches as a prefix, so `Pay` scores
  1.0 against `Payroll reconciliation dashboard`.
- Inverse document frequency is computed over the searched types, so the same
  pair scores differently in a different workspace.
- They are computed only over the hits the search returns. The importer
  searches with a limit of 50 per query, and a candidate past it gets no
  search evidence.

That is why `title_match` and `text_match` rank below the exact codes and
never decide anything. Search filters to one type today (the core's `Type` is
a single name), so likeness needs one change in core search: a filter for
several types. It needs no index and no new scoring.

**Ranking.** Candidates sort by their strongest code, then by the number of
codes they carry, then by the BM25 `score` of the name query, ascending (it is
negative, and lower is better, as `search` reports it), with a candidate that
query did not return after every one it did, then by `id` bytewise. The report
lists at most five. The same workspace and the same source give the same list.

**Fixed in the first version.** The 0.8 thresholds, the search limit of 50 and
the cap of five are constants, not schema configuration. The goldens below are
how they get tuned; a `policy.yaml` knob waits until a tuned value differs
between workspaces.

Evidence never resolves anything. Two entities with equal titles are a
proposal, however strong, and the only ways a proposal becomes an edge or a
merge are the verbs ingestion-conflicts.md lists under Identity, plus
`khub link` for a mention.

## Two modes

KAG 0.7 reports that a lightweight build cut its token cost by 89% with
little loss. khub spends no tokens aligning, but the cost has a khub-side
analogue: every proposal is something an agent or a person reviews. So the
importer ships both modes from the start:

- **`--align exact`** skips rung 4 for references: stubs get no candidates,
  and a mention is reported as `unresolved` with none. A duplicate can be
  minted as a stub, and only the report's `created` rows say so. A later
  `full` run proposes it.
- **`--align full`**, the default, runs rung 4 for every subject.

The modes govern references only. The record ladder of
ingestion-conflicts.md, its likeness rung included, runs the same in both.

Both modes write the same edges, so switching between runs never moves an
edge. They differ in the `identity` rows, and so in the exit code: a run whose
only findings are stubs exits 0 under `exact` and 1 under `full` when a stub
has look-alikes.

## The report

The `identity` array of ingestion-conflicts.md's report carries reference
proposals too. A row about a reference adds `from` (the entity the record
became, with `id`, `type` and `slug`), `relation` (the predicate) and
`reference` (the value as the source gave it):

```json
{
  "source": {"system": "facet", "id": "UC-031", "path": "scan/use-cases.json"},
  "from": {"id": "use-case/uc-refund", "type": "use-case", "slug": "uc-refund"},
  "relation": "capability",
  "reference": "Billing",
  "rung": "unresolved",
  "written": false,
  "candidates": [
    {"id": "capability/cap-billing", "type": "capability", "slug": "cap-billing", "evidence": ["title_equal", "title_match"]}
  ]
}
```

A stub is a `created` row with `"stub": true`. A non-empty `identity` exits 1,
as ingestion-conflicts.md sets out, so a mention with no candidates still
fails the run: something was not written.

## Measuring it

The planned ingestion goldens measure type and edge precision and recall of an
extraction. Alignment joins them with four numbers per golden. A golden is a
source, a starting workspace, and for every record and reference the entity it
should land on, or "new".

- **Link precision**: of the edges rungs 1 and 2 wrote, the share that land
  where the golden says. It must be 1.0, since both rungs are exact; a golden
  where neither rung writes an edge passes it vacuously. Anything lower is a
  bug, and this number is a gate.
- **Stub precision**: of the stubs minted, the share the golden marks "new".
  A stub for something that already exists is the duplicate this whole pass is
  meant to prevent.
- **Proposal recall at five**: of the subjects the golden says are an existing
  entity but the exact rungs missed, the share whose right entity is among the
  candidates.
- **Proposal precision at one**: of the subjects that got candidates, the
  share whose first candidate is right.

Stub precision and the proposal numbers are reported, not gated, and the
proposal numbers trade against each other through the thresholds. Stub
precision and recall are measured in both modes, which says what `exact`
gives up. Proposal precision is not reported under `exact`, which proposes
nothing for references. okfm's example bundles are the first OKF v0.2 inputs.
Nothing in a golden is an LLM call: the extraction is an input, and the
goldens score khub's half.

## Across a workspace boundary

A reference is cross-workspace when the source mapping declares its system as
another khub workspace. Nothing is inferred from the reference's text. Such a
reference is never matched by likeness and never stubbed. Federation, when it
exists, resolves only exact ids across the boundary, and anything else is
reported as `unresolved` with no candidates. A guess at a boundary is a wrong
edge in someone else's graph.

## Later

- The ingestion verb itself, with ingestion-conflicts.md.
- The multi-type filter in core search.
- Tuned thresholds, and a `policy.yaml` key for them if workspaces need
  different ones.
- Likeness for a mention against entities this run created, which needs a
  second scoring pass after pass one.
- The goldens harness, and its first fixtures from okfm's bundles.
