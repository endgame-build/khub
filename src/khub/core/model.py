"""The resolved-schema in-memory model (post base-merge) — WPK-000-1.

The single in-memory contract every surface reads: attributes (scalars/enums)
and relations (typed/union/any edges), plus khub storage config
(``layout``/``path``/``format``). Populated by ``core.resolve``; consumed by
introspection, validation, and the graph/query layers.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Literal


@dataclass(frozen=True)
class StorageConfig:
    """khub-only storage metadata — never enters LinkML validation.

    ``collection`` layout: one file holds every entity of the type as a row;
    ``path`` names that file (default ``{type}.{fmt}``) instead of a directory.
    """

    layout: Literal["file", "folder", "collection", "singleton"]
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
    inverse: str | None = None


@dataclass(frozen=True)
class ResolvedType:
    """One resolved entity type: base merged in, overrides applied, predicates resolved."""

    name: str
    storage: StorageConfig
    attributes: dict[str, ResolvedAttribute] = field(default_factory=dict)
    relations: dict[str, ResolvedRelation] = field(default_factory=dict)
    # Singleton-only (see TypeDecl.required): a missing required singleton is a
    # `check` finding.
    required: bool = False

    @property
    def collection_relpath(self) -> str:
        """The one workspace-relative path of a collection type's inventory file.

        The single source of the default-path rule (``path`` else ``{name}.{fmt}``) —
        scan, write, integrity, and gitlog all address the file through here.
        """
        return self.storage.path or f"{self.name}.{self.storage.fmt}"


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
