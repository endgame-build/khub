"""Entity filtering — ``khub query`` (WPK-003-1, FS-003).

ANDs a set of filters (type, frontmatter field, tag, ``--has``/``--missing``
predicate presence, orphan/stale flags, draft scope) over the entity index and
the resolved edge graph. Filters read frontmatter and derived edges only, never
body prose (QRY-001). An empty result is a success, not an error (QRY-002). Every
match carries its ``orphan`` and ``stale`` flags by default (QRY-009), computed
identically to ``khub status`` (the graph for orphan, ``core.project.is_stale``
for stale). A field naming an undeclared attribute/relation of the filtered type
is a located error (QRY-001).
"""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import date
from pathlib import Path
from typing import Any

import networkx as nx

from khub.core.errors import LocatedError
from khub.core.graph import build_graph
from khub.core.index import build_index, filter_index, stray_nodes
from khub.core.introspect import load_schema
from khub.core.model import ResolvedSchema
from khub.core.project import is_stale, stale_days
from khub.core.values import as_bool


@dataclass(frozen=True)
class Match:
    """One entity passing every filter, with its derived health flags."""

    type: str
    slug: str
    draft: bool
    orphan: bool
    stale: bool
    # Carried so listing a type does not cost one `get` per row. Defaulted to keep the
    # positional shape stable for existing constructors.
    title: str = ""


@dataclass(frozen=True)
class QueryFilters:
    """The ANDed filter set parsed from the CLI."""

    type: str | None = None
    fields: dict[str, str] = field(default_factory=dict)
    tag: str | None = None
    has: str | None = None
    missing: str | None = None
    orphan: bool = False
    stale: bool = False
    draft_only: bool = False
    active_only: bool = False
    limit: int | None = None


def query(root: Path, filters: QueryFilters, *, now: date) -> list[Match]:
    """Return the entities passing every filter, each annotated orphan/stale."""
    resolved = load_schema(root)
    scanned = build_index(root, resolved)
    index = filter_index(scanned, stray_nodes(scanned))  # strays are not entities
    g = build_graph(index)
    days = stale_days(root)

    _validate_filter_names(resolved, root, filters)

    matches: list[Match] = []
    for node in sorted(index.nodes):
        type_, slug = node
        if filters.type and type_ != filters.type:
            continue
        meta = index.meta[node]
        # A type declaring `orphan: true` is never flagged: edge-less is its
        # expected state (see TypeDecl.orphan). Kept identical to the `check`
        # gate and the `status` count — one notion, three read sites.
        orphan = (
            g.in_degree(node) == 0
            and g.out_degree(node) == 0
            and not resolved.types[type_].orphan
        )
        stale = is_stale(meta, now=now, stale_days=days)
        if not _passes(node, meta, g, filters, resolved, orphan=orphan, stale=stale):
            continue
        matches.append(
            Match(
                type=type_,
                slug=slug,
                draft=as_bool(meta.get("draft", False)),
                orphan=orphan,
                stale=stale,
                title=str(meta.get("title") or meta.get("name") or slug),
            )
        )
    if filters.limit is not None:
        matches = matches[: filters.limit]
    return matches


def _validate_filter_names(resolved: ResolvedSchema, root: Path, filters: QueryFilters) -> None:
    """Reject a field or ``--has``/``--missing`` predicate the schema does not declare.

    Type-scoped when ``--type`` is set (the located error names that type); otherwise
    a name must be declared on at least one type, else it is a typo that would
    silently match nothing (``--stagee`` returning an empty set as if a success).
    """
    preds = [p for p in (filters.has, filters.missing) if p is not None]
    if filters.type is not None:
        rtype = resolved.types.get(filters.type)
        if rtype is None:
            from khub.core.locate import provenance

            preset = provenance(root).get("preset") or "the"
            raise LocatedError.unknown_type(filters.type, preset, sorted(resolved.types))
        for fname in filters.fields:
            if fname not in rtype.attributes and fname not in rtype.relations:
                raise LocatedError.unknown_filter_field(fname, filters.type)
        for pred in preds:
            if (
                pred not in rtype.relations
                and pred not in rtype.attributes
                and not _inverse_sources(resolved, pred, filters.type)
            ):
                raise LocatedError.unknown_filter_field(pred, filters.type)
        return
    attrs = {a for t in resolved.types.values() for a in t.attributes}
    rels = {r for t in resolved.types.values() for r in t.relations}
    for fname in filters.fields:
        if fname not in attrs and fname not in rels:
            raise LocatedError.unknown_filter_field(fname, "any")
    for pred in preds:
        if pred not in rels and pred not in attrs and not _inverse_sources(resolved, pred):
            raise LocatedError.unknown_filter_field(pred, "any")


