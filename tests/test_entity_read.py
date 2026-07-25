"""TS-ENT-002 — Read an Entity (WPK-002-1).

Covers id resolution and ambiguity, the three output formats (Rich / JSON / raw),
and read-time derivation of inverse edges. firm-ops declares no inverse, so the
derived-edge cases run against a generic fixture preset that declares a
``supersedes`` relation with an ``inverse: superseded_by``.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from pathlib import Path

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.entity import get, resolve_id
from khub.core.errors import LocatedError
from khub.core.index import build_index
from khub.core.introspect import load_schema
from khub.core.workspace import init_workspace

runner = CliRunner()

Seed = Callable[..., None]

FIXTURE_PRESET = """
version: "0.1.0"
entities:
  doc:
    layout: file
    path: docs
    relations:
      supersedes: { to: doc, inverse: superseded_by }
"""


@pytest.fixture
def inverse_ws(tmp_path: Path, seed: Seed) -> Path:
    """A workspace whose schema declares a stored→derived inverse pair."""
    src = tmp_path / "presets"
    src.mkdir()
    (src / "fixture").mkdir()
    (src / "fixture" / "schema.yaml").write_text(FIXTURE_PRESET)
    ws = tmp_path / "ws"
    init_workspace("fixture", ws, preset_source=src)
    seed(ws, "docs/node-a.md", type="doc", supersedes="node-b")
    seed(ws, "docs/node-b.md", type="doc")
    return ws


# --- unit --------------------------------------------------------------------


@pytest.mark.unit
def test_resolve_id_bare_slug(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-002-U01: a bare slug resolves to its one node."""
    seed(fresh_ws, "clients/initech.md", type="client", name="Initech")
    index = build_index(fresh_ws, load_schema(fresh_ws))
    assert resolve_id(index, "initech") == ("client", "initech")


@pytest.mark.unit
def test_resolve_id_ambiguous_requires_qualifier(fresh_ws: Path, seed: Seed) -> None:
    """TS-ENT-002-U02: a slug across two types is ambiguous; type/slug is exact."""
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme")
    seed(fresh_ws, "partnerships/acme/_index.md", type="partnership", partner="Acme")
    index = build_index(fresh_ws, load_schema(fresh_ws))
    with pytest.raises(LocatedError) as err:
        resolve_id(index, "acme")
    assert err.value.code == "ambiguity_error"
    assert resolve_id(index, "client/acme") == ("client", "acme")


@pytest.mark.unit
def test_lookup_error_for_missing_id(fresh_ws: Path) -> None:
    """TS-ENT-002-U05: an unresolvable id raises a lookup error."""
    index = build_index(fresh_ws, load_schema(fresh_ws))
    with pytest.raises(LocatedError) as err:
        resolve_id(index, "nope")
    assert err.value.code == "lookup_error"


@pytest.mark.unit
def test_inverse_edge_derived_and_marked(inverse_ws: Path) -> None:
    """TS-ENT-002-U03/U04: superseded_by derives at read time, marked derived, never stored."""
    source = get(inverse_ws, "node-a", edges=True)
    assert source.edges is not None
    stored = [e for e in source.edges if not e.derived]
    assert any(e.predicate == "supersedes" and e.target == "node-b" for e in stored)

    target = get(inverse_ws, "node-b", edges=True)
    assert target.edges is not None
    derived = [e for e in target.edges if e.derived]
    # Derived edges are qualified type/slug (the source type isn't in a bare slug).
    assert any(e.predicate == "superseded_by" and e.target == "doc/node-a" for e in derived)
    # Never persisted: the inverse is absent from node-b's stored frontmatter.
    assert "superseded_by" not in frontmatter.load(str(inverse_ws / "docs" / "node-b.md")).metadata


# --- integration / e2e -------------------------------------------------------


@pytest.mark.e2e
def test_cli_get_prints_frontmatter_body_and_formats(fresh_ws: Path, monkeypatch) -> None:
    """TS-ENT-002-01: get prints frontmatter+body; json and raw branches agree with the file."""
    path = fresh_ws / "projects" / "initech-pov" / "_index.md"
    path.parent.mkdir(parents=True, exist_ok=True)
    raw = "---\ntype: project\nclient: initech\nowner: noor\n---\nProof of value.\n"
    path.write_text(raw)
    monkeypatch.chdir(fresh_ws)

    default = runner.invoke(app, ["get", "initech-pov"])
    assert default.exit_code == 0
    data = json.loads(default.output)
    assert data["type"] == "project" and data["body"].strip() == "Proof of value."
    assert data["frontmatter"]["client"] == "initech"

    raw_out = runner.invoke(app, ["get", "initech-pov", "--format", "raw"])
    assert raw_out.output == raw

    json_out = runner.invoke(app, ["get", "initech-pov", "--format", "json"])
    record = json.loads(json_out.output)
    assert record["id"] == "project/initech-pov"  # id is qualified type/slug
    assert record["type"] == "project" and record["slug"] == "initech-pov"


@pytest.mark.integration
def test_cli_get_edges_includes_derived(inverse_ws: Path, monkeypatch) -> None:
    """TS-ENT-002-02: get --edges marks the stored forward edge and the derived inverse."""
    monkeypatch.chdir(inverse_ws)
    result = runner.invoke(app, ["get", "node-b", "--edges", "--format", "json"])
    assert result.exit_code == 0
    edges = json.loads(result.output)["edges"]
    assert {"predicate": "superseded_by", "target": "doc/node-a", "derived": True} in edges


@pytest.mark.integration
def test_cli_get_unknown_id(fresh_ws: Path, monkeypatch) -> None:
    """TS-ENT-002-03: an unresolvable id reports a lookup error."""
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["get", "nope"])
    assert result.exit_code == 1
    assert "No entity 'nope' found" in result.output


@pytest.mark.integration
def test_cli_get_ambiguous_slug(fresh_ws: Path, seed: Seed, monkeypatch) -> None:
    """TS-ENT-002-04: a slug shared across types asks for a type/slug qualifier."""
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme")
    seed(fresh_ws, "partnerships/acme/_index.md", type="partnership", partner="Acme")
    monkeypatch.chdir(fresh_ws)
    result = runner.invoke(app, ["get", "acme"])
    assert result.exit_code == 1
    assert "Slug 'acme' is ambiguous: client/acme, partnership/acme. Qualify as type/slug" in result.output
