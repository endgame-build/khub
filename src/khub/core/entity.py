"""Entity authoring verbs — the write surface over the graph (FS-002).

``create`` and ``get`` are the foundational pair: mint a typed entity as one
Markdown file (slug minting, layout resolution, field/enum/pattern validation,
referential-integrity hard-fail) and read one back (id resolution, ambiguity
detection, read-time inverse-edge derivation). ``update``/``link``/``unlink``/
``delete`` extend this module in WPK-002-2/3.

``draft`` is a manual publish flag (FS-002): ``add`` defaults it to
``false``, ``--draft`` sets it, and ``edit <id> draft …`` toggles it. khub never
derives it from completeness — capture is never blocked, and an active-but-
incomplete entity is surfaced by ``check`` (FS-004), not by this flag.

Every verb is schema-generic: it introspects the compiled schema at runtime and
has no per-type code path. The schema and git are the only gates.
"""

from __future__ import annotations

import re
import shutil
from dataclasses import dataclass, field
from datetime import date
from io import StringIO
from pathlib import Path
from typing import Any

import frontmatter
from ruamel.yaml import YAML

from khub.core.errors import LocatedError
from khub.core.index import Index, build_index, resolve_target
from khub.core.introspect import load_schema
from khub.core.model import ResolvedAttribute, ResolvedSchema, ResolvedType

_yaml = YAML()  # round-trip: preserves key order and comments on edit
_yaml.default_flow_style = False
# Never emit YAML anchors/aliases: two keys sharing a value (e.g. created==updated
# on a fresh entity) must each serialize in full, not collapse to &id/*id — anchors
# leak into raw reads and break the minimal-diff guarantee on the first edit.
_yaml.representer.ignore_aliases = lambda *_: True


@dataclass(frozen=True)
class CreateResult:
    """The outcome of a create: where it landed and whether it is a draft."""

    type: str
    slug: str
    path: Path
    draft: bool


@dataclass(frozen=True)
class UpdateResult:
    """The outcome of an edit: where it landed and whether it is now a draft."""

    type: str
    slug: str
    path: Path
    draft: bool


@dataclass(frozen=True)
class LinkResult:
    """The outcome of a link/unlink, for the confirmation line."""

    type: str
    slug: str
    predicate: str
    target: str


@dataclass(frozen=True)
class Inbound:
    """An edge pointing *at* an entity: which entity and predicate resolve to it."""

    source_type: str
    source_slug: str
    predicate: str


@dataclass(frozen=True)
class DeleteResult:
    """The outcome of a remove: deleted, or refused with the inbound edges listed."""

    type: str
    slug: str
    removed: bool
    inbound: list[Inbound] = field(default_factory=list)


@dataclass(frozen=True)
class Edge:
    """One relation on an entity, stored (forward) or derived (inverse)."""

    predicate: str
    target: str
    derived: bool


@dataclass(frozen=True)
class EntityView:
    """A read of one entity: its frontmatter, body, and optionally its edges."""

    type: str
    slug: str
    path: Path
    meta: dict[str, Any]
    body: str
    raw: str
    edges: list[Edge] | None = None


# --- create ------------------------------------------------------------------


def create(
    root: Path,
    type_: str,
    fields: dict[str, str],
    *,
    id_: str | None = None,
    strict: bool = False,
    body: str = "",
    draft: bool = False,
) -> CreateResult:
    """Mint a new entity of ``type_`` from ``fields`` (raw ``--field value`` strings).

    Validates each field and hard-fails (writing nothing) if a relation target
    does not resolve. ``draft`` is the manual publish flag (default ``false``);
    a missing required field never blocks capture — `check` (FS-004) surfaces the
    gap as active-but-incomplete.
    """
    resolved = load_schema(root)
    rtype = resolved.types.get(type_)
    if rtype is None:
        raise LocatedError.unknown_type(type_, _preset(root), sorted(resolved.types))

    index = build_index(root, resolved)
    attrs, rels, extras = _partition(rtype, fields, strict=strict)

    # Referential integrity hard-fails before any byte is written.
    for predicate, values in rels.items():
        rel = rtype.relations[predicate]
        for value in values:
            if not resolve_target(rel, value, index.nodes, index.types_by_slug):
                target_type = "/".join(rel.targets)
                raise LocatedError.referential_integrity(target_type, value, predicate)

    # `draft` is manual: the --draft flag, or an explicit `draft` field, else false.
    explicit = attrs.pop("draft", None)
    is_draft = bool(explicit) if explicit is not None else draft

    slug = _mint_slug(_slug_source(type_, attrs, id_), type_, index)
    meta: dict[str, Any] = {
        "type": type_,
        "created": date.today(),
        "updated": date.today(),
        "draft": is_draft,
    }
    for name in rtype.attributes:
        if name in attrs:
            meta[name] = attrs[name]
    for predicate in rtype.relations:
        if predicate in rels:
            rel = rtype.relations[predicate]
            meta[predicate] = rels[predicate] if rel.many else rels[predicate][0]
    meta.update(extras)

    path = entity_path(root, rtype, slug)
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(_render_file(meta, body))
    return CreateResult(type=type_, slug=slug, path=path, draft=is_draft)


