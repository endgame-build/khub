"""The khub meta-schema — WPK-000-1.

Hand-written Pydantic models describing what a valid khub schema *file* may
contain. ``extra="forbid"`` rejects unknown / smuggled-LinkML keys (SCH-001);
``Literal`` value sets pin the legal ``layout`` and scalar ``type`` vocabulary.
``attributes`` and ``relations`` are maps keyed by name (the key is the
attribute name / the predicate — SCH-002).

This model VALIDATES the authored YAML — it is NOT generated from it. (The
generated entity models under ``.khub/generated/`` are the separate, compiled
output that validates entity frontmatter.)
"""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, field_validator

ScalarType = Literal["text", "number", "date", "datetime", "bool", "list"]


class _Strict(BaseModel):
    """Base for every meta-schema node: unknown keys are rejected."""

    model_config = ConfigDict(extra="forbid")


class AttrDecl(_Strict):
    """A scalar or enum attribute declaration. ``type`` may be omitted on an
    override (it is inherited from the base) or when ``enum`` is given."""

    type: ScalarType | None = None
    # None = not declared: an override inherits the base's required, while an
    # explicit `required: false` is distinguishable and wins over the base.
    required: bool | None = None
    default: Any = None
    enum: list[str] | None = None
    pattern: str | None = None


class RelationDecl(_Strict):
    """A relation declaration. ``to`` is a single type, a list of types (union),
    or the literal ``any``. The field name (the map key) is the predicate.
    ``inverse`` names the read-time derived edge on the target (never stored);
    firm-ops declares none."""

    to: str | list[str]
    many: bool = False
    required: bool = False
    inverse: str | None = None


class TypeDecl(_Strict):
    """One entity type's storage config plus its attribute/relation deltas.

    ``format`` is pinned to ``md`` in v1: the scan globs only ``*.md``, so any other
    format would write a write-only, invisible entity. Reject it at schema-validation
    time rather than silently.
    """

    layout: Literal["file", "folder"]
    path: str | None = None
    format: str = "md"
    attributes: dict[str, AttrDecl] = {}
    relations: dict[str, RelationDecl] = {}

    @field_validator("format")
    @classmethod
    def _only_md(cls, value: str) -> str:
        if value != "md":
            raise ValueError(
                f"format '{value}' is not yet implemented; only 'md' is supported in v1"
            )
        return value


class BaseBlock(_Strict):
    """The base block: attributes and relations every entity inherits."""

    attributes: dict[str, AttrDecl] = {}
    relations: dict[str, RelationDecl] = {}


class SchemaFile(_Strict):
    """A whole authored schema input (base header + entities)."""

    base: BaseBlock | None = None
    entities: dict[str, TypeDecl] = {}
