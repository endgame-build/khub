"""The build-hub preset, resolved end to end.

Covers the twelve types, the ten predicates (eighteen declarations), the
split-by-churn contract edges, the symmetric decision shape, the spec family,
the implementation-loop edges, the facet bootstrap spine, and the
storage/status vocabulary. Design capture: docs/build-hub-preset.md.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from khub.core import resolve

PRESETS = Path(__file__).resolve().parents[1] / "src" / "khub" / "presets"
CORE = PRESETS / "core.yaml"
BUILD_HUB = PRESETS / "build-hub.yaml"

TWELVE_TYPES = {
    "capability", "feature", "work-package", "feature-spec", "test-spec",
    "solution-spec", "requirement", "adr", "pdr", "repo", "contract",
    "external-system",
}
BUILD_HUB_PREDICATES = {
    "capabilities", "feature", "requirements", "verifies", "supersedes",
    "affects", "provider", "consumes", "repo", "realized_in",
}


@pytest.fixture(scope="module")
def build_hub_schema():
    return resolve([CORE, BUILD_HUB])


def test_declares_twelve_types(build_hub_schema):
    assert set(build_hub_schema.types) == TWELVE_TYPES


def test_declares_build_hub_predicates(build_hub_schema):
    """Ten predicate names beyond the universal four; eighteen declarations."""
    s = build_hub_schema
    preds = {p for t in s.types.values() for p in t.relations if p not in s.base_relations}
    assert preds == BUILD_HUB_PREDICATES
    declarations = sum(
        1 for t in s.types.values() for p in t.relations if p not in s.base_relations
    )
    assert declarations == 18


def test_work_spine(build_hub_schema):
    """feature binds capabilities (placement) and requirements (obligation);
    a work-package routes to exactly one repo; realized_in is the stored
    backstop for reality outside the work spine."""
    s = build_hub_schema
    assert s.types["work-package"].relations["feature"].required is True
    wp_repo = s.types["work-package"].relations["repo"]
    assert wp_repo.targets == ("repo",)
    assert wp_repo.many is False
    assert s.types["feature"].relations["capabilities"].targets == ("capability",)
    assert s.types["feature"].relations["requirements"].many is True
    assert "requirements" not in s.types["work-package"].relations
    realized = s.types["requirement"].relations["realized_in"]
    assert realized.targets == ("repo",)
    assert realized.many is True


def test_facet_bootstrap_spine(build_hub_schema):
    """facet_id rides the five import-target types; pdr carries none (facet
    has no product-decision concept)."""
    s = build_hub_schema
    for name in ("capability", "requirement", "adr", "solution-spec", "contract"):
        assert s.types[name].attributes["facet_id"].base_type == "text"
    assert "facet_id" not in s.types["pdr"].attributes


def test_spec_pair(build_hub_schema):
    """feature-spec elaborates a feature; test-spec verifies a feature-spec."""
    s = build_hub_schema
    fs = s.types["feature-spec"].relations["feature"]
    assert fs.targets == ("feature",)
    assert fs.required is True
    ts = s.types["test-spec"].relations["verifies"]
    assert ts.targets == ("feature-spec",)
    assert ts.required is True


def test_split_by_churn_contract_edges(build_hub_schema):
    """provider on the contract (required union); consumes on each consumer."""
    s = build_hub_schema
    provider = s.types["contract"].relations["provider"]
    assert provider.kind == "union"
    assert provider.targets == ("repo", "external-system")
    assert provider.required is True
    # no stored consumers list on the contract — consumed_by is computed
    assert "consumers" not in s.types["contract"].relations
    assert s.types["repo"].relations["consumes"].targets == ("contract",)
    assert s.types["external-system"].relations["consumes"].many is True


def test_decision_records_symmetric(build_hub_schema):
    """adr, pdr, and solution-spec share the RFC shape; supersedes is
    self-typed; no stored inverse anywhere."""
    s = build_hub_schema
    for name in ("adr", "pdr", "solution-spec"):
        t = s.types[name]
        assert t.relations["supersedes"].targets == (name,)
        assert t.relations["affects"].kind == "any"
        assert t.attributes["status"].enum == ("proposed", "accepted", "rejected")
    assert all("superseded_by" not in t.relations for t in s.types.values())


def test_status_vocabulary(build_hub_schema):
    """Coarse work statuses; no status field carries a default — `add` applies
    no schema defaults, so status is always an explicit statement."""
    s = build_hub_schema
    for name in ("feature", "work-package"):
        status = s.types[name].attributes["status"]
        assert status.enum == ("planned", "active", "done", "dropped")
    assert s.types["contract"].attributes["status"].enum == ("proposed", "active", "deprecated")
    for name in ("feature", "work-package", "adr", "pdr", "contract", "repo"):
        assert s.types[name].attributes["status"].default is None


def test_storage(build_hub_schema):
    """repo is a jsonl collection (format derived from the path); the rest are files."""
    s = build_hub_schema
    repo = s.types["repo"].storage
    assert repo.layout == "collection"
    assert repo.fmt == "jsonl"
    assert repo.path == "repos.jsonl"
    assert s.types["capability"].storage.layout == "file"
    # qualifier-named sibling dirs: dirs by domain, types by term
    assert s.types["adr"].storage.path == "decisions/architecture"
    assert s.types["pdr"].storage.path == "decisions/product"
    assert s.types["feature-spec"].storage.path == "specs/feature"
    assert s.types["test-spec"].storage.path == "specs/test"
    assert s.types["solution-spec"].storage.path == "specs/solution"
