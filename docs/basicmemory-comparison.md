# Basic Memory: feature-by-feature against khub

**Status:** comparative review, 2026-08. Background reading, not a commitment.
No new candidate numbers; the actionable output is the reinforcement notes in
[`feature-candidates.md`](feature-candidates.md) (#13, #53, #57, #59).

Reviewed at [`basicmachines-co/basic-memory`](https://github.com/basicmachines-co/basic-memory)
(v0.22.x line) from the README, release notes, and the public docs site — **not**
from the source tree. Unlike the IWE review, enforcement claims below are the
project's own description of its behavior, taken at face value; nothing here
was checked against the Python.

## Why this one gets its own document

The IWE review listed Basic Memory among the surveyed MCP-native memory layers
(Graphiti, Cognee, ByteRover, Falconer) and moved on. Of that group it is the
one that shares khub's exact substrate — Markdown + YAML frontmatter on disk as
the source of truth, a relation graph derived from the files, the agent and the
human as symmetric writers of the same tree — and then takes the opposite
position on nearly every axis built on top of it:

- schema **emergent and advisory** where khub's is authored and enforced,
- relations as **freeform body wikilinks** where khub's are typed frontmatter
  edges,
- a **persisted, synced index** where khub rebuilds per invocation,
- **MCP-first** where khub evaluated and rejected an MCP server,
- a **cloud sync/teams product** where khub holds a no-egress line.

That makes it the best available control group for khub's bets: the shipped
version of what this substrate becomes when capture friction is minimized
instead of structure being enforced. Where IWE was khub's closest sibling,
Basic Memory is khub's mirror image.

## Project facts

| | |
|---|---|
| Language | Python 3.12+, `uv`; pip / Homebrew install |
| License | AGPL-3.0, public, ~3.7k stars |
| Author | Basic Machines (commercial cloud attached) |
| Index | SQLite locally; Postgres + vector store (FastEmbed embeddings, reranking) in cloud |
| Surface | MCP server, ~18 tools, stdio/HTTP; CLI is secondary (`basic-memory tool <mcp_tool>`) |
| Clients | Claude Desktop/Code, Cursor, VS Code, Codex, ChatGPT (search/fetch actions), Obsidian side-by-side |
| Cloud | basicmemory.com — $15/mo beta; rclone sync, snapshots, teams with RBAC; WorkOS/Neon/Tigris stack |
| Positioning | personal AI memory ("never re-explain your project to your AI again") |

## The three structural differences

Everything else is detail on these.

### 1. Schema is inferred and advisory; khub's is authored and enforced

Basic Memory requires no schema. Notes accumulate freeform; three MCP tools —
`schema_infer`, `schema_validate`, `schema_diff` — derive a schema *from* the
existing notes, check notes against it, and report drift. Nothing gates a
write; validation is a conversation the agent can have, not a contract the
engine holds.

khub starts where Basic Memory's schema tools point: the schema is authored
first (presets as encoded judgment), resolved into the contract every surface
introspects, and enforced — malformed values and dangling relations reject the
write, `check` gates the active subgraph on required-completeness.

The convergence is worth noting: Basic Memory shipping schema tools at all is
the emergent-structure camp conceding that structure eventually needs
governance. They arrive bottom-up (infer, then advise); khub is top-down
(author, then enforce). Both directions exist in khub's own backlog — #13
(`schema discover`) is exactly `schema_infer`, and this is now the second
shipped reference for it after IWE's profiler.

### 2. Relations live in the body; khub's live in frontmatter

Basic Memory's graph is read out of the note body:

```markdown
- [method] Pour over highlights subtle flavors    # observation: [category] fact #tag (context)
- pairs_well_with [[Chemex]]                      # relation: freeform predicate + wikilink
```

Any predicate is legal, bare links default to `links_to`, and a link to a note
that does not exist yet is a *feature* (a forward reference that resolves when
the target appears). This buys frictionless mid-conversation capture and
Obsidian compatibility, and it costs the graph its semantics: no target types,
no cardinality, no required relations, no derived inverses, no cycle
detection — none of it is expressible on a wikilink.

khub takes the same two element kinds and assigns them opposite roles: typed
frontmatter fields are the graph (predicate legality, target type, cardinality
all schema-checked at `link` time; referential integrity hard-fails on write),
and inline body links are explicitly navigational-only. Basic Memory promotes
to the graph exactly what khub demotes.

The middle altitude — observations, categorized facts *inside* the body — has
no khub analogue at all: khub structure is frontmatter, khub body is prose
(with template headings as the only body shape check). See "considered and
declined."

### 3. The index is persisted and synced; khub's is rebuilt per call

Basic Memory maintains a SQLite (cloud: Postgres) index with file watching,
bidirectional file↔DB sync, conflict resolution, and a `doctor` command —
because a persisted index can drift from the files. That machinery is also
what powers the product's headline features: cross-device sync, mobile access,
team workspaces.

khub's projection is in-memory per invocation — the index cannot be stale, and
the entire drift/conflict bug class is structurally absent — at the cost of
rebuild time per call and no cross-device story. The planned `khub build`
SQLite projection is a cache with the same derived-never-authoritative status,
not a sync system.

## Feature by feature

### Capture and integrity

Both projects say "capture is never blocked," and mean different things.
Basic Memory means it absolutely: any note, any predicate, any dangling link.
khub means it *within the contract*: a missing required field never blocks a
write (`check` reports it later), but a malformed value or a relation to a
nonexistent target rejects. Basic Memory's answer to "the target doesn't exist
yet" is an unresolved forward reference; khub's khub-shaped answer is the #59
ingestion ladder — mint the target as a draft carrying provenance, so the edge
resolves on write and `check` surfaces the draft for review.

khub's integrity loop has no counterpart: no draft/publish lifecycle, no
orphan/stale/cycle/stray findings, no graph-wide gate, no `--strict` closed
schema. Basic Memory's nearest neighbors are `doctor`/diagnostics (installation
health, not graph health) and advisory `schema_validate`.

### Traversal and retrieval

Basic Memory navigates: `build_context` walks `memory://` URLs to assemble
conversation context, `recent_activity` surfaces what changed, `search_notes`
finds entry points. khub queries: typed filters (`query`), one-hop with
predicate selection (`neighbors`), transitive closure (`impact`), supersession
chains (`history`). Basic Memory has nothing predicate-aware or transitive;
khub has nothing like `build_context`'s one-call context assembly — that gap
is #53 (`retrieve`), for which `build_context` is now a second shipped
reference next to IWE's `iwe_retrieve` (the weaker of the two: no seed/expand
separation, no token budgets).

