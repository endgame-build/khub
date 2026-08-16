// Package introspect ports core/introspect.py — pure reads over the resolved
// .khub/schema.yaml. Every view derives from the compiled contract at runtime,
// with no per-type code path (the schema-generic invariant).
package introspect

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// LoadSchema resolves the workspace's flattened .khub/schema.yaml. A missing
// or unparseable file becomes a located schema_error naming the file.
func LoadSchema(root string) (*schema.ResolvedSchema, error) {
	path := filepath.Join(root, ".khub", "schema.yaml")
	if _, err := os.Stat(path); err != nil {
		return nil, errs.New("schema_error", fmt.Sprintf("Cannot read schema %s: file not found", path))
	}
	resolved, err := schema.Resolve([]string{path})
	if err != nil {
		var located *errs.Located
		if asLocated(err, &located) {
			return nil, err // resolver-level located errors pass through untouched
		}
		return nil, errs.New("schema_error", fmt.Sprintf("Cannot parse schema %s: %s", path, err.Error()))
	}
	return resolved, nil
}

func asLocated(err error, target **errs.Located) bool {
	for e := err; e != nil; {
		if l, ok := e.(*errs.Located); ok {
			*target = l
			return true
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = u.Unwrap()
	}
	return false
}

// TypesList returns the declared type names in declaration order.
func TypesList(resolved *schema.ResolvedSchema) []string { return resolved.Types.Keys() }

// TypeView renders one type's fields, enums, required flags, relations, and
// storage layout.
func TypeView(resolved *schema.ResolvedSchema, name, preset string) (*omap.Map, error) {
	rtype, ok := resolved.Types.Get(name)
	if !ok {
		known := append([]string{}, resolved.Types.Keys()...)
		sort.Strings(known)
		return nil, errs.UnknownType(name, preset, known)
	}
	return typeView(resolved, rtype), nil
}

// SchemaView renders the full effective schema plus source provenance.
func SchemaView(resolved *schema.ResolvedSchema, provenance *omap.Map) *omap.Map {
	out := omap.New()
	out.Set("provenance", provenance)
	types := []any{}
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		types = append(types, typeView(resolved, rtype))
	}
	out.Set("types", types)
	return out
}

// EdgesView renders the relation vocabulary: one row per DISTINCT declaration.
// Keying by predicate name alone is lossy in a way that misleads (build-lite
// declares `supersedes` on two types with different targets), so a row is
// keyed by the whole declaration.
func EdgesView(resolved *schema.ResolvedSchema) []any {
	type row struct {
		sig     string
		rel     *schema.ResolvedRelation
		sources []string
		base    bool
	}
	rows := map[string]*row{}
	var order []string
	for _, p := range resolved.BaseRelations.Keys() {
		rel, _ := resolved.BaseRelations.Get(p)
		sig := signature(rel)
		if _, ok := rows[sig]; !ok {
			rows[sig] = &row{sig: sig, rel: rel, sources: []string{"any"}, base: true}
			order = append(order, sig)
		}
	}
	srcs := map[string]map[string]bool{}
	for _, tname := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(tname)
		for _, p := range rtype.Relations.Keys() {
			if resolved.BaseRelations.Has(p) {
				continue
			}
			rel, _ := rtype.Relations.Get(p)
			sig := signature(rel)
			if srcs[sig] == nil {
				srcs[sig] = map[string]bool{}
			}
			srcs[sig][tname] = true
			if _, ok := rows[sig]; !ok {
				rows[sig] = &row{sig: sig, rel: rel}
				order = append(order, sig)
			}
		}
	}
	for sig, set := range srcs {
		var names []string
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		rows[sig].sources = names
	}
	// Python sorts by (str(predicate), str(targets)) with a STABLE sort, where
	// str(tuple) renders "('a', 'b')". Reproduce both the key and the
	// stability: sort.Slice is not stable, and a joined-string compare over
	// targets is exactly the R7 trap.
	sort.SliceStable(order, func(i, j int) bool {
		a, b := rows[order[i]].rel, rows[order[j]].rel
		if a.Predicate != b.Predicate {
			return a.Predicate < b.Predicate
		}
		return pyTupleRepr(a.Targets) < pyTupleRepr(b.Targets)
	})
	out := []any{}
	for _, sig := range order {
		r := rows[sig]
		out = append(out, edgeView(r.rel, r.sources))
	}
	// The derived half of the graph. Every declared inverse is a real read
	// surface (`get --edges`, `query --has/--missing`) that this listing used to
	// omit entirely, so a caller enumerating edges saw only the stored
	// direction. `from` is the type that answers to the inverse; `to` is where
	// the stored edge came from.
	// One row per derived DECLARATION, aggregated across every type carrying it:
	// `from` is the types answering to the inverse, `to` the union of types
	// storing the forward edge. Aggregating matters — build-hub declares
	// `supersedes` on adr, feature-spec and pdr, and reporting only whichever
	// was seen first would understate the surface.
	var derivedOrder []string
	agg := map[string]*derivedInverse{}
	froms := map[string][]string{}
	for _, tname := range resolved.Types.Keys() {
		for _, d := range derivedInversesFor(resolved, tname) {
			key := fmt.Sprintf("%s|%s|%s|%t", d.predicate, d.forward, d.kind, d.acyclic)
			cur, seen := agg[key]
			if !seen {
				copied := d
				agg[key] = &copied
				derivedOrder = append(derivedOrder, key)
				cur = &copied
			}
			for _, src := range d.sources {
				if !containsStr(cur.sources, src) {
					cur.sources = append(cur.sources, src)
				}
			}
			if !containsStr(froms[key], tname) {
				froms[key] = append(froms[key], tname)
			}
		}
	}
	sort.Strings(derivedOrder)
	for _, key := range derivedOrder {
		d := agg[key]
		sort.Strings(d.sources)
		from := froms[key]
		sort.Strings(from)
		out = append(out, derivedEdgeView(*d, from))
	}
	return out
}

