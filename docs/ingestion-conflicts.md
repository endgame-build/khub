# Ingestion conflicts: the policy

**Status: design. Nothing on this page ships yet.** khub has no ingestion verb.
This note fixes the conflict policy before one is built, so the first importer
(facet's artifacts, an OKF bundle) is written against a contract instead of
inventing one.

Ingestion here means reading records from an outside source (a facet scan, an
OKF v0.2 bundle, a later connector) and writing them into a workspace as
entities. The source is re-read over time: the same records arrive again,
changed or not, after humans and agents have edited what the last run wrote.
The policy answers one question: what happens when the source and the
workspace disagree?

## The rule

**Ingestion never silently overwrites a non-draft value.** A disagreement in a
published entity is either left for a decision (a conflict) or settled by a
declared strategy, and the run that settles it reports it. The local value
stays until something decides otherwise.

The rule extends from field values to identity: **ingestion never merges two
entities on its own.** When an incoming record might be an entity that already
exists, the candidates are proposed with their evidence, and nothing is merged.

## What it inherits

Every invariant in the [design memo](design-memo.md) holds, and a few decide
the shape of this design.

- **`draft` is the line.** khub contains writes driven by untrusted content
  with provenance plus `draft`. An ingested entity is created `draft: true`,
  with `author` naming the source. While it stays a draft the source owns it,
  and a re-run may update it freely, including a value someone edited by hand.
  Publishing is what protects an edit. Ingestion never toggles `draft`.
- **Create is exclusive.** A new entity is published exclusively, so a file
  that appeared since the scan is never replaced, and a taken slug is refused,
  never suffixed. Ingestion keeps that publish primitive. An agent's "search
  before you write" habit is guidance, not a guarantee. okfm's probe, where
  `create` overwrote an existing id, is the failure only an exclusive create
  rules out.
- **Capture is never blocked.** A record that lacks a required field is still
  written, and `check` reports the gap. A field that fails validation is
  dropped from the write and reported, and the rest of the record is written.
  This is not what `add` and `edit` do: they refuse the whole entity on one
  bad value. Ingestion needs a per-field variant of the write path, with the
  same validation and the same publication.
- **khub never prompts.** A strategy is chosen in the schema, never asked for
  in the middle of a run.
- **Determinism.** Records are processed in the source's order (a bundle's
  paths sorted bytewise), and fields in schema declaration order. Every input
  to a decision comes from the workspace or from inside the source records, and
  never from file modification times. The same workspace and the same source
  produce the same report, byte for byte.
- **Zero per-type code.** Matching, comparing and reporting read the resolved
  schema. A per-field strategy is schema configuration, not a branch in a
  surface.
- **Nothing is persisted beside the truth.** The report is derived. Re-running
  against an unchanged source reproduces every part of it except the writes
  the earlier run already made.

## Field values

### Fields ingestion owns

Six base fields are never compared:

| Field | On create | Afterwards |
|---|---|---|
| `type` | the mapped type | never written |
| `draft` | `true` | never written |
| `author` | the source | never written |
| `created` | the run's date, as `add` writes it | never written |
| `updated` | as `add` writes it | bumped by each write, as `edit` bumps it |
| `aliases` | the record's `<system>:<id>`, plus any aliases the record carries | only ever unioned: an alias the record adds is filled, and an alias the workspace holds is never removed |

### Fields that are compared

Once a record is matched to a local entity (see Identity below), every other
declared field is compared: the domain's, `title`, `description`, `resource`,
`tags` and `body`. Undeclared keys follow the same table. They are extensions,
which khub preserves verbatim.

| Local entity | Ours | Theirs | Outcome |
|---|---|---|---|
| any | absent | present | **filled**: theirs is written |
| any | present | absent | **kept**: ingestion never deletes a field |
| any | equal | equal | **unchanged** |
| draft | present | differs | **updated**: theirs is written, and ours is reported |
| non-draft | present | differs | decided by the field's strategy, `manual` by default |

Equality compares values as khub would write them, so a list compares in
order. `body` is one value, so a one-character difference reports both bodies
in full. On create, an md entity whose record carries no body is seeded from
the type's template, as `add` seeds it.

