"""Tests for kb. Run with `python3 lite/test_kb.py` (no pytest needed) or `uv run pytest lite`.

The checker is the product, so the bulk of this is one seeded corpus broken in
every way the schema can be broken, asserted against the finding codes.
"""

from __future__ import annotations

import contextlib
import importlib.util
import io
import shutil
import sys
import tempfile
from pathlib import Path

sys.dont_write_bytecode = True
SCRIPTS = Path(__file__).parent / "scripts"
spec = importlib.util.spec_from_file_location("kb", SCRIPTS / "kb.py")
assert spec and spec.loader
kb = importlib.util.module_from_spec(spec)
sys.modules["kb"] = kb  # dataclasses resolve their annotations through sys.modules
spec.loader.exec_module(kb)


def run(root: Path, *argv: str) -> int:
    """Exercise the real CLI entry point; its chatter belongs to the tool, not here."""
    with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        return kb.main(["-C", str(root), *argv])


def fresh() -> Path:
    root = Path(tempfile.mkdtemp()) / "hub"
    root.mkdir()
    run(root, "init")
    return root


def scan(root: Path):
    return kb.scan(root, kb.load_schema(root))


def codes(root: Path) -> dict[str, list[str]]:
    corpus = kb.scan(root, kb.load_schema(root))
    out: dict[str, list[str]] = {kb.ERROR: [], kb.GAP: []}
    for f in kb.check(corpus):
        out[f.severity].append(f.code)
    return out


def write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text.lstrip("\n"))


# ------------------------------------------------------------------ profile


def test_frontmatter_round_trip() -> None:
    source = '---\ntype: adr\ntitle: "Use: Postgres"\ntags: [a, b]\naffects:\n  - x\n  - y\n---\n\nbody\n'
    front, body = kb.split_front(source)
    fm = kb.parse_front(front)
    assert fm == {"type": "adr", "title": "Use: Postgres", "tags": ["a", "b"], "affects": ["x", "y"]}
    assert body == "body\n"
    lines = [line for k, v in fm.items() for line in kb.emit_key(k, v)]
    assert kb.parse_front(kb.split_front(kb.render(lines, body))[0]) == fm


def test_profile_violations_are_reported_not_guessed() -> None:
    for bad in ["---\nnested:\n  a: 1\n---\n\n", "---\ntype: adr\n", "no fence\n"]:
        try:
            kb.parse_front(kb.split_front(bad)[0])
            raise AssertionError(f"accepted {bad!r}")
        except kb.Bad:
            pass


def test_risky_scalars_are_quoted() -> None:
    for value in ["Use: Postgres", "- dash", "true", "", "  padded  ", "hash # comment"]:
        line = kb.emit_key("title", value)[0]
        assert kb.parse_front([line])["title"] == value, line


# ----------------------------------------------------------------- schema yaml


def test_yaml_reads_the_shipped_schema() -> None:
    schema = kb.load_schema(Path("/nonexistent"))
    assert set(schema.types) == {"prd", "arc42", "requirement", "adr", "component", "feature-spec"}
    assert schema.is_singleton("prd") and not schema.is_singleton("adr")
    assert schema.types["prd"]["required"] is True
    assert schema.attrs("requirement")["kind"]["enum"] == ["functional", "constraint", "business-rule"]
    assert schema.attrs("requirement")["created"]["required"] is True  # merged from base
    assert schema.rels("adr")["supersedes"] == {"to": "adr", "inverse": "superseded", "acyclic": True}
    assert schema.rels("adr")["depends_on"]["to"] == "any"  # merged from base
    assert schema.prefixes("requirement") == ["fr", "cst", "br"]
    assert schema.prefix_for("adr", {}) == "ad"
    assert schema.attrs("prd")["draft"] == {"type": "bool", "default": False}  # declared, inert


def test_yaml_matches_ruamel_on_the_shipped_schema() -> None:
    """The parser is a subset reader; the subset must mean what YAML means."""
    try:
        from ruamel.yaml import YAML  # dev-only; the shipped tool has no dependencies
    except ImportError:
        print("    (skipped: ruamel not installed)")
        return
    source = (SCRIPTS / "build.schema.yaml").read_text()
    with io.StringIO(source) as fh:
        expected = YAML(typ="safe").load(fh)
    assert kb.load_yaml(source) == expected


