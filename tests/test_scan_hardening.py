"""Audit hardening — scan robustness, gate correctness, and the latent-fix regressions.

One malformed file must not brick every command; a corrupt/missing schema is a located
error, not a traceback; validate rejects a no-match target and flags an ambiguous stored
slug; the draft flag reads via ``as_bool``; strays leave the query/status counts; a
self-loop is a cycle; the graph walks reject an undeclared predicate. Plus the LinkML-emit
and resolver latent fixes and the reindex link-escaping.
"""

from __future__ import annotations

from datetime import date
from pathlib import Path
from typing import Callable

import pytest

from khub.core import resolve
from khub.core.errors import LocatedError
from khub.core.gitlog import stale
from khub.core.graph import walk_history, walk_impact, walk_neighbors
from khub.core.integrity import check, validate
from khub.core.introspect import load_schema
from khub.core.linkml_emit import to_linkml_dict
from khub.core.project import project
from khub.core.query import QueryFilters, query
from khub.core.reindex import _entity_link

Seed = Callable[..., None]
NOW = date(2026, 6, 27)
RECENT = {"created": date(2026, 6, 1), "updated": date(2026, 6, 1)}

# A file whose frontmatter cannot be parsed (unterminated quote + unclosed flow seq).
MALFORMED_MD = '---\ntype: client\nname: "unterminated\nbad: [1, 2\n---\nbody\n'


# --- Fix #1: a malformed file becomes an entry, never a raised ParserError -----


@pytest.fixture
def malformed_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A sound client beside a malformed client file inside the same layout."""
    seed(fresh_ws, "clients/real.md", type="client", name="Real", **RECENT)
    (fresh_ws / "clients" / "broken.md").write_text(MALFORMED_MD)
    return fresh_ws


@pytest.mark.unit
def test_malformed_file_does_not_crash_scans(malformed_ws: Path) -> None:
    """None of the read/gate verbs raise on one unparseable file."""
    # each of these previously crashed with a raw ParserError
    validate(malformed_ws)
    check(malformed_ws)
    project(malformed_ws, stale_days=90, now=NOW)
    query(malformed_ws, QueryFilters(), now=NOW)


@pytest.mark.unit
def test_validate_reports_malformed_as_frontmatter_error(malformed_ws: Path) -> None:
    """validate surfaces the bad file as a `frontmatter` error and is not clean."""
    report = validate(malformed_ws)
    assert not report.ok
    assert any(e.field == "frontmatter" for e in report.errors)


@pytest.mark.unit
def test_check_reports_malformed_and_fails(malformed_ws: Path) -> None:
    """check lists the malformed path and fails the gate."""
    report = check(malformed_ws)
    assert any(m.endswith("clients/broken.md") for m in report.malformed)
    assert not report.passed


@pytest.mark.unit
def test_project_counts_malformed_separately(malformed_ws: Path) -> None:
    """status counts the malformed file apart from the real entity count."""
    proj = project(malformed_ws, stale_days=90, now=NOW)
    assert proj.malformed == 1
    assert proj.counts["client"] == 1  # only the real client, never the broken file


# --- Fix #2: corrupt / missing schema.yaml is a located error ------------------


@pytest.mark.unit
def test_corrupt_schema_is_located_error(fresh_ws: Path) -> None:
    """A syntactically broken schema.yaml raises a `schema_error`, not a ParserError."""
    (fresh_ws / ".khub" / "schema.yaml").write_text("entities: [unterminated\n")
    with pytest.raises(LocatedError) as err:
        load_schema(fresh_ws)
    assert err.value.code == "schema_error"
    assert "schema.yaml" in err.value.message


@pytest.mark.unit
def test_missing_schema_is_located_error(fresh_ws: Path) -> None:
    """A missing schema.yaml raises a `schema_error`, not a FileNotFoundError."""
    (fresh_ws / ".khub" / "schema.yaml").unlink()
    with pytest.raises(LocatedError) as err:
        load_schema(fresh_ws)
    assert err.value.code == "schema_error"


# --- Fix #3: a validate target selecting nothing is a typo (unless a bare type) -


@pytest.mark.unit
def test_validate_bare_declared_type_zero_entities_is_ok(fresh_ws: Path) -> None:
    """A declared type with zero entities is a legitimate count-0 run, not an error."""
    report = validate(fresh_ws, "meeting")  # firm-ops declares meeting; none seeded
    assert report.ok and report.count == 0


@pytest.mark.unit
def test_validate_unknown_target_raises(fresh_ws: Path) -> None:
    """A target matching neither a type nor an entity is a located error."""
    with pytest.raises(LocatedError) as err:
        validate(fresh_ws, "nonsense")
    assert err.value.code == "validate_target"


@pytest.mark.unit
def test_validate_missing_qualified_target_raises(fresh_ws: Path) -> None:
    """A type/slug selecting no entity raises rather than a false-clean pass."""
    with pytest.raises(LocatedError) as err:
        validate(fresh_ws, "client/ghost")
    assert err.value.code == "validate_target"


# --- Fix #4: a stored bare slug resolving to >1 node is flagged ambiguous -------


@pytest.mark.unit
def test_validate_flags_ambiguous_stored_bare_slug(fresh_ws: Path, seed: Seed) -> None:
    """A bare relation value resolving to two nodes is reported (qualify as type/slug)."""
    seed(fresh_ws, "clients/dup.md", type="client", name="Dup Co", **RECENT)
    seed(fresh_ws, "identity/team/dup.md", type="person", name="Dup Person",
         role="consultant", created=date(2026, 6, 1))
    seed(fresh_ws, "clients/refs.md", type="client", name="Refs",
         depends_on=["dup"], **RECENT)  # `dup` is both a client and a person
    errs = {(e.id, e.field): e.reason for e in validate(fresh_ws).errors}
    assert ("client/refs", "depends_on") in errs
    assert "ambiguous" in errs[("client/refs", "depends_on")]


# --- Fix #5: draft reads via as_bool, not truthiness ---------------------------


@pytest.fixture
def draft_string_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A `draft: "false"` (published) and a `draft: yes` (draft) project, both gapped."""
    seed(fresh_ws, "clients/c.md", type="client", name="C", **RECENT)
    seed(fresh_ws, "projects/pub/_index.md", type="project", client="c",
         draft="false", **RECENT)  # a string "false" is PUBLISHED, not a draft
    seed(fresh_ws, "projects/hidden/_index.md", type="project", client="c",
         draft="yes", **RECENT)    # a string "yes" is a draft
    return fresh_ws


