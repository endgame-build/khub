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
    # Cycle-checked by `check`. A hierarchy predicate (depends_on, supersedes) is
    # acyclic by contract; a plain association (related, affects) is not.
    acyclic: bool = False


@dataclass(frozen=True)
class IdPrefix:
    """A type's enumerated-id policy: a literal prefix, or one per enum member.

    See ``TypeDecl.id_prefix``. Frozen and tuple-backed so ``ResolvedType`` stays
    hashable; ``by``/``members`` are empty for the literal form.
    """

    literal: str | None = None
    by: str | None = None
    members: tuple[tuple[str, str], ...] = ()

    def resolve(self, attributes: dict[str, Any]) -> str | None:
        """The prefix for one entity's attributes, or None when its ``by`` is absent."""
        if self.literal is not None:
            return self.literal
        value = attributes.get(self.by or "")
        return dict(self.members).get(value) if isinstance(value, str) else None

    @property
    def all(self) -> tuple[str, ...]:
        if self.literal is not None:
            return (self.literal,)
        return tuple(dict.fromkeys(prefix for _, prefix in self.members))


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
    # See TypeDecl.orphan: this type's instances are exempt from the orphan sweep.
    orphan: bool = False
    # See TypeDecl.id_prefix: `add` mints `<prefix>-NNN-<slug>` when this is set.
    id_prefix: IdPrefix | None = None
    # See TypeDecl.when: the moment to capture this type, in domain language.
    when: str | None = None

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
