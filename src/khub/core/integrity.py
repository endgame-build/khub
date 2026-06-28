"""The v1 integrity gate — ``khub validate`` and ``khub check`` (WPK-004-1, FS-004).

Two verbs, two engines, shipped together as the single v1 gate:

- ``validate`` is per-entity. It checks each *present* declared field against the
  schema (type, enum, pattern, cardinality), confirms every relation resolves, and
  leaves undeclared extensions alone unless ``--strict`` closes the schema. It
  collects every error rather than stopping at the first (REQ-INT001-02). It does
  not enforce required-completeness — that is ``check``'s job.
- ``check`` is graph-wide over the active (``draft: false``) subgraph. It computes
  required-completeness from the schema (never from the ``draft`` flag —
  REQ-INT002-05), and finds the structural gaps that define it: active-but-
  incomplete entities, drafts that cannot satisfy a required relation, orphans,
  dangling edges, stray files, and edge cycles.

Both uphold one invariant: khub guarantees structural integrity, never semantic
truth (INT-SHARED-003). A false-but-legal write passes both gates by design — git
history, not a gate, is the backstop.
"""

from __future__ import annotations

import math
import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import networkx as nx

from khub.core.entity import _read_doc, _write_doc, entity_path
from khub.core.graph import _predicate_digraph, build_graph
from khub.core.index import build_index, filter_index, resolve_target, stray_nodes
from khub.core.introspect import load_schema
from khub.core.model import ResolvedAttribute, ResolvedSchema, ResolvedType

# ponytail: `depends_on` is the acyclic-by-contract predicate (the "hard
# dependency" edge), so cycle detection runs over it. Lift to a per-predicate
# acyclic flag in the schema if another predicate ever needs the same guard.
_ACYCLIC_PREDICATE = "depends_on"


# --- validate ----------------------------------------------------------------


@dataclass(frozen=True)
class FieldError:
    """One per-entity validation failure: which entity, which field, why."""

    type: str
    slug: str
    field: str
    reason: str

    @property
    def id(self) -> str:
        return f"{self.type}/{self.slug}"


@dataclass(frozen=True)
class ValidateReport:
    """The outcome of a validate run: how many typed entities, and every error."""

    count: int  # typed entities validated (reference markdown is never scanned)
    errors: list[FieldError]
    fixed: list[str] = field(default_factory=list)  # ids whose `updated` --fix backfilled

    @property
    def ok(self) -> bool:
        return not self.errors


def validate(
    root: Path,
    target: str | None = None,
    *,
    strict: bool = False,
    fix: bool = False,
) -> ValidateReport:
    """Validate present declared fields and referential integrity over the tree.

    ``target`` restricts to a type or ``type/slug`` (default: the whole workspace).
    ``--strict`` rejects undeclared keys. ``--fix`` (v1 scope) backfills a missing
    ``updated`` date from ``git log`` before validating. Every error is collected.
    """
    resolved = load_schema(root)
    fixed = _fix_updated(root, resolved, target) if fix else []

    index = build_index(root, resolved)
    valid = filter_index(index, stray_nodes(index))  # strays are not entities of their layout
    errors: list[FieldError] = []
    count = 0
    for node in sorted(valid.nodes):
        type_, slug = node
        if not _in_target(node, target):
            continue
        count += 1
        rtype = resolved.types[type_]
        errors.extend(_validate_entity(rtype, type_, slug, valid.meta[node], valid, strict=strict))
    return ValidateReport(count=count, errors=errors, fixed=fixed)


def _in_target(node: tuple[str, str], target: str | None) -> bool:
    """Whether a node falls inside the (optional) ``target`` selector."""
    if target is None:
        return True
    type_, slug = node
    if "/" in target:
        return f"{type_}/{slug}" == target
    return type_ == target


def _validate_entity(
    rtype: ResolvedType,
    type_: str,
    slug: str,
    meta: dict[str, Any],
    index: Any,
    *,
    strict: bool,
) -> list[FieldError]:
    """Every error on one entity — present fields, relations, and strict keys."""
    errors: list[FieldError] = []
    for key, value in meta.items():
        if key in rtype.attributes:
            reason = _attr_error(rtype.attributes[key], value)
            if reason:
                errors.append(FieldError(type_, slug, key, reason))
        elif key in rtype.relations:
            errors.extend(_relation_errors(rtype, type_, slug, key, value, index))
        elif strict:
            errors.append(FieldError(type_, slug, key, "undeclared key rejected under --strict"))
        # else: an undeclared key is an allowed extension — left unchecked.
    return errors


