"""Schema resolver — WPK-000-1.

``resolve`` turns authored khub-vocabulary YAML into a ``ResolvedSchema``: parse
via the ``schema_model`` meta-schema (which rejects smuggled raw LinkML), merge
the base block into every type, apply per-type overrides, treat each relation's
field name as its predicate, and reject malformed declarations (unknown target,
missing base) with located errors — before any artifact is written.
"""

from __future__ import annotations

from pathlib import Path
from typing import Any

from pydantic import ValidationError
from ruamel.yaml import YAML

from khub.core.errors import LocatedError
from khub.core.model import (
    ResolvedAttribute,
    ResolvedRelation,
    ResolvedSchema,
    ResolvedType,
    StorageConfig,
)
from khub.core.schema_model import AttrDecl, BaseBlock, RelationDecl, SchemaFile, TypeDecl

_yaml = YAML(typ="safe")


def resolve(schema_files: list[Path]) -> ResolvedSchema:
    """Resolve authored schema files into a ``ResolvedSchema`` (WPK-000-1)."""
    base_raw: Any = None
    entities_raw: dict[str, Any] = {}
    for f in schema_files:
        data = load_yaml(Path(f))
        if data.get("base") is not None:
            base_raw = data["base"]
        for name, decl in (data.get("entities") or {}).items():
            entities_raw[name] = decl

    raw: dict[str, Any] = {"entities": entities_raw}
    if base_raw is not None:
        raw["base"] = base_raw

    try:
        schema = SchemaFile.model_validate(raw)
    except ValidationError as exc:
        raise _smuggled_error(exc) from exc

    if schema.entities and schema.base is None:
        raise LocatedError.missing_base()

    declared = set(schema.entities)
    base = schema.base or BaseBlock()
    types = {
        name: _resolve_type(name, decl, base, declared)
        for name, decl in schema.entities.items()
    }
    base_attributes = {an: _attr(an, ad, overridden=False) for an, ad in base.attributes.items()}
    base_relations = {
        rn: _relation("(base)", rn, rd, declared) for rn, rd in base.relations.items()
    }
    return ResolvedSchema(types=types, base_attributes=base_attributes, base_relations=base_relations)


def load_yaml(path: Path) -> dict[str, Any]:
    with path.open() as fh:
        data = _yaml.load(fh)
    return data or {}


def _resolve_type(
    name: str, decl: TypeDecl, base: BaseBlock, declared: set[str]
) -> ResolvedType:
    # Attributes: base first, then the type's delta (an existing key is an override).
    attributes: dict[str, ResolvedAttribute] = {
        an: _attr(an, ad, overridden=False) for an, ad in base.attributes.items()
    }
    for an, ad in decl.attributes.items():
        if an in attributes:
            attributes[an] = _override_attr(an, attributes[an], ad)
        else:
            attributes[an] = _attr(an, ad, overridden=False)

    # Relations: universal edges from the base, then the type's own edges.
    relations: dict[str, ResolvedRelation] = {
        rn: _relation(name, rn, rd, declared) for rn, rd in base.relations.items()
    }
    for rn, rd in decl.relations.items():
        relations[rn] = _relation(name, rn, rd, declared)

    storage = StorageConfig(layout=decl.layout, path=decl.path, fmt=decl.format)
    return ResolvedType(name=name, storage=storage, attributes=attributes, relations=relations)


def _attr(name: str, ad: AttrDecl, *, overridden: bool) -> ResolvedAttribute:
    return ResolvedAttribute(
        name=name,
        base_type=ad.type or "text",
        required=bool(ad.required),  # None (undeclared) means not required here
        pattern=ad.pattern,
        enum=tuple(ad.enum) if ad.enum else None,
        default=ad.default,
        overridden_from_base=overridden,
    )


def _override_attr(name: str, base_attr: ResolvedAttribute, ad: AttrDecl) -> ResolvedAttribute:
    # The type-level declaration wins, but an override that only tightens one facet
    # (e.g. `updated: { required: true }`) must not silently drop the base's
    # required/pattern/enum/default — inherit each the override does not redeclare.
    return ResolvedAttribute(
        name=name,
        base_type=ad.type or base_attr.base_type,
        required=base_attr.required if ad.required is None else ad.required,
        pattern=ad.pattern if ad.pattern is not None else base_attr.pattern,
        enum=tuple(ad.enum) if ad.enum else base_attr.enum,
        default=ad.default if ad.default is not None else base_attr.default,
        overridden_from_base=True,
    )


def _relation(type_name: str, predicate: str, rd: RelationDecl, declared: set[str]) -> ResolvedRelation:
    to = rd.to
    if to == "any":
        return ResolvedRelation(
            predicate=predicate, targets=("any",), kind="any",
            many=rd.many, required=rd.required, inverse=rd.inverse,
        )
    if isinstance(to, list):
        for target in to:
            if target not in declared:
                raise LocatedError.unknown_target(type_name, predicate, target)
        return ResolvedRelation(
            predicate=predicate, targets=tuple(to), kind="union",
            many=rd.many, required=rd.required, inverse=rd.inverse,
        )
    # single typed target
    if to not in declared:
        raise LocatedError.unknown_target(type_name, predicate, to)
    return ResolvedRelation(
        predicate=predicate, targets=(to,), kind="typed",
        many=rd.many, required=rd.required, inverse=rd.inverse,
    )


def _smuggled_error(exc: ValidationError) -> LocatedError:
    for err in exc.errors():
        if err["type"] == "extra_forbidden":
            loc = err["loc"]
            construct = str(loc[-1])
            type_name = str(loc[1]) if len(loc) > 1 and loc[0] == "entities" else None
            return LocatedError.raw_linkml_smuggled(type_name, construct, ".".join(map(str, loc)))
    first = exc.errors()[0]
    where = ".".join(map(str, first["loc"]))
    return LocatedError(code="invalid_schema", message=f"Invalid schema at {where}: {first['msg']}")