Relations follow the same table, with one difference for a many-valued
relation. A target the source adds is **filled**, the same as `khub link`. A
target the source no longer lists is **kept**. A single-valued relation that
differs is decided like any field.

A run writes in two passes: entities first, relations second. A record may
name a target that comes later in the source order, and it resolves.

**Refused.** A value that fails validation (a target that does not resolve, an
enum, date or pattern violation, a value of the wrong type) is not written. It
is reported under `refused` with khub's error code, and is not a conflict. A
whole record is refused, with nothing written, when the source maps it to no
declared type, or when its id equals another record's id in the same run
except for case (aliases match case-insensitively, as lookup does, so the two
would claim one entity).

## Strategies

A strategy decides a disagreement in a non-draft entity:

| Strategy | Decides | Reported under |
|---|---|---|
| `manual` (default) | ours stays | `conflicts` |
| `prefer-source` | theirs is written | `overwritten` |
| `prefer-ours` | ours stays | `kept`, which never fails the run |

`prefer-ours` is how a deliberate local divergence stops being a conflict: it
moves to `kept` and no longer affects the exit code. `prefer-source` lists each
overwrite with both values, so nothing changes in a published entity without a
line saying so.

**Where the strategy is set.** The first version takes `manual` only. Per-field
configuration comes later. It belongs in `policy.yaml`, because it is a
workspace gate and not part of the domain. The policy vocabulary refuses an
`ingest:` key today, so it has to grow first:

```yaml
# .khub/policy.yaml  (later; illustrative, against build-hub)
ingest:
  conflicts:
    default: manual
    fields:
      requirement.kind: prefer-source
      capability.title: prefer-ours
```

A key naming a type or a field the schema does not declare is a schema error
when the schema resolves, like any other dangling schema reference. So an
undeclared extension key always takes `default`.

**Resolving a manual conflict** uses the existing verbs. `khub edit <id> <field>
<theirs>` accepts the source's value, which makes the two sides equal, so the
next run reports nothing. Keeping ours for good means setting that field to
`prefer-ours`. Until then the conflict is reported on every run, by design.

## Identity

Before values can be compared, an incoming record has to be matched to an
entity, or to none. The source's id is written as the alias `<system>:<id>`
(as in `facet:FUNC-007`), which is the `source_id` alias the design memo
describes. Matching is a ladder, and only the first rung merges.

1. **Source id.** The alias is looked up across every type, case-insensitively,
   as lookup resolves aliases. A hit in the mapped type is the same entity, and
   the record goes to field comparison. A hit in another type is not a match.
   The record is not written, and the report carries an identity proposal with
   the evidence `type_mismatch`.
2. **Slug collision.** No alias matches, and the slug the record would mint (or
   the id it brings) cannot be created: it is taken in that type
   (`slug_taken`), or it is another entity's alias in any type (`alias_taken`).
   This is not a match. The record is not written, and the report carries an
   identity proposal whose evidence is the refusal's code.
3. **Likeness.** No alias matches and the slug is free, but the workspace holds
   entities that resemble the record (an equal title, another system's alias,
   a strong search hit). The record is created as a draft, and the report
   proposes the candidates with their evidence. The proposal is recomputed on
   every run while that entity is a draft and still resembles them.

A proposal never merges anything, and stands until the workspace changes.

- **Settle a type mismatch**: the source's mapping or the alias is wrong. Fix
  the mapping, or move the alias to an entity of the mapped type.
- **Accept a rung-2 proposal**: add `<system>:<id>` to the candidate's aliases.
- **Accept a rung-3 proposal**: first `khub remove` the draft the run created
  (relink anything pointing at it), then add `<system>:<id>` to the candidate's
  aliases. In that order, the alias never has two claimants.
- **Reject a rung-2 proposal**: `khub add --id <other-slug> --aliases
  <system>:<id> …` creates the entity, and the next run matches it on rung 1.
- **Reject a rung-3 proposal**: publish the draft (`khub edit <id> draft false`).
  A published entity is no longer re-proposed.

`khub edit <id> aliases <list>` replaces the whole list, so pass every alias the
entity keeps plus the new one. On rung 1 the next run compares the record
against the accepted entity, which can now raise conflicts. The evidence each
rung gathers, and how strong a likeness has to be, belong to the
alignment-ladder work, which is the mechanism this rule constrains.