def _attr_error(attr: ResolvedAttribute, value: Any) -> str | None:
    """A reason string if ``value`` is illegal for ``attr``, else None.

    Enum and pattern are the strong checks; the scalar-type check is conservative —
    it flags a clear mismatch (a word where a number belongs) but never a value the
    write path already coerced and stored.
    """
    if value is None:
        return None  # an absent value is a completeness concern, not well-formedness
    if attr.enum is not None:
        if str(value) not in attr.enum:
            return f"'{value}' is not a valid {attr.name} ({', '.join(attr.enum)})"
        return None
    if attr.pattern is not None and not re.fullmatch(attr.pattern, str(value)):
        return f"'{value}' does not match the pattern for {attr.name} ({attr.pattern})"
    return _type_error(attr, value)


def _type_error(attr: ResolvedAttribute, value: Any) -> str | None:
    kind = attr.base_type
    if kind == "bool" and not _is_bool(value):
        return f"'{value}' is not a valid bool for {attr.name}"
    if kind == "number" and not _is_number(value):
        return f"'{value}' is not a valid number for {attr.name}"
    if kind in ("date", "datetime") and not _is_dateish(value):
        return f"'{value}' is not a valid {kind} for {attr.name}"
    return None


def _relation_errors(
    rtype: ResolvedType, type_: str, slug: str, predicate: str, value: Any, index: Any
) -> list[FieldError]:
    """Cardinality and referential-integrity errors for one relation value."""
    rel = rtype.relations[predicate]
    errors: list[FieldError] = []
    if isinstance(value, list) and not rel.many and len(value) > 1:
        errors.append(
            FieldError(type_, slug, predicate, "single-valued relation has multiple values")
        )
    values = value if isinstance(value, list) else [value]
    for target in values:
        if not _present(target):
            continue
        if not resolve_target(rel, str(target), index.nodes, index.types_by_slug):
            errors.append(
                FieldError(
                    type_,
                    slug,
                    predicate,
                    f"no {'/'.join(rel.targets)} '{target}' to satisfy relation '{predicate}'",
                )
            )
    return errors


def _fix_updated(root: Path, resolved: ResolvedSchema, target: str | None) -> list[str]:
    """Backfill a missing ``updated`` from git for each in-target entity lacking one.

    The only repair v1 ``--fix`` performs (the broader auto-repair is deferred).
    Honors the ``target`` selector — a scoped run touches only the named entity, not
    the whole tree — and skips strays (a stray is not an entity to repair). Reuses the
    shared ``git log`` date helper; entities outside git, or already carrying
    ``updated``, are left untouched.
    """
    from khub.core.gitlog import has_git_history, last_commit_date

    if not has_git_history(root):
        return []
    index = build_index(root, resolved)
    valid = filter_index(index, stray_nodes(index))
    fixed: list[str] = []
    for node in sorted(valid.nodes):
        if not _in_target(node, target):
            continue
        type_, slug = node
        if valid.meta[node].get("updated"):
            continue
        path = entity_path(root, resolved.types[type_], slug)
        git_date = last_commit_date(root, str(path.relative_to(root)))
        if git_date is None:
            continue
        cmap, body = _read_doc(path)
        cmap["updated"] = git_date
        _write_doc(path, cmap, body)
        fixed.append(f"{type_}/{slug}")
    return fixed


# --- check -------------------------------------------------------------------


@dataclass(frozen=True)
class Incomplete:
    """An active entity missing required fields and/or required relations."""

    type: str
    slug: str
    missing_fields: list[str]
    missing_relations: list[str]

    @property
    def id(self) -> str:
        return f"{self.type}/{self.slug}"


@dataclass(frozen=True)
class Dangling:
    """A stored relation value that no longer resolves to an existing entity."""

    type: str
    slug: str
    predicate: str
    target: str

    @property
    def id(self) -> str:
        return f"{self.type}/{self.slug}"


@dataclass(frozen=True)
class CheckReport:
    """The graph-wide structural verdict — empty everywhere means pass."""

    incomplete: list[Incomplete]
    orphans: list[str]
    dangling: list[Dangling]
    strays: list[str]
    cycles: list[list[str]]

    @property
    def passed(self) -> bool:
        return not (self.incomplete or self.orphans or self.dangling or self.strays or self.cycles)