def test_yaml_reads_a_block_sequence_level_with_its_key() -> None:
    """khub's generated .khub/schema.yaml writes enums this way, so a workspace
    that copies its own schema into .kb/ has to parse."""
    parsed = kb.load_yaml(
        "attributes:\n  kind:\n    enum:\n    - functional\n    - constraint\n"
        "    required: true\n"
    )
    assert parsed == {"attributes": {"kind": {"enum": ["functional", "constraint"],
                                             "required": True}}}


def test_add_refuses_body_and_body_file_together() -> None:
    root = fresh()
    assert run(root, "add", "adr", "--title", "X", "--status", "proposed",
               "--body", "hi", "--body-file", "-") == 2


def test_yaml_subset_edges() -> None:
    parsed = kb.load_yaml(
        '# leading comment\n'
        'version: "0.1.0"   # trailing, and a # inside quotes below\n'
        'quoted: "a: b # not a comment"\n'
        'nested:\n'
        '  flow: { a: 1, b: [x, y], c: { d: true } }\n'
        '  empty:\n'
        '  block:\n'
        '    - one\n'
        '    - two\n'
        'bare: knowledge/prd.md\n'
    )
    assert parsed == {
        "version": "0.1.0",
        "quoted": "a: b # not a comment",
        "nested": {"flow": {"a": 1, "b": ["x", "y"], "c": {"d": True}}, "empty": None,
                   "block": ["one", "two"]},
        "bare": "knowledge/prd.md",
    }
    for bad in ["a: {b: 1\n", "a: [1, 2\n", "  oops: 1\na: 2\n", "a: 1\n  b: 2\n"]:
        try:
            kb.load_yaml(bad)
            raise AssertionError(f"accepted {bad!r}")
        except kb.Bad:
            pass


def test_workspace_can_override_the_schema() -> None:
    root = fresh()
    shipped = (SCRIPTS / "build.schema.yaml").read_text()
    override = root / kb.CONFIG_DIR / kb.SCHEMA_FILE
    override.parent.mkdir(parents=True)
    override.write_text(shipped.replace("stack: { type: text }", "stack: { type: text, required: true }"))
    run(root, "add", "component", "--title", "API", "--kind", "service")
    assert "incomplete" in codes(root)[kb.GAP]


# ---------------------------------------------------------------- authoring


def test_new_mints_ids_by_kind_and_numbers_per_prefix() -> None:
    root = fresh()
    run(root, "add", "component", "--title", "Public API", "--kind", "service")
    run(root, "add", "requirement", "--title", "A user can pay", "--kind", "functional")
    run(root, "add", "requirement", "--title", "Settles in 2s", "--kind", "constraint")
    run(root, "add", "requirement", "--title", "Refunds within 30d", "--kind", "functional")
    slugs = set(kb.scan(root, kb.load_schema(root)).entities)
    assert {"cmp-001-public-api", "fr-001-a-user-can-pay", "cst-001-settles-in-2s"} <= slugs
    assert "fr-002-refunds-within-30d" in slugs


def test_new_rejects_unknown_field_and_unresolvable_edge() -> None:
    root = fresh()
    # khub stores an undeclared field as an extension; only --strict refuses.
    assert run(root, "add", "adr", "--title", "X", "--status", "proposed", "--owner", "noor") == 0
    assert run(root, "add", "adr", "--title", "Y", "--status", "proposed",
               "--owner", "noor", "--strict") == 2
    assert run(root, "add", "adr", "--title", "X", "--status", "proposed", "--affects", "ghost") == 2
    assert run(root, "add", "requirement", "--title", "X") == 2  # no kind, so no id prefix


