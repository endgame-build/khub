# Feature Candidates from Related-Work Review

**Status:** proposal backlog — pick-and-choose input, not a commitment. Numbered
for reference; numbering is stable (do not renumber when pruning — mark items
`dropped` instead).

Compiled from a comparative review (2026-08) of three external projects against
khub's design memo and code:

| Source tag | Project | One-line characterization |
|---|---|---|
| **[bwrb]** | [3mdistal/bwrb](https://github.com/3mdistal/bwrb) (Bowerbird) | "The type system for your notes" — schema-validated Markdown vault CLI built to sit under AI agents; deterministic, no LLM, no database. khub's closest independent sibling (PKM domain). Strongest on schema migration, audit/fix gating, and agent output contracts. |
| **[oo]** | [fabio-rovai/open-ontologies](https://github.com/fabio-rovai/open-ontologies) | Rust MCP server + CLI for OWL/RDF ontology engineering; "server provides validation and scaffolding, the connected LLM does the intelligence." Strongest on schema-*evolution* governance (Terraform-style plan/lock/apply/drift) and ontology QA (lint, align, competency questions). |
| **[sem]** | [semantica-agi/semantica](https://github.com/semantica-agi/semantica) | Extraction-first knowledge-graph pipeline ("open-source Palantir for agents"): ingest → NER/relation extraction → conflict detection → KG → provenance/decision intelligence. khub's thesis inverted (bottom-up extraction into a DB vs. top-down authoring in git); useful mainly as ingestion-path and temporal-query inspiration. |
| **[mds]** | [jackchuka/mdschema](https://github.com/jackchuka/mdschema) | Go CLI validating Markdown *document structure* against a YAML schema (headings, code blocks, tables, link integrity, basic frontmatter typing). Rejected as an engine — no relations, no graph, second schema dialect — but its rule vocabulary informs body content assertions (#49). |

Full comparative analysis lives in the review session; this file records only
the actionable candidates. Effort: **S** ≈ a day or less, **M** ≈ days,
**L** ≈ a week+. Status: `proposed` unless marked.

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

## Shortlist (reviewer's recommendation)

- **Highest leverage:** the migration cluster **#1–3** (with #4 folded in) —
  the biggest named gap, with a proven same-substrate design to port.
- **Best value/effort:** **#13** (discover), **#9** (bundles).
- **Cheap-wins batch:** **#27, #30, #38, #39**.
- **Protects the IP:** **#10** (competency questions make presets testable).
- **Hold** until the workflow question is deliberately reopened: **#22/#23**.
- **G (#44–46)** is agreed and proceeds independently.
