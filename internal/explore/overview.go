package explore

import (
	"time"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/trust"
)

// markerNames are the markers a count is kept of, in the order the page shows
// them: what a reviewer acts on first leads.
var markerNames = []string{"incomplete", "superseded", "expired", "orphan", "draft", "stale"}

// Overview is the workspace in numbers (/api/overview): per type, its count,
// its markers and its review tiers; per relation, its links, how many entities
// of its type have one, and the types it really reaches.
//
// Every number is a sum over what Rows reports for the same scan, so a count
// on the overview is the length of the list it leads to (overview_test.go).
func Overview(root string, now time.Time) (*omap.Map, error) {
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	type tally struct {
		count   int
		markers map[string]int
		tiers   map[string]int
	}
	tallies := map[string]*tally{}
	for _, name := range s.resolved.Types.Keys() {
		tallies[name] = &tally{markers: map[string]int{}, tiers: map[string]int{}}
	}
	type use struct {
		links, have int
		to          map[string]int
	}
	uses := map[string]*use{} // "<type>.<predicate>"
	useOf := func(typ, predicate string) *use {
		k := typ + "." + predicate
		if uses[k] == nil {
			uses[k] = &use{to: map[string]int{}}
		}
		return uses[k]
	}
	for _, node := range s.idx.Order {
		t := tallies[node.Type]
		t.count++
		m := s.markers(node)
		record := m.record()
		for _, name := range markerNames {
			if set, _ := record.Get(name); set == true {
				t.markers[name]++
			}
		}
		t.tiers[m.Trust]++
		for _, g := range s.g.PredicateGroups(node) {
			if !g.Out || len(g.Nodes) == 0 {
				continue
			}
			u := useOf(node.Type, g.Predicate)
			u.have++
			u.links += len(g.Nodes)
			for _, n := range g.Nodes {
				u.to[n.Type]++
			}
		}
	}

	types, relations := []any{}, []any{}
	for _, name := range s.resolved.Types.Keys() {
		t := tallies[name]
		rtype, _ := s.resolved.Types.Get(name)
		r := omap.New()
		r.Set("name", name)
		r.Set("count", t.count)
		markers := omap.New()
		for _, m := range markerNames {
			markers.Set(m, t.markers[m])
		}
		r.Set("markers", markers)
		tiers := omap.New()
		for _, tier := range trust.Tiers {
			tiers.Set(tier, t.tiers[tier])
		}
		r.Set("trust", tiers)
		types = append(types, r)

		for _, predicate := range rtype.Relations.Keys() {
			u := useOf(name, predicate)
			rel := omap.New()
			rel.Set("from", name)
			rel.Set("predicate", predicate)
			rel.Set("base", isBase(s.resolved, predicate))
			rel.Set("links", u.links)
			rel.Set("have", u.have)
			rel.Set("total", t.count)
			reach := []any{}
			for _, target := range s.resolved.Types.Keys() {
				if n := u.to[target]; n > 0 {
					e := omap.New()
					e.Set("type", target)
					e.Set("links", n)
					reach = append(reach, e)
				}
			}
			rel.Set("to", reach)
			relations = append(relations, rel)
		}
	}
	out := omap.New()
	out.Set("types", types)
	out.Set("relations", relations)
	out.Set("malformed", strList(s.malformed))
	return out, nil
}