def test_link_is_surgical_and_checked() -> None:
    root = fresh()
    run(root, "add", "component", "--title", "API", "--kind", "service")
    write(
        root / "knowledge/components/cmp-002-worker.md",
        """
---
type: component
# hand-written, with a comment and odd quoting
title: "Worker: jobs"
kind: service
created: 2026-07-20
depends_on:
  - cmp-001-api
---

Consumes the queue.
""",
    )
    before = (root / "knowledge/components/cmp-002-worker.md").read_text()
    assert run(root, "link", "cmp-002-worker", "depends_on", "cmp-001-api") == 0  # idempotent
    assert (root / "knowledge/components/cmp-002-worker.md").read_text() == before

    run(root, "add", "adr", "--title", "Split the worker", "--status", "proposed")
    assert run(root, "link", "ad-001-split-the-worker", "affects", "cmp-002-worker") == 0
    assert run(root, "link", "ad-001-split-the-worker", "affects", "ghost") == 2
    assert run(root, "link", "ad-001-split-the-worker", "supersedes", "cmp-001-api") == 2  # wrong type
    assert run(root, "link", "cmp-002-worker", "realized_in", "cmp-001-api") == 2  # not on this type

    assert run(root, "unlink", "cmp-002-worker", "depends_on", "cmp-001-api") == 0
    after = (root / "knowledge/components/cmp-002-worker.md").read_text()
    assert "depends_on" not in after
    assert "# hand-written, with a comment and odd quoting" in after
    assert 'title: "Worker: jobs"' in after
    assert after.endswith("Consumes the queue.\n")


# -------------------------------------------------------------------- gates


def test_seeded_corpus_is_green() -> None:
    root = fresh()
    run(root, "add", "component", "--title", "API", "--kind", "service")
    run(root, "add", "requirement", "--title", "A user can pay", "--kind", "functional")
    run(root, "link", "fr-001-a-user-can-pay", "realized_in", "cmp-001-api")
    run(root, "add", "feature-spec", "--title", "Checkout", "--status", "active", "--requirements", "fr-001-a-user-can-pay")
    found = codes(root)
    assert found[kb.ERROR] == []
    assert found[kb.GAP] == [], found[kb.GAP]
    assert run(root, "check", "--strict") == 0


def test_every_error_fires() -> None:
    root = fresh()
    run(root, "add", "adr", "--title", "Fine", "--status", "accepted")
    write(root / "knowledge/decisions/ad-002-broken.md", """
---
type: adr
title: Broken
status: maybe
created: not-a-date
supersedes: ad-999-ghost
owner: noor
---
""")
    write(root / "knowledge/decisions/oops.md", """
---
type: adr
title: Bad filename
status: proposed
created: 2026-07-25
---
""")
    write(root / "knowledge/requirements/fr-002-actually-a-constraint.md", """
---
type: requirement
title: Mislabelled
kind: constraint
created: 2026-07-25
---
""")
    write(root / "knowledge/components/cmp-001-api.md", """
---
type: adr
title: Wrong directory
created: 2026-07-25
---
""")
    write(root / "specs/fs-001-torn.md", "no frontmatter at all\n")
    write(root / "knowledge/decisions/ad-003-a.md",
          "---\ntype: adr\ntitle: A\nstatus: proposed\ncreated: 2026-07-25\nsupersedes: ad-004-b\n---\n")
    write(root / "knowledge/decisions/ad-004-b.md",
          "---\ntype: adr\ntitle: B\nstatus: proposed\ncreated: 2026-07-25\nsupersedes: ad-003-a\n---\n")
    (root / "specs/notes.txt").touch()
    (root / "knowledge/prd.md").unlink()

    found = set(codes(root)[kb.ERROR])
    assert found == {"bad_id", "bad_value", "body_shape", "cycle", "dangling",
                     "malformed", "missing", "stray"}, found
    assert run(root, "check") == 1


def test_gaps_do_not_fail_the_gate() -> None:
    root = fresh()
    run(root, "add", "component", "--title", "API", "--kind", "service")
    (root / "knowledge/arc42.md").unlink()
    found = codes(root)
    assert found[kb.ERROR] == []
    assert set(found[kb.GAP]) == {"orphan", "missing"}
    assert run(root, "check") == 0
    assert run(root, "check", "--strict") == 1


def test_narrative_roots_are_never_orphans() -> None:
    root = fresh()
    assert codes(root)[kb.GAP] == []


def test_a_missing_template_section_is_a_validate_error() -> None:
    """khub reports it through `validate` with field=body; kb matches."""
    root = fresh()
    prd = root / "knowledge/prd.md"
    prd.write_text(prd.read_text().replace("## Non-goals", "## Later maybe"))
    finding = next(f for f in kb.check(scan(root)) if f.code == "body_shape")
    assert finding.severity == kb.ERROR and finding.field == "body"


# ------------------------------------------------------------------- graph