def _partition(
    rtype: ResolvedType, fields: dict[str, str], *, strict: bool
) -> tuple[dict[str, Any], dict[str, list[str]], dict[str, Any]]:
    """Split raw fields into validated attributes, relation value-lists, and extras."""
    attrs: dict[str, Any] = {}
    rels: dict[str, list[str]] = {}
    extras: dict[str, Any] = {}
    for key, raw in fields.items():
        if key in rtype.attributes:
            attrs[key] = _validate_attr(rtype.attributes[key], raw)
        elif key in rtype.relations:
            rels[key] = [v.strip() for v in raw.split(",")] if raw else []
        elif strict:
            raise LocatedError.strict_unknown_field(key)
        else:
            extras[key] = raw
    return attrs, rels, extras


def _validate_attr(attr: ResolvedAttribute, raw: str) -> Any:
    """Validate and coerce a raw string against one attribute's type/enum/pattern."""
    if attr.enum is not None:
        if raw not in attr.enum:
            raise LocatedError.enum_violation(raw, attr.name, attr.enum)
        return raw
    if attr.pattern is not None and not re.fullmatch(attr.pattern, raw):
        raise LocatedError.pattern_violation(raw, attr.name, attr.pattern)
    if attr.base_type == "bool":
        return _to_bool(raw)
    if attr.base_type == "number":
        return _to_number(raw)
    if attr.base_type == "list":
        return [v.strip() for v in raw.split(",")]
    return raw


def _slug_source(type_: str, attrs: dict[str, Any], id_: str | None) -> str:
    """The string a slug is minted from: explicit id, else a name field, else the type."""
    if id_:
        return id_
    name = attrs.get("name")
    return str(name) if name else type_


def _mint_slug(source: str, type_: str, index: Index) -> str:
    """A bare slug unique within ``type_``; a within-type collision gets a -N suffix."""
    base = slugify(source)
    if not base:  # an all-symbol/empty source would write a hidden, collision-blind file
        raise LocatedError.invalid_slug(source)
    if (type_, base) not in index.nodes:
        return base
    n = 2
    while (type_, f"{base}-{n}") in index.nodes:
        n += 1
    return f"{base}-{n}"


def slugify(text: str) -> str:
    """Lowercase, collapse non-alphanumeric runs to single hyphens, trim hyphens."""
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-")


# --- get ---------------------------------------------------------------------


def get(root: Path, id_: str, *, edges: bool = False) -> EntityView:
    """Read one entity by id; optionally include stored and derived edges."""
    resolved = load_schema(root)
    index = build_index(root, resolved)
    type_, slug = resolve_id(index, id_)
    rtype = resolved.types[type_]
    path = entity_path(root, rtype, slug)
    raw = path.read_text()
    post = frontmatter.loads(raw)
    view_edges = _edges(index, resolved, type_, slug, dict(post.metadata)) if edges else None
    return EntityView(
        type=type_,
        slug=slug,
        path=path,
        meta=dict(post.metadata),
        body=post.content,
        raw=raw,
        edges=view_edges,
    )


def resolve_id(index: Index, id_: str) -> tuple[str, str]:
    """Resolve a bare slug (or qualified ``type/slug``) to one ``(type, slug)`` node.

    A bare slug shared by two types is ambiguous; a ``type/slug`` qualifier is exact.
    """
    if "/" in id_:
        type_, slug = id_.split("/", 1)
        if (type_, slug) in index.nodes:
            return type_, slug
        raise LocatedError.lookup_error(id_)
    types = index.types_by_slug.get(id_)
    if not types:
        raise LocatedError.lookup_error(id_)
    if len(types) > 1:
        candidates = [f"{t}/{id_}" for t in sorted(types)]
        raise LocatedError.ambiguous_slug(id_, candidates)
    return next(iter(types)), id_


