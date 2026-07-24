# Concepts

**The mental model behind the commands.**

You have run the quickstart: seeded a workspace, added a few entities, walked the graph. This page is how khub thinks, in seven ideas. For the reasoning under each, see [`design-memo.md`](design-memo.md).

## Markdown is truth

One entity is one file in git: Markdown with YAML frontmatter, or one row of a single-file collection. That file is the source of truth. Audit, diff, PR review, and portability all come from git, for free. No database is ever the source of truth.

## The graph is a derived projection

khub reads the frontmatter, builds an in-memory graph on demand, answers your query, and throws the graph away. It never stores it. `khub stale` derives dates from git the same way. Because the projection is computed and never persisted, it is never stale and never the authority. Delete the index and nothing is lost; the next command rebuilds it.

## The schema is the contract

The schema declares the entity types, their attributes, and the legal relations. Every surface (the CLI and skill today, the planned MCP server) reads that schema at runtime and hardcodes no per-type knowledge. Adding a type or changing a relation is a schema edit the surfaces pick up at runtime.

## Relations are single-sided; inverses are derived

A relation is a role-named frontmatter field the schema marks as an edge. The field name is the predicate; the value is the target: `owner: dana-lee` is one edge, `owner`, pointing at `dana-lee`. You store the forward edge on one entity. The inverse (who owns this?) is computed at read time, never written. Inline links in the body are navigational only; typed edges live in frontmatter.

## validate vs check

This is the distinction that trips people up. `validate` is per-entity: is this one entity well-formed against the schema, and do its relations resolve? A missing required field does not fail validate, and it never blocks the write; capture is never blocked. `check` is graph-wide over the active (non-draft) subgraph: it enforces required-completeness and reports active-but-incomplete entities, plus orphans, dangling edges, stray files, cycles, and missing required singletons. Write freely; gate with check.

Body structure follows the same split: a type's template (`.khub/templates/<type>.yaml`) is both the scaffold `add`/`init` seed a body from and the contract `validate` holds it to — required section headings in order, extras allowed, capture never blocked.

## draft, orphan, and stale are projection properties

`draft` is a manual publish flag, default false, never inferred from completeness. A published entity can be incomplete; a draft can be complete; a draft never satisfies another entity's required relation. `orphan` (no edge in or out) and `stale` (past an `updated` threshold, dates backfilled from git) are computed on every read. `check` and `stale` gate on the same computation the reads surface; what you see is what the gate sees.

## Structural integrity is not semantic truth

khub guarantees an entity is well-formed and every relation resolves. It does not guarantee an assertion is correct. A schema-legal but false write validates cleanly; khub checks shape, whether a write conforms to the schema. The backstop is attributable git history and `git revert`.

---

Where to go next: [`design-memo.md`](design-memo.md) for the why, [`schema.md`](schema.md) to author your own types, [`cli.md`](cli.md) for the full command surface.
