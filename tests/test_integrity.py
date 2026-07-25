"""TS-INT-001 / TS-INT-002 — the v1 integrity gate (WPK-004-1).

`validate` checks per-entity well-formedness and referential integrity over a
firm-ops tree; `check` runs graph-wide over the active subgraph for completeness,
orphans, dangling edges, stray files, and edge cycles. Clean trees are seeded via
`core.create` (so referential integrity holds); breaks are written directly so the
gate sees them.
"""

from __future__ import annotations

import json
from collections.abc import Callable
from pathlib import Path

import frontmatter
import pytest
from typer.testing import CliRunner

from khub.cli.main import app
from khub.core import entity
from khub.core.integrity import check, validate

runner = CliRunner()
Seed = Callable[..., None]


def seed_clean(root: Path) -> None:
    """The clean firm-ops seed: every required field present, every relation resolving."""
    entity.create(root, "person", {"name": "Noor", "role": "partner", "mood": "good"}, id_="noor")
    entity.create(root, "client", {"name": "Initech"}, id_="initech")
    entity.create(
        root, "opportunity",
        {"name": "Initech Deal", "stage": "prospect", "client": "initech", "owner": "noor"},
        id_="initech-deal",
    )
    entity.create(
        root, "project", {"client": "initech", "owner": "noor", "active": "true"},
        id_="initech-pov",
    )
    entity.create(
        root, "meeting",
        {"date": "2026-06-01T10:00:00", "call_type": "client", "source": "recording",
         "engagement": "initech-pov"},
        id_="kickoff",
    )


@pytest.fixture
def clean_ws(fresh_ws: Path) -> Path:
    """A sound firm-ops tree plus a frontmatter-less reference doc outside every layout."""
    seed_clean(fresh_ws)
    (fresh_ws / "identity").mkdir(exist_ok=True)
    (fresh_ws / "identity" / "mission.md").write_text("# Mission\n\nNo frontmatter here.\n")
    return fresh_ws


# --- validate: clean / skip / passthrough ------------------------------------


@pytest.mark.e2e
def test_validate_clean_tree(clean_ws: Path) -> None:
    """TS-INT-001-01 (AC-001): a clean tree validates; extensions pass; reference skipped."""
    report = validate(clean_ws)
    assert report.ok and report.errors == []
    assert report.count == 5  # only typed entities; mission.md is not counted


@pytest.mark.unit
def test_validate_extension_passthrough(clean_ws: Path) -> None:
    """TS-INT-001-U03 (REQ-INT001-04, INT-001): undeclared `mood` is left unchecked."""
    assert validate(clean_ws).ok  # person/noor carries an undeclared `mood`
    stored = frontmatter.load(str(clean_ws / "identity" / "team" / "noor.md")).metadata
    assert stored["mood"] == "good"


@pytest.mark.unit
def test_validate_skips_reference_files(clean_ws: Path) -> None:
    """TS-INT-001-U05 (INT-003): no-frontmatter markdown is skipped, not counted."""
    report = validate(clean_ws)
    assert report.count == 5
    assert all(e.type != "" for e in report.errors)  # mission.md never produced an error


# --- validate: per-entity errors, collect-all -------------------------------


@pytest.fixture
def error_ws(clean_ws: Path, seed: Seed) -> Path:
    """Two distinct breaks: an off-enum stage and an unresolved owner relation."""
    seed(clean_ws, "opportunities/bad-stage/_index.md", type="opportunity",
         stage="vibing", client="initech", owner="noor", created="2026-06-01", updated="2026-06-01")
    seed(clean_ws, "projects/ghost-owner/_index.md", type="project",
         client="initech", owner="nobody", created="2026-06-01", updated="2026-06-01")
    return clean_ws


@pytest.mark.unit
def test_validate_enum_violation(error_ws: Path) -> None:
    """TS-INT-001-U01 (REQ-INT001-01, INT-001): an off-enum value is reported."""
    errs = {(e.id, e.field): e.reason for e in validate(error_ws).errors}
    assert ("opportunity/bad-stage", "stage") in errs
    assert "vibing" in errs[("opportunity/bad-stage", "stage")]


