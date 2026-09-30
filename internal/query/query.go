// Package query ports src/khub/core/query.py — entity filtering behind
// `khub query`.
//
// ANDs a set of filters (type, frontmatter field, tag, --has/--missing predicate
// presence, orphan/stale flags, draft scope) over the entity index and the
// resolved edge graph. Filters read frontmatter and derived edges only, never
// body prose (QRY-001). An empty result is a success, not an error (QRY-002).
// Every match carries its orphan and stale flags (QRY-009), computed identically
// to `khub status`. A field naming an undeclared attribute/relation of the
// filtered type is a located error (QRY-001).
package query

import (
	"sort"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/project"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// Match is one entity passing every filter, with its derived health flags.
type Match struct {
	Type   string
	Slug   string
	Draft  bool
	Orphan bool
	Stale  bool
	// Carried so listing a type does not cost one `get` per row.
	Title string
}

// Filters is core.query.QueryFilters: the ANDed filter set parsed from the CLI.
// Every pointer field is Python's None when nil — `--type ""` and an absent
// `--type` are NOT the same thing (an empty string reaches the unknown-type
// error, an absent one skips the type gate entirely).
type Filters struct {
	Type *string
	// Fields is the parsed --<field> <value> set in CLI input order; values are
	// strings.
	Fields     *omap.Map
	Tag        *string
	Has        *string
	Missing    *string
	Orphan     bool
	Stale      bool
	DraftOnly  bool
	ActiveOnly bool
	Limit      *int
}

// Query returns the entities passing every filter, each annotated orphan/stale.
func Query(root string, filters Filters, now time.Time) ([]Match, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities
	g := graph.BuildGraph(idx)
	days, err := workspace.StaleDays(root)
	if err != nil {
		return nil, err
	}

	inverses := buildInverses(resolved)
	if err := validateFilterNames(resolved, root, filters, inverses); err != nil {
		return nil, err
	}

	nodes := append([]index.Node(nil), idx.Order...)
	index.SortNodes(nodes)

	matches := []Match{}
	for _, node := range nodes {
		if filters.Type != nil && *filters.Type != "" && node.Type != *filters.Type {
			continue
		}
		meta := idx.Meta[node]
		// A type declaring `orphan: true` is never flagged: edge-less is its
		// expected state. Kept identical to the `check` gate and the `status`
		// count — one notion, three read sites.
		rtype, _ := resolved.Types.Get(node.Type)
		orphan := graph.IsOrphan(g, node, rtype)
		stale := project.IsStale(meta, now, days, nil)
		if !passes(node, meta, g, filters, resolved, inverses, orphan, stale) {
			continue
		}
		draftRaw, _ := meta.Get("draft")
		matches = append(matches, Match{
			Type:   node.Type,
			Slug:   node.Slug,
			Draft:  values.AsBool(draftRaw),
			Orphan: orphan,
			Stale:  stale,
			Title:  title(meta, node.Slug),
		})
	}
	if filters.Limit != nil {
		matches = limitMatches(matches, *filters.Limit)
	}
	return matches, nil
}

// title is `str(meta.get("title") or meta.get("name") or slug)`.
func title(meta *omap.Map, slug string) string {
	for _, key := range []string{"title", "name"} {
		v, _ := meta.Get(key)
		if values.Truthy(v) {
			return values.Str(v)
		}
	}
	return slug
}

// limitMatches is Python's matches[:limit], including the negative-index form.
func limitMatches(matches []Match, n int) []Match {
	if n < 0 {
		n += len(matches)
		if n < 0 {
			n = 0
		}
	}
	if n > len(matches) {
		n = len(matches)
	}
	return matches[:n]
}

// validateFilterNames rejects a field or --has/--missing predicate the schema
// does not declare — _validate_filter_names.
//
// Type-scoped when --type is set (the located error names that type); otherwise
// a name must be declared on at least one type, else it is a typo that would
// silently match nothing (`--stagee` returning an empty set as if a success).
func validateFilterNames(resolved *schema.ResolvedSchema, root string, filters Filters, inverses inverseIndex) error {
	var preds []string
	for _, p := range []*string{filters.Has, filters.Missing} {
		if p != nil {
			preds = append(preds, *p)
		}
	}
	if filters.Type != nil {
		rtype, ok := resolved.Types.Get(*filters.Type)
		if !ok {
			preset := "the"
			if prov, perr := workspace.Provenance(root); perr == nil {
				if p, _ := prov.Get("preset"); p != nil && p != "" {
					preset, _ = p.(string)
				}
			}
			known := append([]string{}, resolved.Types.Keys()...)
			sort.Strings(known)
			return errs.UnknownType(*filters.Type, preset, known)
		}
		fields := map[string]bool{}
		for _, n := range rtype.FieldNames() {
			fields[n] = true
		}
		for _, fname := range fieldKeys(filters) {
			if !fields[fname] {
				return errs.UnknownFilterField(fname, *filters.Type, declaredNames(nil, "", fields))
			}
		}
		for _, pred := range preds {
			if !fields[pred] && len(inverses[inverseKey{*filters.Type, pred}]) == 0 {
				return errs.UnknownFilterField(pred, *filters.Type, declaredNames(inverses, *filters.Type, fields))
			}
		}
		return nil
	}
	attrs, rels := map[string]bool{}, map[string]bool{}
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		for _, a := range rtype.Attributes.Keys() {
			attrs[a] = true
		}
		for _, r := range rtype.Relations.Keys() {
			rels[r] = true
		}
	}
	for _, fname := range fieldKeys(filters) {
		if !attrs[fname] && !rels[fname] {
			return errs.UnknownFilterField(fname, "any", declaredNames(nil, "", attrs, rels))
		}
	}
	for _, pred := range preds {
		if !rels[pred] && !attrs[pred] && len(inverses[inverseKey{"", pred}]) == 0 {
			return errs.UnknownFilterField(pred, "any", declaredNames(inverses, "", attrs, rels))
		}
	}
	return nil
}

