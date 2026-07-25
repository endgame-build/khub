"""STORY-WS-003 — the graph projection behind `khub status`.

Covers the projection unit rows TS-WS-003-U01..U04: per-type counts, draft/active
split, orphan (zero edges in or out), stale derivation, and the OKF flag. Counts
are derived from the scanned tree, never stored (WS-006/WS-008).
"""

from __future__ import annotations

from collections.abc import Callable
from datetime import date
from pathlib import Path

import pytest

from khub.core.project import project

NOW = date(2026, 6, 24)


@pytest.fixture
def seeded(fresh_ws: Path, seed: Callable[..., None]) -> Path:
    """A workspace with a referenced/stale client, two people (one orphan draft),
    and one opportunity carrying edges to the client and a person."""
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme",
         created=date(2025, 1, 1), updated=date(2000, 1, 1))           # referenced + stale
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor",
         role="manager", created=date(2026, 6, 1))                      # referenced
    seed(fresh_ws, "identity/team/nobody.md", type="person", name="Nobody",
         role="consultant", draft=True, created=date(2026, 6, 1))       # orphan + draft
    seed(fresh_ws, "opportunities/acme-pov/_index.md", type="opportunity",
         stage="prospect", created=date(2026, 6, 20), updated=date(2026, 6, 20),
         client="acme", owner="noor")                                   # has edges
    return fresh_ws


@pytest.mark.unit
def test_counts_and_total(seeded: Path) -> None:
    """TS-WS-003-U01: per-type counts (incl. zeros) and the total."""
    proj = project(seeded, stale_days=90, now=NOW)
    assert proj.counts["client"] == 1
    assert proj.counts["person"] == 2
    assert proj.counts["opportunity"] == 1
    assert proj.counts["meeting"] == 0  # empty types still reported
    assert proj.total == 4


@pytest.mark.unit
def test_draft_active_split(seeded: Path) -> None:
    """TS-WS-003-U02: draft vs active counts."""
    proj = project(seeded, stale_days=90, now=NOW)
    assert proj.draft == 1
    assert proj.active == 3


@pytest.mark.unit
def test_orphan_is_zero_edges(seeded: Path) -> None:
    """TS-WS-003-U03 (WS-008): orphan = no relations in or out (only `nobody`)."""
    proj = project(seeded, stale_days=90, now=NOW)
    assert proj.orphan == 1


@pytest.mark.unit
def test_stale_from_updated(seeded: Path) -> None:
    """TS-WS-003-U03: the year-2000 client is the only stale entity."""
    proj = project(seeded, stale_days=90, now=NOW)
    assert proj.stale == 1


@pytest.mark.unit
def test_okf_conformant_when_refs_resolve(seeded: Path) -> None:
    """TS-WS-003-U04 (WS-007): types present and refs resolve → OKF-conformant."""
    proj = project(seeded, stale_days=90, now=NOW)
    assert proj.okf_conformant is True


@pytest.mark.unit
def test_okf_broken_when_ref_dangles(fresh_ws: Path, seed: Callable[..., None]) -> None:
    """TS-WS-003-U04: a dangling relation target breaks OKF conformance."""
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor",
         role="manager", created=date(2026, 6, 1))
    seed(fresh_ws, "opportunities/ghosted/_index.md", type="opportunity",
         stage="prospect", created=date(2026, 6, 20), updated=date(2026, 6, 20),
         client="missing-client", owner="noor")  # client target does not exist
    proj = project(fresh_ws, stale_days=90, now=NOW)
    assert proj.okf_conformant is False


@pytest.mark.unit
def test_empty_workspace_zeroes(fresh_ws: Path) -> None:
    """TS-WS-003-02: a freshly initialized workspace projects to all zeros."""
    proj = project(fresh_ws, stale_days=90, now=NOW)
    assert proj.total == 0
    assert all(n == 0 for n in proj.counts.values())


@pytest.mark.unit
def test_cross_type_slug_collision_keeps_nodes_distinct(
    fresh_ws: Path, seed: Callable[..., None]
) -> None:
    """Identity is (type, slug): a client and a person sharing a slug are two nodes."""
    seed(fresh_ws, "clients/dup.md", type="client", name="Dup Co",
         created=date(2026, 6, 1), updated=date(2026, 6, 1))
    seed(fresh_ws, "identity/team/dup.md", type="person", name="Dup Person",
         role="manager", created=date(2026, 6, 1))
    proj = project(fresh_ws, stale_days=90, now=NOW)
    assert proj.total == 2
    assert proj.orphan == 2  # neither is referenced; slug-only keying would collapse to 1


@pytest.mark.unit
def test_typed_edge_to_wrong_type_breaks_okf(
    fresh_ws: Path, seed: Callable[..., None]
) -> None:
    """A typed `client` edge pointing at a person slug does not resolve → OKF false."""
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor",
         role="manager", created=date(2026, 6, 1))
    seed(fresh_ws, "opportunities/op/_index.md", type="opportunity", stage="prospect",
         created=date(2026, 6, 1), updated=date(2026, 6, 1), client="noor", owner="noor")
    proj = project(fresh_ws, stale_days=90, now=NOW)
    assert proj.okf_conformant is False  # (client, noor) is not a node; only (person, noor) is


@pytest.mark.unit
def test_universal_any_edge_to_nonentity_keeps_okf(
    fresh_ws: Path, seed: Callable[..., None]
) -> None:
    """A `references` (to: any) edge may point at a URL without breaking OKF."""
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme",
         created=date(2026, 6, 1), updated=date(2026, 6, 1),
         references=["https://example.com/spec"])
    proj = project(fresh_ws, stale_days=90, now=NOW)
    assert proj.okf_conformant is True


@pytest.mark.unit
def test_self_reference_stays_orphan(fresh_ws: Path, seed: Callable[..., None]) -> None:
    """An entity whose only edge points at itself has no real link → still orphan."""
    seed(fresh_ws, "identity/team/solo.md", type="person", name="Solo", role="manager",
         created=date(2026, 6, 1), related=["solo"])
    proj = project(fresh_ws, stale_days=90, now=NOW)
    assert proj.orphan == 1


@pytest.mark.unit
def test_malformed_date_counts_as_stale(fresh_ws: Path, seed: Callable[..., None]) -> None:
    """A present-but-unparseable date is surfaced as stale, not silently dropped."""
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme",
         created=date(2026, 6, 1), updated="not-a-date")
    proj = project(fresh_ws, stale_days=90, now=NOW)
    assert proj.stale == 1