def _edges(
    index: Index, resolved: ResolvedSchema, type_: str, slug: str, meta: dict[str, Any]
) -> list[Edge]:
    """The entity's stored forward edges plus its read-time derived inverses."""
    node = (type_, slug)
    edges: list[Edge] = []
    for predicate, rel in resolved.types[type_].relations.items():
        value = meta.get(predicate)
        if not value:
            continue
        for target in value if isinstance(value, list) else [value]:
            edges.append(Edge(predicate=predicate, target=str(target), derived=False))
    # Derived inverses: any stored edge declaring an `inverse` that resolves to us.
    # The derived edge is qualified `type/slug` — the source type isn't recoverable
    # from a bare slug (any type may declare the inverse), unlike a stored forward edge.
    for (etype, eslug), emeta in index.meta.items():
        if (etype, eslug) == node:  # a self-reference is not its own inverse
            continue
        for predicate, rel in resolved.types[etype].relations.items():
            if not rel.inverse:
                continue
            value = emeta.get(predicate)
            if not value:
                continue
            for target in value if isinstance(value, list) else [value]:
                if node in resolve_target(rel, str(target), index.nodes, index.types_by_slug):
                    edges.append(Edge(predicate=rel.inverse, target=f"{etype}/{eslug}", derived=True))
    return edges


# --- update ------------------------------------------------------------------


def update(
    root: Path,
    id_: str,
    fields: dict[str, str],
    *,
    strict: bool = False,
    body: str | None = None,
) -> UpdateResult:
    """Edit ``id_``'s fields: re-validate, bump ``updated``, write a minimal diff.

    ``draft`` moves only when the user edits it (`edit <id> draft true|false`);
    no completeness recompute, no auto-promote.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    type_, slug = resolve_id(index, id_)
    rtype = resolved.types[type_]

    # Validate everything before touching the file, so a rejected edit leaves it
    # byte-for-byte unchanged (enum/pattern/strict raise here).
    attrs, rels, extras = _partition(rtype, fields, strict=strict)
    for predicate, values in rels.items():
        rel = rtype.relations[predicate]
        for value in values:
            if not resolve_target(rel, value, index.nodes, index.types_by_slug):
                raise LocatedError.referential_integrity("/".join(rel.targets), value, predicate)

    path = entity_path(root, rtype, slug)
    cmap, body_text = _read_doc(path)
    for name, value in attrs.items():
        cmap[name] = value
    for predicate, values in rels.items():
        rel = rtype.relations[predicate]
        cmap[predicate] = values if rel.many else values[0]
    for key, raw in extras.items():
        cmap[key] = raw
    cmap["updated"] = date.today()

    _write_doc(path, cmap, body_text if body is None else _normalize_body(body))
    return UpdateResult(type=type_, slug=slug, path=path, draft=bool(cmap.get("draft", False)))


# --- link / unlink -----------------------------------------------------------


def link(root: Path, id_: str, predicate: str, target: str) -> LinkResult:
    """Add a schema-checked edge ``predicate → target`` on the source entity."""
    resolved = load_schema(root)
    index = build_index(root, resolved)
    type_, slug = resolve_id(index, id_)
    rtype = resolved.types[type_]
    rel = rtype.relations.get(predicate)
    if rel is None:
        raise LocatedError.illegal_predicate(predicate, type_)
    if not resolve_target(rel, target, index.nodes, index.types_by_slug):
        raise LocatedError.referential_integrity(
            "/".join(rel.targets), target, predicate, noun="predicate"
        )

    path = entity_path(root, rtype, slug)
    cmap, body = _read_doc(path)
    existing = cmap.get(predicate)
    if rel.many:
        values = list(existing) if existing else []
        if target not in values:
            values.append(target)
        cmap[predicate] = values
    else:
        if existing not in (None, "", target):
            raise LocatedError.cardinality_violation(predicate)
        cmap[predicate] = target
    _write_doc(path, cmap, body)
    return LinkResult(type=type_, slug=slug, predicate=predicate, target=target)


def unlink(root: Path, id_: str, predicate: str, target: str) -> LinkResult:
    """Remove the edge ``predicate → target`` from the source; inverses recompute."""
    resolved = load_schema(root)
    index = build_index(root, resolved)
    type_, slug = resolve_id(index, id_)
    rtype = resolved.types[type_]
    rel = rtype.relations.get(predicate)
    if rel is None:
        raise LocatedError.illegal_predicate(predicate, type_)

    path = entity_path(root, rtype, slug)
    cmap, body = _read_doc(path)
    existing = cmap.get(predicate)
    changed = False
    if rel.many and existing and target in existing:
        remaining = [v for v in existing if v != target]
        if remaining:
            cmap[predicate] = remaining
        else:
            del cmap[predicate]
        changed = True
    elif existing == target:
        del cmap[predicate]
        changed = True
    if changed:  # a no-op unlink leaves the file untouched (no spurious rewrite)
        _write_doc(path, cmap, body)
    return LinkResult(type=type_, slug=slug, predicate=predicate, target=target)


# --- delete ------------------------------------------------------------------


def delete(root: Path, id_: str, *, force: bool = False) -> DeleteResult:
    """Remove an entity, refusing while inbound edges resolve unless ``force``.

    A forced removal deletes the entity and leaves the now-dangling inbound edges
    in place — ``khub check`` (FS-004) surfaces the breakage; we never repair it.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    type_, slug = resolve_id(index, id_)
    rtype = resolved.types[type_]

    inbound = _inbound_edges(index, resolved, (type_, slug))
    if inbound and not force:
        return DeleteResult(type=type_, slug=slug, removed=False, inbound=inbound)

    # ponytail: forced delete leaves dangling inbound edges on disk; `khub check`
    # (FS-004) surfaces them. Wire the surfacing to a real check run when FS-004 lands.
    base = root / (rtype.storage.path or rtype.name)
    if rtype.storage.layout == "folder":
        shutil.rmtree(base / slug)
    else:
        (base / f"{slug}.{rtype.storage.fmt}").unlink()
    return DeleteResult(type=type_, slug=slug, removed=True, inbound=inbound)


