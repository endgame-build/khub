package retrieve

// The JSON document `khub retrieve` prints. It lives in the core rather than
// in the CLI because the byte cap is defined over these bytes: the cap and
// the printed document must be one rendering.

import (
	"github.com/endgame-build/khub/internal/omap"
)

// Render is the document: `{"count", "entities": [...], "omitted": {...}}`.
// Each entity is the `get --edges` record with `title`, the lifecycle flags
// and `reason` ahead of the frontmatter, so a reader sees why a record is
// here before its bulk; `body` is left out under --no-body.
func Render(res *Result) *omap.Map {
	entities := make([]any, 0, len(res.Records))
	for _, r := range res.Records {
		entities = append(entities, renderRecord(r, res.Body))
	}
	return renderDocument(res, len(res.Records), entities)
}

// renderDocument is the envelope around already rendered entities; the byte
// cap measures it with the real count and an empty list.
func renderDocument(res *Result, count int, entities []any) *omap.Map {
	doc := omap.New()
	doc.Set("count", count)
	doc.Set("entities", entities)
	doc.Set("omitted", renderOmitted(res.Omitted))
	return doc
}

func renderRecord(r Record, body bool) *omap.Map {
	record := omap.New()
	record.Set("id", r.Node.ID())
	record.Set("type", r.Node.Type)
	record.Set("slug", r.Node.Slug)
	record.Set("title", r.Title)
	record.Set("draft", r.Draft)
	record.Set("orphan", r.Orphan)
	record.Set("stale", r.Stale)
	record.Set("reason", renderReason(r.Reason))
	record.Set("path", r.Path)
	if r.View != nil {
		record.Set("frontmatter", r.View.Meta)
		if body {
			record.Set("body", r.View.Body)
		}
		if r.View.Locator != "" {
			record.Set("locator", r.View.Locator)
		}
		edges := make([]any, 0, len(r.View.Edges))
		for _, e := range r.View.Edges {
			edge := omap.New()
			edge.Set("predicate", e.Predicate)
			edge.Set("target", e.Target)
			edge.Set("derived", e.Derived)
			edges = append(edges, edge)
		}
		record.Set("edges", edges)
	}
	return record
}

func renderReason(reason Reason) *omap.Map {
	m := omap.New()
	m.Set("kind", reason.Kind)
	m.Set("depth", reason.Depth)
	if reason.Kind == ReasonSeed {
		if reason.Scored {
			m.Set("score", reason.Score)
			m.Set("snippet", reason.Snippet)
		}
		return m
	}
	m.Set("predicate", reason.Predicate)
	m.Set("direction", reason.Direction)
	m.Set("from", reason.From)
	return m
}

func renderOmitted(o Omitted) *omap.Map {
	named := o.IDs()
	ids := make([]any, 0, len(named))
	for _, id := range named {
		ids = append(ids, id)
	}
	m := omap.New()
	m.Set("limit", o.Limit)
	m.Set("max_entities", o.MaxEntities)
	m.Set("max_bytes", o.MaxBytes)
	m.Set("ids", ids)
	m.Set("truncated", o.Truncated())
	m.Set("insufficient", o.Insufficient)
	return m
}