// pyTupleRepr renders a target list the way Python's str(tuple) does, because
// that string IS the sort key introspect.py uses.
func pyTupleRepr(ss []string) string {
	if len(ss) == 0 {
		return "()"
	}
	if len(ss) == 1 {
		return "('" + ss[0] + "',)"
	}
	quoted := make([]string, len(ss))
	for i, s := range ss {
		quoted[i] = "'" + s + "'"
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

func signature(rel *schema.ResolvedRelation) string {
	inv := ""
	if rel.Inverse != nil {
		inv = *rel.Inverse
	}
	return fmt.Sprintf("%s|%v|%s|%t|%t|%s|%t",
		rel.Predicate, rel.Targets, rel.Kind, rel.Many, rel.Required, inv, rel.Acyclic)
}

func typeView(resolved *schema.ResolvedSchema, rtype *schema.ResolvedType) *omap.Map {
	v := omap.New()
	v.Set("name", rtype.Name)
	v.Set("layout", rtype.Storage.Layout)
	v.Set("format", rtype.Storage.Fmt)
	v.Set("path", strPtr(rtype.Storage.Path))
	v.Set("required", rtype.Required)
	v.Set("orphan", rtype.Orphan)
	v.Set("when", strPtr(rtype.When))
	fields := []any{}
	for _, name := range rtype.Attributes.Keys() {
		a, _ := rtype.Attributes.Get(name)
		f := omap.New()
		f.Set("name", a.Name)
		f.Set("type", a.BaseType)
		f.Set("required", a.Required)
		if len(a.Enum) > 0 {
			enum := []any{}
			for _, e := range a.Enum {
				enum = append(enum, e)
			}
			f.Set("enum", enum)
		} else {
			f.Set("enum", nil)
		}
		f.Set("pattern", strPtr(a.Pattern))
		f.Set("default", a.Default)
		fields = append(fields, f)
	}
	v.Set("fields", fields)
	rels := []any{}
	for _, name := range rtype.Relations.Keys() {
		r, _ := rtype.Relations.Get(name)
		rels = append(rels, relationView(r))
	}
	// Stored relations first, in declaration order, then the derived inverses
	// alphabetically — so an existing caller reading relations[0] keeps reading
	// what it always read.
	for _, d := range derivedInversesFor(resolved, rtype.Name) {
		rels = append(rels, derivedRelationView(d))
	}
	v.Set("relations", rels)
	return v
}

func relationView(rel *schema.ResolvedRelation) *omap.Map {
	r := omap.New()
	r.Set("predicate", rel.Predicate)
	r.Set("to", toAny(rel.Targets))
	r.Set("kind", rel.Kind)
	r.Set("many", rel.Many)
	r.Set("required", rel.Required)
	r.Set("inverse", strPtr(rel.Inverse))
	r.Set("acyclic", rel.Acyclic)
	r.Set("derived", false)
	return r
}

// derivedInverse is one inverse predicate a type carries but does not store.
//
// `supersedes: {to: adr, inverse: superseded}` means an adr answers to
// `superseded` — `get --edges` returns it and `query --missing superseded`
// filters on it — while nothing writes it to disk. Introspection listed only
// the stored side, so the one surface skills/khub/SKILL.md tells agents to
// build writes from ("never from a hardcoded shape") omitted predicates those
// same agents are allowed to use.
type derivedInverse struct {
	predicate string
	sources   []string // types whose forward relation points here
	forward   string   // the stored predicate this inverts
	kind      string
	acyclic   bool
}

// derivedInversesFor returns the inverse predicates typeName answers to.
//
// The membership rule is query.inverseSources read forwards: a relation's
// inverse lands on a type when the relation can actually point AT that type —
// `kind: any`, or the type is among its declared targets. Anything looser would
// advertise a predicate that filters nothing.
func derivedInversesFor(resolved *schema.ResolvedSchema, typeName string) []derivedInverse {
	// Keyed by the whole declaration, not the inverse NAME — the same reason
	// EdgesView keys stored rows by signature. Two forwards may share one
	// inverse (`blocks` and `depends_on` both inverting to `blocked_by`), and
	// first-seen-wins would report one, silently drop the other, and attribute
	// the survivor's kind and acyclic flag to both.
	byDecl := map[string]*derivedInverse{}
	var order []string
	for _, sname := range resolved.Types.Keys() {
		stype, _ := resolved.Types.Get(sname)
		for _, p := range stype.Relations.Keys() {
			rel, _ := stype.Relations.Get(p)
			if rel.Inverse == nil {
				continue
			}
			if rel.Kind != schema.KindAny && !containsStr(rel.Targets, typeName) {
				continue
			}
			key := fmt.Sprintf("%s|%s|%s|%t", *rel.Inverse, rel.Predicate, rel.Kind, rel.Acyclic)
			d, seen := byDecl[key]
			if !seen {
				d = &derivedInverse{
					predicate: *rel.Inverse, forward: rel.Predicate,
					kind: rel.Kind, acyclic: rel.Acyclic,
				}
				byDecl[key] = d
				order = append(order, key)
			}
			if !containsStr(d.sources, sname) {
				d.sources = append(d.sources, sname)
			}
		}
	}
	sort.Strings(order)
	out := make([]derivedInverse, 0, len(order))
	for _, k := range order {
		d := byDecl[k]
		sort.Strings(d.sources)
		out = append(out, *d)
	}
	return out
}

// derivedEdgeView is edgeView's peer for the derived half: `from` is the types
// answering to the inverse, `to` the types storing the forward edge. A peer
// rather than copying keys out of derivedRelationView by name, because key
// order is contract and a renamed key would have gone silently nil.
func derivedEdgeView(d derivedInverse, from []string) *omap.Map {
	e := omap.New()
	e.Set("predicate", d.predicate)
	e.Set("from", toAny(from))
	e.Set("to", toAny(d.sources))
	e.Set("kind", d.kind)
	e.Set("many", true)
	e.Set("required", false)
	e.Set("inverse", d.forward)
	e.Set("acyclic", d.acyclic)
	e.Set("derived", true)
	return e
}

// derivedRelationView renders an inverse in the same shape as a stored
// relation. `many` is always true and `required` always false: any number of
// entities may point at this one, and inbound edges cannot be mandated.
func derivedRelationView(d derivedInverse) *omap.Map {
	r := omap.New()
	r.Set("predicate", d.predicate)
	r.Set("to", toAny(d.sources))
	r.Set("kind", d.kind)
	r.Set("many", true)
	r.Set("required", false)
	r.Set("inverse", d.forward)
	r.Set("acyclic", d.acyclic)
	r.Set("derived", true)
	return r
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func edgeView(rel *schema.ResolvedRelation, sources []string) *omap.Map {
	e := omap.New()
	e.Set("predicate", rel.Predicate)
	e.Set("from", toAny(sources))
	e.Set("to", toAny(rel.Targets))
	e.Set("kind", rel.Kind)
	e.Set("many", rel.Many)
	e.Set("required", rel.Required)
	e.Set("inverse", strPtr(rel.Inverse))
	e.Set("acyclic", rel.Acyclic)
	e.Set("derived", false)
	return e
}

func toAny(ss []string) []any {
	out := []any{}
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

func strPtr(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}
