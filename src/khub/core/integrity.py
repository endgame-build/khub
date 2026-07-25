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

from khub.core.entity import _read_doc, entity_path
from khub.core.errors import LocatedError
from khub.core.formats import load_meta
from khub.core.graph import _predicate_digraph, build_graph
from khub.core.index import Index, build_index, filter_index, resolve_target, stray_nodes
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

# Cycle detection runs over every predicate the schema marks `acyclic: true`
# (core declares it on `depends_on`; build-hub adds `supersedes` on adr/pdr/
# feature-spec). It was hardcoded to `depends_on` until 0.11.0, which let a
# supersedes cycle through: three ADRs each superseding the next, all reported
# current, with `history` giving a different answer per entry point.


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

    @property
    def ok(self) -> bool:
        return not self.errors


def validate(
    root: Path,
    target: str | None = None,
    *,
    strict: bool = False,
) -> ValidateReport:
    """Validate present declared fields and referential integrity over the tree.

    ``target`` restricts to a type or ``type/slug`` (default: the whole workspace).
    ``--strict`` rejects undeclared keys. Every error is collected. Validate never
    writes: repairing a missing date is ``khub backfill``'s job, so the read gate
    stays a read.
    """
    resolved = load_schema(root)

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
    return ValidateReport(count=count, errors=errors)


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
        # Honour the target selector: `validate capability/cap` reported an unrelated
        # type's broken template and exited 1, so an agent checking its own entity got
        # a failure it did not cause and could not act on.
        if not _type_in_target(type_, target):
            continue
        # A broken template must not abort the run: validate's contract is to
        # collect every finding. Report it once, on the type, and move on.
        try:
            tpl = load_template(root, type_)
        except Exception as err:  # noqa: BLE001 — LocatedError or a raw YAML parse error
            reason = getattr(err, "message", None) or str(err)
            errors.append(FieldError(type=type_, slug="*", field="template", reason=reason))
            continue
        if tpl is None or not tpl.sections:
            continue  # no template, or an explicitly empty contract
        for node in sorted(valid.nodes):
            if node[0] != type_ or not _in_target(node, target):
                continue
            path = entity_path(root, rtype, node[1])
            try:
                _, body = _read_doc(path)
            except Exception:  # noqa: BLE001 — frontmatter parsed (the node exists) but the
                # full read failed; scan did NOT flag this file, so stay loud here.
                errors.append(
                    FieldError(
                        type=type_, slug=node[1], field="body",
                        reason="body could not be read for the structure check",
                    )
                )
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


def _type_in_target(type_: str, target: str | None) -> bool:
    """Whether a whole TYPE is in scope — for findings reported per type, not per node."""
    if target is None:
        return True
    return target.split("/", 1)[0] == type_


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
    """Every error on one entity — its id, present fields, relations, strict keys."""
    errors: list[FieldError] = []
    reason = _id_error(rtype, slug, meta)
    if reason:
        errors.append(FieldError(type_, slug, "id", reason))
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


_ENUMERATED_ID = re.compile(r"^(?:([a-z][a-z0-9]*)-)?(\d+)-[a-z0-9-]+$")


def _id_error(rtype: ResolvedType, slug: str, meta: dict[str, Any]) -> str | None:
    """A reason if the slug disagrees with the type's declared ``id_prefix``, else None.

    Only types that DECLARE a prefix are checked: a type without one mints a plain
    ``NNN-slug`` today, but corpora predate that and their bare slugs are legal.

    The point is the by-value form. ``requirement`` mints ``fr-`` for a functional and
    ``cst-`` for a constraint, so the prefix carries the kind — and an entity whose
    kind was edited afterwards, or whose file was hand-named, now says one thing in
    its filename and another in its frontmatter. That disagreement is invisible to
    every other gate: the enum is legal, the relations resolve, nothing dangles.

    A singleton is skipped (its slug is its type name), and so is an entity whose
    ``by`` attribute is absent — a missing ``kind`` is already reported by `check` as
    incomplete, and no id could be right until it is set.
    """
    prefix_decl = rtype.id_prefix
    if prefix_decl is None or rtype.storage.layout == "singleton":
        return None
    expected = prefix_decl.resolve(meta)
    if expected is None:
        return None  # the deciding attribute is unset; `check` reports that instead
    match = _ENUMERATED_ID.match(slug)
    if match is None:
        shape = "|".join(prefix_decl.all)
        return f"slug does not follow this type's id scheme ({shape}-NNN-slug)"
    if match.group(1) is None:
        # A bare `NNN-slug`: minted while the deciding attribute was still unset,
        # which capture-is-never-blocked permits. Filling the attribute in later
        # must not strand the entity behind a gate no verb can clear — khub has no
        # rename. The ordinal is there; the prefix is a nicety it missed.
        return None
    if match.group(1) != expected:
        deciding = f" for {prefix_decl.by} '{meta.get(prefix_decl.by)}'" if prefix_decl.by else ""
        return f"slug says '{match.group(1)}-' but the schema mints '{expected}-'{deciding}"
    return None


