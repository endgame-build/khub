"""Per-format entity serialization — the one strategy module (FS-001 formats).

Every read and write of an entity document funnels through here, dispatched on
the schema's per-type ``format``. Three per-item formats: ``md`` (YAML
frontmatter + prose body, the default), ``json`` and ``yaml`` (a single
mapping — pure metadata, with prose carried in a reserved ``body`` key).

The ``body`` key is format-reserved for non-md documents: popped on every read
(so it never reaches ``Index.meta``, keeping ``validate --strict`` and query
filters clean) and placed back on write when non-empty. Its value must be a
string or null — anything else is a malformed document (silently ``str()``-ing
a mapping would persist a Python repr on the next write). md keeps its fence
semantics — a frontmatter field named ``body`` in an md file stays an ordinary
field, exactly as today; the write verbs reject the name for non-md types.

Round-trip guarantees per format: md and yaml preserve key order and comments
(ruamel round-trip); json preserves key order — JSON admits no comments, and
hand-formatted whitespace normalizes to the canonical style on first write.
# ponytail: hand-authored JSON whitespace is not preserved; add a raw-text
# splice writer if byte-stable third-party JSON ever matters.
"""

from __future__ import annotations

import json
from datetime import date, datetime
from io import StringIO
from pathlib import Path
from typing import Any

import frontmatter
from ruamel.yaml import YAML

from khub.core.errors import LocatedError

# The format/layout compatibility matrix's two sides — the single source the
# meta-schema validator (schema_model) checks against. jsonl is collection-only
# (a one-line jsonl file is json wearing the wrong extension); md is per-item
# only (prose files don't hold rows); gjson is named in the grammar but
# undefined — rejected everywhere until designed.
PER_ITEM = frozenset({"md", "json", "yaml"})
COLLECTION = frozenset({"json", "jsonl", "yaml"})

_BODY_KEY = "body"

# Round-trip instance (moved from entity.py): preserves key order and comments
# on edit. Never emit YAML anchors/aliases: two keys sharing a value (e.g.
# created==updated on a fresh entity) must each serialize in full, not collapse
# to &id/*id — anchors leak into raw reads and break the minimal-diff guarantee.
_yaml_rt = YAML()
_yaml_rt.default_flow_style = False
_yaml_rt.representer.ignore_aliases = lambda *_: True

_yaml_safe = YAML(typ="safe")


def fmt_of(path: Path | str) -> str:
    """The format a path stores: its suffix sans dot (``.yml`` is NOT aliased)."""
    return Path(path).suffix.lstrip(".")


def parse(text: str, fmt: str) -> tuple[dict[str, Any], str]:
    """``(meta, body)`` from document text; raises on a malformed document.

    (md raises too when the fenced YAML itself is invalid — python-frontmatter
    propagates the YAML error.) For json/yaml the document must be a mapping,
    and the reserved ``body`` key is extracted as the body.
    """
    if fmt == "md":
        post = frontmatter.loads(text)
        return dict(post.metadata), post.content
    if fmt not in PER_ITEM:  # a collection fmt reaching the per-item parser is a wiring bug
        raise LocatedError(
            code="malformed_entity",
            message=f"'{fmt}' is not a per-item format; collections load via load_collection",
        )
    data = _load_mapping(text, fmt, _yaml_safe)
    return data, _pop_body(data, fmt)


def try_parse(path: Path) -> tuple[dict[str, Any], str] | None:
    """``parse`` guarded for scan-altitude callers: None on ANY read/parse failure.

    The guard is the malformed-file contract (one bad file never bricks a scan)
    plus the read race (a file deleted between scan and read). The read is
    strict utf-8 — undecodable bytes make the file malformed, same as the
    pre-formats scan. Callers that need the failure loud use ``parse`` directly.
    """
    try:
        return parse(path.read_text(encoding="utf-8"), fmt_of(path))
    except Exception:  # noqa: BLE001 — the scan contract absorbs a bad file, never a scan bug
        return None


def load_meta(path: Path) -> dict[str, Any] | None:
    """Scan-altitude read: the document's metadata, or None if unparseable.

    For non-md formats the reserved ``body`` key has already been popped, so it
    never enters ``Index.meta``.
    """
    parsed = try_parse(path)
    return None if parsed is None else parsed[0]


def read_doc(path: Path) -> tuple[Any, str]:
    """Edit-altitude round-trip load: ``(cmap, body)`` with formatting preserved.

    md: fence-split, ruamel round-trip on the frontmatter (comments live).
    yaml: ruamel round-trip on the whole document; ``body`` key popped.
    json: ordered load; ``body`` key popped.
    # ponytail: a non-md body key re-lands at the end of the mapping on edit —
    # its original position (and any comment on it) migrates; fix if it ever bites.
    """
    text = path.read_text(encoding="utf-8")
    fmt = fmt_of(path)
    if fmt == "md":
        yaml_text, body = _split_frontmatter(text)
        cmap = _yaml_rt.load(yaml_text)
        return (cmap if cmap is not None else {}), body
    data = _load_mapping(text, fmt, _yaml_rt)
    return data, _pop_body(data, fmt)


