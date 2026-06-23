"""STORY-SCH-003 / TS-SCH-003-U01 — khub vocabulary -> LinkML translation.

The emitter is a pure transform (ResolvedSchema -> a LinkML schema dict), unit
tested here without invoking the real generators.
"""

from __future__ import annotations

from khub.core import resolve
from khub.core.linkml_emit import to_linkml_dict


def test_entity_base_and_inheritance(write_schema, core_base):
    preset = """
entities:
  person: { layout: file }
  project:
    layout: folder
    path: projects
    attributes:
      stage:  { enum: [diagnose, prove, scale, complete], required: true }
      active: { type: bool, default: true }
    relations:
      owner: { to: person, required: true }
      team:  { to: person, many: true }
"""
    d = to_linkml_dict(resolve(write_schema(core=core_base, preset=preset)))

    eb = d["classes"]["EntityBase"]
    assert eb["abstract"] is True
    assert eb["attributes"]["type"]["range"] == "string"
    assert eb["attributes"]["type"]["required"] is True
    assert eb["attributes"]["draft"]["ifabsent"] == "boolean(false)"
    # universal relations live on the base, as slug strings, multivalued
    assert eb["attributes"]["related"]["range"] == "string"
    assert eb["attributes"]["related"]["multivalued"] is True

    p = d["classes"]["Project"]
    assert p["is_a"] == "EntityBase"
    assert "active" in p["attributes"]
    assert "draft" not in p.get("attributes", {})  # inherited from EntityBase, not re-listed
    assert p["slot_usage"]["type"]["equals_string"] == "project"
    assert p["annotations"]["layout"] == "folder"
    assert p["annotations"]["path"] == "projects"

    # enum -> a per-(type,field) enum class
    assert p["attributes"]["stage"]["range"] == "ProjectStageEnum"
    assert set(d["enums"]["ProjectStageEnum"]["permissible_values"]) == {
        "diagnose", "prove", "scale", "complete"
    }

    # relations are slug strings carrying cardinality (target-type is khub graph metadata)
    assert p["attributes"]["owner"] == {"range": "string", "required": True}
    assert p["attributes"]["team"] == {"range": "string", "multivalued": True}


def test_override_emits_slot_usage(write_schema, core_base):
    preset = "entities: { a: { layout: file, attributes: { updated: { required: true } } } }"
    d = to_linkml_dict(resolve(write_schema(core=core_base, preset=preset)))
    assert d["classes"]["A"]["slot_usage"]["updated"] == {"required": True}


def test_hyphenated_type_name_and_discriminator(write_schema, core_base):
    d = to_linkml_dict(resolve(write_schema(core=core_base, preset="entities: { case-study: { layout: file } }")))
    assert "CaseStudy" in d["classes"]
    assert d["classes"]["CaseStudy"]["slot_usage"]["type"]["equals_string"] == "case-study"
