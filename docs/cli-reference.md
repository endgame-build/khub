# `khub`

khub — schema-bound context management.

**Usage**:

```console
$ khub [OPTIONS] COMMAND [ARGS]...
```

**Options**:

* `-C, --workspace PATH`: Operate on this workspace instead of the working directory.
* `--agent`: Agent mode: never prompt (agents cannot use interactivity). Output format is unchanged.
* `--version`: Print the khub version and exit.
* `--help`: Show this message and exit.

**Commands**:

* `init`: Scaffold a workspace from a preset, then...
* `status`: Summarize the workspace: counts,...
* `add`: Create an entity: khub add opportunity...
* `get`: Read an entity&#x27;s frontmatter and body,...
* `edit`: Edit an entity: khub edit initech-deal...
* `link`: Add a relation: khub link initech-pov...
* `unlink`: Remove a relation: khub unlink initech-pov...
* `remove`: Remove an entity, guarded by inbound...
* `query`: Filter entities: khub query --type...
* `search`: Full-text search: khub search...
* `neighbors`: Walk one-hop neighbors: khub neighbors...
* `impact`: Compute blast radius: khub impact node-a...
* `history`: Trace supersession lineage: khub history...
* `validate`: Validate entities: khub validate [TARGET]...
* `check`: Check the active graph: completeness,...
* `stale`: List entities past the `updated`...
* `log`: Render git history at ontology altitude:...
* `reindex`: Regenerate the OKF index.md from the...
* `viz`: Render the typed graph to a self-contained...
* `backfill`: Backfill missing dates and frontmatter:...
* `wire`: Wire the workspace into agent context...
* `schema`: Introspect the active schema.

## `khub init`

Scaffold a workspace from a preset, then wire it and install the agent skill.

On a TTY, a missing preset/path launches a wizard and the wire/skill tails are
confirmed; an agent (``--agent``), a pipe, or ``--format json`` never prompts.

**Usage**:

```console
$ khub init [OPTIONS] [PRESET] [PATH]
```

**Arguments**:

* `[PRESET]`: Named preset to seed from (e.g. firm-ops).
* `[PATH]`: Target directory (default: .).

**Options**:

* `--preset-source PATH`: Where to resolve the preset if not packaged with khub.
* `--name TEXT`: Workspace name (default: the target dir name).
* `--force`: Scaffold into a non-empty target.
* `--no-wire`: Skip wiring the schema into the agent files.
* `--no-skill`: Skip installing the agent skill via npx skills.
* `--format TEXT`: text confirmation (default); json emits resolved provenance.  [default: text]
* `--help`: Show this message and exit.

## `khub status`

Summarize the workspace: counts, draft/active, orphan/stale, OKF conformance.

**Usage**:

```console
$ khub status [OPTIONS]
```

**Options**:

* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub add`

Create an entity: khub add opportunity --client initech --owner noor --stage prospect.

On a TTY, a bare ``khub add`` picks the type and walks the schema fields; an agent
passes the type and ``--field value`` pairs exactly as before.

**Usage**:

```console
$ khub add [OPTIONS] [TYPE]
```

**Arguments**:

* `[TYPE]`: The entity type to create.

**Options**:

* `--id TEXT`: Explicit slug (else minted from name/type).
* `--draft`: Mark the entity unpublished (default: active).
* `--strict`: Reject fields the schema does not declare.
* `--body TEXT`: Body prose as a string.
* `--body-file TEXT`: Read the body from a file (&#x27;-&#x27; for stdin).
* `--format TEXT`: text or json (emits the written record).  [default: text]
* `--help`: Show this message and exit.

## `khub get`

Read an entity&#x27;s frontmatter and body, optionally with derived edges.

**Usage**:

```console
$ khub get [OPTIONS] [ID]
```

**Arguments**:

* `[ID]`: A bare slug, or type/slug on ambiguity.

**Options**:

* `--edges`: Include stored and derived edges.
* `--format TEXT`: json, table, raw, or text (Rich on a TTY).  [default: text]
* `--help`: Show this message and exit.

## `khub edit`

Edit an entity: khub edit initech-deal stage proposal-sent  (or --field value).

On a TTY, a bare ``khub edit`` picks the entity, then the field, then its value; an
agent passes the id and the field/value exactly as before.

**Usage**:

```console
$ khub edit [OPTIONS] [ID]
```

**Arguments**:

* `[ID]`: A bare slug, or type/slug on ambiguity.

**Options**:

* `--strict`: Reject fields the schema does not declare.
* `--body TEXT`: Replace the body with this string (&#x27;&#x27; clears it).
* `--body-file TEXT`: Replace the body from a file (&#x27;-&#x27; for stdin).
* `--format TEXT`: text or json (emits the updated record).  [default: text]
* `--help`: Show this message and exit.

## `khub link`

Add a relation: khub link initech-pov partner northwind.

**Usage**:

```console
$ khub link [OPTIONS] [ID] [PREDICATE] [TARGET]
```

**Arguments**:

* `[ID]`
* `[PREDICATE]`
* `[TARGET]`

**Options**:

* `--help`: Show this message and exit.

## `khub unlink`

Remove a relation: khub unlink initech-pov partner northwind.

**Usage**:

```console
$ khub unlink [OPTIONS] [ID] [PREDICATE] [TARGET]
```

**Arguments**:

* `[ID]`
* `[PREDICATE]`
* `[TARGET]`

**Options**:

* `--help`: Show this message and exit.

## `khub remove`

Remove an entity, guarded by inbound edges: khub remove old-fragment [--force].

**Usage**:

```console
$ khub remove [OPTIONS] [ID]
```

**Arguments**:

* `[ID]`: A bare slug, or type/slug on ambiguity.

**Options**:

* `--force`: Delete despite inbound edges (leaves them dangling).
* `--help`: Show this message and exit.

## `khub query`

Filter entities: khub query --type opportunity --stage prospect --format json.

**Usage**:

```console
$ khub query [OPTIONS]
```

**Options**:

* `--type TEXT`: Restrict to one entity type.
* `--tag TEXT`: Keep entities carrying this tag.
* `--has TEXT`: Keep entities with a resolvable edge for the predicate.
* `--missing TEXT`: Keep entities lacking a resolvable edge (gap finder).
* `--orphan`: Keep only orphan (edge-less) entities.
* `--stale`: Keep only stale entities.
* `--draft`: Isolate drafts.
* `--active`: Exclude drafts.
* `--limit INTEGER`: Cap the returned set.
* `--format TEXT`: text (Rich table on a TTY), json, or ids.  [default: text]
* `--help`: Show this message and exit.

## `khub search`

Full-text search: khub search modernization --type transcript --format json.

**Usage**:

```console
$ khub search [OPTIONS] TEXT
```

**Arguments**:

* `TEXT`: FTS5 MATCH text: terms, &quot;phrases&quot;, OR, NEAR, prefix*.  [required]

**Options**:

* `--type TEXT`: Restrict to one entity type.
* `--limit INTEGER`: Cap the returned set.  [default: 20]
* `--format TEXT`: text (Rich table on a TTY), json, or ids.  [default: text]
* `--help`: Show this message and exit.

## `khub neighbors`

Walk one-hop neighbors: khub neighbors initech-pov [--predicate client --in].

**Usage**:

```console
$ khub neighbors [OPTIONS] ID
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.  [required]

**Options**:

* `--predicate TEXT`: Restrict adjacency to one predicate.
* `--in`: Inbound edges only (incl. derived inverses).
* `--out`: Outbound (stored) edges only.
* `--depth INTEGER`: Bounded multi-hop adjacency over all predicates.  [default: 1]
* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub impact`

Compute blast radius: khub impact node-a [--reverse] [--predicate &lt;p&gt;].

**Usage**:

```console
$ khub impact [OPTIONS] ID
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.  [required]

**Options**:

* `--predicate TEXT`: The edge to walk the closure over.  [default: depends_on]
* `--reverse`: Walk ancestors (what reaches this node).
* `--format TEXT`: tree (depth-marked, the TTY default) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub history`

Trace supersession lineage: khub history decision-0012 [--limit 3].

**Usage**:

```console
$ khub history [OPTIONS] ID
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.  [required]

