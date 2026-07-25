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

from pathlib import Path
from typing import Annotated, Any, Literal

from pydantic import BaseModel, ConfigDict, StringConstraints, model_validator

from khub.core.formats import COLLECTION, PER_ITEM

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
    # `check` reports elementary and self cycles over every acyclic predicate.
    acyclic: bool = False


# A prefix is a slug token: it is concatenated with the ordinal and the slugified
# title, and everything else in an id is lowercase alphanumeric. An empty one would
# mint a leading hyphen; a hyphenated one would make the prefix unreadable back out
# of the id.
ID_PREFIX_RE = r"^[a-z][a-z0-9]*$"


class IdPrefixDecl(_Strict):
    """A prefix chosen by the value of another attribute (``by``), one per enum member.

    ``requirement`` mints ``fr-`` for a functional and ``cst-`` for a constraint, so
    the prefix carries the kind and a mislabelled file is visible in its filename.
    """

    by: str
    map: dict[str, Annotated[str, StringConstraints(pattern=ID_PREFIX_RE)]]


class TypeDecl(_Strict):
    """One entity type's storage config plus its attribute/relation deltas.

    The format/layout matrix (whitelists sourced from ``core.formats``):
    ``file``/``folder`` take md, json, or yaml (one entity per file);
    ``collection`` takes json, jsonl, or yaml (one file, row-level entities;
    ``path`` names the file, and the format may be derived from its suffix).
    ``gjson`` is named in the grammar but undefined — rejected everywhere.
    """

    layout: Literal["file", "folder", "collection", "singleton"]
    path: str | None = None
    format: str = "md"
    # Singleton-only: `check` reports a missing required singleton. Meaningless
    # (and rejected) on the other layouts — per-entity requiredness lives on
    # attributes/relations.
    required: bool = False
    # Opt out of the orphan sweep: `orphan: true` declares that edge-less is this
    # type's expected state, so `check` stops reporting its instances as orphans
    # (and `--strict` stops failing on them). For a narrative root nothing points
    # at by design — a prd, an arc42 — orphan-ness is a finding no authoring can
    # close. Declared per type rather than inferred from `layout: singleton`: a
    # singleton that DOES carry relations should still be swept, and a non-
    # singleton type may legitimately be edge-less.
    orphan: bool = False
    # Enumerated ids: `add` mints `<prefix>-NNN-<slug>` instead of a bare slug, with
    # NNN the next free number for that prefix. A convention several presets already
    # carried in prose (`ad-NNN`, `fr-NNN`) and every author had to type by hand into
    # `--id`; declaring it makes the schema mint it. Absent = khub's plain slug.
    id_prefix: Annotated[str, StringConstraints(pattern=ID_PREFIX_RE)] | IdPrefixDecl | None = None
    attributes: dict[str, AttrDecl] = {}
    relations: dict[str, RelationDecl] = {}

    @model_validator(mode="after")
    def _id_prefix_matches_its_enum(self) -> TypeDecl:
        """A by-value prefix must name a declared enum and cover every member.

        Otherwise a legal `kind` mints no id, and the failure surfaces at `add`
        time on one unlucky entity rather than when the schema is read.
        """
        spec = self.id_prefix
        if not isinstance(spec, IdPrefixDecl):
            return self
        attr = self.attributes.get(spec.by)
        if attr is None or not attr.enum:
            raise ValueError(
                f"id_prefix.by '{spec.by}' must name an attribute of this type "
                "that declares an enum"
            )
        missing = [member for member in attr.enum if member not in spec.map]
        unknown = [key for key in spec.map if key not in attr.enum]
        if missing or unknown:
            raise ValueError(
                f"id_prefix.map must cover exactly {spec.by}'s enum; "
                f"missing {missing or '[]'}, unknown {unknown or '[]'}"
            )
        return self

    @model_validator(mode="after")
    def _storage_matrix(self) -> TypeDecl:
        if self.required and self.layout != "singleton":
            raise ValueError(
                "'required' is singleton-only (a required file/folder/collection "
                "type has no single artifact to require)"
            )
        if self.layout == "singleton":
            if not self.path:
                raise ValueError("a singleton type needs path: the exact file it lives at")
            suffix = Path(self.path).suffix.lstrip(".")
            explicit = "format" in self.model_fields_set
            fmt = self.format if explicit else (suffix or "md")
            if fmt not in PER_ITEM:
                raise ValueError(
                    f"format '{fmt}' is not supported for a singleton; use md, json, or yaml"
                )
            if suffix and suffix != fmt:
                raise ValueError(f"path suffix '.{suffix}' disagrees with format '{fmt}'")
            self.format = fmt
        elif self.layout == "collection":
            suffix = Path(self.path).suffix.lstrip(".") if self.path else ""
            # model_fields_set distinguishes an authored `format: md` (rejected —
            # md is never a collection format) from the field default (derivable
            # from the path suffix).
            explicit = "format" in self.model_fields_set
            fmt = self.format if explicit else suffix
            if not fmt:
                raise ValueError(
                    "a collection type needs format: json|jsonl|yaml "
                    "(or a path carrying that extension)"
                )
            if fmt not in COLLECTION:
                raise ValueError(
                    f"format '{fmt}' is not a collection format; use json, jsonl, or yaml "
                    "(md is per-item only)"
                )
            if explicit and suffix and suffix != fmt:
                raise ValueError(f"path suffix '.{suffix}' disagrees with format '{fmt}'")
            self.format = fmt
            for reserved in ("slug", "type"):
                if reserved in self.attributes or reserved in self.relations:
                    raise ValueError(
                        f"'{reserved}' is a reserved row key on a collection type "
                        "(row identity / the schema binding); rename the field"
                    )
        elif self.format not in PER_ITEM:
            raise ValueError(
                f"format '{self.format}' is not supported for a file/folder layout; "
                "use md, json, or yaml (jsonl is collection-only, gjson is deferred)"
            )
        # On any non-md type the `body` key is the prose channel: a field so
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
