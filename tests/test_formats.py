"""TS-FMT-001 — Per-Format Serialization (core.formats, FS-001 formats).

Covers the parse/render/read_doc round-trip per format, the reserved ``body``
key mapping for non-md documents (survives a round-trip, empty body writes no
key, never leaks into meta), date serialization in JSON, key-order stability,
the malformed contract (``load_meta`` → None), the non-mapping rejection, and
``fts_body``'s scalar-string join for body-less entities.
"""

from __future__ import annotations

from datetime import date
from pathlib import Path

import pytest

from khub.core.errors import LocatedError
from khub.core.formats import fmt_of, fts_body, load_meta, parse, read_doc, render


@pytest.mark.unit
def test_fmt_of_suffixes() -> None:
    assert fmt_of(Path("clients/acme.md")) == "md"
    assert fmt_of("fragments/x.json") == "json"
    assert fmt_of(Path("projects/demo/_index.yaml")) == "yaml"


@pytest.mark.unit
def test_md_parse_render_roundtrip() -> None:
    text = "---\ntype: client\nname: Acme\n---\nprose body\n"
    meta, body = parse(text, "md")
    # python-frontmatter strips the trailing newline on parse
    assert meta == {"type": "client", "name": "Acme"} and body == "prose body"
    # render is byte-preserving: exactly the body it is given, no normalization
    assert render(meta, body + "\n", "md") == text
    assert render(meta, body, "md") == text[:-1]


@pytest.mark.unit
@pytest.mark.parametrize("fmt", ["json", "yaml"])
def test_body_key_maps_to_body(fmt: str) -> None:
    """The reserved `body` key pops out as the body and never stays in meta."""
    text = render({"type": "fragment", "stage": "raw"}, "some prose", fmt)
    meta, body = parse(text, fmt)
    assert body == "some prose"
    assert "body" not in meta and meta["stage"] == "raw"


@pytest.mark.unit
@pytest.mark.parametrize("fmt", ["json", "yaml"])
def test_empty_body_writes_no_key(fmt: str) -> None:
    text = render({"type": "fragment", "stage": "raw"}, "", fmt)
    assert "body" not in text
    meta, body = parse(text, fmt)
    assert body == "" and "body" not in meta


@pytest.mark.unit
def test_json_dates_serialize_iso() -> None:
    text = render({"type": "client", "created": date(2026, 6, 1)}, "", "json")
    assert '"created": "2026-06-01"' in text
    meta, _ = parse(text, "json")
    assert meta["created"] == "2026-06-01"  # ISO string; downstream reads tolerate it


@pytest.mark.unit
def test_json_key_order_stable() -> None:
    """An untouched round-trip re-emits byte-identically (insertion order kept)."""
    text = render({"type": "client", "zeta": "1", "alpha": "2"}, "", "json")
    meta, body = parse(text, "json")
    assert render(dict(meta), body, "json") == text


@pytest.mark.unit
def test_yaml_read_doc_preserves_comments(tmp_path: Path) -> None:
    p = tmp_path / "acme.yaml"
    p.write_text("type: client  # the discriminator\nname: Acme\nbody: prose\n")
    cmap, body = read_doc(p)
    assert body == "prose" and "body" not in cmap
    cmap["name"] = "Acme Corp"
    out = render(cmap, body, "yaml")
    assert "# the discriminator" in out and "body: prose" in out


@pytest.mark.unit
def test_load_meta_none_on_malformed(tmp_path: Path) -> None:
    bad_json = tmp_path / "x.json"
    bad_json.write_text('{"type": "client", unquoted}')
    bad_yaml = tmp_path / "y.yaml"
    bad_yaml.write_text("type: [unclosed\n")
    assert load_meta(bad_json) is None
    assert load_meta(bad_yaml) is None
    assert load_meta(tmp_path / "missing.json") is None


@pytest.mark.unit
def test_non_mapping_document_is_located_error() -> None:
    with pytest.raises(LocatedError) as err:
        parse("[1, 2, 3]", "json")
    assert err.value.code == "malformed_entity"


@pytest.mark.unit
@pytest.mark.parametrize("doc", ["0", "false"])
def test_falsy_scalar_document_is_malformed_in_both_formats(doc: str) -> None:
    """A scalar document never coerces to an empty mapping — json and yaml agree."""
    for fmt in ("json", "yaml"):
        with pytest.raises(LocatedError):
            parse(doc, fmt)


@pytest.mark.unit
def test_body_key_must_be_string_or_null() -> None:
    """A non-string body is malformed (str()-ing it would persist a Python repr)."""
    with pytest.raises(LocatedError) as err:
        parse('{"type": "x", "body": {"nested": [1]}}', "json")
    assert err.value.code == "malformed_entity" and "body" in err.value.message
    with pytest.raises(LocatedError):
        parse('{"type": "x", "body": 0}', "json")
    meta, body = parse("type: x\nbody:\n", "yaml")  # null = no body
    assert body == "" and "body" not in meta


@pytest.mark.unit
def test_render_does_not_mutate_caller_mapping() -> None:
    meta = {"type": "fragment", "stage": "raw"}
    out = render(meta, "prose", "json")
    assert '"body": "prose"' in out
    assert "body" not in meta  # the reserved key is render-internal


@pytest.mark.unit
def test_try_parse_strict_utf8(tmp_path: Path) -> None:
    """Undecodable bytes make the file malformed — never silently-indexed mojibake."""
    from khub.core.formats import try_parse

    bad = tmp_path / "bad.md"
    bad.write_bytes(b"---\ntype: client\nname: Acm\xff\n---\n")
    assert try_parse(bad) is None


@pytest.mark.unit
def test_fts_body_joins_scalar_strings() -> None:
    meta = {
        "type": "fragment",  # excluded as noise
        "name": "Zeppelin",
        "url": "https://example.com/z",
        "active": True,  # non-str excluded
        "tags": ["a", "b"],  # lists excluded
    }
    joined = fts_body(meta, "prose here", "json")
    assert "prose here" in joined and "Zeppelin" in joined and "example.com" in joined
    assert "fragment" not in joined and "True" not in joined
    assert fts_body(meta, "prose only", "md") == "prose only"
