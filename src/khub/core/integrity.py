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

import re
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

import networkx as nx

from khub.core.entity import _read_doc, _write_doc, entity_path
from khub.core.errors import LocatedError
from khub.core.graph import _predicate_digraph, build_graph
from khub.core.index import build_index, filter_index, resolve_target, stray_nodes
from khub.core.introspect import load_schema
from khub.core.model import ResolvedAttribute, ResolvedSchema, ResolvedType

# The write gate and the integrity gate check the same values via one module, so a
# value the write path accepts is exactly a value validate accepts (draft included).
# Re-bound as module attributes (not bare import-aliases) so `backfill` can keep
# importing `_present` from here, with these predicates now sharing one source.
from khub.core.values import as_bool, is_bool, is_dateish, is_number, present

_present = present
_is_bool = is_bool
_is_number = is_number
_is_dateish = is_dateish

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

    # Body-structure contract: an md type with a workspace template requires the
    # template's section headings in every instance body, in order (extras
    # allowed). The index carries frontmatter only, so bodies are re-read here —
    # and only for templated types.
    errors.extend(_body_structure_errors(root, resolved, valid, target))

    # A file inside a layout that could not be parsed is a frontmatter error, not a
    # silent skip — one bad file is reported, never a raised ParserError.
    malformed_errors = _malformed_errors(resolved, index.malformed, target)
    errors.extend(malformed_errors)

    # A target that selects nothing is a typo (`--stagee`) returning a false-clean pass,
    # EXCEPT a bare target naming a declared type — a type with zero entities is a
    # legitimate count-0 run.
    if target is not None and count == 0 and not malformed_errors:
        is_declared_type = "/" not in target and target in resolved.types
        if not is_declared_type:
            raise LocatedError(
                code="validate_target",
                message=(
                    f"No entity or type matches validate target '{target}'; "
                    "use a type (e.g. client) or a type/slug (e.g. client/acme)"
                ),
                target=target,
            )
    return ValidateReport(count=count, errors=errors, fixed=fixed)


def _body_structure_errors(
    root: Path, resolved: ResolvedSchema, valid: Index, target: str | None
) -> list[FieldError]:
    """Template-contract findings: per md entity of a templated type, the first
    template heading missing (or out of order) in the body's H2 sequence."""
    from khub.core.template import load_template, missing_heading

    errors: list[FieldError] = []
    for type_, rtype in resolved.types.items():
        if rtype.storage.fmt != "md" or rtype.storage.layout == "collection":
            continue
        tpl = load_template(root, type_)
        if tpl is None:
            continue
        for node in sorted(valid.nodes):
            if node[0] != type_ or not _in_target(node, target):
                continue
            path = entity_path(root, rtype, node[1])
            try:
                _, body = _read_doc(path)
            except Exception:  # noqa: BLE001 — unparseable files are reported elsewhere
                continue
            missing = missing_heading(tpl, body)
            if missing is not None:
                errors.append(
                    FieldError(
                        type=type_,
                        slug=node[1],
                        field="body",
                        reason=(
                            f"missing or out-of-order section '## {missing}' "
                            f"(template {type_}.yaml requires its headings in order)"
                        ),
                    )
                )
    return errors


def _in_target(node: tuple[str, str], target: str | None) -> bool:
    """Whether a node falls inside the (optional) ``target`` selector."""
    if target is None:
        return True
    type_, slug = node
    if "/" in target:
        return f"{type_}/{slug}" == target
    return type_ == target


def _malformed_errors(
    resolved: ResolvedSchema, malformed: list[Path], target: str | None
) -> list[FieldError]:
    """A ``frontmatter`` error per unparseable file, located to its layout type/slug.

    A malformed COLLECTION file matches any target of its type: `validate
    repo/acme` must report "the inventory is broken", not misdiagnose the row
    id as a typo (the row exists — it is unreadable).
    """
    errors: list[FieldError] = []
    for relpath in malformed:
        type_, slug = _malformed_identity(resolved, relpath)
        whole_type = (
            type_ in resolved.types
            and resolved.types[type_].storage.layout == "collection"
            and target is not None
            and target.split("/")[0] == type_
        )
        if not _in_target((type_, slug), target) and not whole_type:
            continue
        errors.append(
            FieldError(type_, slug, "frontmatter", f"could not parse frontmatter of {relpath}")
        )
    return errors