@pytest.mark.unit
def test_validate_referential_integrity(error_ws: Path) -> None:
    """TS-INT-001-U02 (REQ-INT001-02, INT-002): an unresolved relation is reported."""
    errs = {(e.id, e.field): e.reason for e in validate(error_ws).errors}
    assert ("project/ghost-owner", "owner") in errs
    assert "nobody" in errs[("project/ghost-owner", "owner")]


@pytest.mark.unit
def test_validate_collects_all_errors(error_ws: Path) -> None:
    """TS-INT-001-U06 (REQ-INT001-02): both broken entities surface, not just the first."""
    ids = {e.id for e in validate(error_ws).errors}
    assert {"opportunity/bad-stage", "project/ghost-owner"} <= ids


@pytest.mark.unit
def test_validate_number_type(fresh_ws: Path, seed: Seed) -> None:
    """TS-INT-001-U01 (type check): a word where a number belongs is reported."""
    entity.create(fresh_ws, "person", {"name": "W", "role": "consultant"}, id_="w")
    seed(fresh_ws, "fragments/f.md", type="fragment", stage="raw", owner="w",
         confidence="high", created="2026-06-01")
    errs = {(e.id, e.field) for e in validate(fresh_ws).errors}
    assert ("fragment/f", "confidence") in errs  # confidence is a number


@pytest.mark.integration
def test_cli_validate_reports_each_error(error_ws: Path, monkeypatch) -> None:
    """TS-INT-001-02 (AC-002): both errors reported with id/field; exit non-zero."""
    monkeypatch.chdir(error_ws)
    out = runner.invoke(app, ["validate", "--format", "json"])
    assert out.exit_code == 1
    data = json.loads(out.output)
    fields = {(e["id"], e["field"]) for e in data["errors"]}
    assert ("opportunity/bad-stage", "stage") in fields
    assert ("project/ghost-owner", "owner") in fields


@pytest.mark.e2e
def test_cli_validate_clean_message(clean_ws: Path, monkeypatch) -> None:
    """TS-INT-001-01 (AC-001): the located success line names N and exits 0."""
    monkeypatch.chdir(clean_ws)
    tty = runner.invoke(app, ["validate"], env={"FORCE_COLOR": "1"})
    assert tty.exit_code == 0
    assert "Validated 5 entities; 0 errors" in tty.output


# --- validate: strict --------------------------------------------------------