def _attr_error(attr: ResolvedAttribute, value: Any) -> str | None:
    """A reason string if ``value`` is illegal for ``attr``, else None.

    Enum and pattern are the strong checks; the scalar-type check is conservative —
    it flags a clear mismatch (a word where a number belongs) but never a value the
    write path already coerced and stored.
    """
    if value is None:
        return None  # an absent value is a completeness concern, not well-formedness
    if isinstance(value, str) and not value.strip():
        # `check` already counts '' as missing (via `present`), and the write path
        # already rejects it for an enum. validate let it through for a plain text
        # field, so the two gates disagreed about the same byte. null is the way to
        # say "absent" — it passes validate and is what backfill scaffolds.
        return f"{attr.name} is empty; omit the field or write null, not ''"
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
class Misplaced:
    """A file that CLAIMS to be an entity but sits where no layout looks.

    The mirror of a stray: a stray is a non-entity inside a layout, this is an
    entity outside every layout. It is the only shape of breakage the scan cannot
    see by construction — the globs follow the schema, so a file the schema does
    not cover is not "absent", it is unscanned, and every gate passes over it.
    """

    path: str
    type: str
    expected: str


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
    # Singleton types declared `required: true` with no node on disk at all (absent, or
    # present-but-stray). A drafted one is reported by `draft_singletons` instead — it is
    # right there on disk, and saying "missing" sent people hunting for a file they had.
    missing_singletons: list[str] = field(default_factory=list)
    # ANY singleton present on disk but unpublished. Not restricted to required types:
    # a drafted optional singleton silently leaves the active subgraph, and reporting it
    # nowhere meant `check` passed while the workspace had quietly lost a document.
    draft_singletons: list[str] = field(default_factory=list)
    # The subset of `draft_singletons` whose type is `required: true` — the only drafts
    # that fail the gate, since an unpublished PRD must not turn the whole gate green.
    draft_required_singletons: list[str] = field(default_factory=list)
    # Files declaring a known `type` that live outside every declared layout — an
    # entity the scan never reaches. See `Misplaced`.
    misplaced: list[Misplaced] = field(default_factory=list)

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
            or self.draft_required_singletons
            or self.misplaced
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
    # A type declaring `orphan: true` is exempt. The sweep asks "was this captured
    # and never wired in?", which presupposes an author who could have wired it —
    # false for a narrative root nothing points at by design (build-hub's edges all
    # point UP the durability ladder, and the prd sits above its top). Reporting one
    # is a finding no authoring can close, which made `check --strict` fail a
    # freshly-initialised correct workspace and so foreclosed strict mode entirely.
    # The exemption is declared per type, never inferred from `layout: singleton`:
    # a singleton that DOES carry relations must still be swept. No signal is lost
    # either way — a missing required edge is required-completeness's finding, and
    # it names the field.
    orphans = [
        f"{t}/{s}"
        for (t, s) in entity_nodes
        if graph.in_degree((t, s)) == 0
        and graph.out_degree((t, s)) == 0
        and not resolved.types[t].orphan
    ]
    # build_graph skips self-edges, so a stored self-reference on the acyclic predicate
    # never reaches nx.simple_cycles — detect it directly as a one-node cycle.
    cycles = _cycles(graph, resolved) + _self_cycles(resolved, valid, entity_nodes)
    stray_paths = sorted({_stray_locator(root, resolved, t, s) for (t, s) in strays})
    # A singleton gap is one the graph cannot express as incompleteness, so report it
    # directly. Two distinct conditions, deliberately not conflated:
    #   missing — no node on disk at all, and the type is required.
    #   draft   — present but unpublished. Swept for EVERY singleton, not just required
    #             ones: a drafted optional singleton leaves the active subgraph just as
    #             completely, and reporting it nowhere let `check` pass while the
    #             workspace had quietly lost a document.
    # Only a drafted REQUIRED singleton fails the gate — a draft is unpublished, and the
    # sibling rule already says a draft target never satisfies a required relation, so it
    # cannot satisfy its own type's requiredness either or an unpublished PRD turns the
    # whole gate green. Until 0.13.0 that fold put one drafted `prd` in both lists at once.
    singletons = [t for t, rt in resolved.types.items() if rt.storage.layout == "singleton"]
    draft_singletons = sorted(
        t
        for t in singletons
        if (t, t) in valid.nodes and as_bool(valid.meta[(t, t)].get("draft", False))
    )
    draft_required_singletons = sorted(
        t for t in draft_singletons if resolved.types[t].required
    )
    missing_singletons = sorted(
        t for t in singletons if resolved.types[t].required and (t, t) not in valid.nodes
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
        draft_required_singletons=draft_required_singletons,
        missing_singletons=missing_singletons,
        draft_singletons=draft_singletons,
        misplaced=_misplaced(root, resolved),
    )


