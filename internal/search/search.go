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
// snippet arguments match search.py; ranking is bm25 weighted towards the
// title (rankFTS). Rows are inserted in sorted node order, as Python inserts
// them, because bm25 ties break on rowid.
//
// NOTE: github.com/ncruces/go-sqlite3/embed must NOT be imported — since v0.35
// it is a deprecated no-op whose init() PRINTS to stdout, which would corrupt
// every `--format json` document.
package search

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/ext/fts5"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/project"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// The DDL, the snippet arguments, and `ORDER BY rank LIMIT ?` are contract.
// rank is bm25 weighted per column (rankFTS), and score reports that same
// weighted value, so the order and the score never disagree.
const (
	createFTS = "CREATE VIRTUAL TABLE fts USING " +
		"fts5(title, body, type UNINDEXED, slug UNINDEXED, path UNINDEXED)"
	insertFTS = "INSERT INTO fts VALUES (?,?,?,?,?)"
	matchFTS  = "SELECT type, slug, title, rank AS score, " +
		"snippet(fts, -1, '', '', '…', 12) AS snip, path, rowid " +
		"FROM fts WHERE fts MATCH ? ORDER BY rank LIMIT ?"
	countFTS = "SELECT count(*) FROM fts WHERE fts MATCH ?"
)

// Hit is one entity matching the search text, ranked by BM25 (best first).
// Locator ("path#slug") is set only for a collection row — its Path is the
// shared inventory file. Draft, Orphan and Stale are the verdicts query
// reports; Out and In count the hit's edges per predicate.
type Hit struct {
	Type    string
	Slug    string
	Title   string
	Draft   bool
	Orphan  bool
	Stale   bool
	Score   float64
	Snippet string
	Path    string
	Locator string // "" when not a collection row
	Out     []EdgeCount
	In      []EdgeCount

	// Match and TitleMatch are set for a plain search only: the
	// IDF-weighted share of the query's words this hit contains anywhere,
	// and in its title alone. Both run 0..1.
	Match      float64
	TitleMatch float64

	rowid int64
}

// Result is one search's hits plus what a caller needs to judge them.
type Result struct {
	Hits      []Hit
	Total     int  // every match, before Limit
	Searched  int  // entities indexed, after the type filter
	Malformed int  // files in the workspace the scan could not parse
	Capped    bool // a plain query held more than MaxPlainTerms words
}

// EdgeCount is how many edges one predicate carries at a hit. Only non-zero
// counts are listed.
type EdgeCount struct {
	Key string // Out: the predicate. In: "<source type>.<predicate>".
	N   int
}

// Options narrow and shape one search.
type Options struct {
	Type  *string   // nil = every type
	Limit int       // at most this many hits; zero or negative returns none
	Plain bool      // text is plain words, not FTS5 MATCH syntax
	Now   time.Time // the day stale is judged against
}

// Search returns the entities whose title or body match text, best BM25 rank
// first.
//
// An unknown opts.Type is a located error, same as query. An empty result is a
// success, not an error (QRY-002).
func Search(root, text string, opts Options) (Result, error) {
	type_ := opts.Type
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return Result{}, err
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
		return Result{}, errs.UnknownType(*type_, preset, known)
	}
	scanned, err := index.BuildWithBodies(root, resolved)
	if err != nil {
		return Result{}, err
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities
	staleDays, err := workspace.StaleDays(root)
	if err != nil {
		return Result{}, err
	}

	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = conn.Close() }() // Python's finally: conn.close() ignores errors

	res := Result{Hits: []Hit{}, Malformed: len(idx.Malformed)}
	if res.Searched, err = buildFTS(conn, root, idx, type_); err != nil {
		return Result{}, err
	}
	var terms []string
	if opts.Plain {
		terms, res.Capped = plainTerms(text)
		if len(terms) == 0 {
			return res, nil // no words to look for; MATCH rejects an empty expression
		}
		text = strings.Join(terms, " OR ")
	}
	capped := opts.Limit
	if capped < 0 {
		capped = 0 // a negative LIMIT is "unbounded" in SQLite
	}
	hits, err := match(conn, text, capped)
	if err != nil {
		return Result{}, err
	}
	res.Hits, res.Total = hits, len(hits)
	if len(hits) == capped { // the limit may have dropped rows; count them all
		if res.Total, err = count(conn, text); err != nil {
			return Result{}, err
		}
	}
	for i := range hits {
		rtype, ok := resolved.Types.Get(hits[i].Type)
		if ok && rtype.Storage.Layout == schema.LayoutCollection {
			hits[i].Locator = hits[i].Path + "#" + hits[i].Slug
		}
	}
	if len(hits) > 0 {
		annotate(hits, idx, opts.Now, staleDays)
	}
	if opts.Plain && len(hits) > 0 {
		if err := matchShares(conn, hits, terms, res.Searched); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// matchShares sets each hit's Match and TitleMatch. A word weighs
// ln((N − df + 0.5)/(df + 0.5) + 1), bm25's inverse document frequency with
// a +1 inside the log. FTS5 omits the +1 and so weighs a word in more than
// half the entities at zero, which would let one common word count for
// nothing; with it, common words weigh little and rare words weigh most.
func matchShares(conn *sqlite3.Conn, hits []Hit, terms []string, n int) error {
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.rowid
	}
	var total float64
	anywhere := make([]float64, len(hits))
	inTitle := make([]float64, len(hits))
	for _, term := range terms {
		df, err := count(conn, term)
		if err != nil {
			return err
		}
		idf := math.Log((float64(n-df)+0.5)/(float64(df)+0.5) + 1)
		total += idf
		for _, scope := range []struct {
			expr string
			sums []float64
		}{{term, anywhere}, {"title : " + term, inTitle}} {
			found, err := rowidsMatching(conn, scope.expr, ids)
			if err != nil {
				return err
			}
			for i, h := range hits {
				if found[h.rowid] {
					scope.sums[i] += idf
				}
			}
		}
	}
	for i := range hits {
		hits[i].Match = math.Round(anywhere[i]/total*1000) / 1000
		hits[i].TitleMatch = math.Round(inTitle[i]/total*1000) / 1000
	}
	return nil
}

