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

The write gate is deliberately strict about corruption an author would never see
until later: ``type`` is pinned to the layout type (a user field cannot clobber
the discriminator); a slug is length-capped and minted with an O_EXCL write so a
concurrent add cannot lose one; an explicit ``--id`` collision refuses rather than
silently suffixing; date/bool/number fields are validated to exactly what
``validate`` accepts (via ``khub.core.values``); a bare relation target that
resolves to more than one node, or to the source itself, is refused with a clean
error. The edit path tolerates a BOM / leading blank lines so any file the reader
can show, the editor can also write.
"""

from __future__ import annotations

import os
import re
import shutil
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import date
from pathlib import Path
from typing import Any

from khub.core import formats
from khub.core.errors import LocatedError
from khub.core.index import Index, build_index, canonical_slug, resolve_target
from khub.core.introspect import load_schema
from khub.core.model import ResolvedAttribute, ResolvedRelation, ResolvedSchema, ResolvedType
from khub.core.template import load_template
from khub.core.values import as_bool, is_bool, is_dateish, is_number

# The longest slug we mint or accept; an over-long --id would otherwise crash at
# path.write_text with an OSError (filename too long) instead of a located error.
_MAX_SLUG = 100

@dataclass(frozen=True)
class CreateResult:
    """The outcome of a create: where it landed and whether it is a draft.

    ``locator`` (``path#slug``) is set only for a collection row — ``path``
    alone addresses a per-item entity, so file/folder records stay unchanged.
    """

    type: str
    slug: str
    path: Path
    draft: bool
    locator: str | None = None


@dataclass(frozen=True)
class UpdateResult:
    """The outcome of an edit: where it landed and whether it is now a draft."""

    type: str
    slug: str
    path: Path
    draft: bool
    locator: str | None = None


@dataclass(frozen=True)
class LinkResult:
    """The outcome of a link/unlink, for the confirmation line.

    ``changed`` is False when the edge already existed (link) or was already absent
    (unlink), so no file was rewritten — the CLI uses it to distinguish a real
    mutation from a no-op.
    """

    type: str
    slug: str
    predicate: str
    target: str
    changed: bool


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
    """A read of one entity: its frontmatter, body, and optionally its edges.

    For a collection row, ``raw`` is the row's stored serialization (never the
    whole file) and ``locator`` addresses it as ``path#slug``.
    """

    type: str
    slug: str
    path: Path
    meta: dict[str, Any]
    body: str
    raw: str
    edges: list[Edge] | None = None
    locator: str | None = None


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
    use_template: bool = True,
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
    # Template seeding: an md type with a workspace template and no explicit body
    # starts from the scaffold (--no-template / use_template=False opts out).
    # A broken template never blocks capture — seed nothing and let `validate`
    # report the template itself.
    if not body.strip() and rtype.storage.fmt == "md":
        try:
            tpl = load_template(root, type_)
        except Exception:  # noqa: BLE001 — validate carries the template finding
            tpl = None
        if tpl is not None and tpl.sections:
            if not use_template:
                # --no-template on a templated type produced an entity `validate` rejects
                # on its very next run ("missing or out-of-order section '## X'"): a
                # documented flag whose only outcome was a red workspace. Refuse instead.
                raise LocatedError(
                    code="template_required",
                    message=(
                        f"Type '{type_}' has a body template, so --no-template would create "
                        f"an entity `khub validate` rejects. Omit the flag, or pass --body "
                        f"with the template's sections"
                    ),
                )
            body = tpl.render()
    body = _md_normalized(body, rtype)

    # Referential integrity (and bare-target ambiguity) hard-fail before any byte
    # is written.
    for predicate, values in rels.items():
        rel = rtype.relations[predicate]
        for value in values:
            _resolve_write_target(rel, value, index, predicate)

    # `draft` is manual: the --draft flag, or an explicit `draft` field, else false.
    explicit = attrs.pop("draft", None)
    is_draft = bool(explicit) if explicit is not None else draft

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

    if rtype.storage.layout == "singleton":
        # The slug IS the type name; an --id saying otherwise is a mistake, not a request.
        if id_ is not None and id_ != type_:
            raise LocatedError(
                code="singleton_id",
                message=f"'{type_}' is a singleton — its id is always '{type_}' (drop --id)",
            )
        slug = type_
        path = entity_path(root, rtype, slug)
        path.parent.mkdir(parents=True, exist_ok=True)
        try:
            _write_new(path, meta, body)
        except FileExistsError:
            raise LocatedError(
                code="singleton_exists",
                message=f"Singleton '{type_}' already exists at {path.relative_to(root)}; "
                "edit it instead of adding another",
            ) from None
        return CreateResult(type=type_, slug=slug, path=path, draft=is_draft)

    if rtype.storage.layout == "collection":
        base = (_slug_base(id_) if id_ is not None
                else _minted_base(rtype, attrs, index))  # collections number too
        slug = _create_row(root, rtype, type_, meta, body, base, explicit=id_ is not None)
        return CreateResult(
            type=type_,
            slug=slug,
            path=entity_path(root, rtype, slug),
            draft=is_draft,
            locator=_locator(root, rtype, slug),
        )

    # Slug + O_EXCL write. An explicit --id collision refuses (never auto-suffixes);
    # a minted slug retries on the next -N suffix if a concurrent add reached it first.
    if id_ is not None:
        slug = _explicit_slug(id_, type_, index)
        path = entity_path(root, rtype, slug)
        path.parent.mkdir(parents=True, exist_ok=True)
        try:
            _write_new(path, meta, body)
        except FileExistsError:
            raise LocatedError(
                code="slug_taken",
                message=f"Slug '{slug}' is already taken in {type_}; choose another --id",
            ) from None
    else:
        base = _minted_base(rtype, attrs, index)
        slug, path = _mint_and_write(root, rtype, base, type_, index, meta, body)
    return CreateResult(type=type_, slug=slug, path=path, draft=is_draft)


def _create_row(
    root: Path,
    rtype: ResolvedType,
    type_: str,
    meta: dict[str, Any],
    body: str,
    base: str,
    *,
    explicit: bool,
) -> str:
    """Insert one new row into a collection; returns the (minted or explicit) slug.

    Uniqueness is gated by the fresh in-lock read, not the index: an explicit
    ``--id`` collision refuses (never auto-suffixes) and a minted slug takes the
    first free ``base``/``base-N``. The row omits ``type`` (the collection's
    schema binding supplies it at scan) and carries prose in the reserved
    ``body`` key.
    """
    row = {k: v for k, v in meta.items() if k != "type"}
    if body:
        row["body"] = body

    def mutate(rows: dict[str, Any]) -> tuple[bool, str]:
        if explicit:
            if base in rows:
                raise LocatedError(
                    code="slug_taken",
                    message=f"Slug '{base}' is already taken in {type_}; choose another --id",
                )
            rows[base] = row
            return True, base
        n, slug = 1, base
        while slug in rows:
            n += 1
            slug = f"{base}-{n}"
        rows[slug] = row
        return True, slug

    slug: str = _mutate_collection(root, rtype, mutate)
    return slug


def _partition(
    rtype: ResolvedType, fields: dict[str, str], *, strict: bool
) -> tuple[dict[str, Any], dict[str, list[str]], dict[str, Any]]:
    """Split raw fields into validated attributes, relation value-lists, and extras."""
    attrs: dict[str, Any] = {}
    rels: dict[str, list[str]] = {}
    extras: dict[str, Any] = {}
    for key, raw in fields.items():
        # The discriminator is set by the command, not the caller: a user `type`
        # field disagreeing with the layout type would silently mis-file the entity.
        if key == "type" and raw != rtype.name:
            raise LocatedError(
                code="type_field_forbidden",
                message=f"The 'type' field is set by the command ({rtype.name}); it cannot be overridden",
            )
        # On a json/yaml type `body` is the prose channel (the reserved key the
        # reader pops); written as a field it would be clobbered by the next
        # render. md keeps `body` as an ordinary frontmatter field.
        if key == "body" and rtype.storage.fmt != "md":
            raise LocatedError(
                code="body_field_reserved",
                message=f"'body' is reserved on a {rtype.storage.fmt} entity; "
                "pass --body/--body-file for prose",
            )
        if key in rtype.attributes:
            if isinstance(raw, str) and not raw.strip():
                # `--field ""` means "clear it". Store null, not '': null is absent
                # (validate passes, `check` reports the gap), while '' is malformed
                # by khub's own rule — writing it here would produce an entity that
                # fails the gate the moment it lands. Mirrors `unlink` for relations.
                attrs[key] = None
                continue
            attrs[key] = _validate_attr(rtype.attributes[key], raw)
        elif key in rtype.relations:
            rel = rtype.relations[key]
            values = [v.strip() for v in raw.split(",") if v.strip()] if raw else []
            # A blank value has no meaning as an edge — refuse it (unlink removes);
            # a comma-list on a single-valued relation used to drop its tail silently —
            # refuse it with the same cardinality error `link` raises.
            if not values:
                raise LocatedError(
                    code="empty_relation_value",
                    message=f"Empty value for relation '{key}'; use unlink to remove an edge",
                )
            if not rel.many and len(values) > 1:
                raise LocatedError.cardinality_violation(key)
            rels[key] = values
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
        return _to_number(raw, attr.name)
    if attr.base_type in ("date", "datetime"):
        # Mirror the number gate: reject a value `validate` would flag (e.g. an
        # impossible 2026-13-45) at write time, not after it has landed on disk.
        if not is_dateish(raw):
            raise LocatedError(
                code="date_violation",
                message=f"'{raw}' is not a valid {attr.base_type} for {attr.name}",
            )
        return _as_dateobj(raw)
    if attr.base_type == "list":
        return [v.strip() for v in raw.split(",")]
    return raw


def _as_dateobj(raw: str) -> Any:
    """An ISO string as a date/datetime object, so YAML stores it unquoted.

    ``add`` writes its ``created``/``updated`` defaults as date objects
    (``created: 2026-07-06``); a user-supplied string kept as ``str`` would
    serialize quoted (``updated: '2026-01-01'``) — same value, noisier diff.
    """
    from datetime import date, datetime

    for parse in (date.fromisoformat, datetime.fromisoformat):
        try:
            return parse(raw)
        except ValueError:
            continue
    return raw  # unreachable behind is_dateish; keep the value rather than crash


def _slug_source(type_: str, attrs: dict[str, Any]) -> str:
    """The string a minted slug derives from: a name, else a title, else the type.

    firm-ops meetings/fragments carry no ``name``, so the title fallback keeps their
    slugs meaningful instead of collapsing every one to the bare type name.
    """
    for key in ("name", "title"):
        value = attrs.get(key)
        if value:
            return str(value)
    return type_


def _slug_base(source: str) -> str:
    """A minted slug's base: slugified, non-empty, and within the length cap."""
    base = slugify(source)
    if not base:  # an all-symbol/empty source would write a hidden, collision-blind file
        raise LocatedError.invalid_slug(source)
    if len(base) > _MAX_SLUG:
        raise LocatedError(
            code="invalid_slug",
            message=f"Slug '{base[:40]}…' exceeds {_MAX_SLUG} characters",
        )
    return base


