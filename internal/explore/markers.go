package explore

import (
	"slices"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/project"
	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/trust"
	"github.com/endgame-build/khub/internal/workspace"
)

// Markers are one entity's status flags and review tier, as a list row and an
// entity page carry them.
type Markers struct {
	Draft      bool
	Stale      bool
	Orphan     bool
	Incomplete bool
	Expired    bool // past its own stale_after
	Superseded bool // its newest review saw an earlier version of the content
	Trust      string
}

// snapshot is one consistent read of the workspace: the scan, the graph and
// the check's completeness verdicts, taken once per request so every row and
// every neighbour on a page is judged against the same tree.
type snapshot struct {
	root       string
	resolved   *schema.ResolvedSchema
	idx        *index.Index // bodies loaded, strays dropped — what query and check read
	g          *graph.Graph
	staleDays  int
	now        time.Time
	incomplete map[index.Node]integrity.Incomplete
	malformed  []string // files the scan could not parse, so nothing here counts
}

// load scans root the way `khub query` does (bodies feed trust.Rev, strays are
// not entities) and takes the completeness verdicts from integrity, the code
// `khub check` runs, waivers applied, so "incomplete" has exactly one
// definition.
func load(root string, now time.Time) (*snapshot, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	scanned, err := index.BuildWithBodies(root, resolved)
	if err != nil {
		return nil, err
	}
	// A file that does not parse is not in the scan, so every count, orphan
	// flag and completeness verdict is judged against a smaller graph. viz,
	// reindex and export refuse, since what they write outlives the fault; a
	// page is read now, so it shows the rest and names the files.
	idx := index.Filter(scanned, index.StrayNodes(scanned))
	days, err := workspace.StaleDays(root)
	if err != nil {
		return nil, err
	}
	// check's own completeness verdicts, over this scan — not a second walk
	// of the tree, which at 2,000 entities doubled the cost of a list.
	found := integrity.IncompleteIn(resolved, idx)
	incomplete := make(map[index.Node]integrity.Incomplete, len(found))
	for _, i := range found {
		incomplete[index.Node{Type: i.Type, Slug: i.Slug}] = i
	}
	return &snapshot{
		root: root, resolved: resolved, idx: idx, g: graph.BuildGraph(idx),
		staleDays: days, now: now, incomplete: incomplete, malformed: scanned.Malformed,
	}, nil
}

// markers judges one entity with the same functions `khub query` calls per
// match (draft, orphan, stale, trust) plus check's completeness verdict. A
// draft is never incomplete: check does not judge drafts.
func (s *snapshot) markers(node index.Node) Markers {
	meta := s.idx.Meta[node]
	rtype, _ := s.resolved.Types.Get(node.Type)
	_, incomplete := s.incomplete[node]
	return Markers{
		Draft:      rtype.IsDraft(meta),
		Stale:      project.IsStale(meta, s.now, s.staleDays, nil),
		Orphan:     graph.IsOrphan(s.g, node, rtype),
		Incomplete: incomplete,
		Expired:    project.Expired(meta, s.now),
		Superseded: s.superseded(node),
		Trust:      trust.Tier(rtype, meta, s.idx.Body[node]),
	}
}

// superseded reports whether the newest review saw an earlier version of the
// content. A newer review that still holds answers an older one that does not.
func (s *snapshot) superseded(node index.Node) bool {
	rtype, _ := s.resolved.Types.Get(node.Type)
	meta := s.idx.Meta[node]
	newest := trust.NewestReview(meta)
	return newest >= 0 && slices.Contains(trust.Superseded(rtype, meta, s.idx.Body[node]), newest)
}

// title is the listing title `khub query` prints.
func (s *snapshot) title(node index.Node) string {
	return query.Title(s.idx.Meta[node], node.Slug)
}

// record renders the markers; the field order is the JSON contract the page reads.
func (m Markers) record() *omap.Map {
	r := omap.New()
	m.into(r)
	return r
}

// into sets the markers on r, after what it already holds.
func (m Markers) into(r *omap.Map) {
	r.Set("draft", m.Draft)
	r.Set("stale", m.Stale)
	r.Set("orphan", m.Orphan)
	r.Set("incomplete", m.Incomplete)
	r.Set("expired", m.Expired)
	r.Set("superseded", m.Superseded)
	r.Set("trust", m.Trust)
}

// missing says what an incomplete entity lacks.
func missing(i integrity.Incomplete) string {
	return "Missing " + strings.Join(append(slices.Clone(i.MissingFields), i.MissingRelations...), ", ")
}

// isBase reports whether a predicate is one every type inherits.
func isBase(resolved *schema.ResolvedSchema, predicate string) bool {
	return resolved.BaseRelations != nil && resolved.BaseRelations.Has(predicate)
}