**Options**:

* `--predicate TEXT`: The self-referential edge to follow.  [default: supersedes]
* `--limit INTEGER`: Cap to the N most recent links.
* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub validate`

Validate entities: khub validate [TARGET] [--strict] [--fix].

**Usage**:

```console
$ khub validate [OPTIONS] [TARGET]
```

**Arguments**:

* `[TARGET]`: A type or type/slug; default: all.

**Options**:

* `--strict`: Close the schema: reject undeclared keys.
* `--fix`: Backfill a missing `updated` from git (v1 scope).
* `--format TEXT`: text (Rich on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub check`

Check the active graph: completeness, orphans, dangling edges, strays, cycles.

**Usage**:

```console
$ khub check [OPTIONS]
```

**Options**:

* `--strict`: Fail the gate on orphans too (default: informational).
* `--format TEXT`: text (Rich on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub stale`

List entities past the `updated` threshold, oldest first: khub stale [--days N].

**Usage**:

```console
$ khub stale [OPTIONS]
```

**Options**:

* `--days INTEGER`: Staleness threshold in days; default: the workspace&#x27;s stale_days.
* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub log`

Render git history at ontology altitude: khub log [ID] [--limit N] [--since DATE].

**Usage**:

```console
$ khub log [OPTIONS] [ID]
```

**Arguments**:

* `[ID]`: A bare slug or type/slug; default: all.

**Options**:

* `--limit INTEGER`: Cap the number of rendered commits.
* `--since TEXT`: Only commits on/after this date.
* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub reindex`

Regenerate the OKF index.md from the graph: khub reindex [--dry-run].

**Usage**:

```console
$ khub reindex [OPTIONS]
```

**Options**:

* `--dry-run`: Print the diff against the current index.md and write nothing.
* `--help`: Show this message and exit.

## `khub viz`

Render the typed graph to a self-contained Cytoscape HTML: khub viz [--out F] [--open] [--type T].

**Usage**:

```console
$ khub viz [OPTIONS]
```

**Options**:

* `--out TEXT`: Output path for the HTML (default viz.html).  [default: viz.html]
* `--open`: Open the written file in the default browser.
* `--type TEXT`: Render only that type and its incident edges.
* `--help`: Show this message and exit.

## `khub backfill`

Backfill missing dates and frontmatter: khub backfill [--type T] [--dry-run].

**Usage**:

```console
$ khub backfill [OPTIONS]
```

**Options**:

* `--type TEXT`: Add missing per-type frontmatter scaffolding for that type.
* `--dry-run`: List the entities and fields that would change; write nothing.
* `--help`: Show this message and exit.

## `khub wire`

Wire the workspace into agent context files (CLAUDE.md gets a ``@.khub/schema.yaml``
import; AGENTS.md gets a schema pointer). Bare ``wire`` updates whichever already exist.

**Usage**:

```console
$ khub wire [OPTIONS]
```

**Options**:

* `--target TEXT`: Create and wire a specific file: claude, agents, or both. Omit to update the agent files that already exist.
* `--dry-run`: Print the block(s); write nothing.
* `--help`: Show this message and exit.

## `khub schema`

Introspect the active schema.

**Usage**:

```console
$ khub schema [OPTIONS] COMMAND [ARGS]...
```

**Options**:

* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

**Commands**:

* `types`: List the declared type names.
* `show`: Detail one type: fields, enums, required...
* `edges`: List the relation vocabulary by predicate.

### `khub schema types`

List the declared type names.

**Usage**:

```console
$ khub schema types [OPTIONS]
```

**Options**:

* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

### `khub schema show`

Detail one type: fields, enums, required flags, relations, layout.

**Usage**:

```console
$ khub schema show [OPTIONS] TYPE
```

**Arguments**:

* `TYPE`: Type name.  [required]

**Options**:

* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

### `khub schema edges`

List the relation vocabulary by predicate.

**Usage**:

```console
$ khub schema edges [OPTIONS]
```

**Options**:

* `--format TEXT`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.