_ORDINAL_WIDTH = 3


def _minted_base(rtype: ResolvedType, attrs: dict[str, Any], index: Index) -> str:
    """A minted slug: ``<prefix>-<number>-<slug>``, or ``<number>-<slug>`` with no prefix.

    Every minted id carries an ordinal, so a corpus reads in the order it was
    authored and an entity can be named in prose by a short stable handle
    (``ad-004``) instead of a whole title. The number is zero-padded to three and
    keeps counting past it — 001, 045, 1200.
    """
    base = _slug_base(_slug_source(rtype.name, attrs))
    prefix = rtype.id_prefix.resolve(attrs) if rtype.id_prefix else None
    stem = f"{prefix}-" if prefix else ""
    return f"{stem}{_next_ordinal(index, rtype.name, prefix):0{_ORDINAL_WIDTH}d}-{base}"


def _next_ordinal(index: Index, type_: str, prefix: str | None) -> int:
    """One past the highest ordinal in use for this type (and prefix, if any).

    Counted per prefix, not per type: a requirement schema minting ``fr-`` and
    ``cst-`` keeps two independent sequences, which is what makes the number
    readable as "the fourth constraint" rather than an arbitrary position.
    """
    pattern = re.compile(rf"^{re.escape(prefix)}-(\d+)-" if prefix else r"^(\d+)-")
    used = [
        int(m.group(1))
        for node_type, slug in index.nodes
        if node_type == type_ and (m := pattern.match(slug))
    ]
    return max(used, default=0) + 1


