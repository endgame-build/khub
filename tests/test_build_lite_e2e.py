"""End-to-end coverage for the build-lite preset, and for the agent-contract fixes.

build-lite shipped in 0.12.0 with schema-resolution tests only: it was never passed to
`khub init` anywhere in the suite, because `fresh_ws` is firm-ops-hardcoded. A smoke test
against a real workspace then found eleven defects that every existing test missed —
every one of them reproduced here.

The workspace built by `lite_ws` mirrors that smoke test: six components (three ours with
a `repo`, three external without), four requirements, five decisions, four feature-specs,
plus the two singletons `init` mints.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from pathlib import Path

import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core.integrity import check, validate

runner = CliRunner()
TTY = {"FORCE_COLOR": "1"}


@pytest.fixture
def lite_ws(ws_for: Callable[[str, Path], Path], tmp_path: Path) -> Path:
    """A populated build-lite workspace: components, requirements, decisions, specs."""
    ws = ws_for("build-lite", tmp_path / "ws")
    add = lambda *a: runner.invoke(app, ["-C", str(ws), "add", *a])
    link = lambda *a: runner.invoke(app, ["-C", str(ws), "link", *a])

    for slug, title, kind, extra in (
        ("cmp-001-core", "Core library", "library", ["--stack", "python", "--repo", "eg/khub"]),
        ("cmp-002-cli", "CLI adapter", "library", ["--stack", "python", "--repo", "eg/khub"]),
        ("cmp-003-presets", "Preset catalogue", "library", ["--stack", "yaml", "--repo", "eg/khub"]),
        ("cmp-004-networkx", "networkx", "external", ["--stack", "python"]),
        ("cmp-005-typer", "Typer", "external", ["--stack", "python"]),
        ("cmp-006-ruamel", "ruamel.yaml", "external", ["--stack", "python"]),
    ):
        assert add("component", "--id", slug, "--title", title, "--kind", kind, *extra).exit_code == 0

    for slug, title, kind in (
        ("fr-001-schema-generic", "Schema-generic surfaces", "functional"),
        ("cst-001-graph-derived", "Graph is derived", "constraint"),
        ("br-001-draft-manual", "Draft is a manual flag", "business-rule"),
    ):
        assert add("requirement", "--id", slug, "--title", title, "--kind", kind).exit_code == 0

    for slug, title, status in (
        ("ad-001-no-linkml", "Remove LinkML", "accepted"),
        ("ad-002-no-database", "No persisted database", "accepted"),
        ("ad-004-kuzu", "Kuzu graph backend", "rejected"),
    ):
        assert add("adr", "--id", slug, "--title", title, "--status", status).exit_code == 0

    for slug, title, status in (
        ("fs-000-lite-draft", "Lite preset sketch", "dropped"),
        ("fs-001-build-lite", "build-lite preset", "done"),
    ):
        assert add("feature-spec", "--id", slug, "--title", title, "--status", status).exit_code == 0

    for args in (
        ("cmp-002-cli", "depends_on", "cmp-001-core"),
        ("cmp-002-cli", "depends_on", "cmp-005-typer"),
        ("cmp-001-core", "depends_on", "cmp-004-networkx"),
        ("cmp-003-presets", "depends_on", "cmp-006-ruamel"),
        ("fr-001-schema-generic", "realized_in", "cmp-001-core"),
        ("cst-001-graph-derived", "realized_in", "cmp-001-core"),
        ("fs-001-build-lite", "requirements", "fr-001-schema-generic"),
        ("fs-001-build-lite", "supersedes", "fs-000-lite-draft"),
        ("ad-002-no-database", "supersedes", "ad-004-kuzu"),
        ("ad-001-no-linkml", "affects", "cmp-001-core"),
    ):
        assert link(*args).exit_code == 0, args
    return ws


# --- the preset itself, end to end ---------------------------------------------


@pytest.mark.e2e
def test_fresh_build_lite_workspace_passes_strict_check(
    ws_for: Callable[[str, Path], Path], tmp_path: Path
) -> None:
    """The `orphan: true` regression gate: prd and arc42 are edge-less BY DESIGN.

    Without the exemption a correct, freshly-scaffolded workspace fails `--strict`, which
    forecloses strict mode entirely. This bit build-hub before the flag existed.
    """
    ws = ws_for("build-lite", tmp_path / "ws")
    assert (ws / "knowledge" / "prd.md").exists() and (ws / "knowledge" / "arc42.md").exists()
    report = check(ws, strict=True)
    assert report.passed, report
    assert report.orphans == []


@pytest.mark.e2e
def test_seeded_workspace_is_clean_and_orphan_sweep_agrees(lite_ws: Path) -> None:
    """`check`, `query --orphan` and `status` are three implementations of one rule."""
    report = check(lite_ws, strict=True)
    # br-001 is deliberately unwired; prd/arc42 are exempt and must not appear.
    assert report.orphans == ["requirement/br-001-draft-manual"]

    queried = runner.invoke(app, ["-C", str(lite_ws), "query", "--orphan"])
    assert [r["id"] for r in json.loads(queried.output)] == ["requirement/br-001-draft-manual"]

    status = json.loads(runner.invoke(app, ["-C", str(lite_ws), "status"]).output)
    assert status["orphan"] == 1


@pytest.mark.e2e
def test_validate_and_check_stay_distinct_under_a_cycle(lite_ws: Path) -> None:
    """A cycle is a graph property: `check` reports it, `validate` cannot see it."""
    assert runner.invoke(
        app, ["-C", str(lite_ws), "link", "cmp-004-networkx", "depends_on", "cmp-001-core"]
    ).exit_code == 0
    assert validate(lite_ws).ok  # per-entity well-formedness is untouched
    cycles = check(lite_ws).cycles
    assert any({"component/cmp-001-core", "component/cmp-004-networkx"} == set(c) for c in cycles)


@pytest.mark.e2e
def test_derived_inverse_is_never_written_to_disk(lite_ws: Path) -> None:
    """`superseded` is computed from the inbound edge; storing it would be a second truth."""
    view = json.loads(
        runner.invoke(app, ["-C", str(lite_ws), "get", "fs-000-lite-draft", "--edges"]).output
    )
    assert {"predicate": "superseded", "target": "feature-spec/fs-001-build-lite", "derived": True} in view["edges"]
    assert "superseded" not in (lite_ws / "specs" / "fs-000-lite-draft.md").read_text()


@pytest.mark.e2e
def test_force_into_a_populated_directory_modifies_nothing(tmp_path: Path) -> None:
    """Landing khub in an EXISTING repo is the common case; --force must be safe."""
    target = tmp_path / "repo"
    (target / "src").mkdir(parents=True)
    source = target / "src" / "main.py"
    source.write_text("print('hi')\n")
    before = source.read_bytes()

    result = runner.invoke(app, ["init", "build-lite", str(target), "--force", "--no-wire"])
    assert result.exit_code == 0, result.output
    assert source.read_bytes() == before
    assert (target / ".khub" / "schema.yaml").exists()


@pytest.mark.e2e
@pytest.mark.parametrize(
    ("mutation", "bucket"),
    [
        ("delete_prd", "missing_singletons"),
        ("dangling", "dangling"),
        ("stray", "strays"),
        ("malformed", "malformed"),
        ("incomplete", "incomplete"),
    ],
)
def test_each_integrity_gate_fires(lite_ws: Path, mutation: str, bucket: str) -> None:
    """One mutation per gate — the nine checks the smoke test ran by hand."""
    if mutation == "delete_prd":
        (lite_ws / "knowledge" / "prd.md").unlink()
    elif mutation == "dangling":
        p = lite_ws / "knowledge" / "requirements" / "fr-001-schema-generic.md"
        p.write_text(p.read_text().replace("realized_in:", "realized_in:\n- cmp-999-ghost", 1))
    elif mutation == "stray":
        (lite_ws / "knowledge" / "components" / "stray.md").write_text("just prose\n")
    elif mutation == "malformed":
        (lite_ws / "knowledge" / "components" / "broken.md").write_text(
            "---\ntype: component\ntitle: [unclosed\n---\nbody\n"
        )
    elif mutation == "incomplete":
        p = lite_ws / "knowledge" / "components" / "cmp-001-core.md"
        p.write_text("\n".join(ln for ln in p.read_text().splitlines() if not ln.startswith("kind:")))

    report = check(lite_ws)
    assert not report.passed
    assert getattr(report, bucket), f"{bucket} empty for mutation {mutation}"


@pytest.mark.e2e
def test_deleting_the_optional_singleton_still_passes(lite_ws: Path) -> None:
    """`required` is per type: arc42 is a singleton but not a required one."""
    (lite_ws / "knowledge" / "arc42.md").unlink()
    report = check(lite_ws)
    assert report.missing_singletons == []
    assert report.passed


# --- the agent-contract fixes ---------------------------------------------------


@pytest.mark.e2e
def test_search_reaches_attribute_values(lite_ws: Path) -> None:
    """`search python` returned [] while `stack: python` sat in frontmatter — md entities
    indexed the body alone, so the one format every preset uses by default was unsearchable
    on exactly the fields an agent filters by."""
    hits = json.loads(runner.invoke(app, ["-C", str(lite_ws), "search", "python"]).output)
    assert {h["slug"] for h in hits} >= {"cmp-001-core", "cmp-004-networkx"}


@pytest.mark.e2e
def test_has_and_missing_accept_an_attribute(lite_ws: Path) -> None:
    """`--has repo` raised "No field 'repo' on type 'component'" — the field is declared.
    Combined with search missing frontmatter, an agent had NO route to find an entity by
    an attribute value, so it fell back to grep: the bypass the wiring exists to prevent."""
    ids = lambda *a: [
        r["slug"] for r in json.loads(runner.invoke(app, ["-C", str(lite_ws), "query", *a]).output)
    ]
    assert ids("--type", "component", "--has", "repo") == [
        "cmp-001-core", "cmp-002-cli", "cmp-003-presets"
    ]
    assert ids("--type", "component", "--missing", "repo") == [
        "cmp-004-networkx", "cmp-005-typer", "cmp-006-ruamel"
    ]

    # The guardrail: widening to attributes must not accept an arbitrary string.
    typo = runner.invoke(app, ["-C", str(lite_ws), "query", "--type", "component", "--has", "repoo"])
    assert typo.exit_code == 1
    assert json.loads(typo.output)["error"]["code"] == "filter_error"


@pytest.mark.e2e
def test_query_records_carry_title(lite_ws: Path) -> None:
    """Listing a type then reading one field per row was N+1: query + one get per entity."""
    rows = json.loads(runner.invoke(app, ["-C", str(lite_ws), "query", "--type", "component"]).output)
    assert {r["slug"]: r["title"] for r in rows}["cmp-001-core"] == "Core library"


@pytest.mark.e2e
def test_an_uppercase_id_round_trips(lite_ws: Path) -> None:
    """`add --id CMP-007-Api` writes `cmp-007-api`, and reads of the passed string failed —
    write and read disagreeing about one identifier. Both resolvers now fold case."""
    assert runner.invoke(
        app, ["-C", str(lite_ws), "add", "component", "--id", "CMP-007-Api",
              "--title", "API", "--kind", "service"]
    ).exit_code == 0
    assert runner.invoke(app, ["-C", str(lite_ws), "get", "CMP-007-Api"]).exit_code == 0
    # resolve_target folds too, so link and get cannot disagree about existence.
    assert runner.invoke(
        app, ["-C", str(lite_ws), "link", "cmp-002-cli", "depends_on", "CMP-007-Api"]
    ).exit_code == 0


@pytest.mark.e2e
def test_case_variant_targets_never_duplicate_an_edge(lite_ws: Path) -> None:
    """Case-insensitive resolution without canonical storage is worse than no folding.

    Resolution accepted `CMP-001-CORE`, but `link` stored the caller's raw string and
    deduped by exact match — so three spellings of one node became three parallel edges,
    each reporting `changed: true`. Before folding existed the mixed-case call hard-failed,
    so this was impossible: the fix must canonicalize on write, not only on read.
    """
    changed = []
    for spelling in ("CMP-001-CORE", "cmp-001-core", "Cmp-001-Core"):
        out = runner.invoke(
            app, ["-C", str(lite_ws), "link", "cmp-003-presets", "depends_on", spelling]
        )
        changed.append(json.loads(out.output)["changed"])
    assert changed == [True, False, False]

    stored = (lite_ws / "knowledge" / "components" / "cmp-003-presets.md").read_text()
    assert stored.count("cmp-001-core") == 1
    assert "CMP-001-CORE" not in stored  # the canonical spelling is what lands

    # ...and unlink by any spelling removes the edge it can see.
    out = runner.invoke(
        app, ["-C", str(lite_ws), "unlink", "cmp-003-presets", "depends_on", "CMP-001-Core"]
    )
    assert json.loads(out.output)["changed"] is True


@pytest.mark.e2e
def test_a_qualified_id_folds_case_for_reads_and_writes_alike(lite_ws: Path) -> None:
    """`get component/CMP-001-Core` resolved while `link ... component/CMP-001-Core` did
    not — the two resolvers disagreed on the qualified form only."""
    assert runner.invoke(app, ["-C", str(lite_ws), "get", "component/CMP-001-Core"]).exit_code == 0
    linked = runner.invoke(
        app, ["-C", str(lite_ws), "link", "cmp-002-cli", "references", "component/CMP-001-Core"]
    )
    assert linked.exit_code == 0, linked.output


@pytest.mark.e2e
def test_missing_skips_types_that_cannot_carry_the_name(lite_ws: Path) -> None:
    """Untyped `--missing kind` returned prd/arc42 too — neither declares `kind`, so they
    are not gaps an author could ever close, and they dilute the gap query."""
    # A genuine gap to find: capture is never blocked, so a required field may be absent.
    assert runner.invoke(
        app, ["-C", str(lite_ws), "add", "requirement", "--id", "fr-gap", "--title", "Gap"]
    ).exit_code == 0

    rows = json.loads(runner.invoke(app, ["-C", str(lite_ws), "query", "--missing", "kind"]).output)
    assert [r["slug"] for r in rows] == ["fr-gap"]  # the only entity that CAN lack `kind`
    assert not any(r["type"] in {"prd", "arc42"} for r in rows)


@pytest.mark.e2e
def test_a_drafted_optional_singleton_is_reported_on_a_tty(lite_ws: Path) -> None:
    """It does not fail the gate, so the human path returned early and printed nothing —
    the exact silence the report was added to end."""
    runner.invoke(app, ["-C", str(lite_ws), "edit", "arc42", "draft", "true"])
    passing = runner.invoke(app, ["-C", str(lite_ws), "check"], env=TTY)
    assert passing.exit_code == 0
    assert "arc42 is unpublished" in passing.output

    # And on the FAILING path, where a separate finding drives the exit code: the human
    # view has two branches, so reporting it in only one is the same silence again.
    (lite_ws / "knowledge" / "prd.md").unlink()
    failing = runner.invoke(app, ["-C", str(lite_ws), "check"], env=TTY)
    assert failing.exit_code == 1
    assert "arc42 is unpublished" in failing.output
    assert "prd is missing" in failing.output


@pytest.mark.e2e
def test_no_template_is_refused_on_a_templated_type(lite_ws: Path) -> None:
    """The flag's only outcome on a templated type was an entity `validate` rejects."""
    result = runner.invoke(
        app, ["-C", str(lite_ws), "add", "adr", "--title", "Probe",
              "--status", "proposed", "--no-template"]
    )
    assert result.exit_code == 1
    assert json.loads(result.output)["error"]["code"] == "template_required"
    assert validate(lite_ws).ok  # nothing was written


