"""STORY-SCH-004 — the firm-ops preset, end to end.

Covers TS-SCH-004-01 (9 types), -02 (predicates), -03 (porting notes), and -04
(the remap-then-validate cutover demonstrator on a synthetic HQ-shaped fixture,
checked against the resolved schema — no real firm data).
"""

from __future__ import annotations

from pathlib import Path

import pytest

from khub.core import resolve

PRESETS = Path(__file__).resolve().parents[1] / "src" / "khub" / "presets"
CORE = PRESETS / "core.yaml"
FIRM_OPS = PRESETS / "firm-ops.yaml"

NINE_TYPES = {
    "opportunity", "project", "meeting", "transcript", "fragment",
    "case-study", "partnership", "person", "client",
}
FIRM_OPS_PREDICATES = {
    "owner", "client", "team", "engagement", "origin_opportunity",
    "source_project", "transcript", "partner", "related_opportunities", "related_projects",
}


@pytest.fixture(scope="module")
def firm_ops_schema():
    return resolve([CORE, FIRM_OPS])


def test_declares_nine_types(firm_ops_schema):
    """TS-SCH-004-01."""
    assert set(firm_ops_schema.types) == NINE_TYPES


def test_declares_firm_ops_predicates(firm_ops_schema):
    """TS-SCH-004-02: the 10 firm-ops predicates (+4 universal from core); union/typed/required."""
    s = firm_ops_schema
    preds = {p for t in s.types.values() for p in t.relations if p not in s.base_relations}
    assert preds == FIRM_OPS_PREDICATES
    eng = s.types["meeting"].relations["engagement"]
    assert eng.kind == "union"
    assert eng.targets == ("opportunity", "project", "partnership")
    assert s.types["opportunity"].relations["owner"].required is True
    assert s.types["partnership"].relations["related_projects"].many is True


def test_porting_notes(firm_ops_schema):
    """TS-SCH-004-03: the four porting notes are reflected in the resolved model."""
    s = firm_ops_schema
    # no stored inverse anywhere (decision/superseded_by folded out)
    assert all("superseded_by" not in t.relations for t in s.types.values())
    # boolean draft from core on every entity
    assert all(t.attributes["draft"].base_type == "bool" for t in s.types.values())
    # partner from-list includes client
    assert "partner" in s.types["client"].relations
    # meeting.engagement explicit union edge; meetings flat (file layout)
    assert s.types["meeting"].relations["engagement"].kind == "union"
    assert s.types["meeting"].storage.layout == "file"


# --- TS-SCH-004-04: remap-then-validate on a synthetic, old-shape HQ fixture ---

# Entities in HQ's *old* shape (pre-refinement), plus one planted referential break.
OLD_HQ = {
    "acme": {"type": "client", "name": "Acme", "created": "2025-01-01", "updated": "2025-01-02"},
    "noor": {"type": "person", "name": "Noor", "role": "principal", "created": "2025-01-01"},
    "acme-pov": {"type": "opportunity", "stage": "discovery", "client": "acme", "owner": "noor",
                 "created": "2025-01-01", "updated": "2025-01-02"},
    "acme-build": {"type": "project", "stage": "diagnose", "client": "acme", "owner": "noor",
                   "created": "2025-01-01", "updated": "2025-01-02"},
    "kickoff": {"type": "meeting", "date": "2025-02-01", "call_type": "client", "source": "recording",
                "engagement": "acme-pov"},
    "ghost-mtg": {"type": "meeting", "date": "2025-02-02", "call_type": "client", "source": "recording",
                  "engagement": "does-not-exist"},  # PLANTED referential break
    "bigco": {"type": "partnership", "partner": "BigCo", "stage": "active", "owner": "noor",
              "created": "2025-01-01", "updated": "2025-01-02"},
}

_STAGE = {"nurturing": "prospect", "discovery": "prospect", "proposal": "proposal-sent",
          "negotiation": "proposal-sent", "closed-won": "won", "closed-lost": "lost"}
_ROLE = {"partner": "partner", "associated-partner": "partner", "business-consultant": "consultant",
         "director": "manager", "principal": "manager", "staff": "consultant", "senior": "consultant",
         "lead": "manager", "hr": "manager"}


def _remap(entity: dict) -> dict:
    """The cutover transform: old HQ values -> the refined firm-ops model."""
    e = dict(entity)
    if e["type"] == "opportunity" and "stage" in e:
        e["stage"] = _STAGE.get(e["stage"], e["stage"])
    if e["type"] == "person" and "role" in e:
        e["role"] = _ROLE.get(e["role"], e["role"])
    if e["type"] in ("project", "partnership"):
        e.pop("stage", None)
        e.setdefault("active", True)
    return e


def _referential_breaks(entities: dict, schema) -> list[tuple[str, str, str]]:
    breaks: list[tuple[str, str, str]] = []
    for slug, e in entities.items():
        for predicate in schema.types[e["type"]].relations:
            if predicate not in e:
                continue
            value = e[predicate]
            for target in value if isinstance(value, list) else [value]:
                if target not in entities:
                    breaks.append((slug, predicate, target))
    return sorted(breaks)


def test_remap_then_validate(firm_ops_schema):
    """TS-SCH-004-04: after the cutover remap, the only referential break in the
    synthetic corpus is the planted one (validated against the resolved schema)."""
    remapped = {slug: _remap(e) for slug, e in OLD_HQ.items()}
    assert _referential_breaks(remapped, firm_ops_schema) == [
        ("ghost-mtg", "engagement", "does-not-exist")
    ]
