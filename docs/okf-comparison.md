# OKF Agent Memory: feature-by-feature against khub

**Reviewed:** 2026-09-07. **Status:** research and feature proposals, not an implementation plan.

Compared the [OKF website](https://okf-memory.dev/), Google's [OKF v0.2 specification][spec], OKF source at [`d4c523e`][okf-root], and this khub checkout at `510fece`. Source and runnable probes take precedence over marketing and older design notes. Existing local edits to `internal/serve/ui/cy.js` were not part of this review.

**Verdict:** khub is the stronger foundation for schema-bound operational knowledge: typed relationships, structural queries, enforceable completeness, storage flexibility, and controlled serialization. OKF is easier to adopt as a small, loosely structured agent memory bundle, has native MCP, and supplies a useful provenance/lifecycle vocabulary. Neither establishes that retrieved assertions are true. There is no evidence that switching khub to OKF would improve task accuracy or cut its token bill by 80%.

## Feature comparison

“Absent” means absent from the reviewed implementation; proposals and advertised cloud features do not count as shipped functionality.

| Feature | khub today | OKF Agent Memory today | Assessment |
|---|---|---|---|
| Source of truth | Git-held authored files/collection rows | Git-held Markdown concepts | Same architectural advantage: inspectable, portable data; no mandatory external database. |
| Runtime | Static Go binary; bundled Go dependencies | Go binary using standard library only | Both run locally. OKF has fewer dependencies; that does not establish parser correctness. |
| Domain model | Authored ontology, policy and storage layers; resolved schema introspection | Arbitrary nonempty `type`, optional standard metadata; no authored domain contract | khub for managed knowledge; OKF for low-friction notes. |
| Presets | `firm-ops`, `build-hub` (`build-lite` is an alias); editable workspace copies | Software, coaching and books examples; generic bootstrap | khub presets encode enforceable working models. OKF examples impose less setup. |
| Attribute validation | Declared scalar types, enums, regex patterns; optional closed-schema strict mode | Checks selected OKF fields, dates and actor strings | khub substantially richer; its default still accepts undeclared extensions. |
| Capture/completeness | Missing required values can be captured; `check` gates active completeness; `draft` remains manual | Minimal mandatory concept structure; no domain completeness model | khub makes “captured” and “complete” independently observable. |
| Relationships | Declared predicates, target types/unions, cardinality, computed inverse predicates | Markdown links; prose describes meaning; derived inbound/outbound adjacency | khub supports executable relationship semantics. OKF links are convenient navigation. |
| Referential integrity | Shared write path rejects missing/ambiguous/wrong-type targets and self-links | `relate` resolves existing concepts; general body writes can contain broken links, detected afterward | khub has stronger write-time guarantees for declared edges. Body links remain navigational in khub. |
| Graph checks | Required relations, active subgraph, orphans, dangling edges, strays, declared acyclic predicates, singleton/template findings | Broken links and degree-zero orphans; no typed completeness or cycle policy | khub decisively stronger. |
| Graph queries | `neighbors`, transitive `impact`, supersession `history`, `query --has/--missing` including inverses | Adjacent IDs in search/human show; no dedicated traversal/query language | khub for dependency analysis and questions requiring multiple hops. `history` is a relation chain, not a Git log viewer. |
| Structured filtering | Type, attribute value, tag, draft/active, stale/orphan, has/missing, limit | No equivalent general query verb | khub. |
| Lexical ranking | SQLite FTS5 `bm25()` over title/body/scalar metadata | Weighted prefix term-frequency × IDF; explicitly boosts title/tags/description/ID | khub uses standard BM25; OKF has useful field weighting but its BM25 label is imprecise. Neither ranking method alone proves better relevance. |
| Search input | Raw FTS5 MATCH: phrases, Boolean operators, proximity, explicit prefixes; adjacent terms imply AND | Loose keyword search: matching any query term/prefix can score | khub for precise queries; OKF for forgiving keyword input. A real khub usability gap. |
| Search result | ID/type/slug/title/score/snippet/path, row locator when applicable | ID/type/title/description/score/matched fields/tags/inbound/outbound IDs | OKF has better explanations and adjacency cues; khub has useful excerpts and smaller output in the tested case. Neither shows a complete trust/freshness decision. |
| Unicode search | Default FTS5 Unicode tokenizer; Cyrillic probe succeeds | Tokenizer admits ASCII letters/digits only; Cyrillic probe fails | khub. This is not a claim of universal linguistic segmentation in FTS5. |
| Progressive disclosure | `query/search → get → selected graph walks`; agent composes calls | `search → show`, hierarchical indexes, agent playbooks | Both implement the pattern. Neither has a hard prompt-token budget. |
| Result boundedness | Search result count and short snippets; `get` returns full entity | Search count bounded, but full neighbor arrays; JSON show duplicates body in `raw_content` | Neither guarantees bounded complete context. khub's lack of one-call context assembly remains. |
| Provenance | `author`, dates, `resource`, typed `sources` edges; arbitrary extensions preserved | Structured source records, generation actor/time, verification events and selective source-footnote checks; metadata optional even under strict | OKF's vocabulary and dedicated diagnostics are stronger. khub can store extensions without understanding their meaning. |
| Human review protection | No authenticated human/agent write distinction; schema validity is not truth | Agent convention says respect human review; ordinary update still overwrites reviewed content | Neither offers the advertised kind of enforced review boundary. OKF can flag superseded verification afterward. |
| Lifecycle | Manual `draft`; preset statuses/supersession; age-based `stale`, Git fallback for undated files | `draft/stable/deprecated`, explicit `stale_after`, separate `--stale` validation gate | Complementary. Explicit expiry is absent from khub's stale calculation. OKF status does not filter search; lifecycle/source/review metadata requires file/library edits, beyond its CLI/MCP update fields. |
| Body contracts | Ordered required headings; per-section content findings; conditional review lenses | Free-form body; no equivalent per-type contract | khub. Body content findings are advisory, not semantic proof. |
| Formats/layouts | Per-file Markdown/JSON/YAML; folder/singleton; JSON/JSONL/YAML collections | Markdown concept files only | khub. |
| Editing fidelity | YAML-aware token splicing and byte-contract regression suite | Handwritten partial YAML parser and emitter | khub materially stronger. OKF title-only update lost a valid folded description in a reproduction. |
| Create collision handling | Existing slug refused; exclusive create for files, locked uniqueness check for rows | `create` at existing ID overwrites it | khub prevents this concrete data-loss case. |
| Delete/unlink | Dedicated commands; delete protects inbound references unless forced; link/unlink idempotent | No dedicated delete/unlink/rename; repeated relate can append duplicate prose | khub broader and safer for routine maintenance. |
| Concurrency/durability | Collection mutations lock, reread and atomically replace; ordinary entity edits still rewrite in place | Direct concept/index/log writes; no cross-process lock or atomic multi-file operation | khub better for collections, but neither has universal stale-read protection. Do not describe all khub writes as atomic. |
| Navigation bookkeeping | Explicit `reindex`, dry-run diff; init/upgrade also regenerate | Writes update immediate parent index and `log.md`; drift check compares summaries | OKF more automatic. Deep creation did not connect the complete ancestor index tree in a probe. khub's explicit rebuild is simpler but can be forgotten. |
| Machine interface | JSON by default on pipes; stable IDs/errors/exit contracts; no prompts | Explicit `--json`; six stdio MCP tools | khub is strong for shell agents; OKF reaches MCP-only hosts directly. |
| Onboarding/refresh | `init` wires agent files; skill installation separate; `upgrade`, `wire`, `install-skills` available | One bootstrap installs bundle, skill, AGENTS instructions, optional Makefile | OKF wins first-run cohesion. khub already has the underlying pieces and a broader maintenance surface. |
| Agent guidance footprint | Full skill plus schema discovery and Claude imports | Short core skill plus separately loaded capability guides | OKF is leaner in measured source bytes; actual prompt loading depends on host. |
| Graph visualization | Self-contained `viz` HTML and read-only loopback `serve` | No local graph UI in reviewed implementation; cloud visualization advertised | khub ships this now. |
| OKF interoperability | Index declares 0.1; export/import still planned; richer graph/storage needs mapping | Targets 0.2 directly; implementation has parser/conformance limitations | OKF closer to current interchange vocabulary. khub is not a drop-in v0.2 bundle producer/consumer. |
| Federation/cloud | Architecture sketch, explicitly unbuilt | Cloud beta advertised; cross-repo federation not in local implementation | No demonstrated shipped federation win on either side. |
| Public distribution | Private GitHub Packages npm channel; per-repo version pinning; macOS/Linux | Public MIT repository, public installation recipes including Go/source/Homebrew | OKF easier for outside adoption; khub's channel serves a different distribution requirement. |

khub evidence: [CLI contract](cli.md), [search implementation](../internal/search/search.go), [entity writes](../internal/entity/entity.go), [storage writes](../internal/entity/doc.go), [filesystem guarantees](../internal/fsio/atomic.go), [core fields](../presets/core/ontology.yaml), [body templates](../internal/template/), [reindex](../internal/reindex/reindex.go), [federation status](federation-design.md). OKF evidence: [command surface][okf-cli], [graph loader][okf-bundle], [search][okf-search], [types][okf-types], [validator][okf-validator], [parser/writer][okf-parser], [mutations][okf-mutate], [MCP][okf-mcp], [bootstrap][okf-bootstrap].

## What “slash token overhead by 80% through in-memory BM25 progressive disclosure” means

There are three separate mechanisms:

1. **Ranking** finds documents likely to contain relevant information.
2. **Progressive disclosure** sends a small result list, then fetches selected documents instead of including the entire corpus.
3. **In-memory execution** reduces retrieval overhead and avoids an external database service. It does not itself reduce LLM tokens.

The token saving comes from document selection. BM25 neither compresses tokens nor summarizes documents. A hand-selected file or another competent retriever would yield the same prompt reduction if it selected the same text. The difficult question is whether selection preserves all task-relevant evidence.

### Published result versus what was measured

The [published cloud report][okf-result] gives:

| Metric | Entire documentation | One selected concept |
|---|---:|---:|
| Reported input tokens | 3,034 | 603 |
| Reported time to first token | 9,226.8 ms | 8,682.0 ms |
| Reported total response time | 15.90 s | 14.93 s |
| Policy checks | 4/4 | 4/4 |

`1 − 603 / 3034 = 80.125%`, rounded to 80.1%. The reported cloud latency improvement is about **5.9%**, not 80%; both responses pass the same checks. These are published results, not cloud runs repeated for this review.

The [benchmark runner][okf-benchmark] limits the strength of the claim:

- **Input tokens are estimated from UTF-8 byte length divided by 3.9**, not counted by the model tokenizer or returned API usage. Output “tokens” count nonempty stream chunks. Neither is a billing measurement.
- It compares eight documentation files with one relevant encryption-policy file for one fixed coding task. Search query and top-1 selection are supplied by the benchmark, not discovered by an agent deciding which tools to call.
- It does not include the normal agent session's tool schemas, skills, persistent instructions, search/show transcript, query reformulation or additional retrieval turns. Nor does it measure cached-input billing.
- The search timer starts **after `LoadBundle`**. Reported focused response time adds search time but excludes bundle loading. “Under 300 µs” is not end-to-end CLI latency.
- Each invocation runs the monolith first and selected context second. There are no repeated trials, randomized ordering or uncertainty intervals in that comparison.
- The four policy checks are substring heuristics. For example, the header check also passes merely on `v2`; the nonce check accepts `12`, `96` or `noncesize`. This is not executable verification of generated code, task accuracy, or absence of hallucinations.
- Dry-run deliberately synthesizes latency. That does **not** establish that the published remote result is synthetic, but dry-run must never be presented as empirical performance.

Current source reproduces **3,058 → 627 estimated tokens (79.5%)**. Removing the current prompt's 93-byte concise-output suffix reproduces **3,034 → 603 (80.1%)** exactly. The small discrepancy is explained by prompt text, without invoking a new retrieval mechanism.

The supportable statement is: **on this sample task, selecting one relevant document instead of the entire sample corpus removes about 80% of estimated prompt text while retaining four string-checked policy indicators.** It is not evidence of an 80% reduction in complete agent cost, a general accuracy guarantee, or superiority over khub.

### The BM25 distinction

OKF's scorer uses smoothed IDF with weighted prefix counts: title ×4, tags ×3.5, description ×2.5, ID ×2, body occurrences capped at five. It has neither standard BM25 document-length normalization nor the standard saturating term-frequency formula. It also scans/tokenizes the corpus during each search rather than maintaining an inverted search index. Equal rounded scores have no deterministic secondary sort. These are implementation facts, not proof that relevance is poor on its intended small corpus. [Scorer][okf-search]

khub calls FTS5's actual `bm25()` and supports its query language. SQLite documents the [ranking function](https://www.sqlite.org/fts5.html#the_bm25_function). Replacing it with OKF's handwritten scorer would lose query semantics and Unicode behavior without demonstrated retrieval benefit.

### khub already supports the same selection pattern

I copied the eight benchmark concepts into a disposable khub workspace, changing only their `type` discriminator from `Decision` to a declared `decision` type and supplying storage configuration. This was a retrieval experiment, not a complete OKF import/conformance test.

```sh
khub search 'encryption' --limit 1
khub get decision/encryption-policy --format raw
```

This retrieves the same document. A material difference emerged:

| Query | khub result |
|---|---|
| `encrypt customer sensitive payload` | No hits: raw FTS5 terms use AND and lack OKF's implicit prefix behavior. |
| `encrypt* OR customer OR sensitive OR payload` | Encryption policy first. |
| `encryption` | Encryption policy first. |

khub can obtain the same selected-content reduction; it does not currently promise the same user-query ergonomics or automate the whole retrieval loop.

### Local latency and context-footprint checks

Built both binaries from the reviewed source using ordinary `go build`. Searched `encryption`, JSON output, limit one, same eight concept texts; discarded three warmups and measured 30 fresh subprocesses per tool with `perf_counter_ns`. These measurements include process startup, parsing, index/graph work, search and output capture, with a warm OS file cache. They are a small-corpus smoke measurement, not a scalable performance ranking or a cold-disk benchmark.

| Local measurement | OKF | khub |
|---|---:|---:|
| Median complete search | 3.50 ms | 7.36 ms |
| Min–max | 2.79–4.78 ms | 6.93–8.81 ms |
| Search-result stdout | 577 bytes | 343 bytes |

OKF has a real latency advantage here. The absolute difference is about 3.9 ms, which does not explain the published LLM token saving. There is no basis here for claiming khub is faster or that either tool meets these times on a larger corpus.

A fresh `khub init build-lite` produced 3,480 bytes of CLAUDE instructions plus 3,486 bytes of imported schema layers: **6,966 bytes** before loading the main **12,809-byte skill**. The OKF core skill was **2,896 bytes**; its six-tool MCP `tools/list` response was **3,518 bytes**. These are source/wire byte counts, not tokenizer measurements, and clients may load extra guides, cache content or discover tools lazily. Still, khub should measure and reduce its own standing context before claiming an efficiency lead. The old “~900 skill tokens versus 14K–36K MCP tokens” rationale in the CLI rules cannot serve as a measured comparison with this six-tool OKF implementation.

## Enforcement findings that affect the verdict

All probes used disposable local files. OKF's existing `go test ./...` passed; passing existing tests did not cover these cases.

| Probe | Observed behavior | Implication |
|---|---|---|
| Update a stable, human-verified concept | Write succeeds, body changes, old verification remains; later strict validation fails on older verification timestamp | Trust convention and retrospective diagnostics exist; pre-write protection does not. |
| Create at that existing ID | Write succeeds and removes previous verification/status | Search-before-write guidance cannot replace an exclusive-create check. |
| Strict validation with no generation/source/verification metadata | Passes without findings | Provenance is optional. Invalid actor syntax also remains a warning; source-footnote matching requires at least one source ID. |
| Edit title of concept with YAML folded description | Description text disappears | Partial YAML support creates a concrete editing risk. |
| Create using parent-directory ID via CLI/MCP | Writes a sibling file outside the selected bundle | Bundle selection is not a write-confinement boundary. This was a local path test, not remote exploitation. |
| Create concept named `index` | Replaces the reserved root index | Reserved paths need enforcement, not only conventions. |
| Create `deep/nested/two` | Leaf index appears; intermediate index and root route to subtree do not | Full progressive navigation is not maintained automatically. |
| Search Cyrillic word present in body | OKF emits no match; equivalent khub probe finds it | Real tokenizer limitation. |

Sources: [write path][okf-mutate], [parser/emitter][okf-parser], [MCP dispatch][okf-mcp], [search][okf-search].

This does not make khub an authorization or transaction system. Its normal file edits still use direct writes; it lacks content-revision preconditions; `author` is an assertion, not authenticated identity; schema-legal false content passes. Collection safety and schema enforcement should be credited precisely, without extending them to guarantees the code does not provide.

## Feature requests for khub

These requests preserve the schema-generic core, manual draft flag, local operation and existing CLI contracts. Existing candidate numbers are references, not claims those candidates have shipped. Priorities are relative to this comparison; efforts are rough S/M estimates, not schedules.

Recorded in [`feature-candidates.md`](feature-candidates.md) as #62 (FR1), #63 (FR2), #64–#65 (FR5), #66 (lifecycle expiry), #67 (FR7), #68 (FR8) and #69 (lower priority); FR3, FR4 and FR6 reinforce #61, #53 and #10/#60 there.

### 1. Measure and reduce standing context — P1, S

**Problem:** a selective content retriever can still waste context on its own skill and full schema imports.

**Minimum:** split detailed authoring/review instructions into supporting skill files; retain a short discovery/retrieval entrypoint. Offer a compact wiring mode that points to `schema types`, `schema show <type>` and capture cues instead of importing every schema layer. Retain existing wiring behavior until real task checks justify a default change.

**Acceptance:** measure actual loaded context with a named tokenizer/host; compare task completion, missing captures and invalid calls before/after. Require no loss of schema discovery or authoring safeguards. Do not substitute byte/4 estimates for exact token claims.

### 2. Add an opt-in forgiving search mode — P1, S–M

**Problem:** the natural keyword query that succeeds in OKF returns no khub hits on the same corpus.

**Minimum:** an explicit plain-text mode such as `search --plain`, using escaped literal terms and a documented term-combination/prefix policy over the existing FTS5 engine. Preserve raw MATCH syntax and its current behavior. Guide agents to reformulate zero-result searches. Make search-before-create an explicit skill step; do not block capture on fuzzy similarity.

**Acceptance:** the fixed sample query retrieves the policy; quotes/punctuation cannot accidentally become MATCH operators; Unicode, zero-hit and long-query cases behave predictably; existing raw-query parity remains unchanged.

### 3. Show result lifecycle and bounded graph context — P1, S

**Problem:** search is where an agent chooses evidence, but khub search omits `draft`, `stale` and supersession context available elsewhere.

**Minimum:** implement the useful portion of [candidate #61](feature-candidates.md#k-ingestion-alignment--retrieval-evaluation-kag): lifecycle flags and optional bounded per-predicate adjacency counts, plus concise description where available. Add an explicit active-only filter. Apply filters before the hit limit. Keep historical/draft retrieval available deliberately; do not silently change current defaults.

**Acceptance:** a draft or superseded hit cannot masquerade as current merely because its text ranks first; high-degree entities do not produce unbounded neighbor lists; output changes get deliberate parity review.

### 4. One-call bounded retrieval — P1, M; extends #53

**Problem:** agents currently assemble search, get and graph walks themselves, paying extra calls and risking missed dependencies.

**Minimum:** `retrieve` takes a query or explicit IDs, chooses a limited seed set, fetches selected content and optionally expands nominated predicates/directions to bounded depth. Reuse existing core functions. Return source IDs, selection reasons, lifecycle signals, truncation and omitted-entity information. Cap total entities and bytes first; if adding a token cap, name the tokenizer and account for the entire response.

**Acceptance:** deterministic output on ties, cycle-safe expansion, explicit overflow, source attribution, no silent clipping of mandatory context. When budget cannot hold needed evidence, report insufficiency so the caller can fetch more. No hidden LLM summarizer, embeddings or new server.

### 5. Review validity and review expiry — P1 design, M implementation

**Problem:** editing recently is not reviewing recently; publishing is not human verification.

**Minimum:** define optional review actor/date and explicit review-due metadata, with validity attached to the reviewed content revision. Start with preset fields and documented semantics; add generic core handling only for validity/expiry behavior fields alone cannot provide. Define which content changes invalidate review, and keep `draft` independent. Expose review due/invalidated as findings rather than blocking capture. An optional strict review gate can be used by a workspace that needs it.

**Acceptance:** changed content does not retain a valid review solely because a timestamp or `human:` label remains; routine metadata edits cannot refresh review age accidentally; CLI and direct-file edits are covered. Actor strings are not authorization—actual human-only approval remains an external Git/repository policy if required.

### 6. A credible retrieval evaluation — P1, S–M; extends #10/#60

**Problem:** khub verifies bytes and structure, not whether an agent finds sufficient evidence economically.

**Minimum:** compare full corpus, khub search/get, proposed retrieve and OKF on the same tasks. Include single-document lookup, synonym/keyword mismatch, conflicting versions, stale/draft distractors, irrelevant documents and multi-hop dependencies. Keep a cheap deterministic expected-evidence tier; run paid agent evaluation only when explicitly enabled.

**Acceptance:** report evidence recall and task success alongside complete input/output usage, cache usage when available, tool calls and latency. Time startup/scan/search/serialization separately. Repeat and randomize runs. Compile/test generated code where appropriate; never label four substring matches “100% accuracy.”

### 7. Versioned OKF v0.2 export/consume — P2, M

**Problem:** khub's format overlap is not full interchange compatibility. Its generated index declares 0.1 and `status.okf_conformant` is a projectability heuristic, not a v0.2 validator.

**Minimum:** design the mapping, then implement the already-planned export boundary. Materialize one Markdown concept per entity/row; map identity and paths; preserve typed-edge meaning through documented extension metadata and navigable links. Resolve the **`sources` collision** explicitly: khub stores entity references there; v0.2 uses source mappings with `resource` and optional citation metadata. Preserve preset-specific `status` separately rather than silently forcing it into draft/stable/deprecated. A consumer imports into a selected schema, reports losses/conflicts and never silently overwrites existing facts. Reuse #25/#26/#59.

**Acceptance:** independent v0.2 conformance tests; source/verification metadata survives supported round trips; typed relations, collection identity, unsupported fields and policy losses have an explicit report. A version-string bump alone does not satisfy this request.

### 8. Close ordinary-file write safety gaps — P1 if parallel writers are expected, M

**Problem:** khub's collection path is safer than its ordinary Markdown edit path. OKF's mistakes make this worth examining without overstating khub's guarantees.

**Minimum:** content-revision preconditions on edits, with the comparison and write protected in one shared path, plus atomic replacement where filesystem/editor behavior is deliberately accepted. Reuse collection locking/durability ideas; do not create a generic transaction framework. Keep exclusive create and inbound-reference delete protection.

**Acceptance:** two stale readers cannot silently overwrite each other's updates when using the guard; interrupted writes do not truncate the prior document; all mutation callers participate. Atomic rename by itself is not protection against lost updates. This changes current inode/byte-contract assumptions and needs an explicit compatibility decision and focused concurrency tests.

### Lower priority: generated navigation freshness — P2, S

Add a machine-readable `reindex --check` or equivalent drift gate that reuses the existing dry-run comparison and returns nonzero on stale navigation without writing. A pre-commit/CI recipe may be enough. Only make every write regenerate navigation if a measured workflow requires it; avoid copying OKF's concept/index/log partial-write chain.

## What this review does not justify adding

- **A different search engine or vector database:** current FTS5 already supplies BM25 and the required syntax. Improve query ergonomics and evaluate recall first.
- **Local MCP by default:** it is a genuine OKF compatibility advantage, but current khub rules reject it. Reconsider for an actual MCP-only/remote consumer with measured discovery cost; do not use an assumed 26-tool prompt footprint as evidence about a six-tool competitor. A stdlib stdio adapter need not technically sacrifice a static binary, but it still adds a contract to maintain.
- **A second handwritten change log:** Git plus selected projections already supplies history. Add a user-facing summary only for a demonstrated discovery need.
- **Cloud federation or automatic fact extraction as parity work:** neither is delivered by the reviewed local OKF implementation. khub's federation design remains a separate project.
- **Human-review labels as a security boundary:** metadata can guide retrieval; authentication and write authorization require actual enforcement.
- **A new generic memory preset immediately:** first test whether build-hub plus compact onboarding serves the use case. Add a smaller preset only when a concrete corpus makes the existing taxonomy burdensome.

## Verification and limitations

Built both tools. khub search/entity/integrity/reindex/schema package tests passed (cached Go results); OKF full Go suite passed. Executed disposable probes for retrieval equivalence, query syntax, Unicode, prompt arithmetic, end-to-end small-corpus timing, MCP tool-list size and wiring size. The OKF audit also executed the mutation/index/parser probes above. No paid LLM API calls were made; no production knowledge files were mutated; no claim of a complete security audit or large-scale relevance benchmark is made.

Temporary reproduction material from this review is under `/tmp/khub-okf-review-20260907/`, `/tmp/okf-audit-reproduce.py` and `/tmp/okf-audit-consolidated-results.json`; temporary paths are not durable repository dependencies. Source links below pin the reviewed competitor version so findings remain attributable after it changes.

[spec]: https://github.com/GoogleCloudPlatform/knowledge-catalog/blob/main/okf/SPEC.md
[okf-root]: https://github.com/okf-memory/okf-agent-memory/tree/d4c523ed5ce916fa207fe314851b98721421c891
[okf-cli]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/cmd/okf/main.go
[okf-bundle]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/pkg/okf/bundle.go
[okf-search]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/pkg/okf/search.go
[okf-types]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/pkg/okf/types.go
[okf-validator]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/pkg/okf/validator.go
[okf-parser]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/pkg/okf/parser.go
[okf-mutate]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/pkg/okf/mutate.go
[okf-mcp]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/cmd/okf/mcp.go
[okf-bootstrap]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/pkg/okf/bootstrap.go
[okf-benchmark]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/cmd/okf-benchmark/main.go
[okf-result]: https://github.com/okf-memory/okf-agent-memory/blob/d4c523ed5ce916fa207fe314851b98721421c891/benchmarks/results/BENCHMARK_RESULTS_openai_gpt-5.6-sol.md
