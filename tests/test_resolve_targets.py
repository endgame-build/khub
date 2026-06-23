"""STORY-SCH-001 — target resolution, delta-only inheritance, and the real presets.

Covers TS-SCH-001-03 (unknown target located error / U06) and
TS-SCH-001-04 (delta-only inheritance / U05), plus an integration check that the
authored core.yaml + firm-ops.yaml resolve.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from khub.core import resolve
from khub.core.errors import LocatedError

PRESETS = Path(__file__).resolve().parents[1] / "src" / "khub" / "presets"


def test_unknown_relation_target_located_error(write_schema, core_base):
    """TS-SCH-001-03 / U06 / SCH-003: a relation targeting an unknown type is
    rejected with a located error carrying type/relation/target."""
    preset = """
entities:
  project:
    layout: folder
    relations:
      owner: { to: persn }
"""
    with pytest.raises(LocatedError) as ei:
        resolve(write_schema(core=core_base, preset=preset))
    e = ei.value
    assert e.code == "unknown_relation_target"
    assert e.type == "project"
    assert e.relation == "owner"
    assert e.target == "persn"
    assert e.message == "Type 'project' relation 'owner' targets unknown type 'persn'"


def test_delta_only_inheritance(write_schema, core_base):
    """TS-SCH-001-04 / U05: a type declares only its domain delta and inherits the
    base block (type, draft, created, updated, tags, OKF fields)."""
    preset = """
entities:
  client:
    layout: file
    attributes:
      name:     { required: true }
      industry: {}
"""
    c = resolve(write_schema(core=core_base, preset=preset)).types["client"]
    # domain delta
    assert "name" in c.attributes
    assert "industry" in c.attributes
    # inherited base, not redeclared by client
    for a in ("type", "draft", "created", "updated", "tags", "title", "description", "resource"):
        assert a in c.attributes


def test_authored_presets_resolve():
    """Integration: the authored core.yaml + firm-ops.yaml resolve to 9 types,
    with the post-review model (union engagement minus build; project.active)."""
    schema = resolve([PRESETS / "core.yaml", PRESETS / "firm-ops.yaml"])
    assert len(schema.types) == 9

    eng = schema.types["meeting"].relations["engagement"]
    assert eng.kind == "union"
    assert eng.targets == ("opportunity", "project", "partnership")

    project = schema.types["project"]
    assert "active" in project.attributes
    assert "stage" not in project.attributes
    # base merged in
    assert "draft" in project.attributes