def check(root: Path) -> CheckReport:
    """Walk the active subgraph for completeness, orphans, dangling, strays, cycles.

    Strays are dropped from the working index up front, so every downstream check —
    edge resolution, degree, completeness — sees only real entities: an edge that
    points at a stray dangles instead of silently resolving to a non-entity.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    strays = stray_nodes(index)
    valid = filter_index(index, strays)
    graph = build_graph(valid)
    entity_nodes = sorted(valid.nodes)

    incomplete = _incomplete(resolved, valid, entity_nodes)
    dangling = _dangling(resolved, valid, entity_nodes)
    orphans = [
        f"{t}/{s}"
        for (t, s) in entity_nodes
        if graph.in_degree((t, s)) == 0 and graph.out_degree((t, s)) == 0
    ]
    cycles = _cycles(graph)
    stray_paths = sorted(
        str(entity_path(root, resolved.types[t], s).relative_to(root)) for (t, s) in strays
    )
    return CheckReport(
        incomplete=incomplete,
        orphans=orphans,
        dangling=dangling,
        strays=stray_paths,
        cycles=cycles,
    )


def _incomplete(
    resolved: ResolvedSchema, index: Any, nodes: list[tuple[str, str]]
) -> list[Incomplete]:
    """Active entities missing a required field or required relation.

    Completeness is derived from the schema over the active subgraph only: a draft
    is exempt, and a draft target never satisfies another entity's required
    relation. A required relation whose value resolves to *nothing* is a dangling
    edge (reported separately), not counted here.
    """
    out: list[Incomplete] = []
    for node in nodes:
        type_, slug = node
        meta = index.meta[node]
        if meta.get("draft"):
            continue  # drafts are exempt from required-completeness
        rtype = resolved.types[type_]
        missing_fields = [
            a.name
            for a in rtype.attributes.values()
            if a.required and not _present(meta.get(a.name))
        ]
        missing_relations: list[str] = []
        for predicate, rel in rtype.relations.items():
            if not rel.required:
                continue
            value = meta.get(predicate)
            if not _present(value):
                missing_relations.append(predicate)
                continue
            resolved_to = _resolved_nodes(rel, value, index)
            if not resolved_to:
                continue  # present but unresolvable → dangling, not incomplete
            if not any(not index.meta[t].get("draft") for t in resolved_to):
                missing_relations.append(predicate)  # all targets are drafts
        if missing_fields or missing_relations:
            out.append(Incomplete(type_, slug, missing_fields, missing_relations))
    return out


def _dangling(
    resolved: ResolvedSchema, index: Any, nodes: list[tuple[str, str]]
) -> list[Dangling]:
    """Stored relation values that resolve to no existing entity."""
    out: list[Dangling] = []
    for node in nodes:
        type_, slug = node
        meta = index.meta[node]
        for predicate, rel in resolved.types[type_].relations.items():
            value = meta.get(predicate)
            if not _present(value):
                continue
            for target in value if isinstance(value, list) else [value]:
                if not _present(target):
                    continue
                if not resolve_target(rel, str(target), index.nodes, index.types_by_slug):
                    out.append(Dangling(type_, slug, predicate, str(target)))
    return out


def _cycles(graph: nx.MultiDiGraph) -> list[list[str]]:
    """Elementary cycles on the acyclic-by-contract predicate, as id lists."""
    sub = _predicate_digraph(graph, _ACYCLIC_PREDICATE)
    return [[f"{t}/{s}" for (t, s) in cycle] for cycle in nx.simple_cycles(sub)]


def _resolved_nodes(rel: Any, value: Any, index: Any) -> set[tuple[str, str]]:
    nodes: set[tuple[str, str]] = set()
    for target in value if isinstance(value, list) else [value]:
        nodes |= resolve_target(rel, str(target), index.nodes, index.types_by_slug)
    return nodes


# --- value predicates --------------------------------------------------------


def _present(value: Any) -> bool:
    """Whether a required value is actually supplied (not blank/empty)."""
    if value is None:
        return False
    if isinstance(value, str):
        return value.strip() != ""
    if isinstance(value, list):
        return len(value) > 0
    return True


def _is_bool(value: Any) -> bool:
    return isinstance(value, bool) or (
        isinstance(value, str) and value.strip().lower() in {"true", "false", "yes", "no", "1", "0", "on", "off"}
    )


def _is_number(value: Any) -> bool:
    if isinstance(value, bool):
        return False
    if isinstance(value, (int, float)):
        return math.isfinite(value)
    if isinstance(value, str):
        try:
            return math.isfinite(float(value))  # reject inf/nan: a finite number is meant
        except ValueError:
            return False
    return False


def _is_dateish(value: Any) -> bool:
    """Strict ISO date/datetime check (validation gate, not the lenient staleness read).

    ``project._as_date`` slices to the first 10 chars for staleness display, which would
    pass ``2026-01-01 junk``; validating a field demands the whole value parse.
    """
    from datetime import date, datetime

    if isinstance(value, (date, datetime)):
        return True
    text = str(value)
    for parse in (date.fromisoformat, datetime.fromisoformat):
        try:
            parse(text)
            return True
        except ValueError:
            continue
    return False
