"""Shared pytest fixtures for the khub test suite.

Each scenario resolves/compiles from its own fixture into its own temp output
dir (compile-into-temp isolation); the HQ snapshot is mounted read-only.
"""

from __future__ import annotations

from pathlib import Path
from typing import Callable

import pytest

FIXTURES = Path(__file__).parent / "fixtures"
HQ_SNAPSHOT = Path(__file__).parent / "snapshots" / "hq"

# A minimal core base block, mirroring src/khub/presets/core.yaml, for resolver tests.
CORE_BASE = """
base:
  attributes:
    type:        { type: text, required: true }
    draft:       { type: bool, default: false }
    author:      { type: text }
    created:     { type: date, required: true }
    updated:     { type: date }
    title:       { type: text }
    description: { type: text }
    resource:    { type: text }
    tags:        { type: list }
  relations:
    related:     { to: any, many: true }
    sources:     { to: any, many: true }
    references:  { to: any, many: true }
    depends_on:  { to: any, many: true }
"""


@pytest.fixture
def core_base() -> str:
    """The core base-block YAML (for tests that need a base alongside a preset)."""
    return CORE_BASE


@pytest.fixture
def write_schema(tmp_path: Path) -> Callable[..., list[Path]]:
    """Write named YAML schema files into tmp_path; return their paths in order.

    Usage: ``paths = write_schema(core=core_base, preset=PRESET_YAML)``.
    """

    def _write(**named: str) -> list[Path]:
        paths: list[Path] = []
        for name, text in named.items():
            p = tmp_path / f"{name}.yaml"
            p.write_text(text)
            paths.append(p)
        return paths

    return _write


@pytest.fixture
def out_dir(tmp_path: Path) -> Path:
    """A clean per-test generated/ output directory."""
    d = tmp_path / "generated"
    d.mkdir()
    return d


@pytest.fixture
def fresh_ws(tmp_path: Path) -> Path:
    """A scaffolded firm-ops workspace at tmp_path (entity tree still empty)."""
    from khub.core.workspace import init_workspace

    init_workspace("firm-ops", tmp_path)
    return tmp_path


@pytest.fixture
def seed() -> Callable[..., None]:
    """Write an entity .md with YAML frontmatter under a workspace root.

    Usage: ``seed(root, "clients/acme.md", type="client", name="Acme", ...)``.
    """
    import yaml

    def _seed(root: Path, relpath: str, **meta: object) -> None:
        p = root / relpath
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text("---\n" + yaml.safe_dump(meta, sort_keys=False) + "---\n")

    return _seed
