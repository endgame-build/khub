"""Determinism helpers — WPK-000-2.

Normalize generator output to a canonical form so two runs on an unchanged
schema produce byte-identical artifacts (TS-SCH-003-04): strip the generator's
run-varying leading comment header from the Pydantic module, and re-dump JSON
Schema with sorted keys.
"""

from __future__ import annotations

import json
import re

# The generator embeds the absolute source path (a per-run temp dir) in the
# module's linkml_meta — neutralize it so re-runs are byte-identical.
_SOURCE_FILE = re.compile(r"('source_file': )'[^']*'")

# A field literally named like an imported datetime type (e.g. `date: date`)
# shadows that type, which pydantic v2 cannot build. Rewrite the type on those
# field lines to a bare aliased import so the name no longer collides. A bare
# alias (not a qualified `_dt.date`) resolves on pydantic's first pass — avoiding
# a spurious arbitrary-type warning.
_TYPE_NAMED_FIELD = re.compile(r"^(\s+)(date|datetime|time)(\s*:\s*)(.*?)(\s*=\s*Field.*)$")
_QUALIFY = {"date": "_khub_date", "datetime": "_khub_datetime", "time": "_khub_time"}
_ALIAS_IMPORT = "from datetime import date as _khub_date, datetime as _khub_datetime, time as _khub_time"


def deconflict_type_named_fields(source: str) -> str:
    """Let a field named like an imported datetime type (date/datetime/time) work:
    rewrite the type on those field lines to an aliased import, so the field name
    no longer shadows the type (an otherwise-fatal pydantic v2 clash). A no-op when
    no such field is present."""
    lines = source.splitlines()
    future_idx: int | None = None
    qualified = False
    for i, line in enumerate(lines):
        if line.strip() == "from __future__ import annotations":
            future_idx = i
        match = _TYPE_NAMED_FIELD.match(line)
        if match:
            indent, name, sep, annotation, rest = match.groups()
            annotation = re.sub(rf"\b{name}\b", _QUALIFY[name], annotation)
            lines[i] = f"{indent}{name}{sep}{annotation}{rest}"
            qualified = True
    if qualified:
        lines.insert(future_idx + 1 if future_idx is not None else 0, _ALIAS_IMPORT)
    return "\n".join(lines)


def canonicalize_pydantic(source: str) -> str:
    """Drop the generator's leading comment/blank header and neutralize the
    run-varying ``source_file`` path, leaving a deterministic module body."""
    lines = source.splitlines()
    start = 0
    while start < len(lines) and (lines[start].lstrip().startswith("#") or not lines[start].strip()):
        start += 1
    body = "\n".join(lines[start:])
    body = _SOURCE_FILE.sub(r"\1'schema.linkml.yaml'", body)
    return body.strip() + "\n"


def canonicalize_json_schema(source: str) -> str:
    """Re-serialize JSON Schema with sorted keys for stable byte output."""
    return json.dumps(json.loads(source), sort_keys=True, indent=2) + "\n"
