package explore

import (
	"errors"
	"time"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/query"
	"github.com/endgame-build/khub/internal/savedquery"
)

// SavedQueries lists the workspace's saved queries with what each matches
// today (/api/queries). One that no longer runs carries its refusal instead.
func SavedQueries(root string, now time.Time) (*omap.Map, error) {
	store, err := savedquery.Load(root)
	if err != nil {
		return nil, err
	}
	list := []any{}
	for _, name := range store.Keys() {
		r := omap.New()
		r.Set("name", name)
		_, matched, err := runSaved(root, name, now)
		var located *errs.Located
		switch {
		case err == nil:
			r.Set("count", matched)
			r.Set("error", nil)
		case errors.As(err, &located):
			r.Set("count", nil)
			r.Set("error", located.Message)
		default:
			return nil, err
		}
		list = append(list, r)
	}
	out := omap.New()
	out.Set("queries", list)
	return out, nil
}

// SavedQuery is one saved query's rows (/api/query), as `khub run` lists them,
// with the markers every list carries.
func SavedQuery(root, name string, now time.Time) (*omap.Map, error) {
	matches, matched, err := runSaved(root, name, now)
	if err != nil {
		return nil, err
	}
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	rows := []any{}
	for _, m := range matches {
		node := index.Node{Type: m.Type, Slug: m.Slug}
		if !s.idx.Nodes[node] {
			continue // removed between the two scans
		}
		rows = append(rows, s.listed(node))
	}
	out := omap.New()
	out.Set("name", name)
	out.Set("matched", matched)
	out.Set("rows", rows)
	out.Set("malformed", strList(s.malformed))
	return out, nil
}

func runSaved(root, name string, now time.Time) ([]query.Match, int, error) {
	def, err := savedquery.Get(root, name)
	if err != nil {
		return nil, 0, err
	}
	filters, err := savedquery.Filters(name, def)
	if err != nil {
		return nil, 0, err
	}
	matches, matched, err := query.QueryCounted(root, filters, now)
	if err != nil {
		return nil, 0, savedquery.Outdated(name, err)
	}
	return matches, matched, nil
}
