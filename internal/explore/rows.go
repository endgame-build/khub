package explore

import (
	"sort"
	"time"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/project"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// Rows is one type's list (/api/rows): every entity of the type,
// strays excluded, in index order, each with its markers, its plain attribute
// values and, per relation column, the distinct entities it links to.
//
// The page sorts and filters; this only derives. Counts are len(links) per
// column, read off the same graph `khub query --count` reads; agree_test.go
// holds each column to `query --count` or, for an incoming relation with no
// declared inverse (which query cannot name), to `get --edges`.
func Rows(root, typeName string, now time.Time) (*omap.Map, error) {
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	rtype, ok := s.resolved.Types.Get(typeName)
	if !ok {
		return nil, unknownType(root, s.resolved, typeName)
	}
	cols := Columns(s.resolved, typeName)
	attrs := listAttributes(s.resolved, rtype)

	rows := []any{}
	for _, node := range s.idx.Order {
		if node.Type != typeName {
			continue
		}
		links := map[string][]string{}
		for _, g := range s.g.PredicateGroups(node) {
			key := columnKey(g.Out, g.SourceType, g.Predicate, isBase(s.resolved, g.Predicate))
			for _, n := range g.Nodes {
				links[key] = appendUnique(links[key], n.ID())
			}
		}
		rows = append(rows, s.row(node, attrs, cols, links))
	}

	attrRecords := []any{}
	for _, a := range attrs {
		r := omap.New()
		r.Set("name", a.Name)
		r.Set("value_type", a.BaseType)
		r.Set("enum", strList(a.Enum))
		attrRecords = append(attrRecords, r)
	}
	colRecords := []any{}
	for _, c := range cols {
		colRecords = append(colRecords, c.record())
	}
	out := omap.New()
	out.Set("type", typeName)
	out.Set("total", len(rows))
	out.Set("attributes", attrRecords)
	out.Set("relations", colRecords)
	out.Set("rows", rows)
	out.Set("malformed", strList(s.malformed))
	return out, nil
}

// row renders one row, in the field order rows_test.go pins. counts and links carry
// every column key, in column order, zeros included.
func (s *snapshot) row(node index.Node, attrs []*schema.ResolvedAttribute, cols []Column, links map[string][]string) *omap.Map {
	m := s.markers(node)
	meta := s.idx.Meta[node]
	r := omap.New()
	r.Set("id", node.ID())
	r.Set("slug", node.Slug)
	r.Set("title", s.title(node))
	m.into(r)
	r.Set("updated", day(meta))
	r.Set("by", writer(meta))
	av := omap.New()
	for _, a := range attrs {
		if v, ok := plainValue(meta, a.Name); ok {
			av.Set(a.Name, v)
		}
	}
	r.Set("attrs", av)
	counts, linked := omap.New(), omap.New()
	for _, c := range cols {
		ids := links[c.Key]
		counts.Set(c.Key, len(ids))
		linked.Set(c.Key, strList(ids))
	}
	r.Set("counts", counts)
	r.Set("links", linked)
	return r
}

// listAttributes are the attribute columns of a type's list: the attributes the
// type declares itself — a base attribute it redeclares (a status with its own
// values) included, the rest of the base block's bookkeeping and provenance
// not — whose values are plain (no mapping, no list of mappings). title is the
// row's own title column, so it is not repeated.
func listAttributes(resolved *schema.ResolvedSchema, rtype *schema.ResolvedType) []*schema.ResolvedAttribute {
	var out []*schema.ResolvedAttribute
	for _, name := range rtype.Attributes.Keys() {
		a, _ := rtype.Attributes.Get(name)
		if name == "title" || a.HoldsObjects() {
			continue
		}
		if !a.OverriddenFromBase && resolved.BaseAttributes != nil && resolved.BaseAttributes.Has(name) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// day is the date the entity is judged stale by, "" when it has none.
func day(meta *omap.Map) string {
	date, _ := project.EffectiveDate(meta, nil)
	if date == nil {
		return ""
	}
	return date.Format("2006-01-02")
}

// plainValue renders a stored attribute value as text, a list as a list of
// texts, so a comma in a value never reads as two. An absent or blank value is
// reported as absent.
func plainValue(meta *omap.Map, name string) (any, bool) {
	v, ok := meta.Get(name)
	if !ok || !values.Present(v) {
		return nil, false
	}
	if list, ok := v.([]any); ok {
		parts := make([]any, 0, len(list))
		for _, item := range list {
			parts = append(parts, values.Str(item))
		}
		return parts, true
	}
	return values.Str(v), true
}

func appendUnique(ids []string, id string) []string {
	for _, have := range ids {
		if have == id {
			return ids
		}
	}
	return append(ids, id)
}

// unknownType is the refusal viz and `khub schema show` give for a type the
// schema does not declare, naming the known ones in sorted order. A config
// that cannot be read is its own refusal, not a blank preset name.
func unknownType(root string, resolved *schema.ResolvedSchema, name string) error {
	prov, err := workspace.Provenance(root)
	if err != nil {
		return err
	}
	p, _ := prov.Get("preset")
	known := append([]string{}, resolved.Types.Keys()...)
	sort.Strings(known)
	return errs.UnknownType(name, values.Str(p), known)
}