@pytest.mark.e2e
def test_every_type_declares_a_capture_trigger(lite_ws: Path) -> None:
    """`when` answers the question the schema could not: not how to write, but WHEN.

    A real-codebase eval showed wired agents losing on inaction, not wrong commands — a
    terse "Note it." read as conversation. The trigger is per-domain, so it lives in the
    schema and reaches every surface without any of them learning a type name.
    """
    view = json.loads(runner.invoke(app, ["-C", str(lite_ws), "schema"]).output)
    whens = {t["name"]: t["when"] for t in view["types"]}
    assert all(whens.values()), f"types with no capture trigger: {[k for k,v in whens.items() if not v]}"
    assert "must satisfy or must never violate" in whens["requirement"]


@pytest.mark.e2e
def test_the_wired_block_carries_the_triggers_and_the_cli_rule(
    ws_for: Callable[[str, Path], Path], tmp_path: Path
) -> None:
    """The block is what `init` installs; the skill is a separate opt-in step, so the
    activation rules have to survive in the block alone."""
    ws = ws_for("build-lite", tmp_path / "ws")
    runner.invoke(app, ["-C", str(ws), "wire", "--target", "claude"])
    block = (ws / "CLAUDE.md").read_text()

    assert "Record as you go" in block
    for type_ in ("prd", "arc42", "requirement", "adr", "component", "feature-spec"):
        assert f"- `{type_}` —" in block, f"{type_} trigger missing from the block"
    # Measured: "Note it." produced "Noted." and no record 5/5, while "Record it." on the
    # identical fact recorded 5/5 — a vocabulary gap, not a capability one. The block has to
    # map the synonyms or it only works for one verb.
    assert "is a capture request, whatever the wording" in block
    assert '"Noted." without a record does not complete the task' in block
    assert "Every write goes through the CLI" in block
    assert "run `khub validate` on it immediately" in block
    assert "query it, do not grep it" in block