@pytest.mark.unit
def test_draft_string_false_is_published(draft_string_ws: Path) -> None:
    """`draft: "false"` is not exempt from completeness; `draft: yes` is."""
    incomplete = {i.id for i in check(draft_string_ws).incomplete}
    assert "project/pub" in incomplete       # published → its missing owner surfaces
    assert "project/hidden" not in incomplete  # a draft → exempt


@pytest.mark.unit
def test_draft_string_counts_as_active(draft_string_ws: Path) -> None:
    """The projection reads `draft: "false"` as active, `draft: yes` as draft."""
    proj = project(draft_string_ws, stale_days=90, now=NOW)
    # c + pub are active; hidden is the one draft
    assert proj.draft == 1
    active = {m.slug for m in query(draft_string_ws, QueryFilters(active_only=True), now=NOW)}
    assert "pub" in active and "hidden" not in active


# --- Fix #6: a non-md format is rejected at schema-validation time --------------


@pytest.mark.unit
def test_non_md_format_rejected(write_schema, core_base) -> None:
    """A type declaring a format other than `md` is a located schema error."""
    preset = "entities:\n  thing:\n    layout: file\n    format: html\n"
    with pytest.raises(LocatedError) as err:
        resolve(write_schema(core=core_base, preset=preset))
    assert "md" in err.value.message


# --- Fix #7: a None stale threshold resolves to the workspace stale_days --------