### Search

Basic Memory: full-text plus semantic vector search (FastEmbed embeddings,
cross-encoder reranking) — the strongest capability khub lacks. khub: FTS5
BM25 with raw MATCH syntax exposed. No change to the standing rejection of
embeddings (feature-candidates, "explicitly rejected"): Basic Memory's corpus
shape — heterogeneous personal notes across every project a person touches,
queried associatively — is the shape that justifies embeddings, and it is not
khub's. An engagement workspace is small, typed, and traversable; FTS5 +
typed walks answer its questions deterministically and offline. Revisit only
if corpus scale or recall failures produce evidence.

### Agent surface

The cleanest philosophical split. Basic Memory is MCP-first — ~18 tools with
MCP behavior annotations (read-only / destructive / idempotent / open-world),
`output_format="json"` on tools, ChatGPT compatibility shims — and buys the
whole non-CLI client ecosystem with it. khub measured the standing token cost
(14–36K per turn vs ~900 for the skill), rejected an MCP server, and reaches
agents through the CLI + SKILL.md with JSON automatic on any pipe.

Basic Memory is evidence the rejection's *scope* is right: its MCP position is
what buys Cursor/ChatGPT/mobile reach, none of which khub targets. If the
deployment model ever changes enough to revisit that rejection, Basic Memory's
behavior annotations are the concrete borrowable detail — the MCP-native
rendering of the same write-safety posture IWE reaches via always-strict guards.

### Sync, cloud, collaboration

Basic Memory: per-project local/cloud routing, rclone-powered sync with
point-in-time snapshots, web/mobile apps, team workspaces with RBAC — a
commercial product layer khub deliberately does not have. khub's rules name
egress as the line (private data + untrusted entity bodies, injection
containment by provenance + draft + `check`), and its collaboration model is
git's: the workspace is a repo, multi-human means a remote and PRs, history
means commits rather than snapshots. Nothing to adopt; the two threat models
are simply different products.

### Importers

Basic Memory ships `import claude`, `import chatgpt`, `import memory-json` —
conversations in wholesale, as notes. khub's ingestion path (facet, OKF
consume, #59 alignment) is extraction-shaped: typed entities, identity
resolved down a ladder, unresolved mentions minted as drafts, gated by
`check`. The importers confirm chat history as a source class worth planning
for; the khub-shaped version runs it through the same alignment pass, not a
wholesale copy.

