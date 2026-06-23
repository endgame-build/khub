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