@pytest.mark.unit
def test_stale_none_threshold_uses_workspace_default(fresh_ws: Path, seed: Seed) -> None:
    """`stale(days=None)` uses the workspace stale_days (90), not a private 30-day default."""
    seed(fresh_ws, "fragments/midnote.md", type="fragment", stage="raw", updated="2026-05-01")
    default = {e.slug for e in stale(fresh_ws, now=NOW).entries}  # None → 90-day window
    assert "midnote" not in default  # 57 days old, within the workspace 90-day window
    narrow = {e.slug for e in stale(fresh_ws, days=30, now=NOW).entries}
    assert "midnote" in narrow  # but past a 30-day window


# --- Fix #8: strays leave the query/status counts, reported as their own total --


@pytest.fixture
def stray_ws(fresh_ws: Path, seed: Seed) -> Path:
    """A real client beside a stray (a `person`-typed file inside the client layout)."""
    seed(fresh_ws, "clients/real.md", type="client", name="Real", **RECENT)
    seed(fresh_ws, "clients/imposter.md", type="person", name="Imposter",
         role="consultant", created=date(2026, 6, 1))
    return fresh_ws


@pytest.mark.unit
def test_query_excludes_strays(stray_ws: Path) -> None:
    """query filters strays — the imposter never appears as a client."""
    slugs = {m.slug for m in query(stray_ws, QueryFilters(type="client"), now=NOW)}
    assert slugs == {"real"}


@pytest.mark.unit
def test_status_reports_stray_count(stray_ws: Path) -> None:
    """status excludes the stray from the type count and reports a stray total."""
    proj = project(stray_ws, stale_days=90, now=NOW)
    assert proj.stray == 1
    assert proj.counts["client"] == 1  # the stray is not a client


# --- Fix #9: a stored self-reference on the acyclic predicate is a cycle --------


@pytest.mark.unit
def test_self_loop_is_a_cycle(fresh_ws: Path, seed: Seed) -> None:
    """A `depends_on` pointing at the entity itself is reported as a one-node cycle."""
    seed(fresh_ws, "clients/selfie.md", type="client", name="Selfie",
         depends_on=["selfie"], **RECENT)
    assert ["client/selfie"] in check(fresh_ws).cycles


# --- Fix #10: reindex escapes a link target with space/parens ------------------


@pytest.mark.unit
def test_reindex_wraps_problematic_link_path(tmp_path: Path, write_schema, core_base) -> None:
    """A path with a space is wrapped in <...>; a clean path is left bare."""
    preset = (
        "entities:\n"
        "  doc:\n    layout: file\n    path: 'my docs'\n"
        "  note:\n    layout: file\n    path: clean\n"
    )
    schema = resolve(write_schema(core=core_base, preset=preset))
    assert _entity_link(tmp_path, schema, "doc", "foo") == "<my docs/foo.md>"
    assert _entity_link(tmp_path, schema, "note", "foo") == "clean/foo.md"


# --- Fix #11: an overridden base relation is emitted via slot_usage -------------


@pytest.mark.unit
def test_overridden_base_relation_emitted(write_schema, core_base) -> None:
    """A type narrowing a base (any/many) edge emits the override, not the base default."""
    preset = (
        "entities:\n"
        "  person: { layout: file }\n"
        "  team:\n    layout: file\n    relations:\n"
        "      related: { to: person, many: false }\n"
    )
    schema = resolve(write_schema(core=core_base, preset=preset))
    team = to_linkml_dict(schema)["classes"]["Team"]
    assert "related" in team["slot_usage"]              # the override is emitted
    assert team["slot_usage"]["related"]["multivalued"] is False  # base many:true overridden
    assert "related" not in team.get("attributes", {})  # not re-listed as a new slot


# --- Fix #12: an attribute override inherits base pattern/enum/default ----------


