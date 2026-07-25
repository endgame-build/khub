"""Schema introspection — WPK-001-2.

Pure reads over the resolved ``.khub/schema.yaml``: every view derives from the
compiled contract at runtime, with no per-type code path (WS-004 / WS-SHARED-001).
``schema_view`` renders the whole schema, ``type_view`` one type, ``edges_view``
the relation vocabulary aggregated by predicate. firm-ops folded out the derived
``superseded_by`` inverse, so v1 surfaces only the stored predicates.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from ruamel.yaml.error import YAMLError

from khub.core.errors import LocatedError
from khub.core.model import ResolvedRelation, ResolvedSchema, ResolvedType
from khub.core.resolve import resolve


def load_schema(root: Path) -> ResolvedSchema:
    """Resolve the workspace's flattened ``.khub/schema.yaml``.

    A missing or unparseable ``schema.yaml`` becomes a located ``schema_error`` naming
    the file and the underlying failure — the CLI catches ``LocatedError`` cleanly, so
    a corrupt schema reports an error line instead of a raw ``FileNotFoundError`` /
    ``ParserError`` traceback. Resolver-level located errors (missing base, unknown
    target) pass through untouched.
    """
    schema_path = root / ".khub" / "schema.yaml"
    try:
        return resolve([schema_path])
    except FileNotFoundError as exc:
        raise LocatedError(
            code="schema_error",
            message=f"Cannot read schema {schema_path}: file not found",
        ) from exc
    except YAMLError as exc:
        raise LocatedError(
            code="schema_error",
            message=f"Cannot parse schema {schema_path}: {exc}",
        ) from exc


def types_list(resolved: ResolvedSchema) -> list[str]:
    """The declared type names."""
    return list(resolved.types)


def type_view(resolved: ResolvedSchema, name: str, preset: str) -> dict[str, Any]:
    """One type's fields, enums, required flags, relations, and storage layout."""
    rtype = resolved.types.get(name)
    if rtype is None:
        raise LocatedError.unknown_type(name, preset, sorted(resolved.types))
    return _type_view(rtype)


def schema_view(resolved: ResolvedSchema, provenance: dict[str, str]) -> dict[str, Any]:
    """The full effective schema: every type plus source provenance."""
    return {
        "provenance": provenance,
        "types": [_type_view(t) for t in resolved.types.values()],
    }


def _signature(rel: ResolvedRelation) -> tuple[Any, ...]:
    """Everything that makes one declaration of a predicate distinct from another."""
    return (rel.predicate, rel.targets, rel.kind, rel.many, rel.required, rel.inverse, rel.acyclic)


def edges_view(resolved: ResolvedSchema) -> list[dict[str, Any]]:
    """The relation vocabulary: one row per DISTINCT declaration, with from/to/cardinality.

    Keying rows by predicate NAME alone is lossy in a way that actively misleads. build-lite
    declares `supersedes` on adr (→ adr) and on feature-spec (→ feature-spec); one row makes
    `from` × `to` a cross product, advertising `feature-spec --supersedes--> adr` — an edge
    `validate` rejects. Merging the targets does not help: it just adds the reverse claim
    too. So a row is keyed by the whole declaration, and only types that declare a predicate
    IDENTICALLY share one. Nothing is merged, so nothing can be misreported.
    """
    # Base (universal) predicates apply to every type — their source is `any`.
    rows: dict[tuple[Any, ...], dict[str, Any]] = {
        _signature(rel): _edge(rel, sources=["any"]) for rel in resolved.base_relations.values()
    }
    sources: dict[tuple[Any, ...], set[str]] = {}
    for tname, rtype in resolved.types.items():
        for predicate, rel in rtype.relations.items():
            if predicate in resolved.base_relations:
                continue
            sig = _signature(rel)
            sources.setdefault(sig, set()).add(tname)
            rows.setdefault(sig, _edge(rel, sources=[]))
    for sig, srcs in sources.items():
        rows[sig]["from"] = sorted(srcs)
    return [rows[s] for s in sorted(rows, key=lambda s: (str(s[0]), str(s[1])))]


def _type_view(rtype: ResolvedType) -> dict[str, Any]:
    return {
        "name": rtype.name,
        "layout": rtype.storage.layout,
        "format": rtype.storage.fmt,
        "path": rtype.storage.path,
        # The two type-level gates. Surfaced because the skill tells agents to
        # discover the schema at runtime: without these, an agent cannot tell that
        # a singleton is check-required, or that a type is exempt from the orphan
        # sweep, and would read `khub check`'s silence as a bug.
        "required": rtype.required,
        "orphan": rtype.orphan,
        # The capture trigger: an agent that knows the shape still has to recognise the
        # moment, and that is per-domain knowledge only the schema can carry.
        "when": rtype.when,
        # `pattern` and `default` are enforced (write-time validation, schema defaults)
        # but were invisible here, so an agent could not tell why a value was rejected.
        "fields": [
            {
                "name": a.name,
                "type": a.base_type,
                "required": a.required,
                "enum": list(a.enum) if a.enum else None,
                "pattern": a.pattern,
                "default": a.default,
            }
            for a in rtype.attributes.values()
        ],
        "relations": [_relation_view(r) for r in rtype.relations.values()],
    }


def _relation_view(rel: ResolvedRelation) -> dict[str, Any]:
    return {
        "predicate": rel.predicate,
        "to": list(rel.targets),
        "kind": rel.kind,
        "many": rel.many,
        "required": rel.required,
        # Both are enforced and both were undiscoverable: `acyclic` decides whether
        # `check` reports a cycle, and `inverse` names a predicate that is never stored
        # yet is queryable (`--missing superseded`) and appears in `get --edges`.
        "inverse": rel.inverse,
        "acyclic": rel.acyclic,
    }


def _edge(rel: ResolvedRelation, *, sources: list[str]) -> dict[str, Any]:
    return {
        "predicate": rel.predicate,
        "from": sources,
        "to": list(rel.targets),
        "kind": rel.kind,
        "many": rel.many,
        "required": rel.required,
        "inverse": rel.inverse,
        "acyclic": rel.acyclic,
        "derived": False,
    }
