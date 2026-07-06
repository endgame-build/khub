"""Audit hardening for the entity write surface (FS-002).

One focused test per non-trivial write-gate fix: scalar-many corruption, the type
discriminator lock, slug length cap, explicit-id collision refusal, comma-list on a
single-valued relation, date/bool write gates, BOM/blank-lead editability, title slug
fallback, self-link and ambiguous-target refusal, backdating, and the LinkResult
``changed`` contract. Uses the shared ``fresh_ws``/``seed`` fixtures from conftest.
"""

from __future__ import annotations

from datetime import date
from pathlib import Path
from typing import Callable

import frontmatter
import pytest

from khub.core.entity import _split_frontmatter, create, link, unlink, update
from khub.core.errors import LocatedError

Seed = Callable[..., None]


def _people(ws: Path, seed: Seed) -> None:
    seed(ws, "identity/team/ann.md", type="person", name="Ann", role="consultant")
    seed(ws, "identity/team/bob.md", type="person", name="Bob", role="engineer")


def _deal(ws: Path, seed: Seed, **over: object) -> Path:
    meta: dict[str, object] = dict(
        type="opportunity", created="2026-01-01", updated="2026-01-01",
        draft=False, stage="prospect", client="initech", owner="noor",
    )
    meta.update(over)
    seed(ws, "opportunities/initech-deal/_index.md", **meta)
    return ws / "opportunities" / "initech-deal" / "_index.md"


def _md(ws: Path) -> list[str]:
    return sorted(str(p.relative_to(ws)) for p in ws.rglob("*.md") if ".khub" not in p.parts)


# --- fix 1: scalar-many corruption -------------------------------------------


@pytest.mark.unit
def test_scalar_many_link_no_char_split(fresh_ws: Path, seed: Seed) -> None:
    """A many-relation stored as a scalar gains a value as a list, never char-split."""
    _people(fresh_ws, seed)
    seed(fresh_ws, "fragments/note.md", type="fragment", stage="raw", owner="ann", related="ann")
    result = link(fresh_ws, "note", "related", "bob")
    assert result.changed is True
    assert frontmatter.load(str(fresh_ws / "fragments" / "note.md")).metadata["related"] == ["ann", "bob"]


@pytest.mark.unit
def test_scalar_many_unlink_clean(fresh_ws: Path, seed: Seed) -> None:
    """Unlinking the lone value of a scalar-stored many-relation removes it cleanly."""
    _people(fresh_ws, seed)
    seed(fresh_ws, "fragments/note.md", type="fragment", stage="raw", owner="ann", related="ann")
    result = unlink(fresh_ws, "note", "related", "ann")
    assert result.changed is True
    # No character survivors (['a','n','n']); the key is dropped entirely.
    assert "related" not in frontmatter.load(str(fresh_ws / "fragments" / "note.md")).metadata


# --- fix 2: type discriminator clobber ---------------------------------------


@pytest.mark.unit
def test_user_type_field_rejected(fresh_ws: Path) -> None:
    """A user `type` field disagreeing with the layout type is refused, writing nothing."""
    before = _md(fresh_ws)
    with pytest.raises(LocatedError) as err:
        create(fresh_ws, "client", {"name": "Widgets", "type": "person"})
    assert err.value.code == "type_field_forbidden"
    assert _md(fresh_ws) == before


# --- fix 3: over-long slug ----------------------------------------------------


@pytest.mark.unit
def test_overlong_id_rejected_cleanly(fresh_ws: Path) -> None:
    """A ~300-char --id raises a located error, not an OSError at path.write_text."""
    with pytest.raises(LocatedError) as err:
        create(fresh_ws, "client", {"name": "Widgets"}, id_="a" * 300)
    assert err.value.code == "invalid_slug"


# --- fix 5: comma-list on a single-valued relation ---------------------------


@pytest.mark.unit
def test_comma_on_single_relation_raises(fresh_ws: Path, seed: Seed) -> None:
    """A comma-list on a single-valued relation refuses instead of dropping its tail."""
    seed(fresh_ws, "clients/initech.md", type="client", name="Initech")
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme")
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor", role="partner")
    before = _md(fresh_ws)
    with pytest.raises(LocatedError) as err:
        create(fresh_ws, "opportunity", {"client": "initech,acme", "owner": "noor", "stage": "prospect"})
    assert err.value.code == "cardinality_violation"
    assert _md(fresh_ws) == before


# --- fix 6: explicit --id collision ------------------------------------------


