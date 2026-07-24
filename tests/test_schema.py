"""STORY-WS-002 — `khub schema`: introspect the active schema.

Covers TS-WS-002-01..04 and the unit rows TS-WS-002-U01..U07. Counts and the
predicate set are read from the compiled schema, never literals (TS-001 Risk:
the 12/17 prose is stale vs the 9-type / 14-predicate firm-ops preset, which
folded out the derived `superseded_by`).
"""

from __future__ import annotations

import json
from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core import resolve
from khub.core.errors import LocatedError
from khub.core.introspect import edges_view, schema_view, type_view, types_list
from khub.core.workspace import init_workspace

runner = CliRunner()

PRESETS = Path(__file__).resolve().parents[1] / "src" / "khub" / "presets"
CORE = PRESETS / "core.yaml"
FIRM_OPS = PRESETS / "firm-ops" / "schema.yaml"


@pytest.fixture(scope="module")
def resolved():
    return resolve([CORE, FIRM_OPS])


@pytest.fixture(scope="module")
def firm_ops_ws(tmp_path_factory) -> Path:
    root = tmp_path_factory.mktemp("hq")
    init_workspace("firm-ops", root)
    return root


# --- TS-WS-002-U01..U05: introspection logic ------------------------------------


@pytest.mark.unit
def test_type_view_fields_and_enum(resolved) -> None:
    """TS-WS-002-U01/U02: a type's fields, enum values, and required flags."""
    view = type_view(resolved, "opportunity", "firm-ops")
    stage = next(f for f in view["fields"] if f["name"] == "stage")
    assert stage["enum"] == ["prospect", "proposal-sent", "won", "signed", "lost"]
    assert stage["required"] is True
    assert view["layout"] == "folder"


@pytest.mark.unit
def test_type_view_flags_required_relations(resolved) -> None:
    """TS-WS-002-U03 (WS-005): required relations come from the schema."""
    rels = {r["predicate"]: r for r in type_view(resolved, "opportunity", "firm-ops")["relations"]}
    assert rels["client"]["required"] is True
    assert rels["owner"]["required"] is True
    assert rels["partner"]["required"] is False


@pytest.mark.unit
def test_unknown_type_lists_known(resolved) -> None:
    """TS-WS-002-U04 (REQ-WS002-03): an unknown type errors and lists known types."""
    with pytest.raises(LocatedError) as ei:
        type_view(resolved, "widget", "firm-ops")
    assert ei.value.code == "unknown_type"
    assert "No type 'widget' in the firm-ops schema" in ei.value.message
    assert "opportunity" in ei.value.message


@pytest.mark.unit
def test_schema_view_covers_every_type(resolved) -> None:
    """TS-WS-002-U05 (WS-004): the full view is derived from the schema for every type."""
    view = schema_view(resolved, {"preset": "firm-ops", "version": "0.1.0"})
    assert {t["name"] for t in view["types"]} == set(resolved.types)
    assert view["provenance"] == {"preset": "firm-ops", "version": "0.1.0"}


@pytest.mark.unit
def test_edges_view_matches_compiled_vocabulary(resolved) -> None:
    """TS-WS-002-U06: the predicate set is read from the schema; no derived edge in firm-ops."""
    expected = set(resolved.base_relations) | {
        p for t in resolved.types.values() for p in t.relations
    }
    edges = edges_view(resolved)
    assert {e["predicate"] for e in edges} == expected
    assert "superseded_by" not in {e["predicate"] for e in edges}
    related = next(e for e in edges if e["predicate"] == "related")
    assert related["kind"] == "any" and related["from"] == ["any"]
    engagement = next(e for e in edges if e["predicate"] == "engagement")
    assert engagement["kind"] == "union" and engagement["required"] is True


# --- TS-WS-002-01..04: command surface (JSON branch on the non-TTY runner) -------


@pytest.mark.integration
def test_cli_full_schema_json(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-WS-002-01 (AC-001): the full schema is valid JSON with types + provenance."""
    monkeypatch.chdir(firm_ops_ws)
    result = runner.invoke(app, ["schema", "--format", "json"])
    assert result.exit_code == 0, result.output
    data = json.loads(result.output)
    assert data["provenance"]["preset"] == "firm-ops"
    assert any(t["name"] == "opportunity" and t["relations"] for t in data["types"])


@pytest.mark.integration
def test_cli_show_type_json(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-WS-002-02 (AC-002): a single type's fields, enum, and required relations."""
    monkeypatch.chdir(firm_ops_ws)
    result = runner.invoke(app, ["schema", "show", "opportunity", "--format", "json"])
    assert result.exit_code == 0, result.output
    data = json.loads(result.output)
    rels = {r["predicate"]: r for r in data["relations"]}
    assert rels["client"]["required"] and rels["owner"]["required"]


@pytest.mark.integration
def test_cli_show_unknown_type(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-WS-002-03 (AC-003): an unknown type errors with the exact line."""
    monkeypatch.chdir(firm_ops_ws)
    result = runner.invoke(app, ["schema", "show", "widget"])
    assert result.exit_code == 1
    assert "No type 'widget' in the firm-ops schema" in result.output


@pytest.mark.integration
def test_cli_types_list(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-WS-002-01: `schema types` returns the type set read from the schema."""
    monkeypatch.chdir(firm_ops_ws)
    result = runner.invoke(app, ["schema", "types", "--format", "json"])
    assert result.exit_code == 0, result.output
    assert set(json.loads(result.output)) == set(resolve([CORE, FIRM_OPS]).types)


@pytest.mark.e2e
def test_cli_edges_vocabulary(firm_ops_ws: Path, monkeypatch) -> None:
    """TS-WS-002-04 (AC-004): `schema edges` returns the full predicate vocabulary."""
    monkeypatch.chdir(firm_ops_ws)
    expected = set(resolve([CORE, FIRM_OPS]).base_relations) | {
        p for t in resolve([CORE, FIRM_OPS]).types.values() for p in t.relations
    }
    result = runner.invoke(app, ["schema", "edges", "--format", "json"])
    assert result.exit_code == 0, result.output
    edges = json.loads(result.output)
    assert {e["predicate"] for e in edges} == expected


@pytest.mark.unit
def test_types_list_helper(resolved) -> None:
    """`types_list` returns the declared type names."""
    assert set(types_list(resolved)) == set(resolved.types)
