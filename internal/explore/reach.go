package explore

import (
	"time"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
)

const (
	// reachDepth is how many links away a reach goes. The first is the
	// entity's own connections, which its page already lists.
	reachDepth = 3
	// reachLimit caps the entities listed per type and distance; totals are whole.
	reachLimit = 12
)

// Reach is what an entity leads to, and what leads to it, through other
// entities (/api/reach): per direction and distance, the entities of each
// type, each at its shortest distance only.
func Reach(root, id string, now time.Time) (*omap.Map, error) {
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	node, err := entity.ResolveID(s.idx, id)
	if err != nil {
		return nil, err
	}
	out := omap.New()
	out.Set("id", node.ID())
	out.Set("malformed", strList(s.malformed))
	for _, direction := range []string{graph.DirectionOut, graph.DirectionIn} {
		out.Set(direction, s.reach(node, direction))
	}
	return out, nil
}

func (s *snapshot) reach(from index.Node, direction string) []any {
	levels := []any{}
	seen := map[index.Node]bool{from: true}
	frontier := []index.Node{from}
	for depth := 1; depth <= reachDepth && len(frontier) > 0; depth++ {
		var next []index.Node
		for _, n := range frontier {
			for _, a := range graph.Adjacent(s.g, n, direction, nil) {
				if !seen[a.Node] {
					seen[a.Node] = true
					next = append(next, a.Node)
				}
			}
		}
		frontier = next
		if depth == 1 || len(next) == 0 {
			continue
		}
		byType := map[string][]index.Node{}
		for _, n := range next {
			byType[n.Type] = append(byType[n.Type], n)
		}
		types := []any{}
		for _, name := range s.resolved.Types.Keys() {
			nodes := byType[name]
			if len(nodes) == 0 {
				continue
			}
			index.SortNodes(nodes)
			items := []any{}
			for _, n := range nodes[:min(len(nodes), reachLimit)] {
				i := omap.New()
				i.Set("id", n.ID())
				i.Set("title", s.title(n))
				items = append(items, i)
			}
			t := omap.New()
			t.Set("type", name)
			t.Set("total", len(nodes))
			t.Set("items", items)
			types = append(types, t)
		}
		level := omap.New()
		level.Set("depth", depth)
		level.Set("total", len(next))
		level.Set("types", types)
		levels = append(levels, level)
	}
	return levels
}
