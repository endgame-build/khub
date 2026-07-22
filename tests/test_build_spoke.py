"""The build-spoke preset, resolved end to end.

Covers the two types, the two predicates, the contract shape parity with
build-hub (the sync compatibility guard), and storage under docs/. Design
capture: docs/build-spoke-preset.md.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from khub.core import resolve

PRESETS = Path(__file__).resolve().parents[1] / "src" / "khub" / "presets"
CORE = PRESETS / "core.yaml"
BUILD_HUB = PRESETS / "build-hub.yaml"
BUILD_SPOKE = PRESETS / "build-spoke.yaml"

TWO_TYPES = {"contract", "tdr"}
BUILD_SPOKE_PREDICATES = {"supersedes", "affects"}


@pytest.fixture(scope="module")
def spoke_schema():
    return resolve([CORE, BUILD_SPOKE])


@pytest.fixture(scope="module")
def hub_schema():
    return resolve([CORE, BUILD_HUB])


def test_declares_two_types(spoke_schema):
    assert set(spoke_schema.types) == TWO_TYPES


def test_declares_spoke_predicates(spoke_schema):
    s = spoke_schema
    preds = {p for t in s.types.values() for p in t.relations if p not in s.base_relations}
    assert preds == BUILD_SPOKE_PREDICATES


def test_contract_parity_with_hub(spoke_schema, hub_schema):
    """The sync compatibility guard: a spoke-born contract must validate
    hub-side, so kind/status enums are identical and only `provider` differs
    (injected hub-side at sync from the repo of origin)."""
    spoke = spoke_schema.types["contract"]
    hub = hub_schema.types["contract"]
    assert spoke.attributes["kind"].enum == hub.attributes["kind"].enum
    assert spoke.attributes["status"].enum == hub.attributes["status"].enum
    assert "provider" not in spoke.relations
    assert "provider" in hub.relations
    # spoke declares no relations on contract at all beyond the universal four
    assert set(spoke.relations) == set(spoke_schema.base_relations)


def test_tdr_shape(spoke_schema):
    """tdr mirrors the hub decision shape; supersedes self-typed, no stored
    inverse; hub_refs is a plain-text claim list, never a typed edge."""
    t = spoke_schema.types["tdr"]
    assert t.relations["supersedes"].targets == ("tdr",)
    assert t.relations["affects"].kind == "any"
    assert t.attributes["status"].enum == ("proposed", "accepted", "rejected")
    assert t.attributes["hub_refs"].base_type == "list"
    assert all("superseded_by" not in ty.relations for ty in spoke_schema.types.values())


def test_storage_under_docs(spoke_schema):
    """A spoke is a code repo — every inventory lives under docs/."""
    s = spoke_schema
    assert s.types["contract"].storage.path == "docs/contracts"
    assert s.types["tdr"].storage.path == "docs/decisions"
    assert all(t.storage.layout == "file" for t in s.types.values())
