"""Located errors raised by the resolver and compiler.

Each error carries the located fields (``type``, ``relation``, ``target``) so
tests assert on structure plus a message substring, keeping the spec's exact
strings as the canonical example (TS-000 Risks). Factories build the located
fields and the canonical message together, so the two can never drift.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass
class LocatedError(Exception):
    """A schema error that names where it occurred."""

    code: str
    message: str
    type: str | None = None
    relation: str | None = None
    target: str | None = None

    def __str__(self) -> str:
        return self.message

    # --- resolver (WPK-000-1) ------------------------------------------------

    @classmethod
    def unknown_target(cls, type_: str, relation: str, target: str) -> "LocatedError":
        return cls(
            code="unknown_relation_target",
            message=f"Type '{type_}' relation '{relation}' targets unknown type '{target}'",
            type=type_,
            relation=relation,
            target=target,
        )

    @classmethod
    def missing_base(cls) -> "LocatedError":
        return cls(
            code="missing_base",
            message="Schema declares entities but no base block; base attributes are missing",
        )

    @classmethod
    def raw_linkml_smuggled(cls, type_: str | None, construct: str, location: str) -> "LocatedError":
        where = f" at {location}" if location else ""
        return cls(
            code="raw_linkml_smuggled",
            message=f"Unknown construct '{construct}' (not khub vocabulary){where}",
            type=type_,
            target=construct,
        )

    # --- compiler (WPK-000-2) ------------------------------------------------

    @classmethod
    def duplicate_type(cls, type_: str) -> "LocatedError":
        return cls(code="duplicate_type", message=f"Duplicate type '{type_}'", type=type_)
