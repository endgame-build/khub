"""Workspace scaffolder — WPK-001-1.

``init_workspace`` flattens ``core`` and a named preset (a DIRECTORY:
``<name>/schema.yaml`` + optional ``<name>/templates/*.yaml``) into one editable
``.khub/schema.yaml`` (+ ``.khub/templates/``), stamps provenance, writes
``config.yaml``, gitignores ``.khub/generated/`` (the runtime collection locks),
and lays down the entity tree — including CREATING each md singleton that has a
template and does not exist yet. It never MODIFIES an existing entity file
(WS-003, amended for singletons: may create, never overwrite), so the same
command green-fields a fresh workspace and force-seeds over a live corpus (the
HQ cutover).
"""

from __future__ import annotations

import hashlib
import io
import shutil
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from ruamel.yaml import YAML

from khub.core.errors import LocatedError
from khub.core.formats import PER_ITEM
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
    # Measured (not assumed): how many pre-existing entity files init changed or
    # removed. The cutover guarantee (AC-004) is that this is 0; a non-zero value
    # is a loud signal the non-destructive guarantee was violated.
    entity_files_modified: int = 0
    seeded_over_corpus: bool = False
    # Singletons created by this init (type names) — creations, never overwrites.
    singletons_created: tuple[str, ...] = ()
    # Workspace-owned files a re-init left alone (`.khub/schema.yaml`,
    # `.khub/config.yaml`, `.khub/templates/*.yaml`). The engagement owns these
    # outright — editing schema.yaml IS the override mechanism — so a re-scaffold
    # reports them instead of silently restoring the preset's copy.
    preserved: tuple[str, ...] = ()


def known_presets(source: Path | None = None) -> list[str]:
    """The presets resolvable from ``source`` (or the packaged presets).

    A preset is a directory: ``<name>/schema.yaml`` (+ optional ``templates/``).
    """
    where = source or PRESETS_DIR
    return sorted(p.parent.name for p in where.glob("*/schema.yaml"))


def resolve_preset(name: str, source: Path | None = None) -> Path:
    """Locate ``<name>/schema.yaml`` in ``source`` or the packaged presets."""
    where = source or PRESETS_DIR
    candidate = where / name / "schema.yaml"
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
    """Scaffold a workspace at ``path`` from ``preset``.

    Writes ``.khub/`` (schema, config, templates), a ``.gitignore`` line, the
    empty type directories, and any missing md singletons (creations only —
    never an existing entity file). On failure the ``.khub/`` it created and
    the singletons it minted are removed; directories and the ``.gitignore``
    line may remain (harmless, idempotent on retry).
    """
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
    created_singletons: list[tuple[str, Path]] = []
    try:
        khub.mkdir(parents=True, exist_ok=True)
        preserved: list[str] = []
        schema_path = khub / "schema.yaml"
        header = f"# khub-preset: {preset}@{version}\n"
        # Creations only, same rule entity files and singletons already follow: the
        # workspace owns schema.yaml, so a re-init must not restore the preset over
        # local edits. Refreshing from a newer preset is an upgrade, not a scaffold.
        if schema_path.exists():
            preserved.append(".khub/schema.yaml")
        else:
            schema_path.write_text(header + _dump_yaml(merged))

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
        config_path = khub / "config.yaml"
        if config_path.exists():
            preserved.append(".khub/config.yaml")  # carries edited defaults (stale_days)
        else:
            config_path.write_text(_dump_yaml(config))

        _append_gitignore(target / ".gitignore", ".khub/generated/")

        # Flatten the preset's templates (if any) into the workspace-owned copy —
        # the same editable-copy relationship schema.yaml has with the preset.
        preset_templates = preset_path.parent / "templates"
        if preset_templates.is_dir():
            tpl_dir = khub / "templates"
            tpl_dir.mkdir(exist_ok=True)
            for tpl in sorted(preset_templates.glob("*.yaml")):
                dest = tpl_dir / tpl.name
                if dest.exists():
                    preserved.append(f".khub/templates/{tpl.name}")
                else:
                    dest.write_text(tpl.read_text())

        # Lay down one directory per type's storage path (file and folder layouts
        # need the dir; a collection's or singleton's path names a FILE — create
        # only its parent, never the file: a missing collection/singleton is
        # legitimately zero entities).
        for type_name, decl in entities.items():
            if decl.get("layout") in ("collection", "singleton"):
                cpath = target / (decl.get("path") or f"{type_name}.{decl.get('format', '')}")
                if cpath.parent != target:
                    cpath.parent.mkdir(parents=True, exist_ok=True)
            elif decl.get("path"):
                (target / decl["path"]).mkdir(parents=True, exist_ok=True)

        # Create each md singleton that has a template and does not exist yet —
        # frontmatter + scaffolded body. Creations only: an existing file is
        # never touched (WS-003 as amended).
        _create_singletons(target, entities, created_singletons)
    except BaseException:
        # Best-effort unwind (not full atomicity — dirs/.gitignore may remain):
        # drop the partial .khub/ we just created, and any
        # singleton files this run minted outside it.
        for _, spath in created_singletons:
            spath.unlink(missing_ok=True)
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
        entity_files_modified=modified,
        seeded_over_corpus=seeded_over_corpus,
        singletons_created=tuple(name for name, _ in created_singletons),
        preserved=tuple(preserved),
    )


