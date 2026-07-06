"""Workspace scaffolder — WPK-001-1.

``init_workspace`` flattens ``core`` and a named preset into one editable
``.khub/schema.yaml``, invokes the FS-000 compiler, stamps provenance, writes
``config.yaml``, gitignores the generated artifacts, and lays down the entity
tree. It owns only ``.khub/`` and the (empty) type directories — it never writes
or overwrites an entity ``.md`` (WS-003), so the same command green-fields a
fresh workspace and force-seeds over a live corpus (the HQ cutover).
"""

from __future__ import annotations

import hashlib
import io
import shutil
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from ruamel.yaml import YAML

from khub.core.compile import compile_schema
from khub.core.errors import LocatedError
from khub.core.resolve import load_yaml

PRESETS_DIR = Path(__file__).resolve().parent.parent / "presets"
DEFAULT_STALE_DAYS = 90

# Authored order is meaningful here (base before entities, declared entity order),
# so keep emission in insertion order — the old yaml.safe_dump(sort_keys=False).
_yaml = YAML(typ="safe")
_yaml.default_flow_style = False
_yaml.representer.sort_base_mapping_type_on_output = False
_yaml.width = 4096


def _dump_yaml(data: Any) -> str:
    buf = io.StringIO()
    _yaml.dump(data, buf)
    return buf.getvalue()


@dataclass(frozen=True)
class InitResult:
    """Outcome of a scaffold: resolved provenance and the cutover guarantee."""

    path: Path
    preset: str
    version: str
    name: str
    source: str | None = None
    # False when the LinkML backend (the optional `compile` extra) was absent and
    # generated artifacts were skipped — `khub compile` regenerates them later.
    compiled: bool = True
    # Measured (not assumed): how many pre-existing entity files init changed or
    # removed. The cutover guarantee (AC-004) is that this is 0; a non-zero value
    # is a loud signal the non-destructive guarantee was violated.
    entity_files_modified: int = 0
    seeded_over_corpus: bool = False


def known_presets(source: Path | None = None) -> list[str]:
    """The presets resolvable from ``source`` (or the packaged presets)."""
    where = source or PRESETS_DIR
    return sorted(p.stem for p in where.glob("*.yaml") if p.stem != "core")


def resolve_preset(name: str, source: Path | None = None) -> Path:
    """Locate ``<name>.yaml`` in ``source`` or the packaged presets."""
    where = source or PRESETS_DIR
    candidate = where / f"{name}.yaml"
    if not candidate.exists():
        raise LocatedError.unknown_preset(name, known_presets(source))
    return candidate


def init_workspace(
    preset: str,
    path: Path | str = ".",
    *,
    preset_source: Path | None = None,
    name: str | None = None,
    force: bool = False,
) -> InitResult:
    """Scaffold a workspace at ``path`` from ``preset``. Writes only ``.khub/``."""
    # Guards run before any write: reject an unknown preset, refuse a non-empty
    # target without --force. Nothing on disk changes until both pass.
    preset_path = resolve_preset(preset, preset_source)
    target = Path(path)
    if target.exists() and any(target.iterdir()) and not force:
        raise LocatedError.target_not_empty(str(path))
    # Snapshot pre-existing entity files so the cutover guarantee is measured, not
    # assumed: a force-seed over a real corpus (pre-existing .md, not just a prior
    # .khub/ from a re-init) must modify zero of them.
    before = _entity_hashes(target)
    seeded_over_corpus = bool(before)

    core = load_yaml(PRESETS_DIR / "core.yaml")
    preset_data = load_yaml(preset_path)
    version = str(preset_data.get("version") or "0.0.0")
    preset_entities = preset_data.get("entities")
    if not preset_entities:
        raise LocatedError(
            code="empty_preset", message=f"Preset '{preset}' declares no entities"
        )
    # core.yaml is base-only in v1, so its `entities` is legitimately absent.
    entities: dict[str, Any] = {**(core.get("entities") or {}), **preset_entities}
    merged = {"base": core.get("base"), "entities": entities}

    khub = target / ".khub"
    created_khub = not khub.exists()
    try:
        khub.mkdir(parents=True, exist_ok=True)
        schema_path = khub / "schema.yaml"
        header = f"# khub-preset: {preset}@{version}\n"
        schema_path.write_text(header + _dump_yaml(merged))

        compiled = True
        try:
            compile_schema(schema_path, khub / "generated")
        except LocatedError as err:
            if err.code != "compile_extra_missing":
                raise
            # No LinkML backend (the optional `compile` extra). The generated
            # artifacts have no runtime consumer, so init proceeds without them;
            # `khub compile` regenerates once the extra is installed.
            compiled = False

        ws_name = name or target.resolve().name or "workspace"
        config = {
            "name": ws_name,
            "preset": preset,
            "version": version,
            "source": str(preset_source) if preset_source else None,
            # `stale_days` drives the status/query staleness flag; no `format` default
            # is written — nothing reads it (format is a per-type schema facet).
            "defaults": {"stale_days": DEFAULT_STALE_DAYS},
        }
        (khub / "config.yaml").write_text(_dump_yaml(config))

        _append_gitignore(target / ".gitignore", ".khub/generated/")

        # Lay down one directory per type's storage path (file and folder layouts
        # both just need the dir); never create an entity .md.
        for decl in entities.values():
            if decl.get("path"):
                (target / decl["path"]).mkdir(parents=True, exist_ok=True)
    except BaseException:
        # Keep init atomic: drop the partial .khub/ we just created.
        if created_khub:
            shutil.rmtree(khub, ignore_errors=True)
        raise

    after = _entity_hashes(target)
    modified = sum(1 for rel, digest in before.items() if after.get(rel) != digest)

    return InitResult(
        path=target,
        preset=preset,
        version=version,
        name=ws_name,
        source=str(preset_source) if preset_source else None,
        compiled=compiled,
        entity_files_modified=modified,
        seeded_over_corpus=seeded_over_corpus,
    )


def _entity_hashes(target: Path) -> dict[str, str]:
    """Content hashes of entity ``.md`` files (outside ``.khub/``), keyed by relpath."""
    if not target.exists():
        return {}
    return {
        str(p.relative_to(target)): hashlib.sha256(p.read_bytes()).hexdigest()
        for p in target.rglob("*.md")
        if ".khub" not in p.parts
    }


def _append_gitignore(gitignore: Path, line: str) -> None:
    existing = gitignore.read_text() if gitignore.exists() else ""
    if line in existing.splitlines():
        return
    sep = "" if existing.endswith("\n") or not existing else "\n"
    gitignore.write_text(f"{existing}{sep}{line}\n")
