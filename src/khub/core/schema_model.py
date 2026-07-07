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

from pydantic import BaseModel, ConfigDict, field_validator, model_validator

from khub.core.formats import PER_ITEM

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

    ``format`` is whitelisted to the per-item formats the scan and write paths
    implement (md, json, yaml). ``jsonl`` is collection-only and ``gjson`` is
    named in the grammar but undefined — both are rejected here rather than
    producing write-only, invisible entities.
    """

    layout: Literal["file", "folder"]
    path: str | None = None
    format: str = "md"
    attributes: dict[str, AttrDecl] = {}
    relations: dict[str, RelationDecl] = {}

    @field_validator("format")
    @classmethod
    def _known_format(cls, value: str) -> str:
        if value not in PER_ITEM:  # the one whitelist source (core.formats)
            raise ValueError(
                f"format '{value}' is not supported for a file/folder layout; "
                "use md, json, or yaml (jsonl is collection-only, gjson is deferred)"
            )
        return value

    @model_validator(mode="after")
    def _body_reserved_off_md(self) -> "TypeDecl":
        # On a json/yaml type the `body` key is the prose channel: a field so
        # named would be popped out of meta on every read (unqueryable, failing
        # `required` despite being on disk) and clobbered on write. Reject it
        # here rather than silently reshaping data.
        if self.format != "md" and ("body" in self.attributes or "body" in self.relations):
            raise ValueError(
                f"'body' is reserved on a {self.format} type (it is the prose channel); "
                "rename the field or use format: md"
            )
        return self


class BaseBlock(_Strict):
    """The base block: attributes and relations every entity inherits."""

    attributes: dict[str, AttrDecl] = {}
    relations: dict[str, RelationDecl] = {}


class SchemaFile(_Strict):
    """A whole authored schema input (base header + entities)."""

    base: BaseBlock | None = None
    entities: dict[str, TypeDecl] = {}
