"""STORY-SCH-002 — merge the base block and override.

Covers TS-SCH-002-01..04 and unit tests U01 (merge), U02 (override),
U03 (draft/type guaranteed), U04 (missing-base reject), U05 (universal edges).
"""

from __future__ import annotations

import pytest

from khub.core import resolve
from khub.core.errors import LocatedError

BASE_ATTRS = ("type", "draft", "author", "created", "updated", "title", "description", "resource", "tags")
UNIVERSAL = ("related", "sources", "references", "depends_on")


def test_merge_base_into_every_entity(write_schema, core_base):
    """TS-SCH-002-01 / U01: base attributes and the universal any->any edges are
    merged into every type; draft defaults to false; merge is at resolve time."""
    preset = """
entities:
  client:  { layout: file }
  project: { layout: folder }
"""
    schema = resolve(write_schema(core=core_base, preset=preset))
    for name in ("client", "project"):
        t = schema.types[name]
        for a in BASE_ATTRS:
            assert a in t.attributes, f"{name} missing base attr {a}"
        assert t.attributes["draft"].base_type == "bool"
        assert t.attributes["draft"].default is False
        for r in UNIVERSAL:
            assert r in t.relations
            assert t.relations[r].kind == "any"
            assert t.relations[r].many is True


def test_override_base_attribute(write_schema, core_base):
    """TS-SCH-002-02 / U02: a type overrides a base attribute by redeclaring it;
    siblings stay inherited unchanged."""
    preset = """
entities:
  a:
    layout: file
    attributes:
      updated: { required: true }
  b: { layout: file }
"""
    schema = resolve(write_schema(core=core_base, preset=preset))
    a_updated = schema.types["a"].attributes["updated"]
    assert a_updated.required is True
    assert a_updated.overridden_from_base is True
    # sibling keeps the base default (optional), unchanged
    assert schema.types["b"].attributes["updated"].required is False
    assert schema.types["b"].attributes["updated"].overridden_from_base is False


def test_draft_and_type_guaranteed(write_schema, core_base):
    """U03 / SCH-006: every entity carries `type` and the boolean `draft` flag."""
    schema = resolve(write_schema(core=core_base, preset="entities: { a: { layout: file } }"))
    a = schema.types["a"]
    assert "type" in a.attributes
    assert a.attributes["draft"].base_type == "bool"


def test_missing_base_block_rejected(write_schema):
    """TS-SCH-002-03 / U04: entities with no base block present is rejected."""
    with pytest.raises(LocatedError) as ei:
        resolve(write_schema(preset="entities: { a: { layout: file } }"))
    assert ei.value.code == "missing_base"
    assert ei.value.message == "Schema declares entities but no base block; base attributes are missing"


def test_universal_edges_without_redeclaration(write_schema, core_base):
    """TS-SCH-002-04 / U05: the universal edges are available on every type
    without that type redeclaring the predicate."""
    preset = """
entities:
  a: { layout: file }
  b: { layout: file }
"""
    schema = resolve(write_schema(core=core_base, preset=preset))
    assert "related" in schema.types["a"].relations
    assert "depends_on" in schema.types["b"].relations
