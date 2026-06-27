"""Schema compiler — WPK-000-2.

``compile_schema`` (the ``core.compile`` verb; named ``compile_schema`` to avoid
shadowing the builtin) resolves the khub schema, emits LinkML, then generates
Pydantic v2 (``extra="allow"``) and JSON Schema into ``.khub/generated/``.
Regeneration is deterministic; a malformed schema fails atomically, writing no
partial artifacts.
"""

from __future__ import annotations

import io
import os
import shutil
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from ruamel.yaml import YAML

from khub.core.determinism import (
    canonicalize_json_schema,
    canonicalize_pydantic,
    deconflict_type_named_fields,
)
from khub.core.errors import LocatedError
from khub.core.linkml_emit import to_linkml_dict
from khub.core.resolve import load_yaml, resolve

ARTIFACTS = ("schema.linkml.yaml", "models.py", "schema.json")

# typ="safe" sorts mapping keys on output (matches the old yaml.safe_dump
# sort_keys=True), so schema.linkml.yaml stays deterministic; wide width keeps
# long scalars on one line.
_yaml = YAML(typ="safe")
_yaml.default_flow_style = False
_yaml.width = 4096


@dataclass(frozen=True)
class CompileResult:
    """Outcome of a compile: the output directory and the artifacts written."""

    out_dir: Path
    artifacts: tuple[str, ...] = ARTIFACTS


def compile_schema(
    schema: Path | str | list[Path], out: Path | str = Path(".khub/generated")
) -> CompileResult:
    """Compile a resolved khub schema into ``out`` (LinkML, Pydantic v2, JSON Schema)."""
    files = [Path(schema)] if isinstance(schema, (str, Path)) else [Path(f) for f in schema]
    out = Path(out)

    # Reject malformed schemas BEFORE writing anything (atomic; SCH-009).
    _check_duplicate_types(files)
    resolved = resolve(files)
    linkml_dict = to_linkml_dict(resolved)

    out.parent.mkdir(parents=True, exist_ok=True)
    tmp = Path(tempfile.mkdtemp(dir=out.parent, prefix=".khub-gen-"))
    try:
        _generate(linkml_dict, tmp)
    except BaseException:
        shutil.rmtree(tmp, ignore_errors=True)
        raise

    # Overwrite prior output deterministically.
    if out.exists():
        shutil.rmtree(out)
    os.replace(tmp, out)
    return CompileResult(out_dir=out)


def _generate(linkml_dict: dict[str, Any], dest: Path) -> None:
    # Local imports: the heavy LinkML backend is only needed at compile time.
    from linkml.generators.jsonschemagen import JsonSchemaGenerator
    from linkml.generators.pydanticgen import PydanticGenerator

    linkml_path = dest / "schema.linkml.yaml"
    buf = io.StringIO()
    _yaml.dump(linkml_dict, buf)
    linkml_path.write_text(buf.getvalue())

    models = PydanticGenerator(str(linkml_path), extra_fields="allow").serialize()
    (dest / "models.py").write_text(canonicalize_pydantic(deconflict_type_named_fields(models)))

    json_schema = JsonSchemaGenerator(str(linkml_path)).serialize()
    (dest / "schema.json").write_text(canonicalize_json_schema(json_schema))


def _check_duplicate_types(files: list[Path]) -> None:
    seen: set[str] = set()
    for f in files:
        for name in (load_yaml(Path(f)).get("entities") or {}):
            if name in seen:
                raise LocatedError.duplicate_type(name)
            seen.add(name)
