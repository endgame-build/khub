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


def edges_view(resolved: ResolvedSchema) -> list[dict[str, Any]]:
    """The relation vocabulary aggregated by predicate, with from/to/cardinality."""
    # Base (universal) predicates apply to every type — their source is `any`.
    edges: dict[str, dict[str, Any]] = {}
    for predicate, rel in resolved.base_relations.items():
        edges[predicate] = _edge(rel, sources=["any"])
    # Type-declared predicates accumulate their declaring types as `from`.
    sources: dict[str, set[str]] = {}
    for tname, rtype in resolved.types.items():
        for predicate, rel in rtype.relations.items():
            if predicate in resolved.base_relations:
                continue
            sources.setdefault(predicate, set()).add(tname)
            edge = edges.setdefault(predicate, _edge(rel, sources=[]))
            edge["required"] = edge["required"] or rel.required
    for predicate, srcs in sources.items():
        edges[predicate]["from"] = sorted(srcs)
    return [edges[p] for p in sorted(edges)]


def _type_view(rtype: ResolvedType) -> dict[str, Any]:
    return {
        "name": rtype.name,
        "layout": rtype.storage.layout,
        "format": rtype.storage.fmt,
        "path": rtype.storage.path,
        "fields": [
            {
                "name": a.name,
                "type": a.base_type,
                "required": a.required,
                "enum": list(a.enum) if a.enum else None,
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
    }


def _edge(rel: ResolvedRelation, *, sources: list[str]) -> dict[str, Any]:
    return {
        "predicate": rel.predicate,
        "from": sources,
        "to": list(rel.targets),
        "kind": rel.kind,
        "many": rel.many,
        "required": rel.required,
        "derived": False,
    }