@pytest.mark.e2e
def test_schema_show_exposes_every_enforced_contract(lite_ws: Path) -> None:
    """`acyclic`, `inverse`, `pattern` and `default` are enforced but were undiscoverable,
    so an agent told to read the schema at runtime could not learn why a write was rejected."""
    view = json.loads(runner.invoke(app, ["-C", str(lite_ws), "schema", "show", "adr"]).output)
    supersedes = next(r for r in view["relations"] if r["predicate"] == "supersedes")
    assert supersedes["acyclic"] is True and supersedes["inverse"] == "superseded"
    draft = next(f for f in view["fields"] if f["name"] == "draft")
    assert draft["default"] is False
    assert "pattern" in draft


@pytest.mark.e2e
def test_schema_edges_keeps_every_declared_target(lite_ws: Path) -> None:
    """`supersedes` is declared on adr AND feature-spec with DIFFERENT targets.

    Keying rows by predicate name collapsed them and made `from` x `to` a cross product,
    advertising `feature-spec --supersedes--> adr` — an edge validate rejects. Merging the
    targets only added the reverse claim as well, so each declaration gets its own row.
    """
    edges = json.loads(runner.invoke(app, ["-C", str(lite_ws), "schema", "edges"]).output)
    rows = [e for e in edges if e["predicate"] == "supersedes"]
    assert {(tuple(r["from"]), tuple(r["to"])) for r in rows} == {
        (("adr",), ("adr",)),
        (("feature-spec",), ("feature-spec",)),
    }
    assert all(r["acyclic"] is True and r["inverse"] == "superseded" for r in rows)


@pytest.mark.e2e
def test_write_verbs_speak_json_on_a_pipe_and_prose_on_a_tty(lite_ws: Path) -> None:
    """The contract an agent consumes: it never has a TTY."""
    piped = runner.invoke(
        app, ["-C", str(lite_ws), "add", "requirement", "--id", "fr-900",
              "--title", "Piped", "--kind", "functional"]
    )
    assert json.loads(piped.output)["id"] == "requirement/fr-900"

    tty = runner.invoke(
        app, ["-C", str(lite_ws), "add", "requirement", "--id", "fr-901",
              "--title", "Tty", "--kind", "functional"], env=TTY
    )
    assert "Created requirement 'fr-901' (active)" in tty.output
