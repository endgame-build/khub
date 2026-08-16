// Package search ports src/khub/core/search.py — full-text search over title
// and body (the SQLite FTS5 projection).
//
// The one sanctioned body reader: query filters frontmatter and derived edges
// only, never prose (QRY-001); search exists precisely to match prose. The FTS5
// index is built in :memory: per invocation from the live tree — a derived
// projection, never a source of truth, and never stale. Raw FTS5 MATCH syntax
// passes through, so an agent gets "quoted phrases", OR, NEAR, and prefix*; a
// malformed expression is a located error, not a panic.
//
// SQLite is ncruces/go-sqlite3, CGO-free (a Go translation of SQLite; no wasm
// runtime, no build tags). FTS5 is a registerable extension there, not a build
// flag, so every connection registers it before the DDL runs. The DDL and the
// MATCH/bm25/snippet SQL are byte-identical to search.py, and rows are inserted
// in the same order Python inserts them (sorted index nodes) because bm25 ties
// break on rowid.
//
// NOTE: github.com/ncruces/go-sqlite3/embed must NOT be imported — since v0.35
// it is a deprecated no-op whose init() PRINTS to stdout, which would corrupt
// every `--format json` document.
package search

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/ext/fts5"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// The SQL is byte-identical to search.py — the DDL, the projection list, the
// snippet arguments, and `ORDER BY rank LIMIT ?` are all contract.
const (
	createFTS = "CREATE VIRTUAL TABLE fts USING " +
		"fts5(title, body, type UNINDEXED, slug UNINDEXED, path UNINDEXED)"
	insertFTS = "INSERT INTO fts VALUES (?,?,?,?,?)"
	matchFTS  = "SELECT type, slug, title, bm25(fts) AS score, " +
		"snippet(fts, -1, '', '', '…', 12) AS snip, path " +
		"FROM fts WHERE fts MATCH ? ORDER BY rank LIMIT ?"
)

// Hit is one entity matching the search text, ranked by BM25 (best first).
// Locator ("path#slug") is set only for a collection row — its Path is the
// shared inventory file.
type Hit struct {
	Type    string
	Slug    string
	Title   string
	Score   float64
	Snippet string
	Path    string
	Locator string // "" when not a collection row
}

// Search returns the entities whose title or body match text, best BM25 rank
// first.
//
// type_ (nil = no filter) narrows to one entity type; an unknown type is a
// located error, same as query. An empty result is a success, not an error
// (QRY-002).
func Search(root, text string, type_ *string, limit int) ([]Hit, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	if type_ != nil && !resolved.Types.Has(*type_) { // before the corpus scan
		preset := "the"
		if prov, perr := workspace.Provenance(root); perr == nil {
			if p, _ := prov.Get("preset"); p != nil && p != "" {
				preset, _ = p.(string)
			}
		}
		known := append([]string{}, resolved.Types.Keys()...)
		sort.Strings(known)
		return nil, errs.UnknownType(*type_, preset, known)
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities

	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }() // Python's finally: conn.close() ignores errors

	if err := buildFTS(conn, root, idx, type_); err != nil {
		return nil, err
	}
	capped := limit
	if capped < 0 {
		capped = 0 // a negative LIMIT is "unbounded" in SQLite
	}
	hits, err := match(conn, text, capped)
	if err != nil {
		return nil, err
	}
	for i := range hits {
		rtype, ok := resolved.Types.Get(hits[i].Type)
		if ok && rtype.Storage.Layout == schema.LayoutCollection {
			hits[i].Locator = hits[i].Path + "#" + hits[i].Slug
		}
	}
	return hits, nil
}

