package explore

import (
	"slices"
	"time"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/trust"
)

// markedLimit caps the rows a marked list holds; its count is always whole.
const markedLimit = 1000

// MarkedNames are the lists Marked answers for: each marker, and unverified for
// the entities no review holds for.
var MarkedNames = append([]string{trust.Unverified}, markerNames...)

// Marked is every entity carrying one marker, across types (/api/marked), in
// the schema's type order and then by slug. Its count is the sum of what
// Overview counts per type.
func Marked(root, name string, now time.Time) (*omap.Map, error) {
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	byType := map[string][]index.Node{}
	matched := 0
	for _, node := range s.idx.Order {
		m := s.markers(node)
		set := name == trust.Unverified && m.Trust == trust.Unverified
		if slices.Contains(markerNames, name) {
			v, _ := m.record().Get(name)
			set = v == true
		}
		if set {
			byType[node.Type] = append(byType[node.Type], node)
			matched++
		}
	}
	rows := []any{}
	for _, typ := range s.resolved.Types.Keys() {
		nodes := byType[typ]
		index.SortNodes(nodes)
		for _, node := range nodes {
			if len(rows) < markedLimit {
				rows = append(rows, s.listed(node))
			}
		}
	}
	out := omap.New()
	out.Set("name", name)
	out.Set("matched", matched)
	out.Set("rows", rows)
	out.Set("malformed", strList(s.malformed))
	return out, nil
}

// listed is one entity in a list across types.
func (s *snapshot) listed(node index.Node) *omap.Map {
	r := omap.New()
	r.Set("id", node.ID())
	r.Set("type", node.Type)
	r.Set("slug", node.Slug)
	r.Set("title", s.title(node))
	r.Set("markers", s.markers(node).record())
	return r
}
