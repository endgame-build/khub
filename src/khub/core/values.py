"""Shared value predicates — the write gate and the integrity gate check the same things.

One module both `entity` (write-time coercion) and `integrity` (validate) import,
so a value the write path accepts is exactly a value validate accepts, and the
``draft`` flag reads the same everywhere (`as_bool`), never by raw truthiness.
"""

from __future__ import annotations

import math
from datetime import date, datetime
from typing import Any

BOOLISH = {"true", "false", "yes", "no", "1", "0", "on", "off"}
_TRUEISH = {"true", "yes", "1", "on"}


def present(value: Any) -> bool:
    """Whether a required value is actually supplied (not blank/empty)."""
    if value is None:
        return False
    if isinstance(value, str):
        return value.strip() != ""
    if isinstance(value, list):
        return len(value) > 0
    return True


def is_bool(value: Any) -> bool:
    return isinstance(value, bool) or (
        isinstance(value, str) and value.strip().lower() in BOOLISH
    )


def as_bool(value: Any) -> bool:
    """Read a stored bool-ish value the way validate accepts it.

    A hand-written ``draft: "false"`` or ``draft: yes`` passes `is_bool`, so the
    projection must parse it, not truthiness it — otherwise a published entity
    reads as a draft.
    """
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in _TRUEISH
    return bool(value)


def is_number(value: Any) -> bool:
    if isinstance(value, bool):
        return False
    if isinstance(value, (int, float)):
        return math.isfinite(value)
    if isinstance(value, str):
        try:
            return math.isfinite(float(value))  # reject inf/nan: a finite number is meant
        except ValueError:
            return False
    return False


def is_dateish(value: Any) -> bool:
    """Strict ISO date/datetime check (validation gate, not the lenient staleness read)."""
    if isinstance(value, (date, datetime)):
        return True
    text = str(value)
    for parse in (date.fromisoformat, datetime.fromisoformat):
        try:
            parse(text)
            return True
        except ValueError:
            continue
    return False
