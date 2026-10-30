// Package retrieve assembles bounded context in one call — the read verb an
// agent otherwise composes from search, get and neighbors by hand while
// managing its own budget.
//
// A retrieval has three parts. The seeds are a search (search.Over, so the
// FTS build and the ranking have one implementation) or explicit ids, capped
// by Limit BEFORE expansion. The expansion is a breadth-first walk from every
// seed over the derived graph — stored forward edges and their derived
// inverses, exactly the adjacency neighbors walks — where each predicate may
// be followed only up to its own depth. The caps then trim the periphery
// first: MaxEntities counts entities, MaxBytes counts the bytes of the JSON
// document khub prints, and both drop the deepest entities first, then the
// greatest (type, slug) within a depth, then the least relevant seed. No
// token budget: a tokenizer is model-specific and would be the one place in
// khub that needs one; bytes are what every caller can measure.
//
// Everything is deterministic by construction: seeds keep their relevance (or
// argument) order, the walk reads insertion-ordered adjacency, ties break on
// (type, slug), and no map is ranged on an output path.
package retrieve

import (
	"math"
	"path/filepath"
	"sort"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/project"
	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/search"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// DefaultLimit caps the seeds of a search when the caller sets none — the
// search default, so `retrieve <text>` seeds from what `search <text>` shows.
const DefaultLimit = 20

// MaxOmittedIDs bounds the ids an Omitted lists; the counts are exact.
const MaxOmittedIDs = 20

// Expansion is one `--expand` entry: a predicate and how many hops it may be
// followed for. A Depth of 0 declares the predicate and never follows it.
type Expansion struct {
	Predicate string
	Depth     int
}

// Options shapes one retrieval. Exactly one of Text and IDs seeds it.
type Options struct {
	Text  string  // a search, raw FTS5 MATCH syntax or plain words under Plain
	Plain bool    // Text is plain words
	Type  *string // restrict the search to one type; nil = every type
	IDs   []string

	// Expand lists the predicates the walk follows, each to its own depth.
	// Empty follows every predicate to Depth.
	Expand    []Expansion
	Direction string // graph.DirectionBoth, DirectionIn or DirectionOut
	Depth     int    // hops for a predicate without its own depth; 0 = seeds alone

	Limit       *int // seeds kept before expansion; nil = DefaultLimit for a search, every id otherwise
	MaxEntities int  // entities kept after expansion; <= 0 = unbounded
	MaxBytes    int  // bytes of the printed document; <= 0 = unbounded
	NoBody      bool // leave bodies out of the records (and of the byte count)
	Now         time.Time
}

// Reason kinds.
const (
	ReasonSeed     = "seed"
	ReasonExpanded = "expanded"
)

// Reason says why a record is in the result. A seed carries its depth (0)
// and, when it came from a search, the BM25 score and the snippet; an
// expanded record carries the hop it was reached at, the predicate, the
// direction and the id it was reached from — the first path the walk found.
type Reason struct {
	Kind    string
	Depth   int
	Score   float64
	Scored  bool // Score and Snippet are set: a search seed
	Snippet string

	Predicate string
	Direction string
	From      string // qualified id
}

// Record is one retrieved entity: its view (the `get` record), its lifecycle
// flags as query judges them, and the reason it is here.
type Record struct {
	Node   index.Node
	Path   string // workspace-relative, slash-separated
	Title  string
	Draft  bool
	Orphan bool
	Stale  bool
	Reason Reason
	View   *entity.EntityView
}

// Omitted is what the caps left out: a count per cap, every id each cap
// dropped in trim order, and whether a seed itself had to go. IDs is the
// bounded list the document names.
type Omitted struct {
	Limit          int
	MaxEntities    int
	MaxBytes       int
	LimitIDs       []string // the seeds Limit dropped, in relevance or argument order
	MaxEntitiesIDs []string // in trim order
	MaxBytesIDs    []string // in trim order
	Insufficient   bool

	// shrunk is how many fewer than MaxOmittedIDs the document names, raised
	// by trimBytes only when the records are gone and the envelope itself is
	// over the cap. The zero value names the full MaxOmittedIDs.
	shrunk int
}

// Truncated reports whether any cap dropped an entity.
func (o Omitted) Truncated() bool { return o.Limit > 0 || o.MaxEntities > 0 || o.MaxBytes > 0 }

// IDs is the bounded list the document names: at most MaxOmittedIDs, the
// cap-trimmed ids first (MaxEntities, then MaxBytes — what an agent cannot
// recover with another call), then what Limit dropped (which `search` or the
// agent's own id list can name again) while room remains.
func (o Omitted) IDs() []string {
	room := max(MaxOmittedIDs-o.shrunk, 0)
	out := make([]string, 0, min(room, len(o.MaxEntitiesIDs)+len(o.MaxBytesIDs)+len(o.LimitIDs)))
	for _, list := range [][]string{o.MaxEntitiesIDs, o.MaxBytesIDs, o.LimitIDs} {
		for _, id := range list {
			if len(out) == room {
				return out
			}
			out = append(out, id)
		}
	}
	return out
}

// Result is one retrieval: the records in output order — seeds first, in
// relevance or argument order, then the expansion in walk order — and what
// was left out. Search is set when the seeds came from a search: its Hits are
// the seeds kept and Total is every hit, for the caller's advisory note.
type Result struct {
	Records []Record
	Omitted Omitted
	Search  *search.Result
	Body    bool // the records carry bodies
	Seeds   int  // seeds after Limit, before the caps

	total int // trimBytes: the summed standalone size of Records
}

// Retrieve runs one retrieval over the workspace at root.
//
// An unknown Expand predicate, an unknown Type, or an id that does not
// resolve is a located error raised before any scan it can be raised before.
// An empty result is a success.
func Retrieve(root string, opts Options) (*Result, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	for _, e := range opts.Expand {
		if err := graph.RequirePredicate(resolved, e.Predicate); err != nil {
			return nil, err
		}
	}
	if opts.Text == "" && len(opts.IDs) == 0 {
		return nil, errs.New("no_seed", "Pass search text or --id <id> to seed the retrieval")
	}
	var scanned *index.Index
	if opts.Text != "" {
		scanned, err = index.BuildWithBodies(root, resolved)
	} else {
		scanned, err = index.Build(root, resolved)
	}
	if err != nil {
		return nil, err
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities
	staleDays, err := workspace.StaleDays(root)
	if err != nil {
		return nil, err
	}
	g := graph.BuildGraph(idx)
	res := &Result{Body: !opts.NoBody}

	seeds, err := seedsOf(root, idx, opts, res)
	if err != nil {
		return nil, err
	}
	res.Seeds = len(seeds)
	records := expand(g, seeds, opts)
	for i := range records {
		annotate(&records[i], idx, g, opts.Now, staleDays)
	}

	// Caps, periphery first. MaxEntities needs no file read; MaxBytes needs the
	// rendered records, so the views are read after the first cap and only for
	// what survived it.
	order := trimOrder(records)
	if opts.MaxEntities > 0 && len(records) > opts.MaxEntities {
		drop := order[:len(records)-opts.MaxEntities]
		records = trim(records, drop, &res.Omitted)
		order = trimOrder(records)
	}
	for i := range records {
		view, verr := entity.View(root, idx, records[i].Node, true)
		if verr != nil {
			return nil, verr
		}
		records[i].View = view
		records[i].Path = relPath(root, view.Path)
	}
	res.Records = records
	if opts.MaxBytes > 0 {
		if err := trimBytes(res, order, opts.MaxBytes); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// seedsOf resolves the seeds: the search hits (every hit, so the dropped ones
// can be named) cut to the limit, or the explicit ids in argument order with
// duplicates folded.
func seedsOf(root string, idx *index.Index, opts Options, res *Result) ([]Record, error) {
	var seeds []Record
	seen := map[index.Node]bool{}
	if opts.Text != "" {
		found, err := search.Over(root, idx, opts.Text, search.Options{
			Type: opts.Type, Limit: math.MaxInt32, Plain: opts.Plain, Now: opts.Now})
		if err != nil {
			return nil, err
		}
		limit := DefaultLimit
		if opts.Limit != nil {
			limit = max(*opts.Limit, 0)
		}
		for i, h := range found.Hits {
			node := index.Node{Type: h.Type, Slug: h.Slug}
			if i >= limit {
				res.Omitted.Limit++
				res.Omitted.LimitIDs = append(res.Omitted.LimitIDs, node.ID())
				continue
			}
			seen[node] = true
			seeds = append(seeds, Record{Node: node, Reason: Reason{
				Kind: ReasonSeed, Score: h.Score, Scored: true, Snippet: h.Snippet}})
		}
		kept := found
		kept.Hits = found.Hits[:min(limit, len(found.Hits))]
		res.Search = &kept
		return seeds, nil
	}
	for _, id := range opts.IDs {
		node, err := entity.ResolveID(idx, id)
		if err != nil {
			return nil, err
		}
		if seen[node] {
			continue
		}
		seen[node] = true
		seeds = append(seeds, Record{Node: node, Reason: Reason{Kind: ReasonSeed}})
	}
	if opts.Limit != nil && *opts.Limit < len(seeds) {
		cut := max(*opts.Limit, 0)
		for _, r := range seeds[cut:] {
			res.Omitted.Limit++
			res.Omitted.LimitIDs = append(res.Omitted.LimitIDs, r.Node.ID())
		}
		seeds = seeds[:cut]
	}
	return seeds, nil
}

// expand walks breadth-first from every seed. An edge of predicate p is
// followed at hop h only when h <= depth(p); a node joins the result the first
// time it is reached, with that first path as its reason. Seeds are never
// re-reached. Walk order is the output order: hop by hop, each frontier node
// in order, its out-edges then its in-edges in insertion order.
func expand(g *graph.Graph, seeds []Record, opts Options) []Record {
	depthOf := map[string]int{}
	maxHop := 0
	if len(opts.Expand) == 0 {
		maxHop = opts.Depth
	}
	for _, e := range opts.Expand {
		depthOf[e.Predicate] = e.Depth // a repeated predicate: the last one wins
	}
	for _, d := range depthOf {
		maxHop = max(maxHop, d)
	}
	allowed := func(predicate string, hop int) bool {
		if len(opts.Expand) == 0 {
			return hop <= opts.Depth
		}
		d, ok := depthOf[predicate]
		return ok && hop <= d
	}
	direction := opts.Direction
	if direction == "" {
		direction = graph.DirectionBoth
	}

	records := append([]Record(nil), seeds...)
	found := map[index.Node]bool{}
	frontier := make([]index.Node, 0, len(seeds))
	for _, s := range seeds {
		found[s.Node] = true
		frontier = append(frontier, s.Node)
	}
	for hop := 1; hop <= maxHop && len(frontier) > 0; hop++ {
		var next []index.Node
		for _, node := range frontier {
			for _, a := range graph.Adjacent(g, node, direction, nil) {
				if found[a.Node] || !allowed(a.Predicate, hop) {
					continue
				}
				found[a.Node] = true
				records = append(records, Record{Node: a.Node, Reason: Reason{
					Kind: ReasonExpanded, Depth: hop,
					Predicate: a.Predicate, Direction: a.Direction, From: node.ID(),
				}})
				next = append(next, a.Node)
			}
		}
		frontier = next
	}
	return records
}

// annotate sets a record's title and lifecycle flags, judged exactly as query
// judges them.
func annotate(r *Record, idx *index.Index, g *graph.Graph, now time.Time, staleDays int) {
	meta := idx.Meta[r.Node]
	rtype, _ := idx.Resolved.Types.Get(r.Node.Type)
	draft, _ := meta.Get("draft")
	r.Draft = values.AsBool(draft)
	r.Orphan = graph.IsOrphan(g, r.Node, rtype)
	r.Stale = project.IsStale(meta, now, staleDays, nil)
	r.Title = query.Title(meta, r.Node.Slug)
}

// trimOrder is the order the caps drop records in, as indices into records:
// the deepest first; within a depth the greatest (type, slug) first; among
// the seeds the last in relevance order first.
func trimOrder(records []Record) []int {
	order := make([]int, len(records))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ra, rb := records[order[a]], records[order[b]]
		if ra.Reason.Depth != rb.Reason.Depth {
			return ra.Reason.Depth > rb.Reason.Depth
		}
		if ra.Reason.Depth == 0 {
			return order[a] > order[b]
		}
		return rb.Node.Less(ra.Node)
	})
	return order
}

// trim drops the records at the given indices, keeping the others in output
// order, and books each drop against MaxEntities.
func trim(records []Record, drop []int, omitted *Omitted) []Record {
	gone := map[int]bool{}
	for _, i := range drop {
		gone[i] = true
	}
	for _, i := range drop {
		omitted.MaxEntities++
		omitted.MaxEntitiesIDs = append(omitted.MaxEntitiesIDs, records[i].Node.ID())
		if records[i].Reason.Depth == 0 {
			omitted.Insufficient = true
		}
	}
	kept := make([]Record, 0, len(records)-len(drop))
	for i, r := range records {
		if !gone[i] {
			kept = append(kept, r)
		}
	}
	return kept
}

// trimBytes drops records in trim order until the printed document fits in
// limit bytes. The document is `{"count", "entities": [...], "omitted"}`,
// and the CLI encoder joins a list's items with ", ", so each candidate size
// is exact arithmetic over one encoding per record plus one of the envelope,
// never a re-encoding of the whole document per drop. The encoder has the
// last word: the candidate is encoded once to confirm, and one more record
// goes if the arithmetic ever disagrees with it.
//
// The envelope has a floor. Once every record is gone it still carries the
// omitted ids, so those are shrunk from the tail until the document fits;
// with none left it is the irreducible floor — `count: 0`, `entities: []`
// and `omitted` with `ids: []`, under 150 bytes — and a cap below that is
// answered with the floor itself, every count still exact.
func trimBytes(res *Result, order []int, limit int) error {
	all := res.Records
	sizes := make([]int, len(all))
	for i, r := range all {
		doc, err := canon.EncodeCLI(renderRecord(r, res.Body))
		if err != nil {
			return err
		}
		sizes[i] = len(doc)
	}
	k := 0
	var trial *Result
	for {
		trial = trimmed(res, order[:k], sizes)
		size, err := documentSize(trial, trial.total)
		if err != nil {
			return err
		}
		if size <= limit {
			doc, err := canon.EncodeCLI(Render(trial))
			if err != nil {
				return err
			}
			if len(doc)+1 <= limit {
				break
			}
		}
		if k == len(all) {
			break
		}
		k++
	}
	for len(trial.Records) == 0 && trial.Omitted.shrunk < MaxOmittedIDs {
		doc, err := canon.EncodeCLI(Render(trial))
		if err != nil {
			return err
		}
		if len(doc)+1 <= limit {
			break
		}
		trial.Omitted.shrunk++
	}
	res.Records = trial.Records
	res.Omitted = trial.Omitted
	return nil
}

// trimmed is res with the records at drop taken out and booked against
// MaxBytes, plus the summed standalone size of what is kept.
func trimmed(res *Result, drop []int, sizes []int) *Result {
	out := &Result{Omitted: res.Omitted, Search: res.Search, Body: res.Body, Seeds: res.Seeds}
	out.Omitted.MaxBytesIDs = append([]string(nil), res.Omitted.MaxBytesIDs...)
	gone := map[int]bool{}
	for _, i := range drop {
		gone[i] = true
		out.Omitted.MaxBytes++
		out.Omitted.MaxBytesIDs = append(out.Omitted.MaxBytesIDs, res.Records[i].Node.ID())
		if res.Records[i].Reason.Depth == 0 {
			out.Omitted.Insufficient = true
		}
	}
	out.Records = make([]Record, 0, len(res.Records)-len(drop))
	for i, r := range res.Records {
		if !gone[i] {
			out.Records = append(out.Records, r)
			out.total += sizes[i]
		}
	}
	return out
}

// documentSize is the byte count of the printed document (newline included)
// for a trial whose records encode to total bytes in all: the envelope with
// the real count and an empty list, plus the items and their ", " separators.
func documentSize(trial *Result, total int) (int, error) {
	n := len(trial.Records)
	env, err := canon.EncodeCLI(renderDocument(trial, n, []any{}))
	if err != nil {
		return 0, err
	}
	size := len(env) + 1 // Emit prints the document and a newline
	if n > 0 {
		size += total + 2*(n-1)
	}
	return size, nil
}

// relPath is the workspace-relative, slash-separated path every record prints.
func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