def _inbound_edges(
    index: Index, resolved: ResolvedSchema, node: tuple[str, str]
) -> list[Inbound]:
    """Every edge from another entity that resolves to ``node``."""
    inbound: list[Inbound] = []
    for (etype, eslug), emeta in index.meta.items():
        if (etype, eslug) == node:
            continue
        for predicate, rel in resolved.types[etype].relations.items():
            value = emeta.get(predicate)
            if not value:
                continue
            for target in value if isinstance(value, list) else [value]:
                if node in resolve_target(rel, str(target), index.nodes, index.types_by_slug):
                    inbound.append(Inbound(source_type=etype, source_slug=eslug, predicate=predicate))
    return inbound


# --- shared ------------------------------------------------------------------


def entity_path(root: Path, rtype: ResolvedType, slug: str) -> Path:
    """The on-disk path for ``slug`` of ``rtype``: ``_index`` under a folder, else flat."""
    base = root / (rtype.storage.path or rtype.name)
    if rtype.storage.layout == "folder":
        return base / slug / f"_index.{rtype.storage.fmt}"
    return base / f"{slug}.{rtype.storage.fmt}"


def _render_file(meta: dict[str, Any], body: str) -> str:
    """Serialize frontmatter + body to file text (empty body → just frontmatter)."""
    stream = StringIO()
    _yaml.dump(meta, stream)
    return f"---\n{stream.getvalue()}---\n{_normalize_body(body)}"


def _normalize_body(body: str) -> str:
    """A non-empty body ends in exactly one newline; an empty body stays empty."""
    if not body:
        return ""
    return body if body.endswith("\n") else body + "\n"


def _read_doc(path: Path) -> tuple[Any, str]:
    """Round-trip-load a file's frontmatter (order + comments preserved) and its body."""
    yaml_text, body = _split_frontmatter(path.read_text())
    cmap = _yaml.load(yaml_text)
    return (cmap if cmap is not None else {}), body


def _write_doc(path: Path, cmap: Any, body: str) -> None:
    """Re-serialize a round-trip map and the (unchanged) body — a minimal diff."""
    stream = StringIO()
    _yaml.dump(cmap, stream)
    path.write_text(f"---\n{stream.getvalue()}---\n{body}")


def _split_frontmatter(text: str) -> tuple[str, str]:
    """Split ``---\\n<yaml>\\n---\\n<body>`` into its YAML text and its body remainder."""
    lines = text.split("\n")
    if not lines or lines[0] != "---":
        raise LocatedError(code="malformed_entity", message=f"No frontmatter fence in {text[:20]!r}")
    for i in range(1, len(lines)):
        if lines[i] == "---":
            return "\n".join(lines[1:i]) + "\n", "\n".join(lines[i + 1 :])
    raise LocatedError(code="malformed_entity", message="Unterminated frontmatter")


def _preset(root: Path) -> str:
    from khub.core.locate import provenance

    return provenance(root).get("preset", "the")


def _to_bool(raw: str) -> bool:
    return raw.strip().lower() in {"true", "yes", "1", "on"}


def _to_number(raw: str) -> int | float:
    try:
        return int(raw)
    except ValueError:
        return float(raw)
