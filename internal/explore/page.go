package explore

import (
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
)

// Page is what one entity's view adds to the record `khub get --edges` prints
// (the fields /api/entity appends): its markers, its body as a block tree, and its
// connections grouped for reading, each neighbour summarized.
//
// Outgoing groups come from the stored values, so a target that resolves to
// nothing is still shown — as unresolved — exactly where `get --edges` shows
// it. Incoming groups come from the graph, keyed like the list's columns. Only
// groups holding a connection are returned, in column order.
func Page(root, id string, now time.Time) (*omap.Map, error) {
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	node, err := entity.ResolveID(s.idx, id)
	if err != nil {
		return nil, err
	}
	// A qualified id names one file. Resolution falls back to case-folding
	// and aliases, so an id this scan has no node for (a stray, which the
	// scan drops) could land on a different entity answering to the same
	// name — and the page would show that entity's body and connections
	// under this one's frontmatter. Only the exact node counts.
	if strings.Contains(id, "/") && node.ID() != id {
		return nil, errs.LookupError(id)
	}
	view, err := entity.View(root, s.idx, node, true)
	if err != nil {
		return nil, err
	}

	cols := Columns(s.resolved, node.Type)
	items := map[string][]*omap.Map{}
	seen := map[string]map[string]bool{}
	add := func(key, id string, item *omap.Map) {
		if seen[key] == nil {
			seen[key] = map[string]bool{}
		}
		if seen[key][id] {
			return
		}
		seen[key][id] = true
		items[key] = append(items[key], item)
	}
	for _, e := range view.Edges {
		if e.Derived {
			continue // incoming groups come from the graph below
		}
		key := DirOut + ":" + e.Predicate
		if len(e.ResolvedTargets) == 0 {
			add(key, e.Target, unresolved(e.Target))
			continue
		}
		for _, rid := range e.ResolvedTargets {
			typ, slug, _ := strings.Cut(rid, "/")
			n := index.Node{Type: typ, Slug: slug}
			add(key, rid, s.neighbour(n))
		}
	}
	isBase := func(predicate string) bool {
		return s.resolved.BaseRelations != nil && s.resolved.BaseRelations.Has(predicate)
	}
	for _, g := range s.g.PredicateGroups(node) {
		if g.Out {
			continue
		}
		key := columnKey(false, g.SourceType, g.Predicate, isBase(g.Predicate))
		for _, n := range g.Nodes {
			add(key, n.ID(), s.neighbour(n))
		}
	}

	groups := []any{}
	for _, c := range cols {
		list := items[c.Key]
		if len(list) == 0 {
			continue
		}
		g := omap.New()
		g.Set("key", c.Key)
		g.Set("label", c.Label)
		g.Set("direction", c.Direction)
		g.Set("base", c.Base)
		entries := make([]any, 0, len(list))
		for _, it := range list {
			entries = append(entries, it)
		}
		g.Set("items", entries)
		groups = append(groups, g)
	}

	out := omap.New()
	out.Set("markers", s.markers(node).record())
	out.Set("body_tree", bodyTree(s.idx.Body[node], s.linkResolver(node)))
	out.Set("neighbours", groups)
	return out, nil
}

// neighbour is one resolved linked entity, in the field order page_test.go pins.
func (s *snapshot) neighbour(n index.Node) *omap.Map {
	m := omap.New()
	m.Set("id", n.ID())
	m.Set("resolved", true)
	m.Set("type", n.Type)
	m.Set("title", s.title(n))
	m.Set("markers", s.markers(n).record())
	m.Set("summary", Summary(s.idx.Meta[n], s.idx.Body[n]))
	return m
}

// unresolved is a stored target that names no entity.
func unresolved(target string) *omap.Map {
	m := omap.New()
	m.Set("id", target)
	m.Set("resolved", false)
	m.Set("type", "")
	m.Set("title", "")
	m.Set("summary", "")
	return m
}

// linkResolver resolves a body's relative link against the entity's own file:
// a destination naming another entity's file becomes that entity's id.
func (s *snapshot) linkResolver(node index.Node) linkResolver {
	own, ok := s.idx.Paths[node]
	if !ok {
		return nil
	}
	dir := path.Dir(own)
	return func(dest string) (string, bool) {
		p := dest
		if i := strings.IndexAny(p, "#?"); i >= 0 {
			p = p[:i]
		}
		if p == "" {
			return "", false
		}
		if decoded, err := url.PathUnescape(p); err == nil {
			p = decoded
		}
		target := path.Clean(path.Join(dir, p))
		if strings.HasPrefix(target, "../") || target == ".." {
			return "", false
		}
		n, ok := s.idx.FileNode(target)
		if !ok {
			return "", false
		}
		return n.ID(), true
	}
}
