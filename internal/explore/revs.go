package explore

import (
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/trust"
)

// Revs is the content revision of each entity named (/api/revs): what
// `khub verify` would record a review of. nil for an id no entity has.
func Revs(root string, ids []string, now time.Time) (*omap.Map, error) {
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	revs := omap.New()
	for _, id := range ids {
		typ, slug, _ := strings.Cut(id, "/")
		node := index.Node{Type: typ, Slug: slug}
		if !s.idx.Nodes[node] {
			revs.Set(id, nil)
			continue
		}
		revs.Set(id, s.rev(node))
	}
	out := omap.New()
	out.Set("revs", revs)
	out.Set("malformed", strList(s.malformed))
	return out, nil
}

func (s *snapshot) rev(node index.Node) string {
	rtype, _ := s.resolved.Types.Get(node.Type)
	return trust.Rev(rtype, s.idx.Meta[node], s.idx.Body[node])
}