@pytest.mark.unit
def test_explicit_id_collision_raises(fresh_ws: Path) -> None:
    """A within-type explicit --id collision refuses (no silent auto-suffix)."""
    create(fresh_ws, "client", {"name": "Acme"}, id_="acme")
    with pytest.raises(LocatedError) as err:
        create(fresh_ws, "client", {"name": "Acme"}, id_="acme")
    assert err.value.code == "slug_taken"
    assert not (fresh_ws / "clients" / "acme-2.md").exists()


# --- fix 7: dates validated at write -----------------------------------------


@pytest.mark.unit
def test_bad_date_rejected(fresh_ws: Path, seed: Seed) -> None:
    """An impossible date is refused at write time (mirrors the number gate)."""
    path = _deal(fresh_ws, seed)
    before = path.read_bytes()
    with pytest.raises(LocatedError) as err:
        update(fresh_ws, "initech-deal", {"created": "2026-13-45"})
    assert err.value.code == "date_violation"
    assert path.read_bytes() == before


@pytest.mark.unit
def test_good_date_accepted(fresh_ws: Path, seed: Seed) -> None:
    """A valid ISO date passes the write gate."""
    path = _deal(fresh_ws, seed)
    update(fresh_ws, "initech-deal", {"created": "2026-02-02"})
    assert str(frontmatter.load(str(path)).metadata["created"]) == "2026-02-02"


# --- fix 8: bool write gate ---------------------------------------------------


@pytest.mark.unit
def test_bad_bool_rejected(fresh_ws: Path, seed: Seed) -> None:
    """A non-BOOLISH bool value is refused, not silently coerced to False."""
    path = _deal(fresh_ws, seed)
    before = path.read_bytes()
    with pytest.raises(LocatedError) as err:
        update(fresh_ws, "initech-deal", {"draft": "banana"})
    assert err.value.code == "bool_violation"
    assert path.read_bytes() == before


@pytest.mark.unit
def test_boolish_value_accepted(fresh_ws: Path, seed: Seed) -> None:
    """A BOOLISH string parses to the right bool (yes → True)."""
    path = _deal(fresh_ws, seed, draft=False)
    update(fresh_ws, "initech-deal", {"draft": "yes"})
    assert frontmatter.load(str(path)).metadata["draft"] is True


# --- fix 9: read/write parser divergence -------------------------------------


@pytest.mark.unit
def test_split_frontmatter_tolerates_bom_and_blank_lines() -> None:
    """A BOM or leading blank lines no longer make an otherwise-valid file un-editable."""
    yaml_bom, body_bom = _split_frontmatter("\ufeff---\ntype: fragment\nstage: raw\n---\nnote\n")
    assert "type: fragment" in yaml_bom and body_bom.strip() == "note"
    yaml_blank, _ = _split_frontmatter("\n\n---\ntype: fragment\nstage: raw\n---\nnote\n")
    assert "type: fragment" in yaml_blank
    # A genuinely missing fence still fails strictly.
    with pytest.raises(LocatedError) as err:
        _split_frontmatter("no fence here\n")
    assert err.value.code == "malformed_entity"


@pytest.mark.unit
def test_bom_entity_is_editable(fresh_ws: Path, seed: Seed) -> None:
    """A BOM-prefixed file is readable AND editable end-to-end."""
    _people(fresh_ws, seed)
    path = fresh_ws / "fragments" / "bomfrag.md"
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("\ufeff---\ntype: fragment\nstage: raw\nowner: ann\n---\nnote\n", encoding="utf-8")
    update(fresh_ws, "bomfrag", {"stage": "mature"})
    assert frontmatter.load(str(path)).metadata["stage"] == "mature"


# --- fix 10: slug minting falls back to title --------------------------------


@pytest.mark.unit
def test_slug_from_title_then_type(fresh_ws: Path, seed: Seed) -> None:
    """A no-name type mints from title, else from the type name."""
    _people(fresh_ws, seed)
    titled = create(fresh_ws, "fragment", {"stage": "raw", "owner": "ann", "title": "My Note"})
    assert titled.slug == "my-note"
    bare = create(fresh_ws, "fragment", {"stage": "raw", "owner": "ann"})
    assert bare.slug == "fragment"


# --- fix 11: self-link --------------------------------------------------------


@pytest.mark.unit
def test_self_link_rejected(fresh_ws: Path, seed: Seed) -> None:
    """Linking an entity to itself is refused — a self-edge connects nothing."""
    _people(fresh_ws, seed)
    with pytest.raises(LocatedError) as err:
        link(fresh_ws, "ann", "related", "ann")
    assert err.value.code == "self_link"