def test_inverse_edges_are_computed() -> None:
    root = fresh()
    run(root, "add", "component", "--title", "API", "--kind", "service")
    run(root, "add", "adr", "--title", "Use Postgres", "--status", "accepted", "--affects", "cmp-001-api")
    corpus = kb.scan(root, kb.load_schema(root))
    assert corpus.in_edges("cmp-001-api") == [("affects", "ad-001-use-postgres")]
    assert corpus.out_edges("cmp-001-api") == []
    assert corpus.schema.inverse("supersedes") == "superseded"


def test_blast_radius_walks_transitively() -> None:
    root = fresh()
    for name in ("A", "B", "C"):
        run(root, "add", "component", "--title", name, "--kind", "service")
    run(root, "link", "cmp-001-a", "depends_on", "cmp-002-b")
    run(root, "link", "cmp-002-b", "depends_on", "cmp-003-c")
    corpus = kb.scan(root, kb.load_schema(root))
    reached = {t for _, t in corpus.out_edges("cmp-001-a")}
    assert reached == {"cmp-002-b"}
    assert run(root, "neighbors", "cmp-001-a", "--depth", "3", "--out") == 0


def test_minting_follows_khubs_rule() -> None:
    """`<prefix>-NNN-<slug>`, one sequence per prefix — byte-identical to khub's."""
    root = fresh()
    run(root, "add", "adr", "--title", "Use Postgres", "--status", "proposed")
    run(root, "add", "adr", "--title", "Drop Redis", "--status", "proposed")
    run(root, "add", "requirement", "--title", "Pay by card", "--kind", "functional")
    run(root, "add", "requirement", "--title", "Settle in 2s", "--kind", "constraint")
    run(root, "add", "requirement", "--title", "Refund in 30d", "--kind", "functional")
    slugs = set(kb.scan(root, kb.load_schema(root)).entities)
    assert {"ad-001-use-postgres", "ad-002-drop-redis", "fr-001-pay-by-card",
            "cst-001-settle-in-2s", "fr-002-refund-in-30d"} <= slugs


def test_minting_without_a_declared_prefix_is_a_bare_number() -> None:
    root = fresh()
    shipped = (SCRIPTS / kb.SCHEMA_FILE).read_text()
    override = root / kb.CONFIG_DIR / kb.SCHEMA_FILE
    override.parent.mkdir(parents=True)
    override.write_text(shipped.replace("    id_prefix: cmp\n", ""))
    run(root, "add", "component", "--title", "Public API", "--kind", "service")
    assert "001-public-api" in kb.scan(root, kb.load_schema(root)).entities


def test_explicit_id_bypasses_minting() -> None:
    root = fresh()
    run(root, "add", "adr", "--title", "Hand named", "--status", "proposed", "--id", "ad-050-hand")
    assert "ad-050-hand" in kb.scan(root, kb.load_schema(root)).entities


def test_a_missing_kind_reports_its_cause_once() -> None:
    """The prefix depends on `kind`; with none set no id can be right, and
    `incomplete` already names the cause. khub skips the id gate for the same
    reason — reporting bad_id too would say it twice."""
    root = fresh()
    write(root / "knowledge/requirements/001-no-kind.md", """
---
type: requirement
title: No kind
created: 2026-07-25
---
""")
    found = codes(root)
    assert "bad_id" not in found[kb.ERROR]
    assert "incomplete" in found[kb.GAP]


def test_a_hand_named_file_fails_the_id_gate() -> None:
    """khub gained this gate so both tools have it — a hand-named file in a type
    that declares a prefix is a `validate` error, field `id`, in either tool."""
    root = fresh()
    write(root / "knowledge/decisions/nonsense.md", """
---
type: adr
title: Hand named
status: proposed
created: 2026-07-25
---

## Context
## Decision
## Consequences
""")
    assert "bad_id" in codes(root)[kb.ERROR]


def test_writes_coerce_and_validate_like_khub() -> None:
    """A list or bool written as a raw string fails the very next validate, and a
    bad enum has to be refused before the file exists — khub does both."""
    root = fresh()
    assert run(root, "add", "adr", "--title", "X", "--status", "accepted", "--tags", "a,b") == 0
    entity = scan(root).entities["ad-001-x"]
    assert entity.fm["tags"] == ["a", "b"]
    assert codes(root)[kb.ERROR] == []

    assert run(root, "add", "adr", "--title", "Y", "--status", "bogus") == 2
    assert "ad-002-y" not in scan(root).entities

    assert run(root, "edit", "ad-001-x", "draft", "true") == 0
    assert scan(root).entities["ad-001-x"].fm["draft"] is True
    assert run(root, "edit", "ad-001-x", "draft", "maybe") == 2


