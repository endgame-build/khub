"""Tests for bl. Run with `python3 lite/test_bl.py` (no pytest needed) or `uv run pytest lite`.

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

spec = importlib.util.spec_from_file_location("bl", Path(__file__).parent / "skill" / "bl.py")
assert spec and spec.loader
bl = importlib.util.module_from_spec(spec)
sys.modules["bl"] = bl  # dataclasses resolve their annotations through sys.modules
spec.loader.exec_module(bl)


def run(root: Path, *argv: str) -> int:
    """Exercise the real CLI entry point; its chatter belongs to the tool, not here."""
    with contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
        return bl.main(["-C", str(root), *argv])


def fresh() -> Path:
    root = Path(tempfile.mkdtemp()) / "hub"
    root.mkdir()
    run(root, "init")
    return root


def codes(root: Path) -> dict[str, list[str]]:
    corpus = bl.scan(root, bl.load_schema(root))
    out: dict[str, list[str]] = {bl.ERROR: [], bl.GAP: []}
    for f in bl.check(corpus):
        out[f.severity].append(f.code)
    return out


def write(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text.lstrip("\n"))


# ------------------------------------------------------------------ profile


def test_frontmatter_round_trip() -> None:
    source = '---\ntype: adr\ntitle: "Use: Postgres"\ntags: [a, b]\naffects:\n  - x\n  - y\n---\n\nbody\n'
    front, body = bl.split_front(source)
    fm = bl.parse_front(front)
    assert fm == {"type": "adr", "title": "Use: Postgres", "tags": ["a", "b"], "affects": ["x", "y"]}
    assert body == "body\n"
    lines = [line for k, v in fm.items() for line in bl.emit_key(k, v)]
    assert bl.parse_front(bl.split_front(bl.render(lines, body))[0]) == fm


def test_profile_violations_are_reported_not_guessed() -> None:
    for bad in ["---\nnested:\n  a: 1\n---\n\n", "---\ntype: adr\n", "no fence\n"]:
        try:
            bl.parse_front(bl.split_front(bad)[0])
            raise AssertionError(f"accepted {bad!r}")
        except bl.Bad:
            pass


def test_risky_scalars_are_quoted() -> None:
    for value in ["Use: Postgres", "- dash", "true", "", "  padded  ", "hash # comment"]:
        line = bl.emit_key("title", value)[0]
        assert bl.parse_front([line])["title"] == value, line


# ---------------------------------------------------------------- authoring


def test_new_mints_ids_by_kind_and_numbers_per_prefix() -> None:
    root = fresh()
    run(root, "new", "component", "Public API", "--set", "kind=service")
    run(root, "new", "requirement", "A user can pay", "--set", "kind=functional")
    run(root, "new", "requirement", "Settles in 2s", "--set", "kind=constraint")
    run(root, "new", "requirement", "Refunds within 30d", "--set", "kind=functional")
    slugs = set(bl.scan(root, bl.load_schema(root)).entities)
    assert {"cmp-001-public-api", "fr-001-a-user-can-pay", "cst-001-settles-in-2s"} <= slugs
    assert "fr-002-refunds-within-30d" in slugs


def test_new_rejects_unknown_field_and_unresolvable_edge() -> None:
    root = fresh()
    assert run(root, "new", "adr", "X", "--set", "status=proposed", "--set", "owner=noor") == 2
    assert run(root, "new", "adr", "X", "--set", "status=proposed", "--set", "affects=ghost") == 2
    assert run(root, "new", "requirement", "X") == 2  # no kind, so no id prefix


def test_link_is_surgical_and_checked() -> None:
    root = fresh()
    run(root, "new", "component", "API", "--set", "kind=service")
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

    run(root, "new", "adr", "Split the worker", "--set", "status=proposed")
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
    run(root, "new", "component", "API", "--set", "kind=service")
    run(root, "new", "requirement", "A user can pay", "--set", "kind=functional")
    run(root, "link", "fr-001-a-user-can-pay", "realized_in", "cmp-001-api")
    run(root, "new", "feature-spec", "Checkout", "--set", "status=active",
        "--set", "requirements=fr-001-a-user-can-pay")
    found = codes(root)
    assert found[bl.ERROR] == []
    assert found[bl.GAP] == [], found[bl.GAP]
    assert run(root, "check", "--strict") == 0


def test_every_error_fires() -> None:
    root = fresh()
    run(root, "new", "adr", "Fine", "--set", "status=accepted")
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

    found = set(codes(root)[bl.ERROR])
    assert found == {
        "bad_id", "bad_value", "cycle", "dangling", "malformed",
        "missing", "stray", "type_mismatch", "unknown_field",
    }, found
    assert run(root, "check") == 1


def test_gaps_do_not_fail_the_gate() -> None:
    root = fresh()
    run(root, "new", "component", "API", "--set", "kind=service")
    (root / "knowledge/arc42.md").unlink()
    found = codes(root)
    assert found[bl.ERROR] == []
    assert set(found[bl.GAP]) == {"orphan", "missing"}
    assert run(root, "check") == 0
    assert run(root, "check", "--strict") == 1


def test_narrative_roots_are_never_orphans() -> None:
    root = fresh()
    assert codes(root)[bl.GAP] == []


def test_body_shape_is_a_gap() -> None:
    root = fresh()
    prd = root / "knowledge/prd.md"
    prd.write_text(prd.read_text().replace("## Non-goals", "## Later maybe"))
    assert "body_shape" in codes(root)[bl.GAP]


# ------------------------------------------------------------------- graph


def test_inverse_edges_are_computed() -> None:
    root = fresh()
    run(root, "new", "component", "API", "--set", "kind=service")
    run(root, "new", "adr", "Use Postgres", "--set", "status=accepted",
        "--set", "affects=cmp-001-api")
    corpus = bl.scan(root, bl.load_schema(root))
    assert corpus.in_edges("cmp-001-api") == [("affects", "ad-001-use-postgres")]
    assert corpus.out_edges("cmp-001-api") == []
    assert corpus.schema.inverse("supersedes") == "superseded"


def test_blast_radius_walks_transitively() -> None:
    root = fresh()
    for name in ("A", "B", "C"):
        run(root, "new", "component", name, "--set", "kind=service")
    run(root, "link", "cmp-001-a", "depends_on", "cmp-002-b")
    run(root, "link", "cmp-002-b", "depends_on", "cmp-003-c")
    corpus = bl.scan(root, bl.load_schema(root))
    reached = {t for _, t in corpus.out_edges("cmp-001-a")}
    assert reached == {"cmp-002-b"}
    assert run(root, "links", "cmp-001-a", "--depth", "3", "--direction", "out") == 0


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
    shutil.rmtree(Path(tempfile.gettempdir()) / "bl-tests", ignore_errors=True)
    raise SystemExit(main())