def _explicit_slug(id_: str, type_: str, index: Index) -> str:
    """An explicit --id's slug: slugified, capped, and unique — a collision refuses.

    Unlike a minted slug, an explicit id is not auto-suffixed: the caller named it,
    so a within-type collision is an error to surface, not a slug to invent.
    """
    base = _slug_base(id_)
    if (type_, base) in index.nodes:
        raise LocatedError(
            code="slug_taken",
            message=f"Slug '{base}' is already taken in {type_}; choose another --id",
        )
    return base


def _mint_and_write(
    root: Path,
    rtype: ResolvedType,
    base: str,
    type_: str,
    index: Index,
    meta: dict[str, Any],
    body: str,
) -> tuple[str, Path]:
    """Pick the first free ``base``/``base-N`` slug and write it with O_EXCL.

    The index gives a cheap first guess; the exclusive create is the real gate, so a
    second add racing to the same slug loses the O_EXCL and retries the next suffix
    instead of clobbering the winner.
    """
    n, slug = 1, base
    while True:
        if (type_, slug) in index.nodes:
            n += 1
            slug = f"{base}-{n}"
            continue
        path = entity_path(root, rtype, slug)
        path.parent.mkdir(parents=True, exist_ok=True)
        try:
            _write_new(path, meta, body)
            return slug, path
        except FileExistsError:
            n += 1
            slug = f"{base}-{n}"


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
    if rtype.storage.layout == "collection":
        rows = formats.load_collection(path.read_text(encoding="utf-8"), rtype.storage.fmt)
        row = rows.get(slug)
        if row is None:  # indexed a moment ago; the row vanished mid-command
            raise LocatedError.lookup_error(id_)
        meta, body = formats.split_row(row, rtype.storage.fmt)
        meta.setdefault("type", type_)  # the binding supplies it, as at scan
        raw = formats.render_row(slug, row, rtype.storage.fmt)  # the row, never the file
    else:
        raw = path.read_text()
        meta, body = formats.parse(raw, formats.fmt_of(path))
    view_edges = _edges(index, resolved, type_, slug, meta) if edges else None
    return EntityView(
        type=type_,
        slug=slug,
        path=path,
        meta=meta,
        body=body,
        raw=raw,
        edges=view_edges,
        locator=_locator(root, rtype, slug),
    )


