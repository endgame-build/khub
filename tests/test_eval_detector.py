"""The wiring eval's bypass detector — the one signal the whole eval rests on.

`tests/eval/` is driven by hand (it spawns real agents), so nothing here ran under pytest
and a silent hole went unnoticed: a singleton's storage path is a FILE, so the prefix test
`rel.startswith("knowledge/prd.md/")` could never match. An agent that hand-edited the PRD
scored ON-RAILS. firm-ops declares no singletons, which is why the published 100% adherence
figure never exposed it; build-lite has two and build-hub five.
"""

from __future__ import annotations

import sys
from pathlib import Path

import pytest

sys.path.insert(0, str(Path(__file__).parent / "eval"))


@pytest.mark.unit
def test_singleton_writes_are_detected_as_bypass() -> None:
    from run import is_entity_path

    # As returned by entity_path_prefixes for build-lite: two singleton FILES,
    # three type directories, one specs/ directory.
    paths = (
        "knowledge/prd.md",
        "knowledge/arc42.md",
        "knowledge/components",
        "knowledge/decisions",
        "knowledge/requirements",
        "specs",
    )

    # The regression: editing a singleton directly is a bypass, not on-rails.
    assert is_entity_path("knowledge/prd.md", paths)
    assert is_entity_path("knowledge/arc42.md", paths)
    # Directory-layout types kept working throughout.
    assert is_entity_path("knowledge/components/cmp-001-core.md", paths)
    assert is_entity_path("specs/fs-001.md", paths)


@pytest.mark.unit
def test_non_entity_writes_are_not_flagged() -> None:
    from run import is_entity_path

    paths = ("knowledge/prd.md", "knowledge/components", "specs")
    assert not is_entity_path("README.md", paths)
    assert not is_entity_path("src/main.py", paths)
    # A near-miss must not match: the prefix has to end at a path boundary.
    assert not is_entity_path("knowledge/prd.md.bak", paths)
    assert not is_entity_path("knowledge/components-notes.md", paths)