def _passes(
    node: tuple[str, str],
    meta: dict[str, Any],
    g: nx.MultiDiGraph,
    f: QueryFilters,
    resolved: ResolvedSchema,
    *,
    orphan: bool,
    stale: bool,
) -> bool:
    """Whether one entity passes every active filter (read-only over frontmatter)."""
    is_draft = as_bool(meta.get("draft", False))
    if f.active_only and is_draft:
        return False
    if f.draft_only and not is_draft:
        return False
    for fname, fval in f.fields.items():
        if not _field_matches(meta.get(fname), fval):
            return False
    if f.tag is not None and not _field_matches(meta.get("tags"), f.tag):
        return False
    if f.has is not None and not _has_value(node, meta, g, resolved, f.has):
        return False
    if f.missing is not None and _has_value(node, meta, g, resolved, f.missing):
        return False
    if f.orphan and not orphan:
        return False
    if f.stale and not stale:  # noqa: SIM103 — one guard per filter reads better than a
        return False             # collapsed boolean; the parallel shape is the point
    return True


def _has_value(
    node: tuple[str, str],
    meta: dict[str, Any],
    g: nx.MultiDiGraph,
    resolved: ResolvedSchema,
    name: str,
) -> bool:
    """Whether ``node`` carries ``name``: a resolved edge for a relation, a populated
    value for an attribute.

    ``--has``/``--missing`` answered from the graph alone until 0.13.0, so an attribute
    (``repo``, ``stack``) raised ``No field 'repo' on type 'component'`` — a message that
    was simply false, since the schema declares it. An attribute has no edge, so presence
    is read off frontmatter: absent, null, or empty counts as a gap, the same
    null-is-absent rule ``validate`` and ``check`` already apply.

    A relation still tests the *resolved* edge — an edge exists in the graph only when its
    value resolved to a node, so an unresolvable target counts as missing.
    """
    rtype = resolved.types[node[0]]
    inverse_of = _inverse_sources(resolved, name, node[0])
    if name in rtype.relations or inverse_of:
        return _has_edge(g, node, name, inverse_of)
    if name in rtype.attributes:
        value = meta.get(name)
        if value is None:
            return False
        return not (isinstance(value, (str, list, dict)) and not value)
    return False  # declared on some other type, so this entity simply lacks it


def _has_edge(
    g: nx.MultiDiGraph, node: tuple[str, str], predicate: str, inverse_of: set[str] | None = None
) -> bool:
    """Whether ``node`` has a resolvable edge for ``predicate``.

    A declared inverse (``superseded`` for ``supersedes``) is never stored, so it is
    answered from the INBOUND side: the forward edge lives on the other entity. That
    makes ``--missing superseded`` the "which decisions are still current?" query.
    """
    if any(pred == predicate for _, _, pred in g.out_edges(node, keys=False, data="predicate")):
        return True  # a stored forward edge always wins; an inverse never shadows it
    if not inverse_of:
        return False
    sources = inverse_of
    return any(pred in sources for _, _, pred in g.in_edges(node, keys=False, data="predicate"))


def _inverse_sources(
    resolved: ResolvedSchema, name: str, on_type: str | None = None
) -> set[str]:
    """The forward predicates whose declared inverse is ``name``.

    With ``on_type``, only relations that can actually point AT that type count — an
    inverse of a relation targeting something else is not a field of this type, and
    accepting it would turn a typo into a filter that silently matches everything.
    """
    out: set[str] = set()
    for rtype in resolved.types.values():
        for rel in rtype.relations.values():
            if rel.inverse != name:
                continue
            if on_type is None or rel.kind == "any" or on_type in rel.targets:
                out.add(rel.predicate)
    return out


def _field_matches(value: Any, wanted: str) -> bool:
    """Whether a frontmatter value matches a CLI filter string.

    A bool matches case-insensitively (so ``--active true`` works against YAML
    ``True``); a list (many-valued relation or list attribute) matches on
    membership (``--team noor`` against ``team: [noor, bob]``, never substring);
    any other scalar matches by string equality.
    """
    if isinstance(value, bool):
        return str(value).lower() == wanted.lower()
    if isinstance(value, list):
        return any(str(v) == wanted for v in value)
    return str(value) == wanted