def _same_target(a: str, b: str) -> bool:
    """Two target spellings naming one node. Case-insensitive, because lookup is."""
    return a.casefold() == b.casefold()


def _canonical_target(target: str, matches: set[tuple[str, str]]) -> str:
    """The spelling to STORE for a resolved target, preserving the caller's qualified form.

    Resolution folds case, so storing the caller's raw string let ``CMP-001-Api`` and
    ``cmp-001-api`` sit side by side as two parallel edges to ONE node, each reporting
    ``changed: true`` — and made an ``unlink`` of the other spelling a silent no-op. One
    node, one stored value.
    """
    if len(matches) != 1:
        return target
    ttype, tslug = next(iter(matches))
    return f"{ttype}/{tslug}" if "/" in target else tslug


def resolve_id(index: Index, id_: str) -> tuple[str, str]:
    """Resolve a bare slug (or qualified ``type/slug``) to one ``(type, slug)`` node.

    A bare slug shared by two types is ambiguous; a ``type/slug`` qualifier is exact.

    Case is resolved leniently as a fallback (see ``canonical_slug``): writes slugify to
    lowercase, so an agent that reuses the ``--id`` it passed must still be able to read
    the entity back.
    """
    if "/" in id_:
        type_, slug = id_.split("/", 1)
        if (type_, slug) in index.nodes:
            return type_, slug
        canon = canonical_slug(slug, index.types_by_slug)
        if canon is not None:
            for t in index.types_by_slug[canon]:
                if t.casefold() == type_.casefold():
                    return t, canon
        raise LocatedError.lookup_error(id_)
    types = index.types_by_slug.get(id_)
    if not types:
        canon = canonical_slug(id_, index.types_by_slug)
        if canon is not None:
            id_, types = canon, index.types_by_slug[canon]
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
            matches = _resolve_write_target(rel, value, index, predicate)
            if (type_, slug) in matches:  # same self-edge gate as `link`
                raise LocatedError(
                    code="self_link",
                    message=f"Cannot link '{id_}' to itself via '{predicate}'",
                )

    path = entity_path(root, rtype, slug)

    def apply(cmap: Any) -> None:
        for name, value in attrs.items():
            cmap[name] = value
        for predicate, values in rels.items():
            rel = rtype.relations[predicate]
            cmap[predicate] = values if rel.many else values[0]
        for key, raw in extras.items():
            cmap[key] = raw
        # Auto-bump `updated`, unless the user backdated it explicitly in this edit
        # (reconciling an import): their value wins over today.
        if "updated" not in attrs and "updated" not in extras:
            cmap["updated"] = date.today()

    if rtype.storage.layout == "collection":

        def mutate(rows: dict[str, Any]) -> tuple[bool, bool]:
            row = rows.get(slug)
            if row is None:
                raise LocatedError.lookup_error(id_)
            apply(row)
            if body is not None:
                if body:
                    row["body"] = body
                else:
                    row.pop("body", None)  # --body '' clears (empty body writes no key)
            return True, as_bool(row.get("draft", False))

        new_draft: bool = _mutate_collection(root, rtype, mutate)
        return UpdateResult(
            type=type_, slug=slug, path=path, draft=new_draft,
            locator=_locator(root, rtype, slug),
        )

    cmap, body_text = _read_doc(path)
    apply(cmap)
    _write_doc(path, cmap, body_text if body is None else _md_normalized(body, rtype))
    # as_bool, not truthiness: a hand-authored draft: "false" must report active,
    # matching how check/query/status read the same flag.
    return UpdateResult(type=type_, slug=slug, path=path, draft=as_bool(cmap.get("draft", False)))


