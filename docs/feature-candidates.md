# Feature Candidates from Related-Work Review

**Status:** proposal backlog — pick-and-choose input, not a commitment. Numbered
for reference; numbering is stable (do not renumber when pruning — mark items
`dropped` instead).

Compiled from comparative reviews (2026-08) of the external projects below
against khub's design memo and code:

| Source tag | Project | One-line characterization |
|---|---|---|
| **[bwrb]** | [3mdistal/bwrb](https://github.com/3mdistal/bwrb) (Bowerbird) | "The type system for your notes" — schema-validated Markdown vault CLI built to sit under AI agents; deterministic, no LLM, no database. khub's closest independent sibling (PKM domain). Strongest on schema migration, audit/fix gating, and agent output contracts. |
| **[oo]** | [fabio-rovai/open-ontologies](https://github.com/fabio-rovai/open-ontologies) | Rust MCP server + CLI for OWL/RDF ontology engineering; "server provides validation and scaffolding, the connected LLM does the intelligence." Strongest on schema-*evolution* governance (Terraform-style plan/lock/apply/drift) and ontology QA (lint, align, competency questions). |
| **[sem]** | [semantica-agi/semantica](https://github.com/semantica-agi/semantica) | Extraction-first knowledge-graph pipeline ("open-source Palantir for agents"): ingest → NER/relation extraction → conflict detection → KG → provenance/decision intelligence. khub's thesis inverted (bottom-up extraction into a DB vs. top-down authoring in git); useful mainly as ingestion-path and temporal-query inspiration. |
| **[mds]** | [jackchuka/mdschema](https://github.com/jackchuka/mdschema) | Go CLI validating Markdown *document structure* against a YAML schema (headings, code blocks, tables, link integrity, basic frontmatter typing). Rejected as an engine — no relations, no graph, second schema dialect — but its rule vocabulary informs body content assertions (#49). |
| **[og]** | [NinePts/OntoGraph](https://github.com/NinePts/OntoGraph) | Java/Spring Boot service graphing OWL ontologies to GraphML in four notations (custom, Graffoo, VOWL, UML), via Stardog and hand layout in yEd. **Dead since Jan 2019 and unbuildable** — rejected as a tool, a port, and a dependency. Contributes one visualization idea (#58), a peer-reviewed citation for khub's "domain experts don't speak OWL" premise, and a caveated OWL test corpus. Full review: [`ontograph-review.md`](ontograph-review.md). |
| **[iwe]** | [iwe-org/iwe](https://github.com/iwe-org/iwe) | Rust markdown knowledge graph with CLI + LSP + MCP over one core library. khub's closest independent sibling on architecture: markdown-in-git as truth, derived in-memory graph, schema as machine-checked policy, agent as first-class writer. Validates documents in isolation (no referential integrity, untyped edges), so it is no threat to the graph layer — but it is ahead on agent write-safety, body-shape validation, and context assembly. Full review: [`iwe-comparison.md`](iwe-comparison.md). |
| **[bm]** | [basicmachines-co/basic-memory](https://github.com/basicmachines-co/basic-memory) | Python MCP-first personal AI memory on khub's exact substrate (markdown + frontmatter as truth, derived graph, agent + human as symmetric writers) with the opposite position on every axis above it: schema inferred and advisory, relations as freeform body wikilinks, persisted synced index, commercial cloud/teams layer. No new candidates; reinforces #13, #53, #57, #59. Full review: [`basicmemory-comparison.md`](basicmemory-comparison.md). |
| **[kag]** | [OpenSPG/KAG](https://github.com/OpenSPG/KAG) | LLM+KG question-answering framework from Ant Group (paper arXiv:2409.13731): builds a mutual-indexed, schema-constrained knowledge graph from documents, answers via a logical-form solver over graph + text. khub's thesis met from the opposite direction — it *reconstructs* the structure khub *authors* — over the stack khub rejects (server, graph store, vector store, models in the loop). Rejected as runtime and dependency; contributes the ingestion alignment pass (#59), the retrieval eval tier (#60), and graph-shaped search hits (#61). Full review: [`kag-review.md`](kag-review.md). |

Full comparative analysis lives in the review session, except for **[iwe]**,
**[og]**, **[kag]**, and **[bm]**, which have written reviews at
[`iwe-comparison.md`](iwe-comparison.md),
[`ontograph-review.md`](ontograph-review.md),
[`kag-review.md`](kag-review.md), and
[`basicmemory-comparison.md`](basicmemory-comparison.md); this file records
only the actionable candidates. Effort: **S** ≈ a day or less,
**M** ≈ days, **L** ≈ a week+. Status: `proposed` unless marked.

---

## A. Schema evolution & migration

The largest named gap: today, editing `schema.yaml` (or drifting from a preset)
has zero guardrails. bwrb ships a working same-substrate migration engine;
open-ontologies contributes the blast-radius/lock framing.

1. **Schema snapshot + `khub schema diff`** [bwrb, M] — Keep
   `.khub/schema.applied.yaml`, the last-migrated snapshot; diff the current
   schema against it and report pending changes. Foundation for #2–#6.
2. **Change classification** [bwrb, M] — Classify each schema change as
   *deterministic* (add field/option, widen single→many: auto-safe),
   *non-deterministic* (remove field, narrow enum: confirm; may drop data), or
   *review-flagged* (tighten required, collapse many→single: khub cannot guess
   a value, so it names the affected entities and stops).
3. **`khub schema migrate`** [bwrb, L] — Dry-run by default; per-entity
   before→after preview (`--show-changes`); `--execute` applies to the corpus
   and **fails closed** with structured blockers when a change would strand a
   required value. Records migration history; suggests a semver bump from
   change severity (removals → major, additions → minor).
4. **Blast radius + risk on schema changes** [oo, M] — The preview reports
   which live entities become invalid/incomplete and which edges lose a legal
   predicate, with a low/medium/high risk summary. Folds into #3.
5. **Locked types/predicates** [oo, S] — A preset marks load-bearing schema
   elements as locked; a migration removing them is refused unless explicitly
   unlocked.
6. **`diff-preset` mechanics** [oo+bwrb, M] — Already on the roadmap by name;
   #1's snapshot diffing plus OO-style rename detection (name-similarity
   pairing) supply the missing design. Reports engagement↔preset drift both
   directions for the promote-back flywheel.
7. **`khub rename`, data-preserving** [bwrb+oo, M] — Already planned for
   slugs. bwrb's lesson: renames are not diffable (a rename looks like
   drop+add, and drop deletes data), so rename is its own explicit verb; any
   similarity-based rename matching must report both what it paired *and* what
   it declined to pair.
8. **`khub layout migrate`** [bwrb-inspired, M] — Per-type `layout`/`format`
   is config, but no verb changes it on a live corpus (file→collection,
   md→json). Template: bwrb `identity migrate` — validate the whole set,
   dry-run, execute under locks.

## B. Schema authoring & vocabulary

9. **Field bundles (traits)** [bwrb, M] — Named, flat, reusable field groups
   composed into types (`bundles:` + per-type `use:`), precedence
   own > bundle > base. In-house evidence: firm-ops hand-copies
   `notes_folder`/`notes_folder_id`/`crm_id` across four types.
   Deliberately *not* type inheritance — bundles only.
10. **Competency questions as preset acceptance tests** [sem+oo, S–M] —
    `competency.yaml` per preset mapping natural-language questions to the
    khub queries that answer them. Executable preset test (goldens tier),
    drift lint, and self-documentation. Two unrelated projects converged on
    this pattern independently.
11. **`khub schema docs`** [sem+oo, S] — Render the resolved schema to
    Markdown (types, fields, enums, edges, requireds, layouts). Kills the
    hand-maintained preset-doc drift class; feeds `wire`.
12. **JSON Schema for `.khub/schema.yaml`** [bwrb, S] — Emit a meta-schema
    JSON Schema (`$schema` pointer) for editor validation/autocomplete.
    Nearly free from the Pydantic meta-schema.
13. **`khub schema discover`** [bwrb, M] — Descriptive (never pass/fail)
    frontmatter census over any folder: fields, frequencies, value types,
    divergent files; with a schema loaded, drift facts (used-but-undeclared,
    declared-but-unused). The tool for designing a preset from a messy corpus
    and for pre-cutover recon — the HQ port did this by hand.
14. **Owned/colocated child entities** [bwrb, L] — `owned: true` relations:
    children live in the parent's folder, cannot be referenced by other
    entities, misplacement is a `check` finding. Directly implements the
    documented "nesting an inventory under a parent item's folder is not yet
    supported" gap, including deriving the parent edge from placement.
15. **Aliases as identity surface** [bwrb, S–M] — Schema-marked alias fields
    join slug/`source_id` in resolution: `get`/`link` targets resolve by
    alias; a real slug beats an alias on collision.
16. **Schema-declared body sections** [bwrb] — `dropped: substantially
    shipped`. Body-template validation already exists: when
    `.khub/templates/<type>.yaml` is present, `validate` requires its section
    headings in every instance body as an ordered subsequence (extras
    allowed), and `add` scaffolds from it. The borrowable remainder
    (per-section content assertions) moved to #49.
17. **Type-level retention policy** [bwrb, S] — Per-type end-of-life rules
    (e.g. meetings older than N days with no outbound edges) evaluated by
    `check` as findings; never auto-deleted.
18. **`--under <node>` scope filter** [bwrb, S] — Query filter by transitive
    containment over a designated hierarchy predicate ("everything under this
    project"). The transitive machinery exists (`impact`); this is the
    query-surface ergonomic.

## C. Authoring & write path

19. **Richer templates** [bwrb, M] — Per-type template *variants*
    (default + named), field defaults, declared prompt-fields; an agent picks
    `--template post-mortem` and gets defaults plus body scaffold in one shot.
    (Today's templates are body text only.)
20. **Fork/lineage provenance in core** [bwrb, M] — `add --fork <id>` writing
    a reserved `forked_from` edge, plus a guarded `lineage adopt` to retrofit
    provenance onto existing entities (dry-run default; cycle/duplicate
    guards). Generalizes the engineering preset's `supersedes` into a core
    mechanic.
21. **`khub bulk`** [bwrb, M] — Batch frontmatter ops with targeting
    (`--type/--where/--path`) and safety gates: `--set/--rename/--append/
    --remove`, dry-run default, `--execute`, `--limit`. The missing verb
    between `edit` (one entity) and nothing.
22. **Transition guards + `khub explain`** [bwrb, L] — Schema-declared
    preconditions on enum transitions ("project may enter `complete` only when
    all inbound `depends_on` stories are `done`"), plus a read-only `explain`
    verb reporting allowed/blocked and why. The bounded deterministic answer
    to design-memo open question #1 (operational procedures in the schema):
    no cron, no daemon, no engine.
23. **Transition effects** [bwrb, M; needs #22] — On transition, apply one
    bounded patch to a directly related entity.
24. **Spawn-on-transition (recurrence)** [bwrb, M; needs #22] — Completing an
    entity spawns a successor from a template with date offsets. Trigger is a
    field transition, not a clock. Most PKM-flavored item; listed for
    completeness.
25. **Ingestion conflict policy** [sem, S design note now] — For the planned
    facet/OKF ingestion: never silently overwrite a non-draft value; emit a
    conflict report (entity/field/ours/theirs/source) with a named
    resolution-strategy menu (manual default; prefer-newest/prefer-source as
    per-field config later). Write the design note before ingestion is built.
26. **Extraction scaffold + conformance validation** [oo, M] —
    `khub schema scaffold [--type T]` emits the resolved schema as an
    extraction prompt contract; ingestion output is validated and
    conformance-scored against it. Keeps the LLM-ingestion layer
    schema-generic; plugs into the planned goldens-eval.

## D. Query & retrieval

27. **Saved queries** [bwrb, S] — `khub query … --save-as <name>` +
    `khub run <name>`. Stable named retrievals for skills and humans.
28. **`--where` expression filters** [bwrb, M] — Small expression language
    over frontmatter (`--where "stage='won' and updated > 2026-01-01"`),
    shared by `query`/`bulk`/custom checks (#37). Today: exact-match filters.
29. **Derived fields** [bwrb, M] — Query-time computed virtual scalars
    declared in schema (bounded expressions; one-hop `all()`/`any()` over a
    relation), never written to disk; `check` flags a persisted copy. Aligned
    with "the graph is derived, never stored."
30. **Point-in-time reads: `--at <ref|date>`** [sem, S–M] — Rebuild the
    projection from a git ref and run any read verb. Semantica's headline
    `state_at()`, nearly free under the derived-graph invariant. Pair with a
    bwrb-style `--as-of` (pin "today" for reproducible date queries).
31. **Type-scoped search** [sem, S] — `khub search <text> --type <t>`: the
    precedent-lookup idiom ("have we decided something like this before?") as
    a first-class flag plus skill guidance.
32. **`khub recent`** [bwrb, S] — Recently created/updated entities from git
    dates.
33. **Valid-time on edges** [sem, deferred] — Optional schema-declared
    `valid_from`/`valid_until` on relations ("Alice was on this project until
    March"): date fields plus query semantics. Parked so it isn't designed
    out; not v1 work.

## E. Integrity, audit & fix

34. **`check --fix` with gated autofix** [bwrb, M] — Classified fixes:
    auto-safe (backfillable dates, formatting) vs. execute-gated vs.
    never-auto (anything lossy); explicit targeting required for vault-wide;
    dry-run default; minimal-diff writes (the ruamel round-trip already
    delivers the write discipline).
35. **Check-finding suppression allowlist** [oo, S] — Git-visible waivers
    with reason strings in `.khub/config.yaml` (e.g. per-entity orphan
    exemptions). Every finding is fixed or deliberately waived; the
    type-level `orphan: true` flag is this idea at type granularity.
36. **Body-mention link suggestions** [bwrb, M] — Opt-in audit mode: entity
    names mentioned in body prose but not linked as edges are suggested
    (never auto-written). Makes "inline body links are navigational only" a
    recoverable signal.
37. **Custom check rules per preset** [oo, M; needs #28] — Presets declare
    extra graph checks as expressions ("every won opportunity has an origin
    project within 30 days"), run by `check` alongside built-ins.
38. **`khub doctor`** [sem, S] — Five-second self-diagnosis: git present,
    schema parses and resolves, provenance stamp readable, locks writable,
    layout matches schema. Distinct from `validate`/`check`, which assume a
    working workspace.

## F. Agent surface & contracts

39. **Formal JSON output + exit-code contract** [bwrb, S] — A
    `docs/cli-output-contract.md`: one JSON value on stdout, typed exit
    codes, and an error DTO carrying `field`/`expected`/`suggestion` —
    machine-readable "did you mean" is agent self-correction fuel.
40. **`--receipt` mode** [bwrb, S] — JSON responses optionally echo the
    applied query, pre-limit match count, returned count, and truncation
    flag, so an agent knows what its query actually did.
41. **Skill rewrite as decision guidance** [oo+bwrb, S] — Restructure the
    khub skill: when-to-use table per verb, generate→validate→iterate loop,
    "decide the next call from the last result — not a fixed pipeline."
42. **MCP tool-exposure filter** [oo, S; with the planned MCP server] —
    Operator config restricting which verbs the server advertises (e.g. a
    read-only khub for a reviewer agent).
43. **Shell completion** [bwrb, S] — `khub completion` generation.

## G. Interop & projections — status: **agreed** (this review)

44. **`docs/rdf-mapping.md`** [oo] — Normative khub↔OWL/SHACL mapping:
    `khub:` annotation vocabulary, IRI minting, deterministic serialization,
    and the **khub profile** (the lossless-round-trip subset). Firm-ops
    Turtle render included as the golden fixture. Key decisions already
    settled: export the *resolved* schema; shared predicate IRIs with
    per-shape constraints; union edges via `owl:unionOf`/`sh:or`; OWL alone
    cannot express khub's closed-world semantics, so SHACL is mandatory;
    `acyclic` and draft-satisfaction rules ride as annotations with khub
    remaining authoritative.
45. **RDF projection into `.khub/generated/schema.ttl`** [oo] — Regenerated
    like the compiled entity models (TBox+SHACL always; ABox behind a flag).
    External RDF tooling points at `.khub/generated/` and sees every
    workspace natively. Authoring format stays YAML; RDF is derived, never a
    second source of truth.
46. **Profile-checked RDF import** [oo, phase 2] — Checker first ("is this
    Turtle in the khub profile; here is what isn't, named, never silently
    dropped"); the full Turtle→`schema.yaml` importer only if preset
    authoring actually shifts to the RDF side.

## H. Engineering hygiene (internal)

47. **Determinism audit** [oo, S] — Any output derived from unordered
    iteration (sets, networkx adjacency) is a per-run coin flip. Sweep graph
    walks and reports for sorted traversal; add a repeat-run equality test.
48. **Docs canonicality policy** [bwrb, S] — "User-facing behavior is
    canonical in one place; rationale links to it, never restates it" —
    formalize what CLAUDE.md / design-memo / cli.md already half-do.

## Addenda (post-initial-list)

49. **Body content assertions + body link integrity** [mds+bwrb, S–M] —
    Extend the existing template checker beyond heading presence/order with
    per-section content assertions declared in `.khub/templates/<type>.yaml`
    (e.g. "Data model must contain ≥1 `mermaid` code block" — a live build-lite
    arc42 case; "Steps must contain a checklist"), plus dead-relative-link
    detection in bodies. Native extension of `validate`'s body findings —
    mdschema itself stays rejected as an engine (see source table). Absorbs
    the remainder of #16.

## I. Agent write-safety and retrieval [iwe]

From the IWE review ([`iwe-comparison.md`](iwe-comparison.md)). The theme: khub
leads on the graph contract and trails on what happens *around* a write — how an
agent proves it knows what it is about to change, what it learns when the write
lands, and how it assembles context in one call instead of five.

50. **`expect` guards + surface-level strictness** [iwe, M] — Every mutating
    verb accepts `--expect N` or `--expect min:max`, asserting how many
    entities the operation will write; a mismatch fails the whole operation
    before anything is written and names the actual count plus each entity that
    matched. The borrowed insight is that **strictness belongs to the surface,
    not the grammar**: guards stay optional for a human at a terminal with git
    behind them, the CLI opts in with `--strict`, and the planned MCP server is
    *always strict with no opt-out* — an unguarded mutation is refused with the
    missing guards named. `--dry-run` is exempt and is how the count is learned:
    dry-run, read the matched set, pin `--expect`, re-run. Highest-value item in
    the IWE review; the natural gate on #21 (`bulk`) and on any MCP write verb.
51. **Validate-all-then-write atomicity** [iwe, M; pairs with #21, #34] — A
    multi-entity operation resolves every target and validates every write
    against the pre-operation state *before* touching disk, in a fixed order
    (parse → per-entity validation → referential integrity → cross-write
    conflict → `expect`), and any failure aborts the whole operation. Today
    khub's write path is per-entity, so a partial bulk edit is possible. IWE
    additionally forbids two applications from touching overlapping extents and
    reports the offending pair; khub's analogue is two writes to the same
    entity in one operation.
52. **Session-scoped integrity warnings on write results** [iwe, S] — Write
    verbs return standing `check` findings alongside their result — one line
    per finding, `<id> › <rule>: <message>` — reported **once per session**, so
    the first write surfaces the workspace's existing debt and later writes
    surface only what changed. Advisory, never blocking (khub's `validate` and
    `check` gates stay exactly as they are); the point is that an agent
    currently has to *choose* to run `check` and therefore does not. Pairs with
    #39's output contract and #40's receipts.
53. **`khub retrieve` — one-call context assembly** [iwe, M] — The read verb
    khub is missing. Today an agent composes `search` + `get` + `neighbors` by
    hand and manages its own budget. `retrieve` takes seeds (a search string, a
    filter, or explicit ids), an expansion spec mapping each predicate — or
    direction — to a depth, `--limit` capping seeds *before* expansion, and
    `--max-entities` capping the result *after* expansion by trimming periphery
    first, returning seeds in relevance order followed by the expansion. A
    token budget is the version worth arguing about: it makes the verb
    context-window-aware, and it is the only place in khub that would need a
    tokenizer.
54. **Near-duplicate detection** [iwe, M] — A `check` finding (or a `stats`
    subcommand) for entities that duplicate one another, behind IWE's three
    gates, which are what make it usable rather than noisy: **mutual** (each
    must be mostly made of the other's content, so a short entity contained in
    a long one is not reported), **comparable size** (both above a floor and
    within ~2× of each other), and **near-identical** (self-normalized BM25
    above a tunable threshold, default 0.85). Reuses the FTS5 index already
    built per invocation.
55. **Cardinality predicates over relations** [iwe, S–M; feeds #37] — Query
    support for the *count* of related entities, not just their existence:
    zero-inbound (orphans), zero-outbound (leaves), "5 or more inbound", "at
    least 2 active projects within 3 hops". IWE spells this `$size` on each
    relational operator, composable with a target filter and depth bounds. The
    payoff is that most `check` findings become expressible as ordinary
    queries, which is what custom per-preset check rules (#37) need if they are
    not to become a second dialect.
56. **Universal `--dry-run`** [iwe, S] — Every mutating verb previews. `backfill`
    and `wire` have it; `add`/`edit`/`link`/`unlink`/`remove` and anything from
    #21 or #34 should too, with one shared preview shape. Cheap on its own and
    load-bearing for #50, since dry-run is how an agent learns the count it
    must then assert.
57. **MCP surface shape** [iwe, S; with the planned MCP server] — Settle the
    shape before building it, using IWE's as the reference: tools for the read,
    write and refactor verbs; **prompts** for the recurring workflows (explore
    the graph, review an entity in context, propose a restructuring);
    **resources** for the stable reads (`khub://entity/{id}`,
    `khub://schema`, `khub://status`) so a client can subscribe rather than
    poll; **file watching** so editor and agent edits reach the in-memory index
    without a restart; and per-tool `dry_run` throughout. Composes with #42's
    exposure filter and #50's always-strict writes.

## J. Schema visualization [og]

From the OntoGraph review ([`ontograph-review.md`](ontograph-review.md)). One
candidate; the rest of that repo is rejected.

58. **`khub viz --aspect schema|instances|both`** [og, S–M; completes #11] —
    `viz` renders the *instance* graph today: entities as nodes, predicates as
    edges, per-type coloring, `--type` to narrow. There is no way to render the
    **schema** — the types, their legal predicates, which relations are
    required, cardinality, union targets. A reader can see the entities and not
    the ontology, which is backwards for a tool whose thesis is that the schema
    is the operational setup. `--aspect schema` draws the resolved schema
    (everything needed is already in `ResolvedSchema`); `--aspect instances` is
    today's behavior and stays the default; `--aspect both` is the UML-style
    combined view, types with their attributes above the instances that realize
    them. Reuses the existing Cytoscape serialization and inlined-library render
    in `core/viz.py` — the new work is the schema walk and the notation, not the
    output path.

    OntoGraph's design rationale is the part worth keeping: it generates
    separate graphs per aspect *"to reduce the number of nodes and edges on any
    single graph, and thereby reduce crowding and help focus the semantics."*
    Supporting findings from the accompanying paper: VOWL was designed for
    "casual ontology users with only little training"; graphs beat indented
    trees for holding attention and for showing overviews and multiple
    inheritance (Fu, Noy & Storey, 2013); and every aspect — classes,
    hierarchies, instances, relationships, properties — needs to be visualizable
    for an ontology to be understood (Katifori et al., 2003). Borrow the visual
    grammar for subclassing, domain/range and cardinality from Graffoo/VOWL;
    ignore the OWL constructs khub does not have.

## K. Ingestion alignment & retrieval evaluation [kag]

From the KAG review ([`kag-review.md`](kag-review.md)). The theme: KAG's
runtime is rejected whole; what transfers are decisions at khub's two open
edges — what happens between extraction and write when unstructured sources
enter, and how retrieval quality is measured and surfaced.

59. **Ingestion alignment pass — link before write** [kag, M; design note now,
    with #25/#26] — KAG's builder separates extraction from *alignment*:
    mentions are normalized and linked against existing nodes before anything
    is written, because unaligned extraction mints duplicate nodes ("knowledge
    alignment to alleviate noise" is the paper's phrase, and the stage that
    makes the rest of its pipeline usable). khub's ingestion path (facet, OKF
    consume) needs the same pass, khub-shaped: for each extracted reference,
    resolve down a ladder — exact `(type, slug)` → alias (#15) → `source_id` →
    FTS5 candidates scored against title/aliases, *proposed but never
    auto-merged* — and mint anything unresolved as a new **draft** entity
    carrying `source_id` and extraction provenance. Referential integrity
    holds on write (the target exists), capture is never blocked, identity is
    never guessed silently, and `check` surfaces the new drafts for review.
    Same-entity candidates ride #25's conflict report extended from field
    values to identity (entity/ours/theirs/evidence). Linker precision/recall
    joins type/edge precision/recall in the planned ingestion goldens-eval.
    KAG 0.7's lightweight-build result (89% token cost cut, minimal loss) is
    the supporting argument for shipping a cheap single-pass mode alongside
    the thorough one from day one.
60. **Multi-hop retrieval eval over a live corpus** [kag, S–M; extends #10] —
    KAG publishes multi-hop QA scores (EM/F1 on HotpotQA/2wiki/MuSiQue) with
    every release, which is why its claims are credible. khub's equivalent
    tier is missing: a question set over the HQ corpus whose answers require
    composing verbs across 2–3 hops ("which active projects depend on a
    component whose owner left this quarter?"), run agent-in-the-loop through
    the skill + CLI, scored fuzzily (graded key facts, LLM-judged), tracked as
    a trend, non-blocking in CI. Distinct from #10, which pins deterministic
    question↔query pairs: this tier scores the *agent's composition* of the
    surface, and is the eval that would catch a retrieval-hostile regression —
    an output-shape change that breaks verb chaining — which the parity suite,
    pinning bytes rather than usefulness, cannot see.
61. **Search hits are graph nodes** [kag, S] — khub's structural answer to
    KAG's mutual index is that a text hit *is* a node — but `search` output
    stops at `title`, `score`, `snippet`, `path`, with no `draft`/`orphan`/
    `stale` flags (docs/cli.md). Add the flags the other reads carry plus a
    one-hop digest — per-predicate out/in edge counts — so an agent picks
    which hit to walk without a round of `get`s. KAG's chunk→node pivot as one
    output-shape change; the interim step toward #53, reusing the projection
    `search` already builds per invocation. Output-shape change = deliberate
    parity re-record.

## Reinforcements to existing items

Where a reviewed project ships a working design for a candidate already on this
list. No new numbers — recorded so the design work is not redone:

| Item | Source | What it contributes |
|---|---|---|
| **#11** schema docs | [og] | The rendered schema wants a picture, not just a table — #58 is the visual half of the same artifact, and both kill the hand-maintained-preset-doc drift class. Ship them together. |
| **#44** RDF mapping memo | [og] | A peer-reviewed citation for the position the memo takes: domain experts "do not know the formal languages or logic that express ontological concepts," and pushing OWL at them "may result in errors or omissions, or in the expert becoming frustrated and losing interest entirely" (Westerinen & Tauber, *Applied Ontology*). This is the argument for RDF as a derived projection and never the authoring surface — worth quoting in the memo's opening rather than asserting the boundary unsupported. |
| **#13** schema discover | [iwe] | The output contract: per field, type distribution with percentages, coverage count/percent, distinct count, value histogram capped at 100. The *enumerable-value* rule is the good part — only null/bool/number and `[A-Za-z0-9_.-/]+` strings count toward distinct and appear in the histogram, so titles and URLs are counted but never enumerated. |
| **#49** body assertions | [iwe] | A specified dialect (`document-schema.org/draft/2026-06`) covering ordered sections, occurrence counts, header patterns, seven block types, list item shapes, and token budgets, with an ordered greedy no-backtracking matching algorithm and load-time rejection of unreachable entries. See the open follow-up in the review: adopt the dialect or extend khub's template vocabulary, but decide it deliberately. |
| **#21** bulk | [iwe] | Filter + `$set`/`$unset`, dry-run to learn counts, guards to assert them, atomic per entity. Effectively #21 + #50 + #51 as one verb. |
| **#28** `--where` filters | [iwe] | A shipped grammar to copy from: bare equality with array-membership semantics, `$eq $ne $gt $gte $lt $lte $in $nin $exists $all $size`, `$and $or $nor`, dotted paths, and no implicit type coercion. |
| **#18** `--under <node>` | [iwe] | Generalized: a relational operator taking a *filter* as its anchor plus `minDepth`/`maxDepth`, so "everything under this project" and "everything under any active project" are the same construct. |
| **#39** output contract | [iwe] | Adopt the exit-code trichotomy: `0` clean, `1` findings, `2` configuration or schema error printed to stderr *before* any entity is examined. khub currently overloads `2` as usage error; a broken `schema.yaml` and a workspace with findings should not look the same to CI. Also worth copying: violations carry a machine path into the schema plus the failing keyword. |
| **#42** MCP exposure filter | [iwe] | Pairs with #50 — "read-only khub for a reviewer agent" and "writes must carry guards" are the same policy surface. |
| **#41** skill as decision guidance | [kag] | The solver's retrieval cascade, ported from code to prose: exact first (`get`, `query --type --<field>`), then graph (`neighbors`/`impact`), then lexical (`search`), then body read — and iterate only on a *named* gap ("X unresolved"), never re-plan blind. KAG's static-vs-iterative planner split maps to "plan the calls for a closed ask; loop with reflection for an open one." |
| **#53** `retrieve` | [kag] | Third independent convergence (after IWE) on one-call context assembly: seeds, then neighborhood. KAG's mutual-index retrieval adds the ordering rationale — snippet and structure must arrive *together*, or the reader spends calls reassembling them. |
| **#57** MCP surface shape | [kag] | A counter-reference: KAG's MCP endpoint serves *answers* (the solver); khub's must serve *evidence* (the primitives) — the client is the planner. KAG 0.8's knowledge-bases-decoupled-from-apps confirms workspace-per-engagement as the right granularity. |
| **#26** extraction scaffold | [kag] | Schema-constrained extraction validated at industrial scale, against the same alternative khub rejects (schema-free openIE). The scaffold prompt derives from the resolved schema; ship a lightweight single-pass mode first (KAG 0.7: 89% cost cut, minimal loss). |
| **#25** ingestion conflict policy | [kag] | Alignment extends the conflict report from field values to *identity*: same-entity candidates are proposed with evidence, never auto-merged. Fold into the design note; #59 is the mechanism. |
| **#15** aliases as identity surface | [kag] | KAG needs a synonym/concept layer at query time because identity was never authored; khub resolves aliases at authoring time instead — #15 is KAG's alignment stage collapsed into the identity surface, and #59's second resolution rung. Raises #15's priority. |
| **#10** competency questions | [kag] | Benchmark discipline: once #60 exists, publish per-preset scores alongside the preset, KAG-style — the preset's claim to encode judgment becomes a measured claim. |
| **#39** output contract | [kag] | Reflection needs machine-actionable misses: "0 matches" plus nearest candidates is what lets an agent iterate instead of abandoning — the same error DTO as did-you-mean, doing retrieval duty. |

---

## Explicitly rejected

Recorded so the picking is complete; each conflicts with an invariant or
solves a problem khub's corpus does not have:

- Reasoning/inference engines (unauthored facts don't exist).
- Embeddings, GraphRAG, vector stores (FTS5 + traversal wins at this corpus
  size; a projection-layer swap remains possible later).
- Graph analytics: centrality, communities, link prediction.
- Persisted graph databases as store; W3C stack as *native* schema format
  (see #44–46 for the boundary version).
- Type inheritance trees (#9 bundles are the borrowable piece).
- Agent-memory stores; plugin/method registries (the schema is the extension
  point); learned suppression (#35 is the explicit version); version-storage
  subsystems (git is this).

From the IWE review, with reasons in [`iwe-comparison.md`](iwe-comparison.md):

- Path-glob schema binding (the ontology would become implicit in the
  filesystem; type-binding is the invariant).
- A block-level mutation surface for entity bodies (frontmatter-first entities;
  body editing is the editor's job — only the *validation* vocabulary transfers,
  as #49).
- `extract`/`inline` body refactoring (wants owned children, #14, first; no
  candidate number until then).
- `normalize` as a corpus verb (the ruamel round-trip is the write discipline,
  and frontmatter-slug relations have no link titles to resync).
- An LSP surface (a real gap, a large project, not now).
- Untyped inclusion/reference edges (khub's typed predicates dominate).

From the KAG review, with reasons in [`kag-review.md`](kag-review.md):

- The runtime in every form — OpenSPG server, graph store, vector store,
  Docker Compose (a store becomes truth; the static binary becomes a
  deployment).
- An in-engine solver and logical-form DSL (khub's consumer is the planner;
  the cascade survives as skill guidance, #41).
- Schema-free/openIE extraction (unauthored facts don't exist; ingestion is
  schema-constrained only).
- The concept-normalization layer (khub's taxonomy is types + enums + tags;
  noise is refused at the write gate, not absorbed semantically after the
  fact).
- Fine-tuned pipeline models (khub ships no model; the frontier agent it
  serves is the model).
- AtomicQuery-style question indexing (wants embeddings and an LLM at index
  time; #10 is the schema-level cousin).
- Predicate semantics beyond `acyclic` + declared inverses (they exist to feed
  inference, which stays rejected; khub's two are already shipped).

From the OntoGraph review, with reasons in [`ontograph-review.md`](ontograph-review.md):

- OntoGraph itself, in every form — as a dependency (Java/Spring Boot 1.5.6, both
  its artifact repositories sunset, unbuildable since ~2021), as a service (requires
  Stardog, a commercial triple store, against the no-database invariant), and as a
  port (GraphML needing hand layout in yEd is strictly worse than the self-contained
  Cytoscape HTML `viz` already emits).
- Full-OWL construct coverage as a conformance target for #45 — its testcases reach
  `owl:oneOf`, `complementOf`, `withRestrictions`, `onDatatype`, transitive and
  asymmetric properties, nested unions. Use them to check that what khub *emits*
  round-trips; never as a bar to clear (#44 rejects full OWL deliberately).

## Shortlist (reviewer's recommendation)

- **Highest leverage:** the migration cluster **#1–3** (with #4 folded in) —
  the biggest named gap, with a proven same-substrate design to port.
- **Highest leverage on the agent surface:** **#50** (guards + surface
  strictness), with **#56** as its prerequisite and **#51** as its completion.
  The one place a competitor is demonstrably ahead of khub.
- **Best value/effort:** **#13** (discover), **#9** (bundles), **#55**
  (cardinality — small, and it unblocks #37), **#58 + #11** shipped as one
  (the schema rendered as a doc and as a diagram).
- **Cheap-wins batch:** **#27, #30, #38, #39, #52, #56**.
- **Protects the IP:** **#10** (competency questions make presets testable).
- **Decide before the MCP server exists:** **#57** (surface shape), **#53**
  (`retrieve`) — both are much cheaper to design in than to retrofit.
- **Decide before ingestion exists:** **#59** (the alignment ladder, one
  design note with #25/#26) — same retrofit argument; **#60** gives the
  retrieval surface the eval tier the parity suite cannot provide.
- **Hold** until the workflow question is deliberately reopened: **#22/#23**.
- **G (#44–46)** is agreed and proceeds independently.