def render(cmap: Any, body: str, fmt: str) -> str:
    """Serialize a document: metadata plus body, in the format's native shape.

    md renders the frontmatter fence + prose body byte-preservingly (no
    normalization here — a metadata-only edit must not touch body bytes; the
    write verbs normalize newly supplied md prose). json/yaml place a non-empty
    ``body`` back as the reserved key (an empty body writes no key); the
    caller's mapping is left unmutated. Dates serialize as ISO strings in
    json — downstream reads are ISO-tolerant.
    """
    if fmt == "md":
        stream = StringIO()
        _yaml_rt.dump(cmap, stream)
        return f"---\n{stream.getvalue()}---\n{body}"
    if body:
        cmap[_BODY_KEY] = body
    try:
        if fmt == "json":
            return json.dumps(cmap, indent=2, ensure_ascii=False, default=_json_scalar) + "\n"
        stream = StringIO()
        _yaml_rt.dump(cmap, stream)
        return stream.getvalue()
    finally:
        if body:  # restore the caller's mapping — the reserved key is render-internal
            del cmap[_BODY_KEY]


def fts_body(meta: dict[str, Any], body: str, fmt: str) -> str:
    """The searchable prose for one entity: its body plus every scalar string field.

    An agent finds an entity by URL, stack, repo, name, or note text — so attribute
    VALUES are searchable, not just prose. Dates/bools/lists are excluded (an agent
    filters those with ``query``, and they only dilute BM25); ``type`` is excluded as
    pure noise, since every entity of a type carries the same token.

    Until 0.13.0 md entities returned the body alone, so `search python` missed an
    entity carrying `stack: python` — while json/yaml entities matched it. Same rule
    for every format now, which is what README and the khub skill always claimed.

    ``title``/``name`` are excluded: they already populate the dedicated ``title`` FTS
    column, and folding them in here counted every title twice in BM25 and let a snippet
    excerpt a run of frontmatter values as if it were prose.
    """
    skip = {"type", "title", "name"}
    scalars = (v for k, v in meta.items() if k not in skip and isinstance(v, str))
    return " ".join([body, *scalars]).strip()


def _load_mapping(text: str, fmt: str, yaml_inst: YAML) -> Any:
    """A json/yaml document as a mapping; anything else is a malformed document.

    An empty/whitespace-only document reads as an empty mapping (the documented
    empty-file contract); a scalar document (``0``, ``false``) is malformed in
    both formats — never coerced to ``{}``.
    """
    if fmt == "json":
        data = json.loads(text, object_pairs_hook=_reject_dup_keys) if text.strip() else {}
    else:
        loaded = yaml_inst.load(text)
        data = loaded if loaded is not None else {}
    if not isinstance(data, dict):
        raise LocatedError(
            code="malformed_entity",
            message=f"A {fmt} entity must be a single mapping, got {type(data).__name__}",
        )
    return data


def _pop_body(data: dict[str, Any], fmt: str, *, ctx: str = "") -> str:
    """Extract the reserved ``body`` key: a string (or null = empty), else malformed."""
    raw = data.pop(_BODY_KEY, None)
    if raw is None:
        return ""
    if isinstance(raw, str):
        return raw
    raise LocatedError(
        code="malformed_entity",
        message=f"{ctx}the reserved 'body' key of a {fmt} entity must be a string, "
        f"got {type(raw).__name__}",
    )


# --- collections (layout: collection — one file, row-level entities) ----------