@pytest.mark.unit
def test_attribute_override_inherits_base_constraints(write_schema) -> None:
    """An override that only tightens `required` keeps the base pattern/enum/default."""
    custom_core = (
        "base:\n  attributes:\n"
        "    type:    { type: text, required: true }\n"
        "    created: { type: date, required: true }\n"
        "    code:    { type: text, pattern: '^[A-Z]+$' }\n"
        "    tier:    { enum: [gold, silver] }\n"
        "    draft:   { type: bool, default: false }\n"
    )
    preset = (
        "entities:\n  thing:\n    layout: file\n    attributes:\n"
        "      code:  { required: true }\n"
        "      tier:  { required: true }\n"
        "      draft: { required: true }\n"
    )
    thing = resolve(write_schema(core=custom_core, preset=preset)).types["thing"]
    assert thing.attributes["code"].pattern == "^[A-Z]+$"       # inherited pattern
    assert thing.attributes["tier"].enum == ("gold", "silver")  # inherited enum
    assert thing.attributes["draft"].default is False           # inherited default


# --- Fix #15: the walks reject an undeclared predicate -------------------------


@pytest.mark.unit
def test_history_default_predicate_errors_on_firm_ops(fresh_ws: Path) -> None:
    """`history` (default `supersedes`) errors cleanly on a preset without that edge."""
    with pytest.raises(LocatedError) as err:
        walk_history(fresh_ws, "ghost")  # firm-ops declares no supersedes
    assert err.value.code == "unknown_predicate"


@pytest.mark.unit
def test_neighbors_unknown_predicate_errors(fresh_ws: Path, seed: Seed) -> None:
    """A named --predicate no type declares is a located error."""
    seed(fresh_ws, "clients/c.md", type="client", name="C", **RECENT)
    with pytest.raises(LocatedError) as err:
        walk_neighbors(fresh_ws, "c", predicate="bogus")
    assert err.value.code == "unknown_predicate"


@pytest.mark.unit
def test_neighbors_no_predicate_filter_stays_valid(fresh_ws: Path, seed: Seed) -> None:
    """`neighbors` with no predicate (None) is unfiltered and never validates a name."""
    seed(fresh_ws, "clients/c.md", type="client", name="C", **RECENT)
    assert walk_neighbors(fresh_ws, "c") == []  # edge-less, but no predicate error
    # a declared/default predicate (depends_on is universal) is accepted too
    assert walk_impact(fresh_ws, "c")[0].slug == "c"


# --- review-round fixes (code-review findings on the audit batch) --------------


@pytest.mark.unit
def test_partial_override_keeps_base_required(write_schema) -> None:
    """An override redeclaring only pattern/enum inherits the base's required flag."""
    custom_core = (
        "base:\n  attributes:\n"
        "    type:  { type: text, required: true }\n"
        "    title: { type: text, required: true, pattern: '^[A-Z].*' }\n"
    )
    preset = (
        "entities:\n  thing:\n    layout: file\n    attributes:\n"
        "      title: { pattern: '^[a-z].*' }\n"
    )
    thing = resolve(write_schema(core=custom_core, preset=preset)).types["thing"]
    assert thing.attributes["title"].required is True   # inherited, not reset to False
    assert thing.attributes["title"].pattern == "^[a-z].*"
    # An explicit `required: false` still wins over the base.
    preset_off = (
        "entities:\n  thing:\n    layout: file\n    attributes:\n"
        "      title: { required: false }\n"
    )
    thing = resolve(write_schema(core2=custom_core, preset_off=preset_off)).types["thing"]
    assert thing.attributes["title"].required is False


@pytest.mark.unit
def test_stray_breaks_okf_conformance(stray_ws: Path) -> None:
    """A stray (or malformed) file means the tree would not export cleanly — not conformant."""
    proj = project(stray_ws, stale_days=90, now=NOW)
    assert proj.stray > 0
    assert proj.okf_conformant is False


@pytest.mark.unit
def test_walks_exclude_strays(fresh_ws: Path, seed: Seed) -> None:
    """The graph walks see the same stray-filtered index as every other verb."""
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor", role="partner", **RECENT)
    seed(fresh_ws, "fragments/frag.md", type="fragment", stage="raw", owner="noor",
         related=["bob"], **RECENT)
    # bob is a stray: a person-typed file inside the client layout.
    seed(fresh_ws, "clients/bob.md", type="person", name="Bob", **RECENT)
    hits = walk_neighbors(fresh_ws, "frag")
    assert all(n.slug != "bob" for n in hits)