def _create_singletons(
    target: Path, entities: dict[str, Any], created: list[tuple[str, Path]]
) -> None:
    """Create missing md singletons with template-scaffolded bodies; skip existing.

    Mutates ``created`` as it goes so the caller can roll creations back on failure.
    """
    from datetime import date

    from khub.core import formats
    from khub.core.template import load_template

    for name, decl in entities.items():
        if decl.get("layout") != "singleton" or not decl.get("path"):
            continue
        spath = target / decl["path"]
        if spath.suffix != ".md" or spath.exists():
            continue
        tpl = load_template(target, name)
        if tpl is None:
            continue
        meta: dict[str, Any] = {
            "type": name,
            "created": date.today(),
            "updated": date.today(),
            "draft": False,
            "title": tpl.title or name,
        }
        spath.parent.mkdir(parents=True, exist_ok=True)
        spath.write_text(formats.render(meta, tpl.render(), "md"))
        created.append((name, spath))


# Entity-capable suffixes (PER_ITEM + .jsonl ahead of collections). Vendor/VCS
# trees are skipped — hashing every package.json in a node_modules would turn
# the cutover snapshot into a full-tree sweep.
_ENTITY_SUFFIXES = tuple(f".{fmt}" for fmt in sorted(PER_ITEM)) + (".jsonl",)
_SKIP_PARTS = frozenset({".khub", ".git", ".venv", "node_modules"})


def _entity_hashes(target: Path) -> dict[str, str]:
    """Content hashes of entity-suffixed files, keyed by relpath.

    Broader than the old md-only sweep (so the cutover guarantee measures
    json/yaml corpora too) — which also broadens ``seeded_over_corpus`` to
    "the target holds any entity-suffixed file". Per-suffix globs keep the
    name filtering in C.
    """
    if not target.exists():
        return {}
    out: dict[str, str] = {}
    for ext in _ENTITY_SUFFIXES:
        for p in target.rglob(f"*{ext}"):
            # is_file(): rglob also matches directories named *.md (real corpora have them)
            if p.is_file() and not _SKIP_PARTS.intersection(p.parts):
                out[str(p.relative_to(target))] = hashlib.sha256(p.read_bytes()).hexdigest()
    return out


def _append_gitignore(gitignore: Path, line: str) -> None:
    existing = gitignore.read_text() if gitignore.exists() else ""
    if line in existing.splitlines():
        return
    sep = "" if existing.endswith("\n") or not existing else "\n"
    gitignore.write_text(f"{existing}{sep}{line}\n")