// buildFTS populates the in-memory FTS5 table with each entity's title and full
// body — _build_fts.
//
// Full body, not a prefix — recall beats the trivial index-size saving at this
// scale. type_ prunes at build time: with exactly one query per in-memory build,
// skipping the row loop beats a WHERE clause (no wasted body reads). Only the
// file read is guarded (a file deleted between scan and read is skipped);
// everything else fails loudly rather than silently thinning results.
func buildFTS(conn *sqlite3.Conn, root string, idx *index.Index, type_ *string) error {
	// The Python build either has FTS5 compiled in or it does not; here the
	// extension is loaded per connection, so a load failure is the same
	// "no FTS5 in this build" condition and takes the same located error.
	if err := fts5.Register(conn); err != nil {
		return errs.FTSUnavailable(sqliteDetail(err))
	}
	if err := conn.Exec(createFTS); err != nil {
		return errs.FTSUnavailable(sqliteDetail(err))
	}
	stmt, _, err := conn.Prepare(insertFTS)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	nodes := append([]index.Node(nil), idx.Order...)
	index.SortNodes(nodes)

	// One read+parse per COLLECTION FILE (not per row): cached rows per type.
	collections := map[string]*omap.Map{}
	for _, node := range nodes {
		if type_ != nil && node.Type != *type_ {
			continue
		}
		rtype, ok := idx.Resolved.Types.Get(node.Type)
		if !ok {
			continue
		}
		path := entity.EntityPath(root, rtype, node.Slug)
		var meta *omap.Map
		var body string
		if rtype.Storage.Layout == schema.LayoutCollection {
			if _, cached := collections[node.Type]; !cached {
				// loud on race corruption, like the per-item parse below
				text, rerr := canon.ReadText(path)
				if rerr != nil {
					return rerr
				}
				rows, lerr := canon.LoadCollection(text, rtype.Storage.Fmt)
				if lerr != nil {
					return lerr
				}
				collections[node.Type] = rows
			}
			rowAny, has := collections[node.Type].Get(node.Slug)
			if !has {
				continue // row vanished between scan and read — skip, never a brick
			}
			row, isMap := rowAny.(*omap.Map)
			if !isMap {
				continue
			}
			meta, body, err = canon.SplitRow(row, rtype.Storage.Fmt)
			if err != nil {
				return err
			}
		} else {
			// utf-8 pinned: the scan decodes utf-8, so a locale default here
			// would index mojibake tokens the scan never saw.
			text, rerr := canon.ReadText(path)
			if rerr != nil {
				continue // deleted between scan and read — skip, never a brick
			}
			meta, body, err = canon.Parse(text, rtype.Storage.Fmt) // loud on race corruption
			if err != nil {
				return err
			}
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		if err := insertRow(stmt, ftsTitle(meta, node.Slug), canon.FTSBody(meta, body),
			node.Type, node.Slug, filepath.ToSlash(rel)); err != nil {
			return err
		}
	}
	return nil
}

func insertRow(stmt *sqlite3.Stmt, cols ...string) error {
	if err := stmt.Reset(); err != nil {
		return err
	}
	for i, c := range cols {
		if err := stmt.BindText(i+1, c); err != nil {
			return err
		}
	}
	return stmt.Exec()
}

// ftsTitle is `str(meta.get("title") or meta.get("name") or slug)`.
func ftsTitle(meta *omap.Map, slug string) string {
	for _, key := range []string{"title", "name"} {
		if v, _ := meta.Get(key); values.Truthy(v) {
			return values.Str(v)
		}
	}
	return slug
}

// match runs the MATCH, best (lowest) BM25 first; ties break on insertion (node)
// order — _match.
//
// `ORDER BY rank` is FTS5's optimized bm25 ordering (no per-row recompute);
// snippet(..., -1, ...) picks the best-matching column, so a title hit excerpts
// the title and a body hit excerpts the body.
func match(conn *sqlite3.Conn, text string, limit int) ([]Hit, error) {
	stmt, _, err := conn.Prepare(matchFTS)
	if err != nil {
		return nil, errs.BadSearchQuery(text, sqliteDetail(err))
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.BindText(1, text); err != nil {
		return nil, err
	}
	if err := stmt.BindInt(2, limit); err != nil {
		return nil, err
	}
	hits := []Hit{}
	for stmt.Step() {
		hits = append(hits, Hit{
			Type:    stmt.ColumnText(0),
			Slug:    stmt.ColumnText(1),
			Title:   stmt.ColumnText(2),
			Score:   stmt.ColumnFloat(3),
			Snippet: stmt.ColumnText(4),
			Path:    stmt.ColumnText(5),
		})
	}
	if err := stmt.Err(); err != nil {
		return nil, errs.BadSearchQuery(text, sqliteDetail(err))
	}
	return hits, nil
}

// sqliteDetail reduces a driver error to sqlite3_errmsg(), which is exactly what
// CPython's sqlite3 module puts in str(OperationalError).
//
// ncruces renders `sqlite3: <result-code text>: <errmsg>` (its
// ErrorCodeString already carries the "sqlite3: " prefix), so both leading
// segments are noise Python never emits. No result-code text contains ": ", so
// cutting at the first one after the prefix is unambiguous. What survives is
// SQLite's own errmsg — still version-dependent (go-port-plan R10), which is the
// accepted delta.
func sqliteDetail(err error) string {
	var serr *sqlite3.Error
	if !errors.As(err, &serr) {
		return err.Error()
	}
	msg := strings.TrimPrefix(serr.Error(), "sqlite3: ")
	if i := strings.Index(msg, ": "); i >= 0 {
		return msg[i+2:]
	}
	return msg
}
