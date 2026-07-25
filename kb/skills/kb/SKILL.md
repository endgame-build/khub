---
name: kb
description: Use when a repository has a knowledge/ directory with prd.md and arc42.md — the build-lite doc corpus. Covers writing and changing product requirements, constraints, architecture decisions (ADRs), components, and feature specs, and answering "what breaks if I change X", "why can't I do Y", "what is this product". Run its check before finishing any change to knowledge/ or specs/.
---

# build-lite

A typed doc corpus for one build project. Six kinds of document, held as Markdown
in git next to the code. The frontmatter is the graph: an edge is a field whose
value is another document's id.

```
knowledge/prd.md              what this product is       (one document)
knowledge/arc42.md            how it is shaped           (one document)
knowledge/requirements/       fr-NNN / cst-NNN / br-NNN   what must hold
knowledge/decisions/          ad-NNN                      why it is so
knowledge/components/         cmp-NNN                     what exists, what talks to what
specs/                        fs-NNN                      what we are building now
```

`kb` does what you cannot do for yourself. Everything else — reading, searching,
writing prose — use your own tools, they are better at it.

Run it as `kb`. If that is not on PATH, it is `python3 kb/scripts/kb.py` from the
project root; run it with no arguments for the verb list. **The verbs and flags
are khub's**, so anything you know from `khub` works here.

```bash
kb schema                  # the exact fields, enums and edges. Read this before writing frontmatter.
kb add <type> --title "..." --kind ... --status ...   # mints the id and the file
kb link <id> <predicate> <target>                     # a checked edge; unlink removes
kb neighbors <id> [--depth 3]      # edges IN and OUT, including the derived inverses
kb impact <id> [--predicate p]     # blast radius; history <id> walks supersedes
kb query [--type t] [--status active] [--missing realized_in]
kb get <id> --edges                # one entity, with what points at it
kb edit <id> <field> <value>       # one attribute; relations go through link
kb search "<text>"         # full-text, when you do not know the id
kb validate                # per-entity errors only
kb check                   # the whole corpus. Run it before you finish.
kb reindex                 # regenerate index.md; wire, stale, status also exist
```

## The loop

1. `kb add` the entity — it mints `ad-007-<slug>`, the frontmatter, and the body
   template with a hint comment under each heading. The whole vocabulary lives in
   `kb/scripts/build.schema.yaml`; `kb schema` prints it.
2. Open the file and write the prose. Replace the hint comments; keep the `##`
   headings and their order.
3. `kb link` every relation. Edit frontmatter by hand only for plain attributes.
4. `kb check`. **Errors** mean the corpus is broken — fix them before you finish.
   **Gaps** mean legal but unfinished; fix the ones your change caused.

## What earns a document

Most things do not. Before creating one, ask where it really belongs:

- Prose a human reads start to finish → a **body** section of a document that
  already exists (usually `prd.md` or `arc42.md`).
- A fact stored once and read once → an **attribute** on a document that exists.
- Anything that churns with the code (schemas, configs, test files) → leave it
  in the repo and point at it with `resource`.
- A document earns its slot only if it is **referenced by id from elsewhere,
  walked as a graph, or gated by `check`.**

There are six types and there is no seventh. If something does not fit, it is a
section of `prd.md` or `arc42.md`, or it is `requirement.kind: constraint`.

## Routing

| The question | Where it goes |
|---|---|
| What am I building right now? | `feature-spec` in `specs/`, `requirements:` → the requirements it satisfies |
| What must always hold? | `requirement`, `kind: functional` (behaviour), `constraint` (an invariant, its measurement, how it is enforced — all in the body), `business-rule` |
| Why can't I do X? | `adr` — the decision, plus `affects:` every id it constrains |
| Where does the code live? What talks to what? | `component`, `kind: service|library|external`; `depends_on` carries the wiring, including to vendors |
| What is this product? What is the roadmap, the glossary? | sections of `prd.md` |
| How is it shaped? The data model? | sections of `arc42.md` |

## Rules

- **Never invent a type, a field, or a predicate.** `kb schema` is the whole
  vocabulary; an unknown field is a `check` error, not an extension.
- **Never grep for what points at something** — the reverse edge is not in the
  file. `kb neighbors <id>` computes it, and `kb impact <id>` is the blast radius.
- **Ids are the currency.** Reference documents by id (`ad-004-postgres`) in
  frontmatter *and* in prose, never by title or path.
- **`kb add` mints ids**; never name a file yourself. Ids are
  `<prefix>-NNN-<slug>` and the prefix follows the kind (`cst-` for a
  constraint). Refer to entities by that id in prose too — `ad-004` is the handle.
- **A relation to a document that does not exist yet is rejected.** Create the
  target first, then link.
- One statement per requirement. If it needs "and", it is two requirements.
- Set `status` deliberately: an `adr` is `proposed` until someone accepts it, and
  a `feature-spec` moves `planned → active → done`. Nothing moves it for you.

## When it stops fitting

Six types is a deliberate floor, not a limit anyone forgot to raise. When a
constraint needs a named enforcement mechanism, or a second repo consumes an
interface, say so — the corpus is meant to graduate to the full khub `build-lite`
preset, and every file here is already valid there. Do not improvise the missing
type in the meantime.
