"""Graph index — the shared entity scan behind status, query, and the write verbs.

A snapshot of the entity tree keyed by ``(type, slug)``: the nodes that exist, the
types each bare slug maps to (for id resolution), and each node's frontmatter. The
projection (``khub status``) and the authoring verbs (FS-002) both derive from this
one scan instead of re-walking the tree per call.

# ponytail: full in-memory scan, fine for a v1 firm corpus. When FS-003/FS-004 need
# incremental queries, lift to a persistent index (networkx is already a dependency).
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from khub.core.errors import LocatedError
from khub.core.formats import load_collection, load_meta, split_row
from khub.core.model import ResolvedRelation, ResolvedSchema, ResolvedType


@dataclass(frozen=True)
class Index:
    """A scanned view of the entity tree: nodes, slug→types, and per-node metadata.

    ``malformed`` holds the workspace-relative paths of files inside a layout that
    could not be parsed as frontmatter — one bad file becomes a reported entry, never
    a raised ``ParserError`` that bricks every command.
    """

    resolved: ResolvedSchema
    nodes: set[tuple[str, str]]
    types_by_slug: dict[str, set[str]]
    meta: dict[tuple[str, str], dict[str, Any]]
    malformed: list[Path] = field(default_factory=list)


def build_index(root: Path, resolved: ResolvedSchema) -> Index:
    """Scan every declared type into an :class:`Index` keyed by ``(type, slug)``."""
    nodes: set[tuple[str, str]] = set()
    types_by_slug: dict[str, set[str]] = {}
    meta: dict[tuple[str, str], dict[str, Any]] = {}
    malformed: list[Path] = []
    for tname, rtype in resolved.types.items():
        pairs, bad = scan_type(root, rtype)
        for slug, m in pairs:
            node = (tname, slug)
            nodes.add(node)
            types_by_slug.setdefault(slug, set()).add(tname)
            meta[node] = m
        malformed.extend(bad)
    malformed_rel = sorted(p.relative_to(root) for p in malformed)
    return Index(
        resolved=resolved,
        nodes=nodes,
        types_by_slug=types_by_slug,
        meta=meta,
        malformed=malformed_rel,
    )


def list_refs(root: Path, resolved: ResolvedSchema) -> list[tuple[str, str]]:
    """Every entity as sorted ``(type, slug)`` pairs — the enumeration verb.

    Backed by the same scan the write-path validator checks against, so a
    pick-list built from it and the referential-integrity gate stay consistent.
    """
    return sorted(build_index(root, resolved).nodes)


def scan_type(root: Path, rtype: ResolvedType) -> tuple[list[tuple[str, dict[str, Any]]], list[Path]]:
    """The ``(slug, frontmatter)`` pairs stored for one type, plus its malformed files.

    Each file is parsed under a tight guard: a single unparseable file (unclosed
    bracket, a tab in the frontmatter) becomes a malformed entry instead of raising —
    so validate/check/query/status/get never crash on one bad file.
    """
    if rtype.storage.layout == "collection":
        return _scan_collection(root, rtype)
    if rtype.storage.layout == "singleton":
        # Exactly one fixed file; slug is the type name. Missing = zero entities
        # (a required-but-absent singleton is a `check` finding, not a scan error).
        spath = root / (rtype.storage.path or "")
        if not spath.is_file():
            return [], []
        parsed = load_meta(spath)
        if parsed is None:
            return [], [spath]
        return [(rtype.name, parsed)], []
    if not rtype.storage.path:
        return [], []
    base = root / rtype.storage.path
    if not base.exists():
        return [], []
    out: list[tuple[str, dict[str, Any]]] = []
    malformed: list[Path] = []
    ext = rtype.storage.fmt
    # is_file(): glob also matches directories named *.<ext> (real corpora have them);
    # a directory is not a malformed file — it is simply not an entity.
    # ponytail: the scan globs only the declared format; an off-format file in the
    # layout is invisible (exactly as a .json file in an md layout is today). Lift to
    # a multi-ext glob with off-format-as-stray if a format migration ever strands
    # files — and note a migration also renames paths, going dark in gitlog history
    # (no --follow); the future migration verb owns both halves.
    if rtype.storage.layout == "folder":
        for idx in sorted(base.glob(f"*/_index.{ext}")):
            if not idx.is_file():
                continue
            parsed = load_meta(idx)
            if parsed is None:
                malformed.append(idx)
            else:
                out.append((idx.parent.name, parsed))
    else:
        for f in sorted(base.glob(f"*.{ext}")):
            if f.name == f"_index.{ext}" or not f.is_file():
                continue
            parsed = load_meta(f)
            if parsed is None:
                malformed.append(f)
            else:
                out.append((f.stem, parsed))
    return out, malformed


def _scan_collection(
    root: Path, rtype: ResolvedType
) -> tuple[list[tuple[str, dict[str, Any]]], list[Path]]:
    """One collection file as ``(slug, meta)`` rows, or one malformed entry.

    A missing or empty file is zero entities, never malformed (the analog of a
    type's missing directory). Rows may omit ``type`` — the schema binding
    injects it; a present-and-disagreeing ``type`` makes the row a stray, same
    predicate as a per-item file. Any bad row makes the WHOLE file malformed
    (v1 contract — khub never partially loads a file it cannot round-trip).
    """
    cpath = root / rtype.collection_relpath
    if not cpath.is_file():
        return [], []
    try:
        rows = load_collection(cpath.read_text(encoding="utf-8"), rtype.storage.fmt)
        out: list[tuple[str, dict[str, Any]]] = []
        for slug, row in rows.items():
            meta, _ = split_row(row, rtype.storage.fmt)
            meta.setdefault("type", rtype.name)
            out.append((slug, meta))
        return out, []
    except Exception:  # noqa: BLE001 — one bad collection must not brick the whole scan
        return [], [cpath]


def canonical_slug(slug: str, types_by_slug: dict[str, set[str]]) -> str | None:
    """The stored slug matching ``slug`` case-insensitively, when exactly one does.

    ``add --id CMP-001-Api`` slugifies to ``cmp-001-api`` on write, so an agent reusing
    the string it just passed got ``lookup_error`` from every read verb — write and read
    disagreeing about one identifier. Exact match always wins; this is only the fallback.

    Returns None when nothing matches, or when two stored slugs differ only by case
    (possible on a case-sensitive filesystem, and genuinely ambiguous), so the caller
    raises its own error rather than picking one arbitrarily.
    """
    if slug in types_by_slug:
        return slug
    folded = slug.casefold()
    hits = [s for s in types_by_slug if s.casefold() == folded]
    return hits[0] if len(hits) == 1 else None


def resolve_target(
    rel: ResolvedRelation,
    target: str,
    nodes: set[tuple[str, str]],
    types_by_slug: dict[str, set[str]],
) -> set[tuple[str, str]]:
    """The nodes a relation value resolves to: any-type for universal edges,
    a declared target type otherwise.

    A qualified ``type/slug`` value resolves to that exact node (the form ``link``
    accepts on an ambiguous slug); a bare slug resolves by slug — across every type
    for a universal (``any``) edge, within the declared targets for a typed edge.
    """
    # Resolve case the same way the read verbs do, so `link x rel CMP-001` and
    # `get CMP-001` cannot disagree about whether that entity exists. Both forms fold:
    # `resolve_id` accepts a qualified id case-insensitively, so this must too.
    if "/" in target:
        t, s = target.split("/", 1)
        if (t, s) not in nodes:
            canon = canonical_slug(s, types_by_slug)
            if canon is None:
                return set()
            match = next(
                (ct for ct in types_by_slug[canon] if ct.casefold() == t.casefold()), None
            )
            if match is None:
                return set()
            t, s = match, canon
        # A qualified id still honors the edge's declared targets: a typed/union edge
        # rejects a node of a disallowed type; a universal (any) edge accepts any.
        return {(t, s)} if rel.kind == "any" or t in rel.targets else set()
    target = canonical_slug(target, types_by_slug) or target
    if rel.kind == "any":
        return {(t, target) for t in types_by_slug.get(target, ())}
    return {(t, target) for t in rel.targets if (t, target) in nodes}


def stray_nodes(index: Index) -> set[tuple[str, str]]:
    """Scanned files whose internal ``type`` does not match their layout type.

    A file inside a type's layout that does not parse as that type is a stray
    (INT-011); reference markdown outside every layout is never scanned, so it is
    skipped, not flagged. Derived from the existing scan — no second walk.
    """
    return {node for node, m in index.meta.items() if m.get("type") != node[0]}


def filter_index(index: Index, drop: set[tuple[str, str]]) -> Index:
    """A view of ``index`` with ``drop`` nodes removed from nodes, slugs, and meta."""
    if not drop:
        return index
    nodes = index.nodes - drop
    types_by_slug: dict[str, set[str]] = {}
    for tname, slug in nodes:
        types_by_slug.setdefault(slug, set()).add(tname)
    meta = {n: m for n, m in index.meta.items() if n not in drop}
    # Malformed files are not nodes, so dropping strays never touches them.
    return Index(
        resolved=index.resolved,
        nodes=nodes,
        types_by_slug=types_by_slug,
        meta=meta,
        malformed=index.malformed,
    )


def reject_malformed(index: Index, verb: str) -> None:
    """Refuse to derive a written projection from a scan that dropped files.

    A malformed file is not in the graph, so `reindex`/`viz` would happily emit an
    artifact with an entire type missing and exit 0 — the same silent-partial-write
    the collection writers already refuse. `check` reports the malformed files; this
    keeps a broken scan from being committed as if it were the whole picture.
    """
    if not index.malformed:
        return
    listed = ", ".join(str(p) for p in sorted(index.malformed)[:3])
    more = f" (+{len(index.malformed) - 3} more)" if len(index.malformed) > 3 else ""
    raise LocatedError(
        code="malformed_projection",
        message=(
            f"Refusing to {verb}: {len(index.malformed)} file(s) could not be parsed, "
            f"so the graph is incomplete — {listed}{more}. Run `khub check` for the list."
        ),
    )