# --- link / unlink -----------------------------------------------------------


def link(root: Path, id_: str, predicate: str, target: str) -> LinkResult:
    """Add a schema-checked edge ``predicate → target`` on the source entity.

    A no-op link (the edge already exists) leaves the file untouched and returns
    ``changed=False``.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    type_, slug = resolve_id(index, id_)
    rtype = resolved.types[type_]
    rel = rtype.relations.get(predicate)
    if rel is None:
        raise LocatedError.illegal_predicate(predicate, type_)
    matches = _resolve_write_target(rel, target, index, predicate, noun="predicate")
    if (type_, slug) in matches:  # a self-edge connects nothing; the graph skips it
        raise LocatedError(
            code="self_link",
            message=f"Cannot link '{id_}' to itself via '{predicate}'",
        )

    target = _canonical_target(target, matches)

    def apply(cmap: Any) -> bool:
        existing = cmap.get(predicate)
        changed = False
        if rel.many:
            values = _as_list(existing)  # a scalar many-value reads as [value], never char-split
            if not any(_same_target(str(v), target) for v in values):
                values.append(target)
                changed = True
            cmap[predicate] = values
        else:
            current = None if existing in (None, "") else str(existing)
            if current is not None and not _same_target(current, target):
                raise LocatedError.cardinality_violation(predicate)
            changed = current != target
            cmap[predicate] = target
        return changed

    changed = _apply_edge_mutation(root, rtype, slug, id_, apply)
    return LinkResult(type=type_, slug=slug, predicate=predicate, target=target, changed=changed)


def unlink(root: Path, id_: str, predicate: str, target: str) -> LinkResult:
    """Remove the edge ``predicate → target`` from the source; inverses recompute.

    A no-op unlink (no such edge) leaves the file untouched and returns
    ``changed=False``.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    type_, slug = resolve_id(index, id_)
    rtype = resolved.types[type_]
    rel = rtype.relations.get(predicate)
    if rel is None:
        raise LocatedError.illegal_predicate(predicate, type_)

    def apply(cmap: Any) -> bool:
        existing = cmap.get(predicate)
        changed = False
        if rel.many:
            values = _as_list(existing)  # a scalar many-value reads as [value], never char-split
            if any(_same_target(str(v), target) for v in values):
                remaining = [v for v in values if not _same_target(str(v), target)]
                if remaining:
                    cmap[predicate] = remaining
                else:
                    del cmap[predicate]
                changed = True
        elif existing is not None and _same_target(str(existing), target):
            del cmap[predicate]
            changed = True
        return changed

    changed = _apply_edge_mutation(root, rtype, slug, id_, apply)
    return LinkResult(type=type_, slug=slug, predicate=predicate, target=target, changed=changed)


