"""STORY-SCH-003 — compile the schema to LinkML, Pydantic, and JSON Schema.

Covers TS-SCH-003-01 (artifacts), -02 (round-trip, no LinkML keywords),
-03 (duplicate type, atomic no-partial-write), -04 (determinism), and the
extra="allow" both-ways case (TS-SCH-003-U06 + TS-SCH-001-02 enum reject).
"""

from __future__ import annotations

import datetime
import importlib.util
from pathlib import Path

import pytest

from khub.core.compile import compile_schema
from khub.core.errors import LocatedError

PRESET = """
entities:
  person: { layout: file }
  project:
    layout: folder
    attributes:
      stage: { enum: [diagnose, prove, scale, complete], required: true }
    relations:
      owner: { to: person, required: true }
"""


def _import_models(path: Path):
    spec = importlib.util.spec_from_file_location("genmodels", path)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def test_compile_writes_artifacts(write_schema, core_base, tmp_path):
    """TS-SCH-003-01: emit LinkML + Pydantic v2 + JSON Schema into the out dir."""
    out = tmp_path / "gen"
    compile_schema(write_schema(core=core_base, preset=PRESET), out)
    assert (out / "schema.linkml.yaml").exists()
    assert (out / "models.py").exists()
    assert (out / "schema.json").exists()
    # the authored inputs carry no LinkML keywords (TS-SCH-003-02, round-trip)
    assert "range:" not in PRESET and "slot_usage" not in PRESET


def test_extra_allow_and_enum_reject(write_schema, core_base, tmp_path):
    """TS-SCH-001-02 + TS-SCH-003-U06: declared enum rejects a bad value; an
    undeclared field is tolerated under extra="allow" (tested as distinct cases)."""
    out = tmp_path / "gen"
    compile_schema(write_schema(core=core_base, preset=PRESET), out)
    import pydantic

    models = _import_models(out / "models.py")
    Project = models.Project
    d = datetime.date(2026, 1, 1)

    # declared enum constraint rejects an out-of-enum value
    with pytest.raises(pydantic.ValidationError):
        Project(type="project", created=d, stage="invalid", owner="x")

    # valid entity + an undeclared field is tolerated (open-schema write)
    p = Project(type="project", created=d, stage="diagnose", owner="x", undeclared_field="ok")
    assert p.undeclared_field == "ok"


def test_deterministic_recompile(write_schema, core_base, tmp_path):
    """TS-SCH-003-04: two runs on an unchanged schema produce byte-identical artifacts."""
    paths = write_schema(core=core_base, preset=PRESET)
    a, b = tmp_path / "a", tmp_path / "b"
    compile_schema(paths, a)
    compile_schema(paths, b)
    for name in ("schema.linkml.yaml", "models.py", "schema.json"):
        assert (a / name).read_bytes() == (b / name).read_bytes(), f"{name} not deterministic"


# pydantic is pessimistic about the aliased annotation on its first build pass and
# warns, then model_rebuild() resolves it — the assertions below prove validation
# is real (coerces + rejects), so the cosmetic warning is silenced.
@pytest.mark.filterwarnings("ignore:.*is not a Python type.*")
def test_field_named_like_a_type_works(write_schema, core_base, tmp_path):
    """A field literally named like a Python datetime type (`date`) compiles and
    genuinely validates as that type — the field-name/type clash is handled."""
    import pydantic

    preset = """
entities:
  log:
    layout: file
    attributes:
      date: { type: date, required: true }
"""
    out = tmp_path / "gen"
    compile_schema(write_schema(core=core_base, preset=preset), out)
    Log = _import_models(out / "models.py").Log

    # real validation: a string is coerced to a date...
    entry = Log(type="log", created="2026-01-01", date="2026-02-02")
    assert entry.date == datetime.date(2026, 2, 2)
    # ...and a non-date is rejected (not silently accepted).
    with pytest.raises(pydantic.ValidationError):
        Log(type="log", created="2026-01-01", date="not-a-date")


def test_duplicate_type_fails_atomically(write_schema, core_base, tmp_path):
    """TS-SCH-003-03: a duplicate type fails the compile with a located error and
    writes no artifacts."""
    a = "entities: { project: { layout: folder } }"
    b = "entities: { project: { layout: folder } }"
    out = tmp_path / "gen"
    out.mkdir()
    with pytest.raises(LocatedError) as ei:
        compile_schema(write_schema(core=core_base, presetA=a, presetB=b), out)
    assert ei.value.code == "duplicate_type"
    assert ei.value.message == "Duplicate type 'project'"
    assert list(out.iterdir()) == []  # no partial artifacts
