# Ingest a document with an extractor

khub turns a source document into entities in two halves, with the extractor
between them outside khub. khub ships the contract and the checker. It never
calls a model, sends nothing outward and makes no network call. The extractor
is whoever reads the source: an agent given the contract as its prompt, a
script, or a person.

```text
khub schema scaffold ──▶ extractor (outside khub) ──▶ khub schema conform ──▶ khub add
   the contract            fills the envelope          scores, writes nothing    one draft per entry
```

Every flag, payload field, error code and exit code of the two commands is
specified in [`cli.md`](cli.md#workspace) (the `schema scaffold` and `schema
conform` rows) and under [Payload shapes](cli.md#payload-shapes). This page
walks the pipeline once and says why each step is shaped the way it is.

## 1. Hand the extractor the contract

```bash
khub schema scaffold --type client --type project --format markdown > prompt.md
```

The scaffold is the resolved schema rendered as what an extractor needs: each
type's fields and stored relations, its id shape, its body sections, the
envelope to return, and a fixed set of rules. Narrow it with `--type` to the
types the source could hold. The prompt goes in front of the source.

Everything in it is walked from the resolved schema, so a schema edit changes
the contract with no code change. The fields khub or the capture step writes
are listed as supplied and never asked for. That covers the dates, the
`generated` stamp, `status` and `author`. The object-valued OKF families
(`verified`, `sources`) are left out as well, because a review event or a
provenance record is khub's to write, never an extractor's to fill.

## 2. Check what came back

```bash
khub schema conform envelope.json --format json
```

conform judges each entry as `add` and then `validate` would judge the file,
through the same code. It uses the same field validator, the same relation
resolution and the same id minting with every refusal `add` raises before it
writes. There is no second validator to drift from the first. The report keeps
three things apart, the way `validate` and `check` keep them apart:

- **errors** are what `add` would refuse or `validate` would fail, the id
  included;
- **unresolved** targets are the ones no entity answers to;
- **gaps** are the required fields the written record would leave empty.

A gap never fails the gate, because capture is never blocked. Fix every error
and every unresolved target, and run conform again until it exits 0.

## 3. Capture each entry as a draft

Each conformant entry becomes one `khub add <type> --<field> <value> …` call,
in envelope order, always with `--draft` and `--author` naming the source:

```bash
khub add client --name "Globex" --draft --author "facet:kickoff-notes" \
  --aliases "facet:CLI-007" --body-file globex.md
```

- **Order.** A target that names an earlier entry resolves to it, because
  that entry is added first. A later entry cannot be a target, so list the
  entries a record points at before it.
- **Draft and author.** [ingestion-conflicts.md](ingestion-conflicts.md#fields-ingestion-owns)
  creates every ingested record as a draft with `author` naming the source.
  The draft is the containment for content an attacker may have influenced,
  and `check` is the human review point before a person publishes it.
- **The source id.** When the source has record ids, add each one as an alias
  spelled `<system>:<id>`. That alias is how the planned importer matches the
  record when the source is read again
  ([Identity](ingestion-conflicts.md#identity)), and a later reference to it
  resolves through the alias.
- **Provenance.** Quote the `source` passage at the top of the body, under the
  first heading or as its own paragraph. The OKF `sources` list has no `add`
  spelling today.

## Where this sits against the design notes

[ingestion-conflicts.md](ingestion-conflicts.md) and
[ingestion-alignment.md](ingestion-alignment.md) describe an importer that
does not exist yet. This pipeline keeps their rules. Identity is never
guessed: conform resolves a target only by the exact rungs of the
[reference ladder](ingestion-alignment.md#the-reference-ladder) (slug, then
alias) and reports anything else as unresolved, never matching by
resemblance. A gap never blocks capture, and every ingested entity is a
draft.

It stops short of the importer in four places:

- No stubs and no likeness candidates. Those are the ladder's later rungs.
- No conflict policy for a re-run. Each run judges one envelope against the
  workspace as it is.
- No second pass. A reference to a later record in the same source resolves
  in the importer's second pass, but here it does not, because `add` refuses a
  target that does not exist yet.
- When a minted id collides with an entity the workspace holds, conform
  reports the collision as `add` would. It does not decide whether that
  entity is the same thing, which is the
  [slug-collision rung](ingestion-conflicts.md#identity).

OKF bundles have their own consume path, `khub import --okf`.