def _apply_edge_mutation(
    root: Path, rtype: ResolvedType, slug: str, id_: str, apply: Callable[[Any], bool]
) -> bool:
    """Run one link/unlink mutation against per-item or collection storage.

    Per-item: round-trip read, apply, write only when changed (no spurious
    rewrite). Collection: the same mutation on the row inside the locked
    read-modify-write; an unchanged row skips the file write the same way.
    """
    if rtype.storage.layout == "collection":

        def mutate(rows: dict[str, Any]) -> tuple[bool, bool]:
            row = rows.get(slug)
            if row is None:
                raise LocatedError.lookup_error(id_)
            changed = apply(row)
            return changed, changed

        result: bool = _mutate_collection(root, rtype, mutate)
        return result

    path = entity_path(root, rtype, slug)
    cmap, body = _read_doc(path)
    changed = apply(cmap)
    if changed:
        _write_doc(path, cmap, body)
    return changed


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
    if rtype.storage.layout == "collection":

        def mutate(rows: dict[str, Any]) -> tuple[bool, None]:
            if slug not in rows:
                raise LocatedError.lookup_error(id_)
            del rows[slug]  # an emptied collection keeps its (empty) file
            return True, None

        _mutate_collection(root, rtype, mutate)
        return DeleteResult(type=type_, slug=slug, removed=True, inbound=inbound)
    # Reuse the one path resolver rather than re-deriving: it is the only place that
    # knows a singleton's `path` IS the file, not a directory to append a name to.
    path = entity_path(root, rtype, slug)
    if rtype.storage.layout == "folder":
        shutil.rmtree(path.parent)  # the entity is the folder, not just its _index
    else:
        path.unlink()
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
    """The on-disk path for ``slug`` of ``rtype``: ``_index`` under a folder, flat
    for a file layout, the shared inventory file for a collection (rows have no
    path of their own — ``path#slug`` is the row locator)."""
    if rtype.storage.layout == "collection":
        return _collection_file(root, rtype)
    if rtype.storage.layout == "singleton":
        # One fixed file regardless of slug (slug is the type name).
        return root / (rtype.storage.path or f"{rtype.name}.{rtype.storage.fmt}")
    base = root / (rtype.storage.path or rtype.name)
    if rtype.storage.layout == "folder":
        return base / slug / f"_index.{rtype.storage.fmt}"
    return base / f"{slug}.{rtype.storage.fmt}"


def _collection_file(root: Path, rtype: ResolvedType) -> Path:
    """A collection type's one file (the shared default-path rule lives on the model)."""
    return root / rtype.collection_relpath


def _locator(root: Path, rtype: ResolvedType, slug: str) -> str | None:
    """The row address ``path#slug`` for a collection row; None for per-item layouts."""
    if rtype.storage.layout != "collection":
        return None
    return f"{_collection_file(root, rtype).relative_to(root)}#{slug}"


