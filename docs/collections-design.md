# Collections: the row model

Single-file collections: one `[inventory].[json|jsonl|yaml]` file holding every entity of a type as a row. This page is the contract for how rows are read and written; the graph semantics above them do not change.

The framing invariant: the `Index` (`internal/index/`) is the format seam. Every
graph semantic (draft, edges, derived inverses, orphans, required-completeness,
query, viz, reindex, search-over-meta) is defined over `(type, slug)` + meta
and **does not change**. The contract below covers only how rows are read,
written, and where the file-based definitions leak (strays, git attribution,
write atomicity).

## Row identity

- **yaml/json collections are mappings keyed by slug.** The mapping key IS the
  slug: uniqueness is syntactic; a duplicate key is a whole-document parse
  error (ruamel `DuplicateKeyError` → malformed). Mirrors SCH-002 ("maps keyed
  by name").
- **jsonl rows carry a reserved `slug` key** (same charset/length rules as
  minted slugs), popped at scan so `validate --strict` never sees it,
  re-inserted first on write. A duplicate slug (jsonl), or a duplicate mapping
  key in yaml/json, makes the whole file malformed; "first occurrence
  wins" is the rule for per-row fault isolation, which is planned but not yet
  implemented.
- `slug` and `type` become **reserved field names**: a type declaring an
  attribute or relation so named is rejected when the schema resolves (firm-ops declares neither).
- A taken slug refuses against a **fresh read under the workspace lock**,
  `slug_taken`, minted or explicit — nothing is suffixed.
- design-memo:88 amendment when this ships: the globally-unique storage key is
  the file path for `file`/`folder` layouts, the `(collection-path, slug)` pair
  for collections.

## Row `type`

Optional in a row: the collection's schema binding supplies it, injected into
meta at scan time. Present-and-mismatched = **stray row**: excluded via the
existing `filter_index(stray_nodes(...))` path, reported in `check.strays` by
locator (`clients.jsonl#acme`) instead of a bare path.

## Output

- `path` keeps its one meaning (a real, openable file), and for a row it is
  the collection file. Row records **add** `locator: "path#slug"`; file/folder
  records carry no `locator`, so existing JSON output stays byte-identical.
- `get <id> --format raw` prints the row's stored serialization (the verbatim
  jsonl line; the yaml mapping entry's block; the row rendered in canonical
  JSON), never the whole file.

## Body

Rows may carry the reserved `body` field, the same mapping the per-entity
json/yaml formats shipped (popped to the body on read, placed back on write).
`search` indexes it via `formats.fts_body`; OKF export materializes a row as
one md concept with the row's fields as frontmatter and the `body` field as the
concept body.

## Writes

One code path for every mutation, collection or per-item:

1. Lock the stable `.khub/generated/locks/workspace.lock` before scanning and
   hold it through validation and commit. Never delete lock files while a
   process may hold them; unlinking would split writers across two inodes.
2. Fresh-read the collection and re-run identity and relation gates.
3. Serialize to a unique exclusive sibling temporary, sync, publish atomically,
   and sync the parent. Existing permissions survive; new files honor umask.

No jsonl append special-case: one path, one proof. Cross-host/branch
concurrency is unchanged: git is the merge surface; a merge
conflict inside a collection either parses after resolution or is malformed
(which blocks writes: conflict markers cannot be silently round-tripped).

Per-format round-trip: yaml preserves key order + comments (full ENT-007);
jsonl keeps untouched lines byte-identical; json preserves key order with
normalized whitespace (comments are an md/yaml concern).

## Malformed (whole-file)

Any bad row, non-mapping row value, missing/invalid `slug`, or duplicate
mapping key → the **whole file** is one `malformed` entry: no rows load, write
verbs targeting the type refuse (khub never rewrites a file it cannot
round-trip), and derivative dangling reports for edges targeting that type are
suppressed and rolled into the malformed finding (the actionable error is "fix
the file", rather than 200 dangles burying it). `check` still exits non-zero.
Per-row fault isolation (bad line ≠ bad file, identity by locator) is the named
upgrade when a real corpus hits a 500-row file with one typo.

## Git attribution

- **`stale`**: a row is judged on its own `updated` only; the file's commit
  date is never attributed to a row (it would make rows never-stale: a lie, not
  an approximation). A row with no `updated` is skipped, as today.
- **`backfill`**: skips collection types and reports the skip; row-level date
  attribution (first/last commit in which the row's value changed) is planned
  but not yet implemented: it needs the per-rev row diff sketched below.
- **Round-trip, precisely.** yaml preserves key order and comments — the document
  header, comments between rows, and comments after the last row all survive a
  write. One authored key does not: a row's own `slug:` key is redundant with the
  mapping key that already names it, so it is dropped from the row on load and
  re-emitted only for jsonl, where it is the identity.
- **Row-level git attribution** (the design for row-dated `backfill`;
  not yet implemented): parse the
  collection blob at `rev` and `rev^` (a `{slug: row}` frontmatter read), diff the
  row by slug: one `LogEntry` per changed row, same two-`git show`s-per-commit
  cost as today. `gitlog.path_to_node` becomes one-path-to-many-nodes.

## Schema authoring

```yaml
repo:
  layout: collection      # third layout value
  format: jsonl           # jsonl | yaml | json (md is illegal here)
  path: repos.jsonl       # the collection FILE, workspace-relative
```

- Compatibility matrix replaces the flat whitelist: `file`/`folder` →
  {md, json, yaml}; `collection` → {json, jsonl, yaml}. `format` may be
  omitted when `path` carries the extension (derived); a disagreement between
  the two is a schema error.
- `init` creates **no** collection file: a missing or empty file means zero
  entities, never malformed (the analog of `scan_type`'s missing-directory
  return). The scaffold loop creates only the parent directory, and
  `.khub/locks/` joins `.khub/generated/` in the gitignore.
- `workspace._entity_hashes` snapshots `.jsonl` files too, so a collection
  file counts in change detection like any entity file.