### Write discipline

khub's token-splice writer and ruamel-shaped emitter make every write a
minimal, review-able git diff — byte stability is a contract with a parity
suite behind it. Basic Memory has no byte contract; `edit_note` rewrites, and
nothing pins output bytes. Consistent with its model — sync and snapshots,
not commits and review, are its record — and exactly why that model would not
survive contact with khub's git-as-review-surface position.

## Full comparison

| Dimension | Basic Memory | khub |
|---|---|---|
| Schema | inferred (`schema_infer`), advisory | authored preset, enforced contract |
| Schema binding | none (notes are `note` type) | declared entity type |
| Relations | freeform predicates, body wikilinks | typed frontmatter edges, schema-checked |
| Referential integrity | forward references welcome | hard-fail on write |
| Required-completeness | none | `check` over active subgraph |
| Inverse edges | none | derived at read |
| Cycles, orphans, strays, stale | not checked | `check` / `stale` |
| Draft lifecycle | none | first-class manual flag |
| Body structure | observations (`[category]` facts) | prose + template heading check |
| Storage | md notes, one per entity | file/folder/collection × md/json/yaml |
| Index | persisted SQLite/Postgres, watched, synced | in-memory per invocation, never stale |
| Search | full-text + semantic vector + rerank | FTS5 BM25, raw MATCH |
| Context assembly | `build_context` over `memory://` | compose `search`/`get`/`neighbors` (#53 gap) |
| Agent surface | MCP-first (~18 tools, behavior hints) | CLI + skill; MCP server rejected for the current deployment model |
| Client reach | Claude, Cursor, VS Code, ChatGPT, Obsidian, mobile | CLI-capable agents |
| Multi-project | first-class, mixed local/cloud | one workspace per repo |
| Sync / cloud / teams | commercial product layer | none, by design (no egress) |
| Importers | claude / chatgpt / memory-json | facet + OKF planned, alignment-gated |
| Byte stability | none | token-splice canon, parity-pinned |
| Git | optional, not load-bearing | truth, history, review surface |
| Runtime | Python 3.12+ | single static Go binary |
| License | AGPL-3.0 public | private |

## What to grab

Nothing mints a new candidate; four existing items gain a second shipped
reference:

| # | Item | What Basic Memory adds |
|---|---|---|
| 13 | `schema discover` | `schema_infer` confirms the demand from the emergent-structure side; IWE's profiler remains the better design to copy |
| 53 | `retrieve` | `build_context` + `memory://` URLs as a minimal shipped shape; IWE's seeds/expand/budgets remains the reference |
| 57 | MCP surface shape | the MCP behavior annotations (read-only / destructive / idempotent / open-world) and per-tool `output_format` — the concrete details to adopt when the server is built |
| 59 | ingestion alignment | chat-history importers confirm the source class; khub's version routes it through the alignment ladder instead of wholesale note import |

## Considered and declined

- **Observations** (categorized facts in the body). A third structure altitude
  between frontmatter and prose. Declined: structure the engine should act on
  belongs in frontmatter where the schema, `validate`, and `query` can see it;
  a fact worth categorizing is an attribute or an entity. Adopting would
  create typed data invisible to every gate.
- **Freeform predicates and forward references.** The capture-friction win is
  real and the contract cost is the whole graph layer. khub's answer to "the
  target doesn't exist yet" is minting a draft with provenance (#59), which
  keeps referential integrity intact on write.
- **Persisted, synced index.** Buys cross-device access khub doesn't target;
  imports the drift/conflict bug class khub structurally lacks. The planned
  `build` projection stays a derived cache, never a sync system.
- **Semantic search.** Already on the explicitly-rejected list; Basic Memory
  is the strongest live counterexample and its corpus shape is still not
  khub's. Noted here so the rejection is re-arguable with evidence, not by
  reflex.
- **Cloud / teams / mobile.** Crosses the no-egress line; git remotes are
  khub's collaboration model.
- **Chat importers as wholesale note import.** The source class transfers
  (#59); the mechanism — untyped notes, no gate — does not.

## Sources

- [basicmachines-co/basic-memory](https://github.com/basicmachines-co/basic-memory) — README, releases
- [docs.basicmemory.com](https://docs.basicmemory.com/) — knowledge format, CLI reference, cloud guide
- [basic-memory on PyPI](https://pypi.org/project/basic-memory/)
- [`iwe-comparison.md`](iwe-comparison.md) — the survey this review extends