An alias that two entities both claim is already a `check` failure
(`alias_conflicts`), so a wrong acceptance is caught by a gate that exists.

**Records that leave the source.** Nothing is deleted. An entity holding an
alias of this source's system whose record no longer appears is listed under
`absent`, which never fails the run. That list is only meaningful for a run
over the whole source.

**Collection rows** match the same way, by alias, and a row's slug plays the
part a file's slug plays on rung 2. Report records for rows carry `locator`, as
they do in `add` and `get`.

## The report

Ingestion prints one document on stdout, in the CLI's JSON dialect. Every
record naming an entity carries `id`, `type` and `slug`, as everywhere in the
CLI:

```json
{
  "source": {"system": "facet", "path": "scan"},
  "created": [{"id": "capability/cap-billing", "type": "capability", "slug": "cap-billing", "source_id": "facet:FUNC-007", "draft": true}],
  "updated": [{"id": "use-case/uc-refund", "type": "use-case", "slug": "uc-refund", "field": "title", "ours": "Refund", "theirs": "Refund an order"}],
  "filled": [],
  "overwritten": [],
  "kept": [],
  "unchanged": 14,
  "conflicts": [
    {
      "id": "requirement/req-refund-window", "type": "requirement", "slug": "req-refund-window",
      "field": "kind",
      "ours": "constraint",
      "theirs": "business-rule",
      "source": {"system": "facet", "id": "BR-012", "path": "scan/rules.json"}
    }
  ],
  "identity": [
    {
      "source": {"system": "facet", "id": "UC-031", "path": "scan/use-cases.json"},
      "rung": "slug_collision",
      "written": false,
      "candidates": [{"id": "use-case/uc-checkout", "type": "use-case", "slug": "uc-checkout", "evidence": ["slug_taken"]}]
    }
  ],
  "refused": [],
  "absent": []
}
```

`filled`, `overwritten` and `kept` rows take the shape of an `updated` row.
`ours` and `theirs` are the values as JSON. `--dry-run` prints the same
document and writes nothing, and takes no lock.

**Exit codes.** 0 when nothing is left to decide. 1 when `conflicts`,
`identity` or `refused` is non-empty: everything else the run could write has
been written, and the report names what needs attention. That is what 1
already means for `validate` and `check`, but no write verb exits 1 today, so
the exit-code text in `docs/cli.md` (Failures) changes when this ships. 2 when
the call itself is wrong (an unreadable source, a malformed flag) and nothing
was written.

## Writes

One run holds the workspace lock for its whole scan, comparison and every
publication, so no other writer interleaves. Concurrent `add` or `edit` calls
wait for it rather than fail, so a large bundle holds them for the length of
the run. Each file publishes atomically, but a run is not one transaction
across files.

An interrupted run leaves a prefix written and prints no report. A re-run
converges: the written records compare equal or match by alias. What the
re-run cannot do is report the interrupted run's overwrites, which now compare
`unchanged`. Those writes are uncommitted changes in the git working tree,
and `git diff` is where to review them. A streamed report (one line per record
as it is written) would close that gap and is listed below.

## Later

- **Per-field strategies** in `policy.yaml`, as sketched above.
- **`prefer-newest`.** It needs a date per field. `updated` is per entity and
  per day, and khub's own writes bump it, so a local edit to one field would
  make every other field look newer and same-day ties would be common. A source
  date has to come from inside the record, never a file time. This waits for
  the three-way compare.
- **A three-way compare.** With the value the source last wrote as a base, a
  field the workspace never touched could take the source's change without a
  conflict. The base would come from git (the field's value in the last commit
  that ingestion made), never from a stored file. This needs a convention for
  identifying ingestion commits, so it waits until the two-way rule is in use.
- **A streamed report**, so an interrupted run's overwrites are not lost.
- **The alignment ladder's evidence**: what counts as a likeness on rung 3, and
  how proposals are ranked.
- **Fixtures.** okfm's example bundles are OKF v0.2 consume-path inputs. The
  first parity cases are an exclusive-create sentinel (an ingested record never
  replaces an entity that exists) and a published-entity sentinel (a re-run
  against a changed source leaves a `draft: false` value unchanged).
