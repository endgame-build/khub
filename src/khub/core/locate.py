"""Workspace resolution — WPK-001-2.

``find_workspace`` walks up from a starting directory to the nearest ancestor
holding a ``.khub/`` directory. Shared by ``khub schema`` and ``khub status``;
both fail with the same resolution error when no workspace is found.
"""

from __future__ import annotations

from pathlib import Path

from khub.core.errors import LocatedError
from khub.core.resolve import load_yaml


def find_workspace(start: Path) -> Path:
    """Return the workspace root (the dir holding ``.khub/``) at or above ``start``."""
    cur = start.resolve()
    for d in (cur, *cur.parents):
        if (d / ".khub").is_dir():
            return d
    raise LocatedError.no_workspace()


def provenance(root: Path) -> dict[str, str]:
    """The workspace's preset name and version, read from ``.khub/config.yaml``."""
    cfg = load_yaml(root / ".khub" / "config.yaml")
    return {"preset": cfg.get("preset", ""), "version": cfg.get("version", "")}