def _mutate_collection(
    root: Path, rtype: ResolvedType, mutate: Callable[[dict[str, Any]], tuple[bool, Any]]
) -> Any:
    """The one write path for every collection mutation.

    Exclusive flock on a sidecar (``.khub/generated/locks/<type>.lock`` — under
    ``generated/`` so it inherits the gitignore and the deletable-anytime
    contract; never the data file, since ``os.replace`` swaps the inode and a
    lock on the collection itself would guard a dead inode after the first
    writer's replace), fresh in-lock read (the O_EXCL replacement; the
    pre-built index is only a hint), mutate, then write-temp + fsync + rename —
    a crash never leaves a torn file. ``mutate`` returns ``(write, result)``:
    ``write=False`` skips the rewrite (idempotent no-op) and ``result`` is
    passed through to the caller. A malformed collection refuses the write:
    khub never rewrites a file it cannot fully round-trip. Cross-host
    concurrency stays deferred — git is the merge surface (FS-002).
    # ponytail: fcntl flock is POSIX-only; an msvcrt shim lands if Windows ever matters.
    """
    import fcntl

    path = _collection_file(root, rtype)
    lock_dir = root / ".khub" / "generated" / "locks"
    lock_dir.mkdir(parents=True, exist_ok=True)
    with (lock_dir / f"{rtype.name}.lock").open("w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        try:
            text = path.read_text(encoding="utf-8") if path.exists() else ""
            try:
                rows = formats.load_collection(text, rtype.storage.fmt)
            except Exception as err:  # noqa: BLE001 — any parse failure is the same refusal
                raise LocatedError(
                    code="malformed_entity",
                    message=f"Refusing to write {path.name}: cannot round-trip it ({err})",
                ) from None
            write, result = mutate(rows)
            if not write:  # no-op: leave the file untouched (no spurious rewrite)
                return result
            path.parent.mkdir(parents=True, exist_ok=True)
            tmp = path.with_name(path.name + ".tmp")
            with tmp.open("w", encoding="utf-8") as fh:
                fh.write(formats.dump_collection(rows, rtype.storage.fmt))
                fh.flush()
                os.fsync(fh.fileno())
            os.replace(tmp, path)
            return result
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


def _write_new(path: Path, meta: dict[str, Any], body: str) -> None:
    """Write a brand-new entity file exclusively (O_EXCL): fail if the slug exists.

    The exclusive create is the atomic slug-uniqueness gate — the index check is only
    a hint, so a concurrent add racing to the same path raises FileExistsError here
    rather than silently overwriting the first writer.
    """
    with path.open("x", encoding="utf-8") as fh:
        fh.write(formats.render(meta, body, formats.fmt_of(path)))


def _as_list(value: Any) -> list[Any]:
    """A stored many-relation value as a list: [] if blank, itself if a list, else [value].

    A many-relation authored as a scalar (``related: alice``) must be read as
    ``['alice']`` before mutation — iterating the string would split it into characters.
    """
    if not value:
        return []
    return list(value) if isinstance(value, list) else [value]


def _resolve_write_target(
    rel: ResolvedRelation, target: str, index: Index, predicate: str, *, noun: str = "relation"
) -> set[tuple[str, str]]:
    """The nodes a write-time relation value resolves to, gated for the write verbs.

    An unresolvable target hard-fails (referential integrity); a bare slug that hits
    more than one node is ambiguous and must be qualified as ``type/slug`` (a qualified
    id already resolves to exactly one). Returns the match set so ``link`` can spot a
    self-edge.
    """
    matches = resolve_target(rel, target, index.nodes, index.types_by_slug)
    if not matches:
        raise LocatedError.referential_integrity("/".join(rel.targets), target, predicate, noun=noun)
    if "/" not in target and len(matches) > 1:
        raise LocatedError.ambiguous_slug(target, sorted(f"{t}/{s}" for t, s in matches))
    return matches


def _md_normalized(body: str, rtype: ResolvedType) -> str:
    """Newly supplied md prose ends in a newline (file-format nicety).

    Applied only where NEW body text enters (create, edit --body) and only for
    md — a round-tripped body is written byte-for-byte (minimal diff, ENT-007),
    and a non-md ``body`` field stores the string verbatim.
    """
    if rtype.storage.fmt == "md" and body and not body.endswith("\n"):
        return body + "\n"
    return body


def _read_doc(path: Path) -> tuple[Any, str]:
    """Round-trip-load a document (order + comments preserved) and its body.

    Kept under this name — ``integrity._body_structure_errors`` and
    ``backfill._apply`` import it; the per-format dispatch lives in ``core.formats``.
    """
    return formats.read_doc(path)


def _write_doc(path: Path, cmap: Any, body: str) -> None:
    """Re-serialize a round-trip map and the (unchanged) body — a minimal diff."""
    path.write_text(formats.render(cmap, body, formats.fmt_of(path)))


def _preset(root: Path) -> str:
    from khub.core.locate import provenance

    return provenance(root).get("preset", "the")


def _to_bool(raw: str) -> bool:
    """Coerce a bool-ish string the way ``validate`` reads it, or raise a located error.

    Re-uses the shared predicates so the write gate accepts exactly the BOOLISH set
    ``validate`` accepts — 'banana' is rejected here, not silently stored as False.
    """
    if not is_bool(raw):
        raise LocatedError(
            code="bool_violation",
            message=f"'{raw}' is not a valid boolean (true/false, yes/no, 1/0, on/off)",
        )
    return as_bool(raw)


def _to_number(raw: str, field: str) -> int | float:
    """Coerce ``raw`` to a finite int/float, or raise a located error.

    Delegates the finite-number check to ``values.is_number`` (the same gate
    ``validate`` uses) and keeps the located-error wrapping: a non-numeric or
    non-finite string raises here instead of a bare ValueError traceback, and never
    lands on disk for a later ``validate`` to flag.
    """
    if not is_number(raw):
        raise LocatedError.number_violation(raw, field)
    try:
        return int(raw)
    except ValueError:
        return float(raw)
