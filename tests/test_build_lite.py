"""The build-lite preset (v0.1.0), resolved end to end.

build-hub cut to necessity: six types, four predicates beyond the universal
four. These tests pin the cuts as much as the keeps — a type reappearing here
is a design change, not a detail. Design capture: the schema header.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from khub.core import resolve

PRESETS = Path(__file__).resolve().parents[1] / "src" / "khub" / "presets"
CORE = PRESETS / "core.yaml"
BUILD_LITE = PRESETS / "build-lite" / "schema.yaml"
TEMPLATES = PRESETS / "build-lite" / "templates"

SINGLETONS = {"prd", "arc42"}
SIX_TYPES = {"requirement", "adr", "component", "feature-spec"} | SINGLETONS
BUILD_LITE_PREDICATES = {"realized_in", "supersedes", "affects", "requirements"}

# Everything build-hub declares that lite deliberately does not. Each one has a
# named replacement in the schema header; the add-back order lives there too.
# Fourteen: build-hub 0.3.0 declares twenty types and lite keeps six of them.
CUT_TYPES = {
    "roadmap", "glossary", "erd", "capability", "pdr", "boundary",
    "quality-attribute", "domain", "entity", "repo", "contract",
    "baseline", "test-spec", "work-package",
}


@pytest.fixture(scope="module")
def build_lite_schema():
    return resolve([CORE, BUILD_LITE])


def test_declares_six_types(build_lite_schema):
    assert set(build_lite_schema.types) == SIX_TYPES


def test_cut_types_stay_cut(build_lite_schema):
    """The fourteen build-hub types lite drops are absent, not renamed."""
    assert len(CUT_TYPES) == 14
    assert CUT_TYPES.isdisjoint(build_lite_schema.types)


def test_external_system_is_gone_from_both_presets(build_lite_schema):
    """Not a lite-only cut: build-hub 0.3.0 folded it into `component.kind` too,
    which is why the add-back ladder brings `contract` back WITHOUT it."""
    assert "external-system" not in build_lite_schema.types
    assert build_lite_schema.types["component"].attributes["kind"].enum == (
        "service", "library", "external",
    )


def test_declares_build_lite_predicates(build_lite_schema):
    """Four predicate names beyond the universal four; five declarations
    (supersedes is declared on both adr and feature-spec)."""
    s = build_lite_schema
    preds = {p for t in s.types.values() for p in t.relations if p not in s.base_relations}
    assert preds == BUILD_LITE_PREDICATES
    declarations = sum(
        1 for t in s.types.values() for p in t.relations if p not in s.base_relations
    )
    assert declarations == 5


def test_narrative_singletons(build_lite_schema):
    """Two narrative docs; prd is the required one — check fails without it."""
    s = build_lite_schema
    for name in SINGLETONS:
        t = s.types[name]
        assert t.storage.layout == "singleton"
        assert t.storage.fmt == "md"
        assert t.attributes["title"].required is True
    assert s.types["prd"].required is True
    assert s.types["prd"].storage.path == "knowledge/prd.md"
    assert s.types["arc42"].storage.path == "knowledge/arc42.md"
    assert not s.types["arc42"].required


def test_narrative_roots_opt_out_of_the_orphan_sweep(build_lite_schema):
    """Both docs sit above the top of the durability ladder, so nothing points
    at them and they point at nothing. Without `orphan: true` they are permanent
    findings and `check --strict` can never go green on a correct workspace."""
    s = build_lite_schema
    assert all(s.types[n].orphan is True for n in SINGLETONS)
    assert all(s.types[n].orphan is False for n in SIX_TYPES - SINGLETONS)


def test_requirement_absorbs_the_constraint_types(build_lite_schema):
    """boundary and quality-attribute collapse into requirement.kind."""
    kind = build_lite_schema.types["requirement"].attributes["kind"]
    assert kind.enum == ("functional", "constraint", "business-rule")
    assert kind.required is True
    realized = build_lite_schema.types["requirement"].relations["realized_in"]
    assert realized.targets == ("component",)  # not repo — there is no repo type
    assert realized.many is True


def test_component_carries_the_ownership_boundary(build_lite_schema):
    """kind is required (ours vs theirs); repo is a plain text attribute, so an
    external component simply omits it — no repo type, no required edge."""
    s = build_lite_schema
    comp = s.types["component"]
    assert comp.attributes["kind"].enum == ("service", "library", "external")
    assert comp.attributes["kind"].required is True
    assert comp.attributes["repo"].base_type == "text"
    assert comp.attributes["repo"].required is False
    assert "repo" not in comp.relations  # attribute, not edge
    assert "consumes" not in comp.relations  # contract is cut


def test_adr_is_the_only_decision_type(build_lite_schema):
    """No pdr: a product decision is prd prose, an implementation constraint is
    an adr. affects is the blast-radius query that earns the type its slot."""
    s = build_lite_schema
    adr = s.types["adr"]
    assert adr.attributes["status"].enum == ("proposed", "accepted", "rejected")
    assert adr.relations["supersedes"].targets == ("adr",)
    assert adr.relations["affects"].kind == "any"
    assert adr.relations["affects"].many is True


def test_feature_spec_is_the_whole_work_spine(build_lite_schema):
    """The FS is the work record: no work-package, no test-spec, no capability
    edge — obligation only."""
    s = build_lite_schema
    fs = s.types["feature-spec"]
    assert fs.attributes["status"].enum == ("planned", "active", "done", "dropped")
    assert fs.relations["requirements"].targets == ("requirement",)
    assert fs.relations["requirements"].many is True
    assert fs.relations["supersedes"].targets == ("feature-spec",)
    assert "capabilities" not in fs.relations


def test_no_stored_inverses_and_no_status_defaults(build_lite_schema):
    """superseded is computed; status is always an explicit statement."""
    s = build_lite_schema
    assert all("superseded" not in t.relations for t in s.types.values())
    for name in ("adr", "feature-spec"):
        assert s.types[name].attributes["status"].default is None


def test_storage_is_files_only(build_lite_schema):
    """No collections: lite has no homogeneous wiring left to keep in a
    registry. One knowledge root plus specs/."""
    s = build_lite_schema
    assert all(s.types[n].storage.layout == "file" for n in SIX_TYPES - SINGLETONS)
    assert all(s.types[n].storage.fmt == "md" for n in SIX_TYPES)
    assert s.types["requirement"].storage.path == "knowledge/requirements"
    assert s.types["adr"].storage.path == "knowledge/decisions"
    assert s.types["component"].storage.path == "knowledge/components"
    assert s.types["feature-spec"].storage.path == "specs"


def test_four_templates_ship():
    """prd, arc42, adr, feature-spec carry a heading contract; requirement and
    component deliberately do not."""
    assert {p.stem for p in TEMPLATES.glob("*.yaml")} == {
        "prd", "arc42", "adr", "feature-spec",
    }
