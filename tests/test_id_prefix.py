"""Enumerated ids: `<prefix>-NNN-<slug>`, or `NNN-<slug>` where no prefix is declared.

Every minted id carries an ordinal, so a corpus reads in authoring order and an
entity can be named in prose by a short stable handle. `id_prefix` adds the
leading token, either literally (`adr: ad`) or chosen by another attribute's value
(`requirement: fr | cst | br`, by `kind`), which puts the kind in the filename
where a mislabelled file is visible.

An explicit `--id` is untouched by any of this: the caller named it.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from khub.core.entity import create
from khub.core.model import IdPrefix
from khub.core.resolve import resolve
from khub.core.workspace import init_workspace


@pytest.fixture
def lite(tmp_path: Path) -> Path:
    root = tmp_path / "ws"
    root.mkdir()
    init_workspace("build-lite", root)
    return root


def _schema(tmp_path: Path, body: str) -> Path:
    path = tmp_path / "schema.yaml"
    path.write_text(body)
    return path


# --------------------------------------------------------------------- minting


@pytest.mark.integration
def test_literal_prefix_numbers_from_one(lite: Path) -> None:
    first = create(lite, "adr", {"title": "Use Postgres", "status": "proposed"})
    second = create(lite, "adr", {"title": "Drop Redis", "status": "proposed"})
    assert first.slug == "ad-001-use-postgres"
    assert second.slug == "ad-002-drop-redis"


@pytest.mark.integration
def test_by_value_prefix_keeps_one_sequence_per_prefix(lite: Path) -> None:
    """fr- and cst- count independently: the number reads as "the Nth constraint"."""
    fr1 = create(lite, "requirement", {"title": "Pay by card", "kind": "functional"})
    cst1 = create(lite, "requirement", {"title": "Settle in 2s", "kind": "constraint"})
    fr2 = create(lite, "requirement", {"title": "Refund in 30d", "kind": "functional"})
    br1 = create(lite, "requirement", {"title": "VAT applies", "kind": "business-rule"})
    assert [fr1.slug, cst1.slug, fr2.slug, br1.slug] == [
        "fr-001-pay-by-card", "cst-001-settle-in-2s",
        "fr-002-refund-in-30d", "br-001-vat-applies",
    ]


@pytest.mark.integration
def test_no_declared_prefix_still_numbers(tmp_path: Path) -> None:
    """firm-ops declares none, so its ids are plain `NNN-slug`."""
    root = tmp_path / "ws"
    root.mkdir()
    init_workspace("firm-ops", root)
    assert create(root, "client", {"name": "Acme Corp"}).slug == "001-acme-corp"
    assert create(root, "client", {"name": "Globex"}).slug == "002-globex"


@pytest.mark.integration
def test_ordinal_is_padded_to_three_and_keeps_counting(lite: Path) -> None:
    """045 pads; past 999 the number simply grows rather than wrapping or breaking."""
    decisions = lite / "knowledge" / "decisions"
    for slug in ("ad-044-a", "ad-999-b"):
        (decisions / f"{slug}.md").write_text(
            f"---\ntype: adr\ntitle: {slug}\nstatus: proposed\ncreated: 2026-01-01\n---\n"
        )
    assert create(lite, "adr", {"title": "Next", "status": "proposed"}).slug == "ad-1000-next"

    # The ordinal is one past the highest in use, so removing the tail reuses it.
    (decisions / "ad-999-b.md").unlink()
    (decisions / "ad-1000-next.md").unlink()
    assert create(lite, "adr", {"title": "After", "status": "proposed"}).slug == "ad-045-after"


@pytest.mark.integration
def test_explicit_id_bypasses_the_scheme(lite: Path) -> None:
    """The caller named it; khub does not renumber or re-prefix an explicit --id."""
    result = create(lite, "adr", {"title": "Hand named", "status": "proposed"}, id_="ad-050-hand")
    assert result.slug == "ad-050-hand"
    # …and the next minted one continues from it, rather than ignoring it.
    assert create(lite, "adr", {"title": "Next", "status": "proposed"}).slug == "ad-051-next"


@pytest.mark.integration
def test_a_missing_by_value_falls_back_to_a_bare_number(lite: Path) -> None:
    """`kind` is required, but capture is never blocked — so minting cannot be either."""
    assert create(lite, "requirement", {"title": "No kind yet"}).slug == "001-no-kind-yet"


# ------------------------------------------------------------------ the schema


@pytest.mark.unit
def test_by_value_prefix_must_match_its_enum(tmp_path: Path) -> None:
    """A map that misses an enum member would mint no id for that member."""
    path = _schema(tmp_path, """
entities:
  requirement:
    layout: file
    path: reqs
    id_prefix: { by: kind, map: { functional: fr } }
    attributes:
      kind: { enum: [functional, constraint], required: true }
""")
    with pytest.raises(Exception) as err:
        resolve([path])
    assert "must cover exactly kind's enum" in str(err.value)


@pytest.mark.unit
def test_by_value_prefix_needs_an_enum_attribute(tmp_path: Path) -> None:
    path = _schema(tmp_path, """
entities:
  requirement:
    layout: file
    path: reqs
    id_prefix: { by: nope, map: { a: x } }
    attributes:
      kind: { enum: [functional], required: true }
""")
    with pytest.raises(Exception) as err:
        resolve([path])
    assert "must name an attribute of this type that declares an enum" in str(err.value)


@pytest.mark.unit
@pytest.mark.parametrize("value", ["''", "'AD'", "'a-d'", "'1st'", "'  '"])
def test_a_prefix_must_be_a_slug_token(tmp_path: Path, value: str) -> None:
    """It is concatenated into a filename: empty mints a leading hyphen, and a
    hyphenated one cannot be read back out of the id."""
    path = _schema(tmp_path, f"""
entities:
  a:
    layout: file
    path: as
    id_prefix: {value}
""")
    with pytest.raises(Exception):
        resolve([path])


@pytest.mark.unit
def test_resolved_prefix_shapes() -> None:
    literal = IdPrefix(literal="ad")
    assert literal.resolve({}) == "ad" and literal.all == ("ad",)

    by_kind = IdPrefix(by="kind", members=(("functional", "fr"), ("constraint", "cst")))
    assert by_kind.resolve({"kind": "constraint"}) == "cst"
    assert by_kind.resolve({"kind": "unknown"}) is None
    assert by_kind.resolve({}) is None
    assert by_kind.all == ("fr", "cst")


@pytest.mark.integration
def test_every_preset_prefix_is_declared_on_a_real_type() -> None:
    """The presets' prose conventions (ad-, fr-, wp-) and their schemas agree."""
    presets = Path(__file__).resolve().parents[1] / "src" / "khub" / "presets"
    for name in ("build-lite", "build-hub", "firm-ops"):
        schema = resolve([presets / "core.yaml", presets / name / "schema.yaml"])
        for type_, rtype in schema.types.items():
            if rtype.id_prefix is None:
                continue
            assert rtype.storage.layout != "singleton", f"{name}/{type_}: a singleton mints nothing"
            assert all(p and p.islower() for p in rtype.id_prefix.all), f"{name}/{type_}"
