"""The resolved-schema in-memory model (post base-merge) — WPK-000-1.

Separates (a) validation-relevant parts (attributes, relations) that become
LinkML from (b) khub-only storage config (``layout``/``path``/``format``) that
LinkML cannot model. Populated by ``core.resolve``; consumed by
``core.linkml_emit``.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal


@dataclass(frozen=True)
class StorageConfig:
    """khub-only storage metadata — never enters LinkML validation."""

    layout: Literal["file", "folder"]
    path: str | None = None
    fmt: str = "md"


@dataclass(frozen=True)
class ResolvedAttribute:
    """A scalar or enum attribute on a resolved type (base merged, overrides applied)."""

    name: str
    base_type: str = "text"
    required: bool = False
    pattern: str | None = None
    enum: tuple[str, ...] | None = None
    default: Any = None
    overridden_from_base: bool = False


@dataclass(frozen=True)
class ResolvedRelation:
    """A typed / union / any edge. The predicate is the authored field name."""

    predicate: str
    targets: tuple[str, ...]
    kind: Literal["typed", "union", "any"]
    many: bool = False
    required: bool = False


@dataclass(frozen=True)
class ResolvedType:
    """One resolved entity type: base merged in, overrides applied, predicates resolved."""

    name: str
    storage: StorageConfig
    attributes: dict[str, ResolvedAttribute] = field(default_factory=dict)
    relations: dict[str, ResolvedRelation] = field(default_factory=dict)


@dataclass(frozen=True)
class ResolvedSchema:
    """All declared types, base merged in — ready for compile.

    ``base_attributes``/``base_relations`` are the resolved base block, retained
    so the compiler can emit them once on an abstract LinkML base class and emit
    only each type's delta (and overrides via ``slot_usage``).
    """

    types: dict[str, ResolvedType] = field(default_factory=dict)
    base_attributes: dict[str, ResolvedAttribute] = field(default_factory=dict)
    base_relations: dict[str, ResolvedRelation] = field(default_factory=dict)
