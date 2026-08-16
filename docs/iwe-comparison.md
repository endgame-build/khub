# IWE: feature-by-feature against khub

**Status:** comparative review, 2026-08. Background reading, not a commitment.
The actionable output is items **#50–57** plus the reinforcement notes in
[`feature-candidates.md`](feature-candidates.md).

Reviewed at [`iwe-org/iwe`](https://github.com/iwe-org/iwe) commit `54074f4`
(v0.19.1, 2026-08-14), from the source tree and the 39-file `docs/` set, not
from the project's marketing pages. Claims below that describe enforcement
behavior were checked against the Rust, and the code path is cited.

## Why this one gets its own document

Of everything surveyed — MCP-native memory layers (Graphiti, Cognee, ByteRover,
Basic Memory, Falconer) and file-based knowledge tooling (Doorstop, StrictDoc,
Sphinx-Needs, Dendron, Astro content collections, LinkML, Backstage) — IWE is
the only project that independently arrived at khub's architecture:

- markdown + YAML frontmatter in git as the only source of truth,
- a derived in-memory graph, rebuilt per invocation, never stored,
- a schema that turns store conventions into machine-checked policy,
- one core library with thin, schema-introspecting adapters over it,
- and the agent as a first-class writer, not just a reader.

It also reaches the same conclusion khub's `validate` / `check` split encodes:
schema violations reject the write, graph-hygiene findings only warn. That
convergence is worth taking seriously, and so is every place the two diverge.

## Project facts

| | |
|---|---|
| Language / size | Rust, ~82k LOC across five crates |
| License | Apache-2.0 |
| Author | single maintainer (Dmytro Halichenko) |
| Version / cadence | 0.19.1, release-plz automated, commits daily |
| Crates | `liwe` core (33k) · `iwe` CLI (25k) · `iwes` LSP (13k) · `iwec` MCP (5k) · `diwe` engine/stats (6k) |
| Schema engine | external crate `schematter-validator 0.1.0` ([iwe-org/schematter](https://github.com/iwe-org/schematter)) |
| Docs | ~12k lines, including a 2,600-line formal query-language spec |

The crate split is khub's five layers under different names: `liwe` is `core/`,
and `iwe` / `iwes` / `iwec` are three thin adapters over it. Same bet, reached
independently, which is mild evidence the bet is right.

## The two structural differences

Everything else is detail on these.

### 1. IWE binds schemas by path glob; khub binds by declared type

```toml
# .iwe/config.toml
[schemas.person]
match = "people/**"
[schemas.note]
match = ["notes/**", "!notes/index"]
```

Schema selection is a gitignore-style glob over the *document key* (relative
path, no extension). A document matching no entry is unvalidated; one matching
several is validated against all of them, composing like JSON Schema `allOf`.

There is no type registry. `type:` is an ordinary frontmatter field, and
nothing connects `type: person` to `person.yaml` except a directory convention.
khub's ontology is type-shaped: the schema declares entity types, every entity
carries one, and storage layout follows from the type rather than the reverse.

The glob model buys cross-cutting overlays (one schema over `clients/acme/**`
regardless of type) at the cost of the ontology being implicit in the
filesystem. khub should not adopt it — but the overlay capability is worth
noting as the one thing type-binding cannot express.

### 2. IWE validates documents in isolation; khub validates the graph

IWE frontmatter is checked by literal JSON Schema draft 2020-12, and **external
and remote `$ref` are rejected**. A schema therefore cannot express "this field
must name an existing entity of type X." There are no typed edges, no inverse
edges, no required-relation constraints, no cycle detection, and no
graph-wide gate.

Cross-document integrity exists only as advisory reporting:
`crates/diwe/src/stats.rs:301` — *"Whole-store graph-state findings that need no
search index: orphan pages and dangling links"* — and the MCP tool descriptions
at `crates/iwec/src/lib.rs:914` say those findings *"may ride the result."* The
documentation is explicit:

> These warnings are **advisory** — nothing is ever blocked by them (schema
> validation remains the only hard reject).

khub's `check` is a gate over the active subgraph. IWE's equivalent is a nudge
attached to whatever the agent happened to write. This is khub's moat, and
nothing in IWE threatens it.

## Feature by feature

### Storage and document model

IWE stores one markdown file per document under a root marked by `.iwe/`.
Formats are `Markdown` and `Djot` only (`crates/liwe/src/model/config.rs:29`) —
**no JSON/YAML entity documents and no collection layout**. Identity is the
*key*: relative path minus extension, subdirectories allowed (`people/ada`).
Frontmatter fields prefixed `_ $ . # @` are engine-reserved, invisible to
filters, projections and schemas, and stripped on writeback.

The internal model is unusual and load-bearing: every header, paragraph, list,
item, code block, table and reference is a graph node with `next` (sibling) and
`child` pointers, and text is stored separately from structure so structural
edits never copy text. That model is what makes block-level querying possible.

**khub:** three layouts × three formats plus collections, lock-serialized with
atomic replace. khub wins on substrate flexibility; IWE wins on intra-document
granularity by a wide margin. Neither is portable to the other cheaply.

### Graph model

Two derived edge kinds, both read out of the text:

| Edge | Source | Operators |
|---|---|---|
| inclusion | a block-reference link alone on its line | `$includes`, `$includedBy` |
| reference | an ordinary inline link | `$references`, `$referencedBy` |

Walks are BFS, cycle-safe via a visited set, bounded by `maxDepth` /
`maxDistance` (omitted = direct edges; `0` = unbounded). Multiple parentage is
supported. Backlinks are derived at read time.

Edges are **untyped**. There is no vocabulary for "a `person` may have a
`works_at` edge to a `company`" — an edge's meaning is positional (inclusion is
hierarchy, reference is mention). khub's typed, schema-declared predicates with
computed inverses have no counterpart.

### Document schemas

Deeper than khub's schema layer for document *shape*, absent for graph shape.
Schemas live at `.iwe/schemas/<name>.yaml`, dialect
`https://document-schema.org/draft/2026-06/schema`.

```yaml
frontmatter:                      # literal JSON Schema 2020-12
  type: object
  required: [status]
  properties:
    status: { enum: [draft, published] }
maxTokens: 1200                   # budget for the rendered body
maxDepth: 3                       # heading nesting limit
sections:
  - header: { pattern: "^[A-Z]", maxTokens: 12 }
    maxContains: 1
    sections:
      - header: { const: Summary }
        description: every note opens with a summary
      - header: { const: Tasks }
        minContains: 0            # optional
additionalSections: false
```

It validates the section tree (ordered shapes, `minContains` / `maxContains`
occurrence counting, `allSections` for depth-wide rules, `additionalSections`
as bool-or-schema), header text (`pattern` / `const` / `enum` / `minLength` /
`maxLength` / `maxTokens`), content blocks across seven types (`paragraph`,
`bullet-list`, `ordered-list`, `code`, `quote`, `table`, `rule`, each with
`text` / `lang` constraints, `items` / `minItems` / `maxItems`, unbounded
nesting), and token budgets at document, section, block and header level.

The matching algorithm is specified formally: **ordered, sequential, greedy,
without backtracking**. A pointer walks the entry list; a section binds to the
first entry at or after the pointer whose `header` schema it satisfies; earlier
entries close forever. Binding is decided by `header` alone, so a malformed
`Tasks` section still binds to the `Tasks` entry and reports *its* missing
pieces rather than falling through to `additionalSections`. A wildcard entry
not in last position is a load error, as is a duplicate identity — both would
starve later entries.

Errors are separated by class: **exit 2** for configuration and schema problems
(bad glob, missing file, unknown keyword — unknown keywords are *rejected*,
unlike JSON Schema, so a typo cannot silently validate nothing), **exit 1** for
document violations, **exit 0** clean. Violations render as
`<key> › <breadcrumb>: <message>` with a `hint:` line taken from the nearest
`description` walking outward, and `-f json` carries `schemaPath` (a JSON
Pointer into the schema file) and the failing `keyword`.

**khub:** validates frontmatter fields from the resolved schema and checks body
templates as an ordered heading subsequence. IWE's dialect is the mature
version of what candidate #49 proposes. See "Open follow-up" below.

### Schema inference

Bare `iwe schema` is a profiler, not a validator: per frontmatter field it
reports type distribution with percentages, coverage count and percent, distinct
*enumerable* value count, and a value histogram capped at 100 distinct.
Enumerable means null/bool/number or strings matching `[A-Za-z0-9_.-/]+`; free
text counts toward coverage but is never enumerated. Scoped by the query
language, drillable with `--field`, emits JSON/YAML.

**khub:** this is candidate #13 (`khub schema discover`), shipped by someone
else. The enumerable-value rule and the coverage/distinct/histogram column set
are worth copying verbatim.

### Query language

YAML, MongoDB-shaped, shared by `find` / `count` / `update` / `delete`, by the
read-only selectors on `retrieve` / `tree` / `export`, and by the MCP
`iwe_query` tool. Marked **experimental** — "syntax, operators, defaults, and
CLI flag names may change without warning," and deliberately not exposed as a
library API.

- **Filter** — bare equality (a scalar against an array tests membership),
  `$eq $ne $gt $gte $lt $lte $in $nin $exists $all $size`, `$and $or $nor`,
  nested fields by dotted path or nesting, and no implicit type coercion
  (`priority: "3"` does not match an integer field).
- **Graph operators inside the filter** — `$key` plus the four relational
  operators, each taking a scalar key or
  `{ match: <filter>, maxDepth|minDepth|maxDistance|minDistance, $size }`.
  `match` is a full recursive filter, so an anchor can be a predicate:
  "documents included by any active project, within 5 hops."
- **`$size` cardinality** — the integrity queries live here.
  `$includedBy: { $size: 0 }` is roots, `$referencedBy: { $size: 0 }` is
  orphans, `$referencedBy: { $size: { $gte: 5 } }` is hubs, all composable with
  `match` and depth bounds.
- **Search** — `search: { lexical, fuzzy }` on `find` only. BM25 over
  title+body, skim fuzzy over title+key, RRF fusion when both are present.
  Search is a *membership* filter, not a sort variant: the result is search
  matches ∩ filter matches, ordered by relevance. IDF is fit corpus-globally,
  not per-filter.
- **Projection** — `project` (exclusive) / `addFields` (additive), with
  `$key $title $titleSlug $content $frontmatter` and the four relations.
- **Sort/limit** — one sort key in v1, ties broken by key ascending.

**khub:** `query` takes exact-match frontmatter filters today. Candidate #28
proposes an expression language; this is a shipped reference design for it, and
`$size` over relations (#55) is the piece that makes custom check rules (#37)
expressible without a second dialect.

### Block surface

No analogue anywhere else surveyed. Nine block predicate operators
(`$header $paragraph $list $quote $item $code $table $ref $hr`) plus `$section`
(tree), `$within` (interior), `$contains` (descendant), `$references`, `$text`,
`$matches`, and logical composition. One grammar consumed at three sites:
`$content` in filter (membership), `$blocks` / `$matches` / `$content` in
projection (read), and six update operators (`$replace`, `$replaceText`,
`$insertBefore`, `$insertAfter`, `$append`, `$delete`).

Notable decisions:

- **No block identity.** No IDs are minted or written to files; addressing is
  by predicate against normalized text. Two byte-identical siblings are
  indistinguishable — every mutation touches both or fails its guard.
- **Own text vs subtree.** Text predicates match a block's own text only, never
  descendants; own texts are pairwise disjoint, which the update disjointness
  rules depend on.
- **Node/tree asymmetry is deliberate.** `$delete: {$header: Goals}` dissolves
  the heading and re-levels its contents; `$delete: {$section: Goals}` removes
  the subtree. The documented rationale: deleting a header node when the
  section was meant leaves visible, recoverable content behind, while the
  reverse mistake under subtree semantics silently destroys a section.
- **Targets coalesce by extent** — selecting a section and a paragraph inside
  it yields one target, not two.

**khub:** out of scope. khub entities are frontmatter-first, and body editing
belongs to the editor. The transferable part is the *validation* vocabulary
(#49), not the mutation surface.

### `expect` guards, strict mode, atomicity

The best idea in the project.

Any block operator may carry `expect: N` or `expect: { min, max }`;
`update` / `delete` additionally take a document-level `expect` asserting how
many documents will be written. Unit operators count coalesced targets;
`$replaceText` counts selected blocks. On violation the operation fails **before
writing anything**, and the error names the actual count plus each target as
`key › section path › first line`.

Validation order is specified and runs against the original normalized
documents before any edit applies: parse-time errors → type compatibility →
`$replaceText` anchor uniqueness (`from` must occur exactly once, byte-exact) →
disjointness of applications → `expect`. Any failure aborts everything; per
document, frontmatter and block edits commit as one atomic rewrite.

Then the move worth stealing outright — **strictness is a property of the
surface, not the grammar**:

> The `expect` guards are optional in the language: a human at a terminal, with
> git behind the store, should not pay ceremony for a bulk edit they can see.
> Agents should.

CLI `update` / `delete` take `--strict`; the MCP `iwe_query` tool is **always
strict with no opt-out**. `--dry-run` is exempt and is the documented way to
learn the counts: dry-run, read, pin `expect`, re-run. The canonical agent
workflow is two operations sharing one predicate — locate with `$blocks`, then
mutate with the same predicate plus a guard.

**khub:** no equivalent. This is candidate #50, and it is the single highest-value
item in this review.

### Integrity reporting

`iwe stats` reports document/section/paragraph/line/word counts, inclusion vs
inline reference counts, per-document edge counts (17 CSV columns), path depth,
top-10 lists, **orphans** (no incoming references — `index` and `<dir>/index`
are exempt as intentional entry points), **leaves**, and network analysis.

`iwe stats similarity` finds near-duplicates behind three gates:

- **mutual** — each page must be mostly made of the other's content, so a short
  page contained in a long one does not count;
- **comparable size** — both ≥ ~50 tokens and within ~2× each other;
- **near-identical** — self-normalized BM25 ≥ 0.85, tunable with `-t`.

Forward matches are computed once per page and run concurrently.

On the MCP surface these become **stats warnings** riding create/update/query
results — one content block per finding, `<key> › <rule>: <message>`, rules
`orphan`, `dangling-link`, `similar-page` — reported **once per session**, so
the first mutation surfaces standing issues and later calls surface only
deltas, with the tool description instructing the agent to resolve them before
ending the session.

**khub:** `check` already reports `orphans`, `dangling`, `strays`, `cycles`,
`incomplete`. What khub lacks is the delivery mechanism (#52) and the
duplicate-detection rule (#54). Note khub's `index.md`-style exemption already
exists in spirit as the type-level `orphan` flag and #35's waiver list.

### Refactoring verbs

`rename` (updates all references), `delete` (with reference cleanup), `extract`
(section → new document, leaving a block reference), `inline` (the inverse),
`squash` (expand all references into one flat document), `attach` (add a
document to a target via a configured action), `normalize` (reformat everything
— link titles resynced from target titles, header levels, list numbering;
"thousands of files in under a second"). All three surfaces expose all of them,
all with `dry_run`.

**khub:** `rename` is planned (#7). `extract` / `inline` as inverse operations
have no khub analogue and would be hard to add without IWE's node-level
document model; the interesting narrow version — promote a body section into a
child entity and replace it with an edge — is listed under "considered and
declined" below. `normalize` earns less in khub because relations are
frontmatter slugs rather than titled links, so there is no link-title drift to
resync.

### Surfaces

**CLI** — `init create new update normalize retrieve find count tree rename
delete extract inline schema stats export squash attach`. Structural anchor
flags (`-k --includes --included-by --references --referenced-by --roots`,
repeatable, ANDed, with `KEY:DEPTH` suffixes) lower to graph operators, and the
lowering is documented flag by flag.

**LSP** (`iwes`) — VS Code, Neovim, Zed, Helix. Go-to-definition, backlinks,
hover preview with frontmatter stripped, link autocomplete, inlay hints showing
parent context and link counts, rename with link updates, extract/inline code
actions, format-on-save normalization, and configurable external-command
actions (`[commands.claude] run = "claude -p"` — it will shell out to an agent
to transform a section).

**MCP** (`iwec`) — stdio or Streamable HTTP. **14 tools**: `iwe_find`,
`iwe_retrieve`, `iwe_tree`, `iwe_stats`, `iwe_squash` (read); `iwe_create`,
`iwe_update`, `iwe_delete` (write); `iwe_query` (the full query surface);
`iwe_rename`, `iwe_extract`, `iwe_inline`, `iwe_normalize`, `iwe_attach`
(refactor). Three **prompts** (`explore`, `review`, `refactor`), four
**resources** (`iwe://documents/{key}`, `iwe://tree`, `iwe://stats`,
`iwe://config`), and **file watching** so editor edits reach the in-memory
graph without a restart.

`iwe_create` has a notably tight contract: `content` is the whole file, written
verbatim, nothing inserted above or below; `if_exists: "fail" | "skip"` makes
retries idempotent.

`iwe_retrieve` assembles reading context in one call: BM25 (`search`) and fuzzy
seeds, an `expand` object mapping each of the four edge kinds to a depth,
`limit` capping seeds *before* expansion, `max_documents` capping the result
*after* expansion by trimming periphery first, and output ordered seeds-first.

**khub:** the MCP server is planned. Its shape should be settled now
(#57), and `retrieve` (#53) is the read verb khub is missing — today an agent
composes `search` + `get` + `neighbors` by hand and manages its own budget.

### OKF

There is no OKF mode because "markdown, frontmatter, and links are its native
data model." `iwe init --okf` scaffolds a conformant bundle: `refs_extension =
".md"`, a root `index.md` carrying `okf_version: "0.2"`, and three schemas
(`okf.yaml`, `okf-index.yaml`, `okf-log.yaml`) wired into validation covering
all three SPEC §11 conformance rules — including the `index.md` and `log.md`
*body shapes*, which a frontmatter-only checker cannot see. Two reference
workspaces ship as validated OKF bundles.

**khub:** khub is an OKF implementation and extension by design, and targets
OKF v0.1 in the design memo while IWE scaffolds v0.2. Worth a separate check of
what moved between the two revisions; not part of this review.

## Full comparison

| Dimension | IWE | khub |
|---|---|---|
| Schema binding | path glob, gitignore syntax, composing | declared entity type |
| Frontmatter validation | JSON Schema 2020-12, complete | khub-vocabulary field types |
| Body/prose validation | deep — sections, blocks, lists, budgets, order | heading subsequence from template |
| Referential integrity | none (advisory only) | hard-fail on write |
| Required-completeness gate | none | `check` over the active subgraph |
| Typed edges / inverses | untyped links, no inverses | typed predicates, inverses derived |
| Cycles, strays | not checked | `check` |
| Draft/publish flag | none (`status` is convention) | first-class, manual |
| Storage layouts | md + djot, one file per doc | file/folder/collection × md/json/yaml |
| Query language | Mongo-style + graph + block | exact-match frontmatter filters |
| Cardinality predicates | `$size` over relations | none |
| Block/prose surgery | extensive | none (out of scope) |
| Mutation guards | `expect` + always-strict MCP | none |
| Multi-entity atomicity | validate-all-then-write | per-entity |
| Search | BM25 + fuzzy + RRF, in-memory | FTS5, in-memory per invocation |
| Context assembly | `retrieve` with seeds, expansion, budgets | compose `search`/`get`/`neighbors` |
| Duplicate detection | mutual + size-gated + BM25 | none |
| Schema inference | yes, profiling | planned (#13) |
| Schema migration | none | planned (#1–#8) |
| Presets | none (`init --okf` only) | preset directories + templates |
| Surfaces | CLI + LSP + MCP | CLI + skill (MCP planned) |
| History | git, external | git-derived (`gitlog.py`) |
| Exit codes | 0 clean / 1 violation / 2 config error | 0 / 1 / 2 (usage) |

## What to grab

New candidates, specified in [`feature-candidates.md`](feature-candidates.md):

| # | Item | Effort |
|---|---|---|
| 50 | `expect` guards + surface-level strictness | M |
| 51 | Validate-all-then-write atomicity for multi-entity operations | M |
| 52 | Session-scoped advisory integrity warnings on write results | S |
| 53 | `khub retrieve` — one-call context assembly with budgets | M |
| 54 | Near-duplicate detection as a `check` finding | M |
| 55 | Cardinality predicates over relations in `query` | S–M |
| 56 | Universal `--dry-run` on every mutating verb | S |
| 57 | MCP surface shape: prompts, resources, file watching, strictness | S |

Reinforcements to items already on the backlog, where IWE supplies a shipped
design rather than a new idea: **#13** (schema discover — copy the
enumerable-value rule and column set), **#49** (body assertions — the
document-schema dialect is the mature version), **#21** (bulk — filter plus
`$set`/`$unset` plus guards is the reference), **#28** (`--where` — the Mongo-
style filter grammar), **#18** (`--under` — generalized by `$includedBy` with
`match` and depth bounds), **#39** (exit codes — adopt the 2-means-config-error
split for `validate`/`check`), **#42** (MCP exposure filter — pairs with #50's
strictness).

## Considered and declined

- **Path-glob schema binding.** Conflicts with the type-shaped ontology and
  makes the model implicit in the filesystem. The one capability it has that
  type-binding lacks — cross-cutting overlays over a subtree — is not a problem
  khub's corpus has.
- **Block-level mutation surface.** khub entities are frontmatter-first; body
  editing is the editor's job. Only the validation vocabulary transfers (#49).
- **`extract` / `inline` refactoring.** Depends on IWE's node-level document
  model. The narrow khub version — promote a body section into a child entity
  and leave an edge — is plausible but wants the owned-child work (#14) first,
  and is not worth a candidate number until then.
- **`normalize` as a corpus verb.** khub's ruamel round-trip already delivers
  the write discipline, and khub relations are frontmatter slugs, so there is
  no link-title drift to resync.
- **An LSP surface.** A real gap in khub's coverage and a large project. Not
  now; noted so it is not re-derived.
- **Djot support.** No demand.
- **Untyped inclusion/reference edge split.** khub's typed predicates strictly
  dominate.

## Open follow-up

`schematter-validator` is a standalone crate at 0.1.0
([iwe-org/schematter](https://github.com/iwe-org/schematter)) implementing the
`document-schema.org/draft/2026-06` dialect. Before building #49 by hand, it is
worth reading — the question is whether khub adopts that dialect for body
assertions (one more schema language in the workspace, but a specified one with
an existing implementation to check against) or extends
`.khub/templates/<type>.yaml` in khub's own vocabulary. The existing rejection
of mdschema as an engine turned partly on "second schema dialect"; the same
objection applies here and should be answered deliberately rather than by
default.

## Sources

- [iwe-org/iwe](https://github.com/iwe-org/iwe) — source and `docs/`, commit `54074f4`
- [iwe-org/schematter](https://github.com/iwe-org/schematter) — the schema engine
- [marketing-workspace](https://github.com/iwe-org/marketing-workspace),
  [dev-workspace](https://github.com/iwe-org/dev-workspace) — reference OKF bundles
- [OKF SPEC](https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md) — the substrate both projects target