// rowidBatch bounds the ids bound into one query, well under SQLite's cap
// on bound parameters (32766), so a large --limit cannot exceed it.
const rowidBatch = 500

// rowidsMatching returns which of ids match the FTS5 expression.
func rowidsMatching(conn *sqlite3.Conn, expr string, ids []int64) (map[int64]bool, error) {
	found := map[int64]bool{}
	for start := 0; start < len(ids); start += rowidBatch {
		batch := ids[start:min(start+rowidBatch, len(ids))]
		sql := "SELECT rowid FROM fts WHERE fts MATCH ? AND rowid IN (?" +
			strings.Repeat(",?", len(batch)-1) + ")"
		stmt, _, err := conn.Prepare(sql)
		if err != nil {
			return nil, err
		}
		err = stmt.BindText(1, expr)
		for i := 0; err == nil && i < len(batch); i++ {
			err = stmt.BindInt64(i+2, batch[i])
		}
		for err == nil && stmt.Step() {
			found[stmt.ColumnInt64(0)] = true
		}
		if err == nil {
			err = stmt.Err()
		}
		if cerr := stmt.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}

// count returns how many rows match the FTS5 expression.
func count(conn *sqlite3.Conn, expr string) (int, error) {
	stmt, _, err := conn.Prepare(countFTS)
	if err != nil {
		return 0, errs.BadSearchQuery(expr, sqliteDetail(err))
	}
	defer func() { _ = stmt.Close() }()
	if err := stmt.BindText(1, expr); err != nil {
		return 0, err
	}
	n := 0
	if stmt.Step() {
		n = stmt.ColumnInt(0)
	}
	if err := stmt.Err(); err != nil {
		return 0, errs.BadSearchQuery(expr, sqliteDetail(err))
	}
	return n, nil
}

// annotate sets each hit's lifecycle flags, judged exactly as query judges
// them, and its per-predicate edge counts. Out follows the type's relation
// declaration order; In is sorted by key.
func annotate(hits []Hit, idx *index.Index, now time.Time, staleDays int) {
	g := graph.BuildGraph(idx)
	for i := range hits {
		node := index.Node{Type: hits[i].Type, Slug: hits[i].Slug}
		meta := idx.Meta[node]
		rtype, _ := idx.Resolved.Types.Get(node.Type)
		draft, _ := meta.Get("draft")
		hits[i].Draft = values.AsBool(draft)
		hits[i].Orphan = graph.IsOrphan(g, node, rtype)
		hits[i].Stale = project.IsStale(meta, now, staleDays, nil)

		out := map[string]int{}
		for _, e := range g.OutEdges(node) {
			out[e.Predicate]++
		}
		for _, predicate := range rtype.Relations.Keys() {
			if n := out[predicate]; n > 0 {
				hits[i].Out = append(hits[i].Out, EdgeCount{Key: predicate, N: n})
			}
		}
		in := map[string]int{}
		for _, e := range g.InEdges(node) {
			in[e.From.Type+"."+e.Predicate]++
		}
		keys := make([]string, 0, len(in))
		for k := range in {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			hits[i].In = append(hits[i].In, EdgeCount{Key: k, N: in[k]})
		}
	}
}

// buildFTS populates the in-memory FTS5 table with each entity's title and full
// body — _build_fts.
//
// Full body, not a prefix — recall beats the trivial index-size saving at this
// scale. type_ prunes at build time: with exactly one query per in-memory build,
// skipping the row loop beats a WHERE clause. Titles and bodies come from the
// scan (index.BuildWithBodies), so no file is read or parsed twice.
func buildFTS(conn *sqlite3.Conn, root string, idx *index.Index, type_ *string) (int, error) {
	// The Python build either has FTS5 compiled in or it does not; here the
	// extension is loaded per connection, so a load failure is the same
	// "no FTS5 in this build" condition and takes the same located error.
	if err := fts5.Register(conn); err != nil {
		return 0, errs.FTSUnavailable(sqliteDetail(err))
	}
	if err := conn.Exec(createFTS); err != nil {
		return 0, errs.FTSUnavailable(sqliteDetail(err))
	}
	if err := conn.Exec(rankFTS()); err != nil {
		return 0, err
	}
	if err := conn.Exec("BEGIN"); err != nil {
		return 0, err
	}
	defer func() { _ = conn.Exec("ROLLBACK") }()
	stmt, _, err := conn.Prepare(insertFTS)
	if err != nil {
		return 0, err
	}
	defer func() { _ = stmt.Close() }()

	nodes := append([]index.Node(nil), idx.Order...)
	index.SortNodes(nodes)

	rows := 0
	for _, node := range nodes {
		if type_ != nil && node.Type != *type_ {
			continue
		}
		rtype, ok := idx.Resolved.Types.Get(node.Type)
		if !ok {
			continue
		}
		meta := idx.Meta[node]
		path := entity.EntityPath(root, rtype, node.Slug)
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		// Comments out, fences IN (kb `_fts_body`). A scaffold's hint comments
		// are the template's words, not the author's, so indexing them made
		// every freshly-added entity a strong hit for whatever its own hints
		// happened to say. Fenced code is the opposite: a mermaid diagram names
		// the components, a bash block names the command, and search is how an
		// agent finds them. Removed rather than blanked: this text is what
		// snippet() renders back, and a blanked scaffold's snippet is mostly
		// empty columns.
		if err := insertRow(stmt, ftsTitle(meta, node.Slug),
			canon.FTSBody(meta, template.StripComments(idx.Body[node])),
			node.Type, node.Slug, filepath.ToSlash(rel)); err != nil {
			return 0, err
		}
		rows++
	}
	return rows, conn.Exec("COMMIT")
}

// Column weights for bm25: a word in the title counts titleWeight times a
// word in the body. Chosen against the relevance suite (relevance_test.go).
var titleWeight, bodyWeight = 5.0, 1.0

// rankFTS configures the table's rank function to the weighted bm25.
func rankFTS() string {
	return fmt.Sprintf("INSERT INTO fts(fts, rank) VALUES('rank', 'bm25(%s, %s)')",
		strconv.FormatFloat(titleWeight, 'f', -1, 64), strconv.FormatFloat(bodyWeight, 'f', -1, 64))
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

// MaxPlainTerms caps the distinct words a plain query looks for.
const MaxPlainTerms = 64

// plainTerms turns plain text into FTS5 terms, any of which a hit may match.
// Each distinct word (case-insensitive, first spelling kept) is quoted and
// prefix-matched from three runes up. A word holds only letters, digits and
// combining marks, so no input can become an FTS5 operator. Words past
// MaxPlainTerms are dropped, and capped reports it.
func plainTerms(text string) (terms []string, capped bool) {
	seen := map[string]bool{}
	for _, word := range strings.FieldsFunc(text, notWordRune) {
		key := strings.ToLower(word)
		if seen[key] {
			continue
		}
		if len(terms) == MaxPlainTerms {
			return terms, true
		}
		seen[key] = true
		term := `"` + word + `"`
		if utf8.RuneCountInString(word) >= 3 {
			term += "*"
		}
		terms = append(terms, term)
	}
	return terms, false
}

func notWordRune(r rune) bool { return !unicode.In(r, unicode.L, unicode.N, unicode.M) }

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
			rowid:   stmt.ColumnInt64(6),
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
// SQLite's own errmsg — still version-dependent, which is the
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