@pytest.fixture
def strict_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A lone client carrying an undeclared `vibe` extension."""
    seed(fresh_ws, "clients/initech.md", type="client", name="Initech",
         vibe="high", created="2026-06-01", updated="2026-06-01")
    return fresh_ws


@pytest.mark.unit
def test_validate_strict_rejects_undeclared(strict_ws: Path) -> None:
    """TS-INT-001-U04 (REQ-INT001-03): --strict rejects the undeclared key."""
    strict = validate(strict_ws, strict=True)
    assert not strict.ok
    assert any(e.field == "vibe" for e in strict.errors)


@pytest.mark.integration
def test_cli_validate_strict_vs_open(strict_ws: Path, monkeypatch) -> None:
    """TS-INT-001-03 (AC-003): --strict fails the same tree that passes open."""
    monkeypatch.chdir(strict_ws)
    closed = runner.invoke(app, ["validate", "--strict"])
    assert closed.exit_code == 1
    opened = runner.invoke(app, ["validate"])
    assert opened.exit_code == 0


# --- check: sound graph + false-but-legal ------------------------------------


@pytest.mark.e2e
def test_check_sound_graph(clean_ws: Path) -> None:
    """TS-INT-002-01 (AC-001): a sound graph passes — no gaps anywhere."""
    report = check(clean_ws)
    assert report.passed, report


@pytest.mark.integration
def test_cli_check_passed_message(clean_ws: Path, monkeypatch) -> None:
    """TS-INT-002-01 (AC-001): the located pass line, exit 0."""
    monkeypatch.chdir(clean_ws)
    tty = runner.invoke(app, ["check"], env={"FORCE_COLOR": "1"})
    assert tty.exit_code == 0 and "Graph check passed" in tty.output


@pytest.mark.e2e
def test_false_but_legal_passes_both_gates(clean_ws: Path) -> None:
    """INT-SHARED-003: a structurally legal but semantically free value passes both.

    `initech-deal` carries `stage: prospect` — a legal enum value the gate cannot
    know is semantically wrong. Both gates pass; git revert, not a gate, is the
    backstop for a false-but-legal write.
    """
    assert validate(clean_ws).ok
    assert check(clean_ws).passed


# --- check: completeness from the schema, not the flag -----------------------


@pytest.mark.unit
def test_check_active_incomplete_and_draft_exempt(clean_ws: Path) -> None:
    """TS-INT-002-U03/U04 (INT-004, INT-012): the flag and the verdict move apart.

    An active project missing required `owner` FAILS; a draft with the same gap is
    EXEMPT — proving completeness comes from the schema, not the `draft` flag.
    """
    entity.create(clean_ws, "project", {"client": "initech"}, id_="orphaned-pov")
    entity.create(clean_ws, "project", {"client": "initech"}, id_="draft-pov", draft=True)
    report = check(clean_ws)
    incomplete = {i.id: i for i in report.incomplete}
    assert "project/orphaned-pov" in incomplete
    assert incomplete["project/orphaned-pov"].missing_relations == ["owner"]
    assert "project/draft-pov" not in incomplete  # same gap, but a draft → exempt


@pytest.mark.integration
def test_cli_check_active_incomplete(clean_ws: Path, monkeypatch) -> None:
    """TS-INT-002-02 (AC-002): an active-but-incomplete entity is named; exit non-zero."""
    entity.create(clean_ws, "project", {"client": "initech"}, id_="orphaned-pov")
    monkeypatch.chdir(clean_ws)
    out = runner.invoke(app, ["check", "--format", "json"])
    assert out.exit_code == 1
    data = json.loads(out.output)
    inc = {i["id"]: i for i in data["incomplete"]}
    assert inc["project/orphaned-pov"]["missing_relations"] == ["owner"]


@pytest.mark.unit
def test_check_draft_does_not_satisfy(clean_ws: Path) -> None:
    """TS-INT-002-U05 (INT-005): an active entity whose required owner is a draft fails."""
    entity.create(clean_ws, "person", {"name": "New Hire", "role": "consultant"},
                  id_="newhire", draft=True)
    entity.create(clean_ws, "opportunity",
                  {"stage": "prospect", "client": "initech", "owner": "newhire"}, id_="deal2")
    report = check(clean_ws)
    incomplete = {i.id: i for i in report.incomplete}
    assert incomplete["opportunity/deal2"].missing_relations == ["owner"]
    assert "person/newhire" not in incomplete  # the draft itself is exempt


@pytest.mark.integration
def test_cli_check_draft_unsatisfied(clean_ws: Path, monkeypatch) -> None:
    """TS-INT-002-03 (AC-003): the active entity and unsatisfied predicate are named."""
    entity.create(clean_ws, "person", {"name": "New Hire", "role": "consultant"},
                  id_="newhire", draft=True)
    entity.create(clean_ws, "opportunity",
                  {"stage": "prospect", "client": "initech", "owner": "newhire"}, id_="deal2")
    monkeypatch.chdir(clean_ws)
    out = runner.invoke(app, ["check"], env={"FORCE_COLOR": "1"})
    assert out.exit_code == 1
    assert "opportunity/deal2" in out.output and "owner" in out.output


# --- check: orphans, dangling, strays ----------------------------------------


@pytest.fixture
def gaps_ws(clean_ws: Path) -> Path:
    """An orphan, a dangling edge (target force-removed), and a stray file."""
    entity.create(clean_ws, "person", {"name": "Orphan", "role": "consultant"}, id_="orphan-person")
    entity.delete(clean_ws, "client/initech", force=True)  # leaves initech-pov.client dangling
    (clean_ws / "clients" / "notes.md").write_text("loose notes, not an entity\n")  # stray
    return clean_ws


@pytest.mark.unit
def test_check_orphan_detection(gaps_ws: Path) -> None:
    """TS-INT-002-U01 (INT-006): an entity with zero edges is an orphan."""
    assert "person/orphan-person" in check(gaps_ws).orphans


@pytest.mark.unit
def test_check_dangling_detection(gaps_ws: Path) -> None:
    """TS-INT-002-U06 (REQ-INT002-04): a relation whose target was removed dangles."""
    dangling = {(d.id, d.predicate, d.target) for d in check(gaps_ws).dangling}
    assert ("project/initech-pov", "client", "initech") in dangling


@pytest.mark.unit
def test_check_stray_vs_skipped(gaps_ws: Path) -> None:
    """TS-INT-002-U07 (INT-011): a file inside the layout is stray; mission.md is not."""
    strays = check(gaps_ws).strays
    assert any(s.endswith("clients/notes.md") for s in strays)
    assert not any("mission.md" in s for s in strays)  # outside every layout → skipped


@pytest.mark.integration
def test_cli_check_reports_gaps(gaps_ws: Path, monkeypatch) -> None:
    """TS-INT-002-04 (AC-004): orphan, dangling, and stray all listed; exit non-zero."""
    monkeypatch.chdir(gaps_ws)
    out = runner.invoke(app, ["check", "--format", "json"])
    assert out.exit_code == 1
    data = json.loads(out.output)
    assert "person/orphan-person" in data["orphans"]
    assert any(s.endswith("clients/notes.md") for s in data["strays"])
    assert any(d["id"] == "project/initech-pov" for d in data["dangling"])


# --- check: edge cycle -------------------------------------------------------


@pytest.fixture
def cycle_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A `depends_on` cycle a -> b -> c -> a over the acyclic predicate."""
    entity.create(fresh_ws, "person", {"name": "Writer", "role": "consultant"}, id_="writer")
    for slug, dep in (("a", "b"), ("b", "c"), ("c", "a")):
        seed(fresh_ws, f"fragments/{slug}.md", type="fragment", stage="raw",
             owner="writer", depends_on=[dep], created="2026-06-01")
    return fresh_ws