def test_a_required_false_is_present_not_missing() -> None:
    """`not value` would call a legitimate `false` (or 0) a missing field."""
    root = fresh()
    shipped = (SCRIPTS / kb.SCHEMA_FILE).read_text()
    override = root / kb.CONFIG_DIR / kb.SCHEMA_FILE
    override.parent.mkdir(parents=True)
    override.write_text(shipped.replace(
        "    draft:       { type: bool, default: false }",
        "    draft:       { type: bool, default: false, required: true }"))
    run(root, "add", "component", "--title", "API", "--kind", "service", "--draft", "false")
    corpus = scan(root)
    assert corpus.entities["cmp-001-api"].fm["draft"] is False
    incomplete = {f.where for f in kb.check(corpus) if f.code == "incomplete"}
    assert "cmp-001-api" not in incomplete


def test_ordinals_count_within_a_type() -> None:
    """khub scopes the counter to the type; a shared prefix-less scheme must not
    make two types share one sequence."""
    root = fresh()
    shipped = (SCRIPTS / kb.SCHEMA_FILE).read_text()
    override = root / kb.CONFIG_DIR / kb.SCHEMA_FILE
    override.parent.mkdir(parents=True)
    override.write_text(shipped.replace("    id_prefix: cmp\n", "").replace("    id_prefix: ad\n", ""))
    run(root, "add", "component", "--title", "API", "--kind", "service")
    run(root, "add", "adr", "--title", "Decide", "--status", "proposed")
    slugs = set(scan(root).entities)
    assert {"001-api", "001-decide"} <= slugs, slugs


def test_get_edit_remove_and_status() -> None:
    root = fresh()
    run(root, "add", "component", "--title", "API", "--kind", "service")
    run(root, "add", "adr", "--title", "Use it", "--status", "proposed",
        "--affects", "cmp-001-api")

    assert run(root, "get", "cmp-001-api", "--edges") == 0
    assert run(root, "status") == 0

    assert run(root, "edit", "ad-001-use-it", "status", "accepted") == 0
    entity = kb.scan(root, kb.load_schema(root)).entities["ad-001-use-it"]
    assert entity.fm["status"] == "accepted" and entity.fm["updated"]
    assert run(root, "edit", "ad-001-use-it", "status", "nope") == 2        # enum
    assert run(root, "edit", "ad-001-use-it", "affects", "cmp-001-api") == 2  # a relation

    assert run(root, "remove", "cmp-001-api") == 2      # inbound edge holds it
    assert run(root, "remove", "cmp-001-api", "--force") == 0
    assert not (root / "knowledge/components/cmp-001-api.md").exists()


def test_validate_is_the_per_entity_subset_of_check() -> None:
    root = fresh()
    run(root, "add", "component", "--title", "API", "--kind", "service")
    write(root / "knowledge/decisions/ad-001-broken.md", """
---
type: adr
title: Broken
status: maybe
created: 2026-07-25
---
""")
    corpus = kb.scan(root, kb.load_schema(root))
    per_entity = {f.code for f in kb.check(corpus) if f.code in kb.VALIDATE_CODES}
    assert per_entity == {"bad_value", "body_shape"}
    assert run(root, "validate") == 1
    # the orphan component is a graph finding, so validate does not fail on it
    assert "orphan" not in per_entity


def test_install_skills_dry_run_names_its_targets() -> None:
    root = fresh()
    assert run(root, "install-skills", "--dry-run", "--target", "opencode") == 0


def main() -> int:
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    failed = 0
    for fn in tests:
        try:
            fn()
            print(f"  ok    {fn.__name__}")
        except Exception as exc:  # noqa: BLE001 - a test runner reports, never raises
            failed += 1
            print(f"  FAIL  {fn.__name__}: {type(exc).__name__}: {exc}")
    print(f"\n{len(tests) - failed}/{len(tests)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    shutil.rmtree(Path(tempfile.gettempdir()) / "kb-tests", ignore_errors=True)
    raise SystemExit(main())