// declaredNames is the sorted name list a filter_error offers: the union of
// sets, plus the inverse names keyed to type_ ("" is the no-type key, which
// holds every inverse).
func declaredNames(inverses inverseIndex, type_ string, sets ...map[string]bool) []string {
	all := map[string]bool{}
	for _, set := range sets {
		for n := range set {
			all[n] = true
		}
	}
	for k := range inverses {
		if k.Type == type_ {
			all[k.Name] = true
		}
	}
	out := make([]string, 0, len(all))
	for n := range all {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func fieldKeys(f Filters) []string {
	if f.Fields == nil {
		return nil
	}
	return f.Fields.Keys()
}

// passes reports whether one entity clears every active filter (read-only over
// frontmatter) — _passes.
func passes(node index.Node, meta *omap.Map, g *graph.Graph, f Filters,
	resolved *schema.ResolvedSchema, inverses inverseIndex, orphan, stale bool) bool {
	draftRaw, _ := meta.Get("draft")
	isDraft := values.AsBool(draftRaw)
	if f.ActiveOnly && isDraft {
		return false
	}
	if f.DraftOnly && !isDraft {
		return false
	}
	for _, fname := range fieldKeys(f) {
		wantedAny, _ := f.Fields.Get(fname)
		wanted, _ := wantedAny.(string)
		got, _ := meta.Get(fname)
		if !fieldMatches(got, wanted) {
			return false
		}
	}
	if f.Tag != nil {
		tags, _ := meta.Get("tags")
		if !fieldMatches(tags, *f.Tag) {
			return false
		}
	}
	if f.Has != nil && !hasValue(node, meta, g, resolved, inverses, *f.Has) {
		return false
	}
	// A type that cannot carry the name has no gap to surface: without --type,
	// `--missing kind` otherwise returned every singleton alongside the components
	// that genuinely lack it, diluting the gap query with unfillable rows.
	if f.Missing != nil && (!declares(node.Type, resolved, inverses, *f.Missing) ||
		hasValue(node, meta, g, resolved, inverses, *f.Missing)) {
		return false
	}
	if f.Orphan && !orphan {
		return false
	}
	if f.Stale && !stale {
		return false
	}
	return true
}

// declares reports whether type_ could carry name at all — as a relation,
// inverse, or attribute.
func declares(type_ string, resolved *schema.ResolvedSchema, inverses inverseIndex, name string) bool {
	rtype, ok := resolved.Types.Get(type_)
	if !ok {
		return false
	}
	return rtype.Relations.Has(name) || rtype.Attributes.Has(name) ||
		len(inverses[inverseKey{type_, name}]) > 0
}

// hasValue reports whether node carries name: a resolved edge for a relation, a
// populated value for an attribute.
//
// --has/--missing were answered from the graph alone until 0.13.0, so an
// attribute (repo, stack) raised `No field 'repo' on type 'component'` — a
// message that was simply false, since the schema declares it. An attribute has
// no edge, so presence is read off frontmatter: absent, null, or empty counts as
// a gap, the same null-is-absent rule validate and check already apply.
//
// A relation still tests the RESOLVED edge — an edge exists in the graph only
// when its value resolved to a node, so an unresolvable target counts as missing.
func hasValue(node index.Node, meta *omap.Map, g *graph.Graph,
	resolved *schema.ResolvedSchema, inverses inverseIndex, name string) bool {
	rtype, ok := resolved.Types.Get(node.Type)
	if !ok {
		return false
	}
	inverseOf := inverses[inverseKey{node.Type, name}]
	if rtype.Relations.Has(name) || len(inverseOf) > 0 {
		return hasEdge(g, node, name, inverseOf)
	}
	if rtype.Attributes.Has(name) {
		value, _ := meta.Get(name)
		if value == nil {
			return false
		}
		return !isEmptyContainer(value)
	}
	return false // declared on some other type, so this entity simply lacks it
}

// isEmptyContainer is Python's `isinstance(value, (str, list, dict)) and not value`.
func isEmptyContainer(v any) bool {
	switch x := v.(type) {
	case string:
		return x == ""
	case []any:
		return len(x) == 0
	case *omap.Map:
		return x.Len() == 0
	default:
		return false
	}
}

// hasEdge reports whether node has a resolvable edge for predicate.
//
// A declared inverse (`superseded` for `supersedes`) is never stored, so it is
// answered from the INBOUND side: the forward edge lives on the other entity.
// That makes `--missing superseded` the "which decisions are still current?"
// query.
func hasEdge(g *graph.Graph, node index.Node, predicate string, inverseOf map[inverseSource]bool) bool {
	for _, e := range g.OutEdges(node) {
		if e.Predicate == predicate {
			return true // a stored forward edge always wins; an inverse never shadows it
		}
	}
	if len(inverseOf) == 0 {
		return false
	}
	for _, e := range g.InEdges(node) {
		if inverseOf[inverseSource{e.From.Type, e.Predicate}] {
			return true
		}
	}
	return false
}

// Inverse definitions retain the declaring source type; equal predicate names
// on unrelated types do not acquire each other's inverses.
type inverseSource struct{ Type, Predicate string }
type inverseKey struct{ Type, Name string }
type inverseIndex map[inverseKey]map[inverseSource]bool

func buildInverses(resolved *schema.ResolvedSchema) inverseIndex {
	out := inverseIndex{}
	for _, name := range resolved.Types.Keys() {
		rt, _ := resolved.Types.Get(name)
		for _, predicate := range rt.Relations.Keys() {
			rel, _ := rt.Relations.Get(predicate)
			if rel.Inverse == nil {
				continue
			}
			targets := rel.Targets
			if rel.Kind == schema.KindAny {
				targets = resolved.Types.Keys()
			}
			for _, target := range append(append([]string{}, targets...), "") {
				key := inverseKey{target, *rel.Inverse}
				if out[key] == nil {
					out[key] = map[inverseSource]bool{}
				}
				out[key][inverseSource{name, predicate}] = true
			}
		}
	}
	return out
}

// fieldMatches reports whether a frontmatter value matches a CLI filter string.
//
// A bool matches case-insensitively (so `--active true` works against YAML
// True); a list (many-valued relation or list attribute) matches on membership
// (`--team noor` against `team: [noor, bob]`, never substring); any other scalar
// matches by string equality.
func fieldMatches(value any, wanted string) bool {
	switch x := value.(type) {
	case bool:
		// Python: str(value).lower() == wanted.lower(). str(True) is "True", so
		// this is ToLower on both sides — NOT EqualFold, whose Unicode folding is
		// a different relation.
		got := strings.ToLower(values.Str(x))
		return got == strings.ToLower(wanted)
	case []any:
		for _, v := range x {
			if values.Str(v) == wanted {
				return true
			}
		}
		return false
	}
	return values.Str(value) == wanted
}