@pytest.mark.unit
def test_check_cycle_guard(cycle_ws: Path) -> None:
    """TS-INT-002-U08 (REQ-INT002-04): a cycle terminates and returns the exact ids."""
    cycles = check(cycle_ws).cycles
    assert len(cycles) == 1
    assert set(cycles[0]) == {"fragment/a", "fragment/b", "fragment/c"}


@pytest.mark.integration
def test_cli_check_cycle(cycle_ws: Path, monkeypatch) -> None:
    """TS-INT-002-05 (AC-005): the cycle is reported with participating ids; exit non-zero.

    `nx.simple_cycles` is finite by construction, so this terminates instead of
    hanging on the cyclic graph (the visited-set guarantee, same as `impact`).
    """
    monkeypatch.chdir(cycle_ws)
    out = runner.invoke(app, ["check", "--format", "json"])
    assert out.exit_code == 1
    data = json.loads(out.output)
    assert len(data["cycles"]) == 1
    assert set(data["cycles"][0]) == {"fragment/a", "fragment/b", "fragment/c"}


# --- review-fix regressions --------------------------------------------------


@pytest.mark.unit
def test_validate_skips_strays(clean_ws: Path) -> None:
    """Review #4: a stray (wrong internal type) is not counted or validated as its layout type."""
    (clean_ws / "clients" / "boss.md").write_text("---\ntype: person\nname: Boss\n---\n")
    report = validate(clean_ws)
    assert report.count == 5  # the stray is excluded, like reference markdown
    assert not any(e.slug == "boss" for e in report.errors)


@pytest.mark.unit
def test_check_edge_to_stray_dangles(clean_ws: Path) -> None:
    """Review #3: an edge pointing at a stray dangles instead of silently resolving."""
    (clean_ws / "clients" / "initech.md").write_text("---\ntype: person\nname: Sys\n---\n")  # now a stray
    report = check(clean_ws)
    assert any(d.id == "project/initech-pov" and d.predicate == "client" for d in report.dangling)
    assert any(s.endswith("clients/initech.md") for s in report.strays)


@pytest.mark.unit
def test_validate_rejects_non_finite_number(fresh_ws: Path, seed: Seed) -> None:
    """Review #6: `inf`/`nan` are not valid numbers."""
    entity.create(fresh_ws, "person", {"name": "W", "role": "consultant"}, id_="w")
    seed(fresh_ws, "fragments/f.md", type="fragment", stage="raw", owner="w",
         confidence="inf", created="2026-01-01")
    errs = {(e.id, e.field) for e in validate(fresh_ws).errors}
    assert ("fragment/f", "confidence") in errs


@pytest.mark.unit
def test_validate_rejects_malformed_date(fresh_ws: Path, seed: Seed) -> None:
    """Review #10: a date with trailing junk is rejected (strict, not the lenient slice)."""
    seed(fresh_ws, "clients/c.md", type="client", name="C",
         created="2026-01-01 not a date", updated="2026-01-02")
    errs = {(e.id, e.field) for e in validate(fresh_ws).errors}
    assert ("client/c", "created") in errs


