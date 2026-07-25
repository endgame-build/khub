# `khub`

khub — schema-bound context management.

**Usage**:

```console
$ khub [OPTIONS] COMMAND [ARGS]...
```

**Options**:

* `-C, --workspace <path>`: Operate on this workspace instead of the working directory.
* `--version`: Print the khub version and exit.
* `--help`: Show this message and exit.

**Commands**:

* `init`: Scaffold a workspace from a preset and...
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
* `reindex`: Regenerate the OKF index.md from the...
* `viz`: Render the typed graph to a self-contained...
* `backfill`: Backfill missing dates and frontmatter:...
* `wire`: Wire the workspace into agent context...
* `install-skills`: Install khub&#x27;s agent skills: khub...
* `schema`: Introspect the active schema.

## `khub init`

Scaffold a workspace from a preset and wire it into the agent context files.

A missing PRESET is a usage error; PATH defaults to the working directory.
Installing the agent skill is a separate step: ``khub install-skills``.

**Usage**:

```console
$ khub init [OPTIONS] [preset] [path]
```

**Arguments**:

* `preset`: Named preset to seed from (e.g. firm-ops).
* `path`: Target directory (default: .).

**Options**:

* `--preset-source <path>`: Where to resolve the preset if not packaged with khub.
* `--name <str>`: Workspace name (default: the target dir name).
* `--force`: Scaffold into a non-empty target.
* `--no-wire`: Skip wiring the schema into the agent files.
* `--format <str>`: text confirmation (default); json emits resolved provenance.  [default: text]
* `--help`: Show this message and exit.

## `khub status`

Summarize the workspace: counts, draft/active, orphan/stale, OKF conformance.

**Usage**:

```console
$ khub status [OPTIONS]
```

**Options**:

* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub add`

Create an entity: khub add opportunity --client initech --owner noor --stage prospect.

A missing TYPE is a usage error; every field is a flag.

**Usage**:

```console
$ khub add [OPTIONS] [TYPE]
```

**Arguments**:

* `TYPE`: The entity type to create.

**Options**:

* `--id <str>`: Explicit slug (else minted from name/type).
* `--draft`: Mark the entity unpublished (default: active).
* `--strict`: Reject fields the schema does not declare.
* `--body <str>`: Body prose as a string.
* `--body-file <str>`: Read the body from a file (&#x27;-&#x27; for stdin).
* `--no-template`: Start with an empty body even when the type has a template.
* `--format <str>`: text or json (emits the written record).  [default: text]
* `--help`: Show this message and exit.

## `khub get`

Read an entity&#x27;s frontmatter and body, optionally with derived edges.

**Usage**:

```console
$ khub get [OPTIONS] [ID]
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.

**Options**:

* `--edges`: Include stored and derived edges.
* `--format <str>`: json, table, raw, or text (Rich on a TTY).  [default: text]
* `--help`: Show this message and exit.

## `khub edit`

Edit an entity: khub edit initech-deal stage proposal-sent  (or --field value).

A missing ID is a usage error; the field and value are positional or flags.

**Usage**:

```console
$ khub edit [OPTIONS] [ID]
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.

**Options**:

* `--strict`: Reject fields the schema does not declare.
* `--body <str>`: Replace the body with this string (&#x27;&#x27; clears it).
* `--body-file <str>`: Replace the body from a file (&#x27;-&#x27; for stdin).
* `--format <str>`: text or json (emits the updated record).  [default: text]
* `--help`: Show this message and exit.

## `khub link`

Add a relation: khub link initech-pov partner northwind.

**Usage**:

```console
$ khub link [OPTIONS] [ID] [PREDICATE] [TARGET]
```

**Arguments**:

* `ID`
* `PREDICATE`
* `TARGET`

**Options**:

* `--format <str>`: text or json (emits the edge record).  [default: text]
* `--help`: Show this message and exit.

## `khub unlink`

Remove a relation: khub unlink initech-pov partner northwind.

**Usage**:

```console
$ khub unlink [OPTIONS] [ID] [PREDICATE] [TARGET]
```

**Arguments**:

* `ID`
* `PREDICATE`
* `TARGET`

**Options**:

* `--format <str>`: text or json (emits the edge record).  [default: text]
* `--help`: Show this message and exit.

## `khub remove`

Remove an entity, guarded by inbound edges: khub remove old-fragment [--force].

**Usage**:

```console
$ khub remove [OPTIONS] [ID]
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.

**Options**:

* `--force`: Delete despite inbound edges (leaves them dangling).
* `--format <str>`: text or json (emits the removed record).  [default: text]
* `--help`: Show this message and exit.

## `khub query`

Filter entities: khub query --type opportunity --stage prospect --format json.

**Usage**:

```console
$ khub query [OPTIONS]
```

**Options**:

