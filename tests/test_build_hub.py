"""The build-hub preset (v0.3.0), resolved end to end.

Covers the twenty types, the seventeen predicates beyond the universal four,
the hub-authored yaml contract, the schema-enforced single-writer rule
(entity.owner), the governance layer (domain / boundary / quality-attribute /
component), the FS-absorbs-feature work spine, the two registries, the facet
bootstrap spine, and the storage/status vocabulary. Design capture:
docs/build-hub-preset.md.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from khub.core import resolve

PRESETS = Path(__file__).resolve().parents[1] / "src" / "khub" / "presets"
CORE = PRESETS / "core.yaml"
BUILD_HUB = PRESETS / "build-hub" / "schema.yaml"

SINGLETONS = {"prd", "roadmap", "glossary", "arc42", "erd"}
TWENTY_TYPES = {
    "capability", "requirement", "pdr", "adr", "domain", "entity", "boundary",
    "quality-attribute", "component", "repo", "contract",
    "baseline", "feature-spec", "test-spec", "work-package",
} | SINGLETONS
BUILD_HUB_PREDICATES = {
    "capabilities", "realized_in", "supersedes", "affects", "drivers",
    "produces", "reads", "owner", "applies_to", "repo", "domains", "consumes",
    "provider", "component", "requirements", "verifies", "feature",
}


@pytest.fixture(scope="module")
def build_hub_schema():
    return resolve([CORE, BUILD_HUB])


def test_declares_twenty_types(build_hub_schema):
    assert set(build_hub_schema.types) == TWENTY_TYPES


def test_narrative_singletons(build_hub_schema):
    """The five narrative docs are singleton types; prd is the required one."""
    s = build_hub_schema
    for name in SINGLETONS:
        t = s.types[name]
        assert t.storage.layout == "singleton"
        assert t.storage.fmt == "md"
        assert t.attributes["title"].required is True
    assert s.types["prd"].required is True
    assert s.types["prd"].storage.path == "knowledge/product/prd.md"
    assert s.types["arc42"].storage.path == "knowledge/architecture/arc42.md"
    assert all(not s.types[n].required for n in SINGLETONS - {"prd"})


def test_narrative_roots_opt_out_of_the_orphan_sweep(build_hub_schema):
    """All five singletons are narrative roots: edges run UP the durability
    ladder and the prd sits above its top, so nothing points at them. Without
    `orphan: true` they are permanent findings and `check --strict` can never go
    green on a correct workspace."""
    s = build_hub_schema
    assert all(s.types[n].orphan is True for n in SINGLETONS)
    assert all(s.types[n].orphan is False for n in TWENTY_TYPES - SINGLETONS)


def test_declares_build_hub_predicates(build_hub_schema):
    """Seventeen predicate names beyond the universal four; twenty-three
    declarations (domain.depends_on narrows a base edge, so it is excluded by
    the base-name filter on both counts)."""
    s = build_hub_schema
    preds = {p for t in s.types.values() for p in t.relations if p not in s.base_relations}
    assert preds == BUILD_HUB_PREDICATES
    declarations = sum(
        1 for t in s.types.values() for p in t.relations if p not in s.base_relations
    )
    assert declarations == 23


def test_depends_on_narrowed_to_domain(build_hub_schema):
    """domain redeclares the universal depends_on as a typed domain→domain
    edge — the predicate integrity/cycle checks key on."""
    d = build_hub_schema.types["domain"].relations["depends_on"]
    assert d.targets == ("domain",)
    assert d.kind == "typed"
    assert d.many is True


def test_single_writer_schema_enforced(build_hub_schema):
    """entity.owner is single-valued and required: exactly one authoritative
    writer per entity is unrepresentable to violate."""
    owner = build_hub_schema.types["entity"].relations["owner"]
    assert owner.targets == ("domain",)
    assert owner.required is True
    assert owner.many is False
    reads = build_hub_schema.types["domain"].relations["reads"]
    assert reads.targets == ("entity",)
    assert reads.many is True


def test_governance_layer(build_hub_schema):
    """boundary carries the invariant rule; quality-attribute the scenario;
    adr binds them via produces/drivers."""
    s = build_hub_schema
    assert s.types["boundary"].attributes["rule"].required is True
    assert s.types["boundary"].attributes["scope"].enum == ("domain", "api", "data", "system")
    assert s.types["quality-attribute"].attributes["scenario"].required is True
    qa_applies = s.types["quality-attribute"].relations["applies_to"]
    assert qa_applies.kind == "union"
    assert qa_applies.targets == ("domain", "component")
    adr = s.types["adr"]
    assert adr.relations["produces"].targets == ("boundary",)
    assert adr.relations["drivers"].targets == ("quality-attribute",)


def test_component_topology_mapping(build_hub_schema):
    """component maps to at most one codebase and carries the churny consumes
    side; repo rows are a pure remotes record with no relations of their own."""
    s = build_hub_schema
    comp = s.types["component"]
    assert comp.relations["repo"].targets == ("repo",)
    assert comp.relations["repo"].required is False  # an external has no codebase
    assert comp.relations["domains"].targets == ("domain",)
    assert comp.relations["consumes"].targets == ("contract",)
    repo_rels = {p for p in s.types["repo"].relations if p not in s.base_relations}
    assert repo_rels == set()


def test_external_is_a_component_kind(build_hub_schema):
    """There is no external-system type: a vendor is a component with
    `kind: external`, and kind is required because it is the sole carrier of
    the ownership boundary once contract.provider stops being a union."""
    s = build_hub_schema
    assert "external-system" not in s.types
    kind = s.types["component"].attributes["kind"]
    assert kind.enum == ("service", "library", "external")
    assert kind.required is True


def test_hub_authored_yaml_contract(build_hub_schema):
    """contract is a yaml-format file entity; provider is a required single
    component edge; no stored consumers list — consumed_by is computed from
    inbound consumes edges."""
    s = build_hub_schema
    contract = s.types["contract"]
    assert contract.storage.layout == "file"
    assert contract.storage.fmt == "yaml"
    provider = contract.relations["provider"]
    assert provider.targets == ("component",)
    assert provider.kind == "typed"
    assert provider.required is True
    assert provider.many is False
    assert "consumers" not in contract.relations
    assert s.types["component"].relations["consumes"].targets == ("contract",)


def test_work_spine_fs_absorbs_feature(build_hub_schema):
    """feature-spec IS the feature record: it carries work status plus the
    placement/obligation edges; a work-package binds to it and routes to
    exactly one repo."""
    s = build_hub_schema
    assert "feature" not in s.types
    assert "solution-spec" not in s.types
    fs = s.types["feature-spec"]
    assert fs.attributes["status"].enum == ("planned", "active", "done", "dropped")
    assert fs.relations["capabilities"].targets == ("capability",)
    assert fs.relations["requirements"].many is True
    assert fs.relations["supersedes"].targets == ("feature-spec",)
    ts = s.types["test-spec"].relations["verifies"]
    assert ts.targets == ("feature-spec",)
    assert ts.required is True
    wp = s.types["work-package"]
    assert wp.relations["feature"].targets == ("feature-spec",)
    assert wp.relations["feature"].required is True
    assert wp.relations["repo"].many is False
    assert "requirements" not in wp.relations


def test_decision_records_symmetric(build_hub_schema):
    """adr and pdr share the shape; supersedes is self-typed; no stored
    inverse anywhere."""
    s = build_hub_schema
    for name in ("adr", "pdr"):
        t = s.types[name]
        assert t.relations["supersedes"].targets == (name,)
        assert t.relations["affects"].kind == "any"
        assert t.attributes["status"].enum == ("proposed", "accepted", "rejected")
    assert all("superseded_by" not in t.relations for t in s.types.values())


def test_facet_bootstrap_spine(build_hub_schema):
    """facet_id rides the four import-target types; pdr carries none (facet
    has no product-decision concept)."""
    s = build_hub_schema
    for name in ("capability", "requirement", "adr", "contract"):
        assert s.types[name].attributes["facet_id"].base_type == "text"
    assert "facet_id" not in s.types["pdr"].attributes


def test_status_vocabulary_no_defaults(build_hub_schema):
    """Coarse work statuses; no status field carries a default — `add` applies
    no schema defaults, so status is always an explicit statement."""
    s = build_hub_schema
    for name in ("feature-spec", "work-package"):
        assert s.types[name].attributes["status"].enum == ("planned", "active", "done", "dropped")
    assert s.types["contract"].attributes["status"].enum == ("proposed", "active", "deprecated")
    for name in ("feature-spec", "work-package", "adr", "pdr", "contract", "repo"):
        assert s.types[name].attributes["status"].default is None


def test_storage(build_hub_schema):
    """Two yaml registries; everything else per-item files under the two
    knowledge roots (knowledge/ durable truth, specs/ delivery state)."""
    s = build_hub_schema
    repo = s.types["repo"].storage
    assert repo.layout == "collection"
    assert repo.fmt == "yaml"
    assert repo.path == "knowledge/architecture/repos.yaml"
    baseline = s.types["baseline"].storage
    assert baseline.layout == "collection"
    assert baseline.fmt == "yaml"
    assert s.types["capability"].storage.path == "knowledge/product/capabilities"
    assert s.types["adr"].storage.path == "knowledge/architecture/decisions"
    assert s.types["pdr"].storage.path == "knowledge/product/decisions"
    assert s.types["feature-spec"].storage.path == "specs/feature-specs"
    assert s.types["test-spec"].storage.path == "specs/test-specs"
    assert s.types["work-package"].storage.path == "specs/work-packages"
    files = TWENTY_TYPES - {"repo", "baseline"} - SINGLETONS
    assert all(s.types[n].storage.layout == "file" for n in files)
