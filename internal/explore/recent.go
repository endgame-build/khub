package explore

import (
	"time"

	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/values"
)

// recentLimit caps the changes listed; the total is always whole.
const recentLimit = 50

// Recent is the entities `khub recent` lists for the window (/api/recent),
// newest first, each with who wrote it, whether it is new, and its markers.
func Recent(root string, days int, now time.Time) (*omap.Map, error) {
	limit := recentLimit
	report, err := gitlog.Recent(root, days, &limit, now)
	if err != nil {
		return nil, err
	}
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	rows := []any{}
	for _, e := range report.Entries {
		node := index.Node{Type: e.Type, Slug: e.Slug}
		if !s.idx.Nodes[node] {
			continue // written or removed between the two scans
		}
		meta := s.idx.Meta[node]
		created, _ := meta.Get("created")
		r := omap.New()
		r.Set("id", node.ID())
		r.Set("type", e.Type)
		r.Set("title", e.Title)
		r.Set("date", e.EffectiveDate.Format("2006-01-02"))
		r.Set("age", e.Age)
		r.Set("new", values.Str(created) == e.EffectiveDate.Format("2006-01-02"))
		r.Set("by", writer(meta))
		r.Set("markers", s.markers(node).record())
		rows = append(rows, r)
	}
	out := omap.New()
	out.Set("days", days)
	out.Set("total", report.Total)
	out.Set("rows", rows)
	out.Set("malformed", strList(s.malformed))
	return out, nil
}

// writer is who last wrote the entity's content, "" when nothing stamped it.
func writer(meta *omap.Map) string {
	raw, _ := meta.Get("generated")
	m, ok := raw.(*omap.Map)
	if !ok {
		return ""
	}
	by, _ := m.Get("by")
	return values.Str(by)
}