@pytest.mark.unit
def test_qualified_any_relation_resolves(clean_ws: Path) -> None:
    """Review #9: an `any` relation stored as qualified type/slug resolves, not dangles."""
    entity.create(clean_ws, "person", {"name": "W", "role": "consultant"}, id_="w")
    entity.create(clean_ws, "fragment",
                  {"stage": "raw", "owner": "w", "depends_on": "project/initech-pov"}, id_="frag")
    report = check(clean_ws)
    assert not any(d.id == "fragment/frag" for d in report.dangling)


@pytest.mark.unit
def test_validate_value_zero_not_skipped(fresh_ws: Path, seed: Seed) -> None:
    """Review #15: a relation value of integer 0 is checked, not skipped by truthiness."""
    entity.create(fresh_ws, "person", {"name": "W", "role": "consultant"}, id_="w")
    seed(fresh_ws, "fragments/f.md", type="fragment", stage="raw", owner="w",
         depends_on=[0], created="2026-01-01")
    errs = {(e.id, e.field) for e in validate(fresh_ws).errors}
    assert ("fragment/f", "depends_on") in errs  # 0 resolves to nothing → referential error


# --- check: orphan gate is strict-only (HQ-port finding) -----------------------


@pytest.fixture
def orphan_only_ws(clean_ws: Path) -> Path:
    """A sound graph plus one fully disconnected entity (a dormant client)."""
    entity.create(clean_ws, "client", {"name": "Dormant Co"}, id_="dormant-co")
    return clean_ws


@pytest.mark.unit
def test_orphans_informational_by_default(orphan_only_ws: Path) -> None:
    """A disconnected entity is reported but does not fail the default gate."""
    report = check(orphan_only_ws)
    assert "client/dormant-co" in report.orphans
    assert report.passed


@pytest.mark.unit
def test_orphans_fail_under_strict(orphan_only_ws: Path) -> None:
    """--strict makes a fully connected graph a gate requirement."""
    report = check(orphan_only_ws, strict=True)
    assert "client/dormant-co" in report.orphans
    assert not report.passed


@pytest.mark.integration
def test_cli_check_strict_gates_orphans(orphan_only_ws: Path, monkeypatch) -> None:
    """Default exit 0 with the orphan listed; --strict exits 1 on the same tree."""
    monkeypatch.chdir(orphan_only_ws)
    ok = runner.invoke(app, ["check", "--format", "json"])
    assert ok.exit_code == 0
    assert "client/dormant-co" in json.loads(ok.output)["orphans"]
    strict = runner.invoke(app, ["check", "--strict", "--format", "json"])
    assert strict.exit_code == 1


# --- acyclic predicates beyond depends_on (0.11.0) -----------------------------


@pytest.mark.unit
def test_supersedes_cycle_is_reported(tmp_path: Path) -> None:
    """Cycle detection was hardcoded to `depends_on`, so three ADRs each superseding
    the next passed clean — every one reported current, `history` answering
    differently per entry point. build-hub marks `supersedes` acyclic."""
    from khub.core import entity
    from khub.core.workspace import init_workspace

    ws = tmp_path / "ws"
    init_workspace("build-hub", ws)
    for i in (1, 2, 3):
        entity.create(ws, "adr", {"title": f"A{i}", "status": "accepted"}, id_=f"ad-{i}")
    entity.link(ws, "ad-3", "supersedes", "ad-2")
    entity.link(ws, "ad-2", "supersedes", "ad-1")
    entity.link(ws, "ad-1", "supersedes", "ad-3")  # closes the cycle

    report = check(ws)
    assert not report.passed
    assert len(report.cycles) == 1
    assert set(report.cycles[0]) == {"adr/ad-1", "adr/ad-2", "adr/ad-3"}


@pytest.mark.unit
def test_self_supersession_is_reported(tmp_path: Path, seed: Seed) -> None:
    """`link` refuses a self-edge, but a hand-edit or an import can still write one,
    and `build_graph` drops self-edges — so `_self_cycles` must cover every acyclic
    predicate, not just depends_on."""
    from khub.core.workspace import init_workspace

    ws = tmp_path / "ws"
    init_workspace("build-hub", ws)
    seed(ws, "knowledge/architecture/decisions/solo.md", type="adr", title="Solo",
         status="accepted", supersedes="solo", created="2026-01-01")

    report = check(ws)
    assert ["adr/solo"] in report.cycles


