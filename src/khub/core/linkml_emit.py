"""khub-vocabulary -> LinkML emission — WPK-000-2.

Pure transform: a ``ResolvedSchema`` becomes a LinkML schema *dict* (which the
compiler dumps to YAML and feeds to the LinkML generators). The base block
becomes an abstract ``EntityBase`` class that every type's class inherits, so
base slots are declared once; a type emits only its delta, with base overrides
via ``slot_usage`` and the ``type`` discriminator pinned by ``equals_string``.

Relations are stored as slug strings, so they emit as ``range: string`` carrying
only cardinality (single vs ``multivalued``); the edge's target *type* is khub
graph metadata (held on the resolved model), enforced by khub's referential
integrity, not by the generated Pydantic. ``layout``/``format``/``path`` ride as
non-validating class annotations.
"""

from __future__ import annotations

import re
from typing import Any

from khub.core.model import ResolvedAttribute, ResolvedRelation, ResolvedSchema

SCHEMA_ID = "https://endgame.dev/khub/firm-ops"
SCHEMA_VERSION = "1.0.0"

_RANGE = {
    "text": "string",
    "number": "float",
    "date": "date",
    "datetime": "datetime",
    "bool": "boolean",
    "list": "string",
}


def _camel(name: str) -> str:
    return "".join(part.capitalize() for part in re.split(r"[-_]", name) if part)


def _enum_name(class_name: str, field: str) -> str:
    return f"{class_name}{_camel(field)}Enum"


def to_linkml_dict(resolved: ResolvedSchema) -> dict[str, Any]:
    """Translate a resolved khub schema into a LinkML schema dict (WPK-000-2)."""
    enums: dict[str, Any] = {}
    classes: dict[str, Any] = {}

    base_slots: dict[str, Any] = {}
    for name, attr in resolved.base_attributes.items():
        base_slots[name] = _attr_slot("EntityBase", attr, enums)
    for name, rel in resolved.base_relations.items():
        base_slots[name] = _relation_slot(rel)
    classes["EntityBase"] = {"abstract": True, "attributes": base_slots}

    for type_name, rtype in resolved.types.items():
        class_name = _camel(type_name)
        attributes: dict[str, Any] = {}
        slot_usage: dict[str, Any] = {"type": {"equals_string": type_name}}

        for name, attr in rtype.attributes.items():
            if name in resolved.base_attributes:
                if attr.overridden_from_base:
                    slot_usage[name] = _override_slot(attr)
                # else inherited from EntityBase — not re-listed
            else:
                attributes[name] = _attr_slot(class_name, attr, enums)

        for name, rel in rtype.relations.items():
            if name in resolved.base_relations:
                if rel != resolved.base_relations[name]:
                    # An overridden base edge (different cardinality/targets) must be
                    # emitted, mirroring the attribute-override path — otherwise the
                    # override is silently dropped and the base cardinality wins.
                    slot_usage[name] = _relation_override_slot(rel)
                # else inherited from EntityBase — not re-listed
            else:
                attributes[name] = _relation_slot(rel)

        cls: dict[str, Any] = {"is_a": "EntityBase", "slot_usage": slot_usage}
        if attributes:
            cls["attributes"] = attributes
        annotations: dict[str, Any] = {"layout": rtype.storage.layout, "format": rtype.storage.fmt}
        if rtype.storage.path:
            annotations["path"] = rtype.storage.path
        cls["annotations"] = annotations
        classes[class_name] = cls

    return {
        "id": SCHEMA_ID,
        "name": "firm_ops",
        "version": SCHEMA_VERSION,
        "prefixes": {"linkml": "https://w3id.org/linkml/", "khub": SCHEMA_ID + "/"},
        "default_prefix": "khub",
        "default_range": "string",
        "imports": ["linkml:types"],
        "classes": classes,
        "enums": enums,
    }


def _attr_slot(class_name: str, attr: ResolvedAttribute, enums: dict[str, Any]) -> dict[str, Any]:
    slot: dict[str, Any] = {}
    if attr.enum:
        name = _enum_name(class_name, attr.name)
        enums[name] = {"permissible_values": {value: None for value in attr.enum}}
        slot["range"] = name
    else:
        slot["range"] = _RANGE.get(attr.base_type, "string")
        if attr.base_type == "list":
            slot["multivalued"] = True
    if attr.required:
        slot["required"] = True
    if attr.pattern:
        slot["pattern"] = attr.pattern
    if attr.base_type == "bool" and attr.default is not None:
        slot["ifabsent"] = f"boolean({str(attr.default).lower()})"
    return slot


def _override_slot(attr: ResolvedAttribute) -> dict[str, Any]:
    slot: dict[str, Any] = {"required": attr.required}
    if attr.pattern:
        slot["pattern"] = attr.pattern
    return slot


def _relation_override_slot(rel: ResolvedRelation) -> dict[str, Any]:
    """A base-relation override as ``slot_usage``: pin cardinality explicitly so the
    override wins over the base slot's inherited ``multivalued``/``required``."""
    return {"required": rel.required, "multivalued": rel.many}


def _relation_slot(rel: ResolvedRelation) -> dict[str, Any]:
    slot: dict[str, Any] = {"range": "string"}
    if rel.many:
        slot["multivalued"] = True
    if rel.required:
        slot["required"] = True
    return slot
