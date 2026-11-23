package explore

import (
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

// Markers are one entity's status flags and review tier — the draft, stale,
// orphan, incomplete and trust fields of a list row, and an entity page's
// markers record.
type Markers struct {
	Draft      bool
	Stale      bool
	Orphan     bool
	Incomplete bool
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
	incomplete map[index.Node]bool
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
	// flag and completeness verdict would be judged against a smaller graph
	// with nothing on the page to say so. Refuse, as viz, reindex and export
	// do; `khub check` names the files.
	if err := index.RejectMalformed(scanned, "build this view"); err != nil {
		return nil, err
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned))
	days, err := workspace.StaleDays(root)
	if err != nil {
		return nil, err
	}
	// check's own completeness verdicts, over this scan — not a second walk
	// of the tree, which at 2,000 entities doubled the cost of a list.
	found := integrity.IncompleteIn(resolved, idx)
	incomplete := make(map[index.Node]bool, len(found))
	for _, i := range found {
		incomplete[index.Node{Type: i.Type, Slug: i.Slug}] = true
	}
	return &snapshot{
		root: root, resolved: resolved, idx: idx, g: graph.BuildGraph(idx),
		staleDays: days, now: now, incomplete: incomplete,
	}, nil
}

// markers judges one entity with the same functions `khub query` calls per
// match (draft, orphan, stale, trust) plus check's completeness verdict. A
// draft is never incomplete: check does not judge drafts.
func (s *snapshot) markers(node index.Node) Markers {
	meta := s.idx.Meta[node]
	rtype, _ := s.resolved.Types.Get(node.Type)
	return Markers{
		Draft:      rtype.IsDraft(meta),
		Stale:      project.IsStale(meta, s.now, s.staleDays, nil),
		Orphan:     graph.IsOrphan(s.g, node, rtype),
		Incomplete: s.incomplete[node],
		Trust:      trust.Tier(rtype, meta, s.idx.Body[node]),
	}
}

// title is the listing title `khub query` prints.
func (s *snapshot) title(node index.Node) string {
	return query.Title(s.idx.Meta[node], node.Slug)
}

// record renders the markers; the field order is the JSON contract the page reads.
func (m Markers) record() *omap.Map {
	r := omap.New()
	r.Set("draft", m.Draft)
	r.Set("stale", m.Stale)
	r.Set("orphan", m.Orphan)
	r.Set("incomplete", m.Incomplete)
	r.Set("trust", m.Trust)
	return r
}
