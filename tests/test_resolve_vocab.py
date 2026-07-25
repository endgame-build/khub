"""STORY-SCH-001 — declare a type in khub vocabulary (vocabulary parsing).

Covers TS-SCH-001-01 (well-formed type), TS-SCH-001-02 (constraints captured),
and unit tests U01 (parse), U02 (predicate=field name), U03 (to: single/list/any),
U04 (reject smuggled raw LinkML).
"""

from __future__ import annotations

import pytest

from khub.core import resolve
from khub.core.errors import LocatedError


def test_well_formed_type_resolves(write_schema, core_base):
    """TS-SCH-001-01 / U01 / U02: attributes, enums, typed relations, storage resolve;
    each relation's field name is its predicate."""
    preset = """
entities:
  client:  { layout: file }
  person:  { layout: file }
  project:
    layout: folder
    attributes:
      stage: { enum: [diagnose, prove, scale, complete], required: true }
    relations:
      client: { to: client, required: true }
      owner:  { to: person, required: true }
"""
    schema = resolve(write_schema(core=core_base, preset=preset))
    proj = schema.types["project"]

    # attributes accepted as scalars and enums
    assert proj.attributes["stage"].enum == ("diagnose", "prove", "scale", "complete")
    assert proj.attributes["stage"].required is True

    # storage config accepted
    assert proj.storage.layout == "folder"

    # relations accepted as typed edges with a `to:` target and cardinality;
    # the field name IS the predicate
    assert proj.relations["client"].predicate == "client"
    assert proj.relations["client"].targets == ("client",)
    assert proj.relations["client"].kind == "typed"
    assert proj.relations["client"].required is True
    assert proj.relations["owner"].targets == ("person",)


def test_relation_target_single_list_or_any(write_schema, core_base):
    """U03 / SCH-003: a `to:` target may be a single type, a list (union), or `any`."""
    preset = """
entities:
  a: { layout: file }
  b: { layout: file }
  meeting:
    layout: file
    relations:
      one:        { to: a }
      engagement: { to: [a, b], required: true }
      anything:   { to: any }
"""
    m = resolve(write_schema(core=core_base, preset=preset)).types["meeting"]
    assert m.relations["one"].kind == "typed" and m.relations["one"].targets == ("a",)
    assert m.relations["engagement"].kind == "union"
    assert m.relations["engagement"].targets == ("a", "b")
    assert m.relations["anything"].kind == "any"


def test_constraints_captured(write_schema, core_base):
    """TS-SCH-001-02 (resolver capture): enum, pattern, and `many` cardinality are
    carried on the resolved model so they can flow through to generated validation."""
    preset = """
entities:
  person: { layout: file }
  thing:
    layout: file
    attributes:
      airtable_id: { type: text, pattern: '^rec[A-Za-z0-9]+$' }
    relations:
      team: { to: person, many: true }
"""
    t = resolve(write_schema(core=core_base, preset=preset)).types["thing"]
    assert t.attributes["airtable_id"].pattern == "^rec[A-Za-z0-9]+$"
    assert t.relations["team"].many is True


def test_reject_smuggled_raw_linkml(write_schema, core_base):
    """U04 / SCH-001: a declaration using a raw LinkML construct (not khub vocab)
    is rejected with a located error."""
    preset = """
entities:
  thing:
    layout: file
    attributes:
      name: { range: string }
"""
    with pytest.raises(LocatedError) as ei:
        resolve(write_schema(core=core_base, preset=preset))
    assert ei.value.code == "raw_linkml_smuggled"


def test_orphan_flag_parses_on_any_layout(write_schema, core_base):
    """`orphan: true` declares that edge-less is a type's expected state, so
    `check` stops sweeping it. It defaults to false and, unlike `required`, is
    NOT singleton-only — a file type whose members are legitimately unwired may
    declare it too."""
    preset = """
entities:
  charter:
    layout: singleton
    path: charter.md
    orphan: true
  note:
    layout: file
    orphan: true
  client:
    layout: file
"""
    schema = resolve(write_schema(core=core_base, preset=preset))
    assert schema.types["charter"].orphan is True
    assert schema.types["note"].orphan is True
    assert schema.types["client"].orphan is False  # default: swept like anything else