def load_collection(text: str, fmt: str) -> dict[str, Any]:
    """Raw rows keyed by slug from one collection file; raises on ANY bad row.

    v1 malformed contract is whole-file: a non-mapping row, a missing/invalid
    jsonl ``slug`` key, a duplicate slug, or a non-string ``body`` makes the
    entire file malformed — khub never partially loads (or rewrites) a file it
    cannot fully round-trip. An empty/whitespace-only text is zero rows.
    # ponytail: per-row fault isolation (bad line ≠ bad file) is the named
    # upgrade when a real corpus hits a 500-row file with one typo.

    Rows are returned at storage altitude: the ``body`` key (validated
    string-or-null) stays inside each row; ``split_row`` strips it for meta.
    jsonl rows carry their slug in a reserved ``slug`` key (popped here);
    yaml/json collections are mappings keyed by slug.
    """
    if fmt == "jsonl":
        rows: dict[str, Any] = {}
        for n, line in enumerate(text.splitlines(), start=1):
            if not line.strip():
                continue
            row = json.loads(line)
            if not isinstance(row, dict):
                raise LocatedError(
                    code="malformed_entity",
                    message=f"jsonl row {n} is not an object ({type(row).__name__})",
                )
            slug = row.pop("slug", None)
            if not isinstance(slug, str) or not slug:
                raise LocatedError(
                    code="malformed_entity",
                    message=f"jsonl row {n} is missing its reserved string 'slug' key",
                )
            if slug in rows:
                raise LocatedError(
                    code="malformed_entity", message=f"duplicate slug '{slug}' at jsonl row {n}"
                )
            rows[slug] = row
    else:
        data = _load_mapping(text, fmt, _yaml_rt)
        # Validate in place and return the loaded mapping itself. Rebuilding it as a
        # plain dict dropped ruamel's document-level comment — the header a registry
        # file carries ("# do not hand-edit") vanished on the first write, against the
        # documented yaml round-trip guarantee. Comments between and after rows
        # survived either way, since those attach to the individual rows.
        for key, row in data.items():
            if not isinstance(key, str) or not key:
                raise LocatedError(
                    code="malformed_entity", message=f"collection key {key!r} is not a slug string"
                )
            if not isinstance(row, dict):
                raise LocatedError(
                    code="malformed_entity",
                    message=f"row '{key}' is not a mapping ({type(row).__name__})",
                )
            inner = row.pop("slug", None)  # an agreeing inner slug never reaches meta
            if inner is not None and inner != key:
                raise LocatedError(
                    code="malformed_entity",
                    message=f"row '{key}' carries a disagreeing slug key '{inner}'",
                )
        rows = data
    for slug, row in rows.items():
        _pop_body(dict(row), fmt, ctx=f"row '{slug}': ")  # validate, discard the copy
    return rows


def dump_collection(rows: dict[str, Any], fmt: str) -> str:
    """Serialize rows back to the collection file's native shape.

    jsonl re-emits one compact object per line (``slug`` first) — untouched
    rows of khub-written files are byte-identical; yaml keeps the ruamel
    round-trip map, so comments on untouched rows survive.
    """
    if fmt == "jsonl":
        lines = [
            json.dumps({"slug": slug, **row}, ensure_ascii=False, default=_json_scalar)
            for slug, row in rows.items()
        ]
        return "\n".join(lines) + ("\n" if lines else "")
    if fmt == "json":
        return json.dumps(rows, indent=2, ensure_ascii=False, default=_json_scalar) + "\n"
    stream = StringIO()
    _yaml_rt.dump(rows, stream)
    return stream.getvalue()


def render_row(slug: str, row: dict[str, Any], fmt: str) -> str:
    """One row's stored serialization — ``get --format raw`` for a collection row."""
    return dump_collection({slug: row}, fmt)


def split_row(row: dict[str, Any], fmt: str) -> tuple[dict[str, Any], str]:
    """A raw row as ``(meta, body)`` — the reserved key stripped, the row untouched."""
    meta = dict(row)
    return meta, _pop_body(meta, fmt)


def _reject_dup_keys(pairs: list[tuple[str, Any]]) -> dict[str, Any]:
    """object_pairs_hook: a duplicate key in a json mapping is malformed, never last-wins."""
    out: dict[str, Any] = {}
    for key, value in pairs:
        if key in out:
            raise LocatedError(code="malformed_entity", message=f"duplicate key '{key}'")
        out[key] = value
    return out


def _json_scalar(value: Any) -> str:
    """JSON fallback for the date/datetime objects the write verbs put in meta."""
    if isinstance(value, (date, datetime)):
        return value.isoformat()
    raise TypeError(f"Not JSON-serializable: {type(value).__name__}")


def _split_frontmatter(text: str) -> tuple[str, str]:
    """Split ``---\\n<yaml>\\n---\\n<body>`` into its YAML text and its body remainder.

    Tolerates a leading UTF-8 BOM and blank lines before the fence — the same leniency
    python-frontmatter (the read path) has — so any file khub can display, it can also
    edit. The fence itself stays strict.
    """
    text = text.lstrip("\ufeff")  # a BOM the read path silently accepts
    lines = text.split("\n")
    start = 0
    while start < len(lines) and lines[start].strip() == "":
        start += 1  # skip leading blank lines the read path also skips
    if start >= len(lines) or lines[start] != "---":
        raise LocatedError(code="malformed_entity", message=f"No frontmatter fence in {text[:20]!r}")
    for i in range(start + 1, len(lines)):
        if lines[i] == "---":
            return "\n".join(lines[start + 1 : i]) + "\n", "\n".join(lines[i + 1 :])
    raise LocatedError(code="malformed_entity", message="Unterminated frontmatter")