def _malformed_identity(resolved: ResolvedSchema, relpath: Path) -> tuple[str, str]:
    """The (type, slug) a malformed file belongs to, derived from its layout path."""
    for tname, rtype in resolved.types.items():
        if rtype.storage.layout == "collection":
            if str(relpath) == rtype.collection_relpath:  # the file IS the whole inventory
                return tname, relpath.stem
            continue
        base = rtype.storage.path
        if not base:
            continue
        try:
            rel = relpath.relative_to(base)
        except ValueError:
            continue
        if rtype.storage.layout == "folder":
            return tname, rel.parts[0] if rel.parts else relpath.stem
        return tname, relpath.stem
    return "", str(relpath)


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
        matches = resolve_target(rel, str(target), index.nodes, index.types_by_slug)
        if not matches:
            errors.append(
                FieldError(
                    type_,
                    slug,
                    predicate,
                    f"no {'/'.join(rel.targets)} '{target}' to satisfy relation '{predicate}'",
                )
            )
        elif "/" not in str(target) and len(matches) > 1:
            # A stored bare slug resolving to >1 node is ambiguous — the write path
            # rejects it, but a hand-authored file bypasses that gate; qualify it.
            candidates = ", ".join(sorted(f"{t}/{s}" for t, s in matches))
            errors.append(
                FieldError(
                    type_,
                    slug,
                    predicate,
                    f"'{target}' is ambiguous — qualify as type/slug (candidates: {candidates})",
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
        if resolved.types[type_].storage.layout == "collection":
            # A collection file's commit date is not a row's date — attributing it
            # would be wrong for every row but the last-touched one. Rows get
            # `updated` from add/edit; row-diff date attribution is the named
            # fast-follow (docs/collections-design.md).
            continue
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
    malformed: list[str] = field(default_factory=list)
    # Orphans are informational by default — a fully disconnected entity can be
    # legitimate (a dormant client whose engagements were archived). ``strict``
    # makes a fully connected graph a gate requirement.
    strict: bool = False
    # Dangling reports suppressed because their target type's collection file is
    # malformed — derivative noise rolled into the malformed finding.
    suppressed_dangling: int = 0
    # Singleton types declared `required: true` whose file does not exist.
    missing_singletons: list[str] = field(default_factory=list)

    @property
    def passed(self) -> bool:
        return not (
            self.incomplete
            or (self.orphans if self.strict else [])
            or self.dangling
            or self.strays
            or self.cycles
            or self.malformed
            or self.missing_singletons
        )


def check(root: Path, *, strict: bool = False) -> CheckReport:
    """Walk the active subgraph for completeness, orphans, dangling, strays, cycles.

    Strays are dropped from the working index up front, so every downstream check —
    edge resolution, degree, completeness — sees only real entities: an edge that
    points at a stray dangles instead of silently resolving to a non-entity. A file
    that could not be parsed is a malformed entry, not a node, and fails the check.
    """
    resolved = load_schema(root)
    index = build_index(root, resolved)
    strays = stray_nodes(index)
    valid = filter_index(index, strays)
    graph = build_graph(valid)
    entity_nodes = sorted(valid.nodes)

    incomplete = _incomplete(resolved, valid, entity_nodes)
    dangling = _dangling(resolved, valid, entity_nodes)
    # A malformed COLLECTION file removes every row of its type at once, so each
    # edge into that type would dangle derivatively — suppress those and count
    # them: the actionable error is "fix the file", not N dangles burying it.
    # `check` still fails on the malformed entry itself.
    malformed_set = {str(p) for p in index.malformed}
    broken_types = {
        t
        for t, rt in resolved.types.items()
        if rt.storage.layout == "collection" and rt.collection_relpath in malformed_set
    }
    suppressed = 0
    if broken_types:
        kept: list[Dangling] = []
        for d in dangling:
            if _derivative_dangle(resolved, d, broken_types):
                suppressed += 1
            else:
                kept.append(d)
        dangling = kept
    orphans = [
        f"{t}/{s}"
        for (t, s) in entity_nodes
        if graph.in_degree((t, s)) == 0 and graph.out_degree((t, s)) == 0
    ]
    # build_graph skips self-edges, so a stored self-reference on the acyclic predicate
    # never reaches nx.simple_cycles — detect it directly as a one-node cycle.
    cycles = _cycles(graph) + _self_cycles(resolved, valid, entity_nodes)
    stray_paths = sorted({_stray_locator(root, resolved, t, s) for (t, s) in strays})
    # A required singleton with no live node (absent file, or present-but-stray)
    # is a gap the graph cannot express as incompleteness — report it directly.
    missing_singletons = sorted(
        t
        for t, rt in resolved.types.items()
        if rt.storage.layout == "singleton" and rt.required and (t, t) not in valid.nodes
    )
    return CheckReport(
        incomplete=incomplete,
        orphans=orphans,
        dangling=dangling,
        strays=stray_paths,
        cycles=cycles,
        malformed=[str(p) for p in index.malformed],
        strict=strict,
        suppressed_dangling=suppressed,
        missing_singletons=missing_singletons,
    )


def _derivative_dangle(resolved: ResolvedSchema, d: Dangling, broken: set[str]) -> bool:
    """Whether a dangle is derivative of a malformed collection (suppress) or real (keep).

    Suppress only when the target provably points into a broken type: a
    qualified ``type/slug`` naming it, or a bare slug whose EVERY declared home
    is broken. A union edge with a healthy alternative target type is kept —
    the dangle might be a genuinely missing entity of the healthy type, and
    hiding it until the collection is repaired would mislead. ``any``-kind bare
    slugs are likewise kept (their home is unknowable while the file is down).
    """
    rel = resolved.types[d.type].relations[d.predicate]
    if "/" in d.target:
        return d.target.split("/", 1)[0] in broken
    return rel.kind != "any" and set(rel.targets) <= broken


def _stray_locator(root: Path, resolved: ResolvedSchema, type_: str, slug: str) -> str:
    """A stray's address: its file path, or ``path#slug`` for a collection row
    (N stray rows in one file must not collapse into N copies of the same path)."""
    rtype = resolved.types[type_]
    rel = str(entity_path(root, rtype, slug).relative_to(root))
    return f"{rel}#{slug}" if rtype.storage.layout == "collection" else rel


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
        if as_bool(meta.get("draft", False)):
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
            if not any(not as_bool(index.meta[t].get("draft", False)) for t in resolved_to):
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


def _self_cycles(
    resolved: ResolvedSchema, index: Any, nodes: list[tuple[str, str]]
) -> list[list[str]]:
    """One-node cycles: a stored acyclic-predicate value resolving to the entity itself.

    ``build_graph`` skips self-edges, so a self-referential ``depends_on`` never reaches
    the graph cycle detector — surface it here as a single-node cycle ``[[id]]``.
    """
    out: list[list[str]] = []
    for node in nodes:
        type_, slug = node
        rel = resolved.types[type_].relations.get(_ACYCLIC_PREDICATE)
        if rel is None:
            continue
        value = index.meta[node].get(_ACYCLIC_PREDICATE)
        if not _present(value):
            continue
        for target in value if isinstance(value, list) else [value]:
            if node in resolve_target(rel, str(target), index.nodes, index.types_by_slug):
                out.append([f"{type_}/{slug}"])
                break
    return out


def _resolved_nodes(rel: Any, value: Any, index: Any) -> set[tuple[str, str]]:
    nodes: set[tuple[str, str]] = set()
    for target in value if isinstance(value, list) else [value]:
        nodes |= resolve_target(rel, str(target), index.nodes, index.types_by_slug)
    return nodes
