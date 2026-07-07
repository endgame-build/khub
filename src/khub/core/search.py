"""Full-text search — ``khub search`` (the SQLite FTS5 projection, FS-003 fast-follow).

The one sanctioned body reader: ``query`` filters frontmatter and derived edges
only, never prose (QRY-001); ``search`` exists precisely to match prose. The
FTS5 index is built in ``:memory:`` per invocation from the live Markdown — a
derived projection, never a source of truth, and never stale. Raw FTS5 MATCH
syntax passes through, so an agent gets ``"quoted phrases"``, ``OR``, ``NEAR``,
and ``prefix*``; a malformed expression is a located error, not a traceback.

# ponytail: per-invocation in-memory rebuild; persist under .khub/generated/
# when the corpus scan passes ~500ms (~10k entities).
"""

from __future__ import annotations

import sqlite3
from dataclasses import dataclass
from pathlib import Path

import frontmatter

from khub.core.entity import entity_path
from khub.core.errors import LocatedError
from khub.core.index import Index, build_index, filter_index, stray_nodes
from khub.core.introspect import load_schema


@dataclass(frozen=True)
class SearchHit:
    """One entity matching the search text, ranked by BM25 (best first)."""

    type: str
    slug: str
    title: str
    score: float
    snippet: str
    path: str


def search(root: Path, text: str, *, type_: str | None = None, limit: int = 20) -> list[SearchHit]:
    """The entities whose title or body match ``text``, best BM25 rank first.

    ``--type`` narrows to one entity type (an unknown type is a located error,
    same as ``query``); an empty result is a success, not an error (QRY-002).
    """
    resolved = load_schema(root)
    if type_ is not None and type_ not in resolved.types:  # before the corpus scan
        from khub.core.locate import provenance

        preset = provenance(root).get("preset") or "the"
        raise LocatedError.unknown_type(type_, preset, sorted(resolved.types))
    scanned = build_index(root, resolved)
    index = filter_index(scanned, stray_nodes(scanned))  # strays are not entities

    conn = sqlite3.connect(":memory:")
    try:
        _build_fts(conn, root, index, type_=type_)
        return _match(conn, text, limit=max(0, limit))  # a negative LIMIT is "unbounded" in SQLite
    finally:
        conn.close()


def _build_fts(conn: sqlite3.Connection, root: Path, index: Index, *, type_: str | None) -> None:
    """Populate an in-memory FTS5 table with each entity's title and full body.

    Full body, not a prefix — recall beats the trivial index-size saving at this
    scale. ``--type`` prunes at build time: with exactly one query per in-memory
    build, skipping the row loop beats a WHERE clause (no wasted body reads).
    Only the file read is guarded (a file deleted between scan and read is
    skipped); everything else crashes loudly rather than silently thinning results.
    """
    try:
        conn.execute(
            "CREATE VIRTUAL TABLE fts USING "
            "fts5(title, body, type UNINDEXED, slug UNINDEXED, path UNINDEXED)"
        )
    except sqlite3.OperationalError as err:
        raise LocatedError.fts_unavailable(str(err)) from None
    rows: list[tuple[str, str, str, str, str]] = []
    for tname, slug in sorted(index.nodes):
        if type_ is not None and tname != type_:
            continue
        path = entity_path(root, index.resolved.types[tname], slug)
        try:
            # utf-8 pinned: frontmatter.load (the scan) decodes utf-8, so a locale
            # default here would index mojibake tokens the scan never saw.
            text = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        meta = index.meta[(tname, slug)]
        title = str(meta.get("title") or meta.get("name") or slug)
        rows.append((title, frontmatter.loads(text).content, tname, slug, str(path.relative_to(root))))
    conn.executemany("INSERT INTO fts VALUES (?,?,?,?,?)", rows)


def _match(conn: sqlite3.Connection, text: str, *, limit: int) -> list[SearchHit]:
    """Run the MATCH, best (lowest) BM25 first; ties break on insertion (node) order.

    ``ORDER BY rank`` is FTS5's optimized bm25 ordering (no per-row recompute);
    ``snippet(..., -1, ...)`` picks the best-matching column, so a title hit
    excerpts the title and a body hit excerpts the body.
    """
    sql = (
        "SELECT type, slug, title, bm25(fts) AS score, "
        "snippet(fts, -1, '', '', '…', 12) AS snip, path "
        "FROM fts WHERE fts MATCH ? ORDER BY rank LIMIT ?"
    )
    try:
        rows = conn.execute(sql, (text, limit)).fetchall()
    except sqlite3.OperationalError as err:
        raise LocatedError.bad_search_query(text, str(err)) from None
    return [
        SearchHit(type=t, slug=s, title=title, score=score, snippet=snip, path=path)
        for t, s, title, score, snip, path in rows
    ]