* `--type <str>`: Restrict to one entity type.
* `--tag <str>`: Keep entities carrying this tag.
* `--has <str>`: Keep entities with a resolvable edge for the predicate.
* `--missing <str>`: Keep entities lacking a resolvable edge (gap finder).
* `--orphan`: Keep only orphan (edge-less) entities.
* `--stale`: Keep only stale entities.
* `--draft`: Isolate drafts.
* `--active`: Exclude drafts.
* `--limit <int>`: Cap the returned set.
* `--format <str>`: text (Rich table on a TTY), json, or ids.  [default: text]
* `--help`: Show this message and exit.

## `khub search`

Full-text search: khub search modernization --type transcript --format json.

**Usage**:

```console
$ khub search [OPTIONS] {text}
```

**Arguments**:

* `text`: FTS5 MATCH text: terms, &quot;phrases&quot;, OR, NEAR, prefix*.  [required]

**Options**:

* `--type <str>`: Restrict to one entity type.
* `--limit <int>`: Cap the returned set.  [default: 20]
* `--format <str>`: text (Rich table on a TTY), json, or ids.  [default: text]
* `--help`: Show this message and exit.

## `khub neighbors`

Walk one-hop neighbors: khub neighbors initech-pov [--predicate client --in].

**Usage**:

```console
$ khub neighbors [OPTIONS] {ID}
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.  [required]

**Options**:

* `--predicate <str>`: Restrict adjacency to one predicate.
* `--in`: Inbound edges only (incl. derived inverses).
* `--out`: Outbound (stored) edges only.
* `--depth <int>`: Bounded multi-hop adjacency over all predicates.  [default: 1]
* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub impact`

Compute blast radius: khub impact node-a [--reverse] [--predicate &lt;p&gt;].

**Usage**:

```console
$ khub impact [OPTIONS] {ID}
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.  [required]

**Options**:

* `--predicate <str>`: The edge to walk the closure over.  [default: depends_on]
* `--reverse`: Walk ancestors (what reaches this node).
* `--format <str>`: tree (depth-marked, the TTY default) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub history`

Trace supersession lineage: khub history decision-0012 [--limit 3].

**Usage**:

```console
$ khub history [OPTIONS] {ID}
```

**Arguments**:

* `ID`: A bare slug, or type/slug on ambiguity.  [required]

**Options**:

* `--predicate <str>`: The self-referential edge to follow.  [default: supersedes]
* `--limit <int>`: Cap to the N most recent links.
* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub validate`

Validate entities: khub validate [TARGET] [--strict].

**Usage**:

```console
$ khub validate [OPTIONS] [TARGET]
```

**Arguments**:

* `TARGET`: A type or type/slug; default: all.

**Options**:

* `--strict`: Close the schema: reject undeclared keys.
* `--format <str>`: text (Rich on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub check`

Check the active graph: completeness, orphans, dangling edges, strays, cycles.

**Usage**:

```console
$ khub check [OPTIONS]
```

**Options**:

* `--strict`: Fail the gate on orphans too (default: informational).
* `--format <str>`: text (Rich on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub stale`

List entities past the `updated` threshold, oldest first: khub stale [--days N].

**Usage**:

```console
$ khub stale [OPTIONS]
```

**Options**:

* `--days <int>`: Staleness threshold in days; default: the workspace&#x27;s stale_days.
* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
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

* `--out <str>`: Output path for the HTML (default viz.html).  [default: viz.html]
* `--open`: Open the written file in the default browser.
* `--type <str>`: Render only that type and its incident edges.
* `--help`: Show this message and exit.

## `khub backfill`

Backfill missing dates and frontmatter: khub backfill [--type T] [--dry-run].

**Usage**:

```console
$ khub backfill [OPTIONS]
```

**Options**:

* `--type <str>`: Add missing per-type frontmatter scaffolding for that type.
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

* `--target <str>`: Create and wire a specific file: claude, agents, or both. Omit to update the agent files that already exist.
* `--dry-run`: Print the block(s); write nothing.
* `--help`: Show this message and exit.

## `khub install-skills`

Install khub&#x27;s agent skills: khub install-skills [--target agents] [--global].

**Usage**:

```console
$ khub install-skills [OPTIONS]
```

**Options**:

* `--target <str>`: claude, agents, or opencode (repeatable). Default: all three.
* `--skill <str>`: Which skill to install (repeatable). Default: all shipped.
* `--global`: Install into the home directories instead of this workspace.
* `--dry-run`: Report what would be written, and write nothing.
* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

## `khub schema`

Introspect the active schema.

**Usage**:

```console
$ khub schema [OPTIONS] COMMAND [ARGS]...
```

**Options**:

* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
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

* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

### `khub schema show`

Detail one type: fields, enums, required flags, relations, layout.

**Usage**:

```console
$ khub schema show [OPTIONS] {type}
```

**Arguments**:

* `type`: Type name.  [required]

**Options**:

* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.

### `khub schema edges`

List the relation vocabulary by predicate.

**Usage**:

```console
$ khub schema edges [OPTIONS]
```

**Options**:

* `--format <str>`: text (Rich table on a TTY) or json.  [default: text]
* `--help`: Show this message and exit.
