package explore

import (
	"errors"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/search"
)

// findLimit caps the hits a find lists; the total is always whole.
const findLimit = 20

// Find is the page's search (/api/search): the entities the text names
// outright, by id, slug or alias, which the full-text index does not hold,
// then the hits of `khub search --plain`, in its order.
func Find(root, q string, now time.Time) (*omap.Map, error) {
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	hits := []any{}
	listed := map[string]bool{}
	add := func(node index.Node, snippet string) {
		if listed[node.ID()] {
			return
		}
		listed[node.ID()] = true
		r := omap.New()
		r.Set("id", node.ID())
		r.Set("type", node.Type)
		r.Set("title", s.title(node))
		r.Set("snippet", snippet)
		r.Set("markers", s.markers(node).record())
		hits = append(hits, r)
	}

	q = strings.TrimSpace(q)
	total := 0
	if q != "" {
		node, err := entity.ResolveID(s.idx, q)
		var located *errs.Located
		switch {
		case err == nil:
			add(node, "")
		case errors.As(err, &located) && located.Code != "ambiguity_error": // a miss
		case located != nil: // several entities answer to it
			for _, id := range located.Expected {
				if typ, slug, ok := strings.Cut(id, "/"); ok && s.idx.Nodes[index.Node{Type: typ, Slug: slug}] {
					add(index.Node{Type: typ, Slug: slug}, "")
				}
			}
		default:
			return nil, err
		}
		named := len(hits)
		opts := search.Options{Limit: findLimit, Plain: true, Now: now}
		res, err := search.Over(root, s.idx, q, opts)
		if err != nil {
			return nil, err
		}
		shown := res.Hits
		if named > 0 && res.Total > len(res.Hits) {
			// Whether the text also finds what it names decides the total.
			opts.Limit = res.Total
			if res, err = search.Over(root, s.idx, q, opts); err != nil {
				return nil, err
			}
		}
		total = named
		for _, h := range res.Hits {
			if !listed[h.Type+"/"+h.Slug] {
				total++
			}
		}
		total += res.Total - len(res.Hits)
		for _, h := range shown {
			add(index.Node{Type: h.Type, Slug: h.Slug}, h.Snippet)
		}
	}
	out := omap.New()
	out.Set("q", q)
	out.Set("total", total)
	out.Set("hits", hits)
	out.Set("malformed", strList(s.malformed))
	return out, nil
}