# --- fix 12: ambiguous bare-slug target at write -----------------------------


@pytest.mark.unit
def test_ambiguous_bare_target_raises(fresh_ws: Path, seed: Seed) -> None:
    """A bare target resolving to >1 node is refused; a qualified type/slug works."""
    seed(fresh_ws, "clients/acme.md", type="client", name="Acme")
    seed(fresh_ws, "partnerships/acme/_index.md", type="partnership", partner="Acme")
    seed(fresh_ws, "identity/team/pat.md", type="person", name="Pat", role="partner")
    with pytest.raises(LocatedError) as err:
        link(fresh_ws, "pat", "related", "acme")
    assert err.value.code == "ambiguity_error"
    result = link(fresh_ws, "pat", "related", "client/acme")
    assert result.changed is True


# --- fix 13: backdating -------------------------------------------------------


@pytest.mark.unit
def test_backdated_updated_preserved(fresh_ws: Path, seed: Seed) -> None:
    """An explicit `updated` in the edit wins; a plain edit still bumps to today."""
    path = _deal(fresh_ws, seed)
    update(fresh_ws, "initech-deal", {"updated": "2026-06-01"})
    meta = frontmatter.load(str(path)).metadata
    assert str(meta["updated"]) == "2026-06-01" and str(meta["updated"]) != str(date.today())
    update(fresh_ws, "initech-deal", {"stage": "won"})
    assert frontmatter.load(str(path)).metadata["updated"] == date.today()


# --- fix 14: LinkResult.changed contract -------------------------------------


@pytest.mark.unit
def test_link_changed_flag_and_no_spurious_rewrite(fresh_ws: Path, seed: Seed) -> None:
    """link/unlink report `changed`, and a no-op leaves the file byte-for-byte intact."""
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor", role="partner")
    seed(fresh_ws, "partnerships/northwind/_index.md", type="partnership", partner="Northwind", owner="noor")
    path = fresh_ws / "projects" / "initech-pov" / "_index.md"
    seed(fresh_ws, "projects/initech-pov/_index.md", type="project", client="initech", owner="noor")

    first = link(fresh_ws, "initech-pov", "partner", "northwind")
    assert first.changed is True
    after_link = path.read_bytes()
    again = link(fresh_ws, "initech-pov", "partner", "northwind")
    assert again.changed is False and path.read_bytes() == after_link  # no spurious rewrite

    removed = unlink(fresh_ws, "initech-pov", "partner", "northwind")
    assert removed.changed is True
    after_unlink = path.read_bytes()
    noop = unlink(fresh_ws, "initech-pov", "partner", "northwind")
    assert noop.changed is False and path.read_bytes() == after_unlink


# --- review-round fixes (code-review findings on the audit batch) -------------


@pytest.mark.unit
def test_empty_relation_value_rejected(fresh_ws: Path, seed: Seed) -> None:
    """A blank relation value is a located error on create AND update, not an IndexError."""
    _people(fresh_ws, seed)
    seed(fresh_ws, "clients/initech.md", type="client", name="Initech")
    with pytest.raises(LocatedError) as err:
        create(fresh_ws, "project", {"client": "", "owner": "noor"}, id_="p-empty")
    assert err.value.code == "empty_relation_value"
    seed(fresh_ws, "projects/p1/_index.md", type="project", client="initech", owner="noor")
    with pytest.raises(LocatedError) as err:
        update(fresh_ws, "p1", {"client": ""})
    assert err.value.code == "empty_relation_value"


@pytest.mark.unit
def test_edit_self_link_rejected(fresh_ws: Path, seed: Seed) -> None:
    """`edit X <relation> X` refuses the self-edge exactly like `link` does."""
    seed(fresh_ws, "identity/team/noor.md", type="person", name="Noor", role="partner")
    with pytest.raises(LocatedError) as err:
        update(fresh_ws, "noor", {"related": "noor"})
    assert err.value.code == "self_link"


@pytest.mark.unit
def test_update_result_draft_reads_boolish(fresh_ws: Path, seed: Seed) -> None:
    """UpdateResult.draft parses a hand-authored draft: 'false' as active (as_bool)."""
    seed(fresh_ws, "identity/team/ann.md", type="person", name="Ann", role="partner", draft="false")
    result = update(fresh_ws, "ann", {"name": "Ann B"})
    assert result.draft is False