@pytest.mark.unit
def test_depends_on_cycles_still_reported(tmp_path: Path) -> None:
    """Regression guard: generalising the check must not lose the original predicate."""
    from khub.core import entity
    from khub.core.workspace import init_workspace

    ws = tmp_path / "ws"
    init_workspace("build-hub", ws)
    for name in ("alpha", "beta", "gamma"):
        entity.create(ws, "domain", {"title": name}, id_=name)
    entity.link(ws, "alpha", "depends_on", "beta")
    entity.link(ws, "beta", "depends_on", "gamma")
    entity.link(ws, "gamma", "depends_on", "alpha")

    assert any(len(c) == 3 for c in check(ws).cycles)


@pytest.mark.unit
def test_draft_required_singleton_is_reported_missing(tmp_path: Path) -> None:
    """The two `required` gates disagreed about `draft`: a draft target already fails
    to satisfy another entity's required relation, but a draft REQUIRED SINGLETON
    passed clean — so an unpublished PRD turned the whole gate green."""
    from khub.core import entity
    from khub.core.workspace import init_workspace

    ws = tmp_path / "ws"
    init_workspace("build-hub", ws)
    assert check(ws).missing_singletons == []  # published: satisfied

    entity.update(ws, "prd", {"draft": "true"})
    report = check(ws)
    assert report.missing_singletons == ["prd"]
    assert not report.passed


@pytest.mark.unit
def test_empty_string_fails_validate_like_check_treats_it(fresh_ws: Path, seed: Seed) -> None:
    """khub's rule: null means absent (passes validate, `check` reports it); '' is
    malformed. `check` already counted '' as missing and the write path already
    rejected it for an enum, but validate let it through for a text field — so the
    two gates disagreed about the same byte."""
    seed(fresh_ws, "clients/blank.md", type="client", name="", created="2026-01-01")
    seed(fresh_ws, "clients/absent.md", type="client", created="2026-01-01")  # null/omitted

    errors = {(e.id, e.field) for e in validate(fresh_ws).errors}
    assert ("client/blank", "name") in errors
    assert ("client/absent", "name") not in errors  # absent is a completeness concern

    incomplete = {i.id for i in check(fresh_ws).incomplete}
    assert {"client/blank", "client/absent"} <= incomplete  # check reports both


@pytest.mark.unit
def test_depends_on_stays_acyclic_without_the_schema_flag(tmp_path: Path) -> None:
    """schema.yaml is copied at init and owned by the workspace, so a workspace made
    before `acyclic:` existed carries no such key. Keying cycle detection purely off
    the schema would have switched it off for every one of them."""
    from khub.core import entity
    from khub.core.workspace import init_workspace

    ws = tmp_path / "ws"
    init_workspace("build-hub", ws)
    schema = ws / ".khub" / "schema.yaml"
    schema.write_text(schema.read_text().replace(", acyclic: true", "").replace("acyclic: true", ""))
    assert "acyclic" not in schema.read_text()  # a pre-0.11 workspace

    for name in ("alpha", "beta", "gamma"):
        entity.create(ws, "domain", {"title": name}, id_=name)
    entity.link(ws, "alpha", "depends_on", "beta")
    entity.link(ws, "beta", "depends_on", "gamma")
    entity.link(ws, "gamma", "depends_on", "alpha")

    assert any(len(c) == 3 for c in check(ws).cycles)


@pytest.mark.unit
def test_draft_singleton_is_distinguished_from_an_absent_one(tmp_path: Path) -> None:
    """"missing" sends a user hunting for a file that is sitting right there."""
    from khub.core import entity
    from khub.core.workspace import init_workspace

    ws = tmp_path / "ws"
    init_workspace("build-hub", ws)
    entity.update(ws, "prd", {"draft": "true"})
    report = check(ws)
    assert report.missing_singletons == ["prd"] and report.draft_singletons == ["prd"]

    entity.update(ws, "prd", {"draft": "false"})
    entity.delete(ws, "prd")
    absent = check(ws)
    assert absent.missing_singletons == ["prd"] and absent.draft_singletons == []