_SKIP_DIRS = {".git", ".khub", ".kb", "node_modules", ".venv", "venv", "__pycache__"}


def _misplaced(root: Path, resolved: ResolvedSchema) -> list[Misplaced]:
    """Markdown outside every layout whose frontmatter names a type the schema knows.

    Deliberately narrow. A README carries no `type`, and a doc about something else
    carries an unknown one — neither fires. It takes a file that positively claims to
    be, say, a `component` while sitting where components are not kept, which is what
    a moved path or a swapped schema leaves behind.
    """
    scanned_dirs: set[Path] = set()
    scanned_files: set[Path] = set()
    for rtype in resolved.types.values():
        if rtype.storage.layout == "singleton" and rtype.storage.path:
            scanned_files.add((root / rtype.storage.path).resolve())
        elif rtype.storage.layout == "collection":
            scanned_files.add((root / rtype.collection_relpath).resolve())
        elif rtype.storage.path:
            scanned_dirs.add((root / rtype.storage.path).resolve())

    out: list[Misplaced] = []
    for path in sorted(root.rglob("*.md")):
        if any(part in _SKIP_DIRS or part.startswith(".") for part in path.relative_to(root).parts):
            continue
        resolved_path = path.resolve()
        if resolved_path in scanned_files:
            continue
        if any(d == resolved_path.parent or d in resolved_path.parents for d in scanned_dirs):
            continue  # inside a layout: a bad file there is a stray, reported already
        meta = load_meta(path)
        tname = (meta or {}).get("type")
        if not isinstance(tname, str) or tname not in resolved.types:
            continue
        rtype = resolved.types[tname]
        expected = rtype.storage.path or rtype.collection_relpath
        out.append(Misplaced(path=str(path.relative_to(root)), type=tname, expected=expected))
    return out


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


# `depends_on` is acyclic by contract in every khub schema — it was hardcoded here
# before the flag existed. Keep it built in: schema.yaml is copied at init and owned
# by the workspace, so a workspace created before the flag shipped carries no
# `acyclic:` key, and keying purely off the schema would silently switch cycle
# detection off for every one of them.
_ALWAYS_ACYCLIC = ("depends_on",)


def _acyclic_predicates(resolved: ResolvedSchema) -> list[str]:
    """Every acyclic predicate: the built-in contract plus whatever the schema marks."""
    declared = {
        rel.predicate
        for rtype in resolved.types.values()
        for rel in rtype.relations.values()
        if rel.acyclic
    }
    return sorted(declared | set(_ALWAYS_ACYCLIC))


def _cycles(graph: nx.MultiDiGraph, resolved: ResolvedSchema) -> list[list[str]]:
    """Elementary cycles on each acyclic-by-contract predicate, as id lists."""
    out: list[list[str]] = []
    for predicate in _acyclic_predicates(resolved):
        sub = _predicate_digraph(graph, predicate)
        out.extend([f"{t}/{s}" for (t, s) in cycle] for cycle in nx.simple_cycles(sub))
    return out


def _self_cycles(
    resolved: ResolvedSchema, index: Any, nodes: list[tuple[str, str]]
) -> list[list[str]]:
    """One-node cycles: a stored acyclic-predicate value resolving to the entity itself.

    ``build_graph`` skips self-edges, so a self-referential ``depends_on`` (or a
    self-superseding ADR) never reaches the graph cycle detector — surface it here as
    a single-node cycle ``[[id]]``. `link` refuses a self-edge, but a hand-edit, an
    import, or a merge resolution can still write one.
    """
    out: list[list[str]] = []
    predicates = _acyclic_predicates(resolved)
    for node in nodes:
        type_, slug = node
        for predicate in predicates:
            rel = resolved.types[type_].relations.get(predicate)
            if rel is None:
                continue
            value = index.meta[node].get(predicate)
            if not _present(value):
                continue
            if any(
                node in resolve_target(rel, str(target), index.nodes, index.types_by_slug)
                for target in (value if isinstance(value, list) else [value])
            ):
                out.append([f"{type_}/{slug}"])
                break  # one self-cycle entry per entity, whichever predicate caused it
    return out


def _resolved_nodes(rel: Any, value: Any, index: Any) -> set[tuple[str, str]]:
    nodes: set[tuple[str, str]] = set()
    for target in value if isinstance(value, list) else [value]:
        nodes |= resolve_target(rel, str(target), index.nodes, index.types_by_slug)
    return nodes
