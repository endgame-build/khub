// Package introspect ports core/introspect.py — pure reads over the resolved
// workspace schema (.khub/{ontology,policy,storage}.yaml plus the embedded
// base). Every view derives from the compiled contract at runtime, with no
// per-type code path (the schema-generic invariant).
package introspect

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/presets"
	"github.com/endgame-build/khub/internal/schema"
)

// LayerFiles lists the schema layer files that exist in the workspace —
// workspace-relative slash paths, in resolve order (the layer names come from
// internal/presets, the vocabulary's owner). It is the one discovery walk
// LoadSchemaLayers resolves from and wire builds its imports from, so the two
// can never disagree about which files a workspace has. ontology.yaml, when
// present, is always the first element — LoadSchemaLayers leans on that.
//
// A stat that fails for any reason OTHER than absence is returned, not read as
// absence: an unsearchable .khub, a dangling symlink or an I/O error would
// otherwise drop the layer silently and leave the caller reporting "file not
// found" — the wrong cause, and the one a reader would act on. An unreadable
// FILE is a different case and needs nothing here: stat succeeds on it, so the
// real permission error surfaces from the read.
func LayerFiles(root string) ([]string, error) {
	var out []string
	for _, name := range []string{presets.OntologyFile, presets.PolicyFile, presets.StorageFile} {
		p := filepath.Join(root, ".khub", name)
		fi, statErr := fsio.Stat(root, p)
		if statErr != nil {
			if errors.Is(statErr, fs.ErrNotExist) {
				continue
			}
			return nil, errs.New("schema_error",
				fmt.Sprintf("Cannot read schema %s: %s", p, statErr.Error()))
		}
		if fi.Mode().IsRegular() {
			out = append(out, ".khub/"+name)
		}
	}
	return out, nil
}

// LoadSchemaLayers resolves the workspace schema — whichever of
// .khub/{ontology,policy,storage}.yaml exist, over the base block embedded in
// the binary — and returns the layer files it resolved from, so a caller
// rendering them (wire's imports) cannot disagree with what was resolved.
// This is the one layer that knows both the workspace and the embedded tree,
// so this is where they join. The embedded document is the only base source —
// an authored `ontology.base` is a resolve error.
//
// ontology.yaml is required: it is the layer that declares which types exist,
// so policy/storage without it is a half-workspace, not an empty one — quietly
// resolving it to zero types would make every gate pass over a corpus the
// schema no longer sees. A workspace with none of the three files gets the
// ordinary schema_error naming ontology.yaml. There is no other layout: khub
// supports no backward compatibility, so nothing else is detected or advised.
func LoadSchemaLayers(root string) (*schema.ResolvedSchema, []string, error) {
	layers, err := LayerFiles(root)
	if err != nil {
		return nil, nil, err
	}
	ontologyPath := filepath.Join(root, ".khub", presets.OntologyFile)
	if len(layers) == 0 {
		return nil, nil, errs.New("schema_error",
			fmt.Sprintf("Cannot read schema %s: file not found", ontologyPath))
	}
	if layers[0] != ".khub/"+presets.OntologyFile {
		others := make([]string, 0, len(layers))
		for _, rel := range layers {
			others = append(others, filepath.Base(rel))
		}
		return nil, nil, errs.New("schema_error", fmt.Sprintf(
			"Cannot read schema %s: file not found (%s present, but ontology.yaml is the layer that declares types)",
			ontologyPath, strings.Join(others, ", ")))
	}
	base, err := embeddedBase()
	if err != nil {
		return nil, nil, errs.New("schema_error", fmt.Sprintf("Cannot read embedded base: %s", err.Error()))
	}
	paths := make([]string, 0, len(layers))
	for _, rel := range layers {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
	}
	resolved, err := schema.ResolveIn(root, base, paths)
	if err != nil {
		var located *errs.Located
		if errors.As(err, &located) {
			return nil, nil, err // resolver-level located errors pass through untouched
		}
		// Every non-located resolver error already names its file (LoadYAML and
		// the shape errors prefix the path), so the wrap adds no path of its
		// own — headlining paths[0] blamed ontology.yaml for a failure in any
		// sibling layer.
		return nil, nil, errs.New("schema_error", fmt.Sprintf("Cannot parse schema: %s", err.Error()))
	}
	return resolved, layers, nil
}

// LoadSchema is LoadSchemaLayers for the callers that only want the contract.
func LoadSchema(root string) (*schema.ResolvedSchema, error) {
	resolved, _, err := LoadSchemaLayers(root)
	return resolved, err
}

// embeddedBase loads the base block khub ships in the binary
// (presets/core/ontology.yaml), once per process — the embedded bytes cannot
// change under a running binary, and every resolve reads the document without
// mutating it. Parsing goes through schema.ParseDoc, the same path every
// authored layer file takes, so the embedded document gets the same scalar
// normalization and duplicate-key rejection.
var embeddedBase = sync.OnceValues(func() (*omap.Map, error) {
	raw, err := fs.ReadFile(presets.Embedded(), presets.CorePath)
	if err != nil {
		return nil, err
	}
	return schema.ParseDoc(presets.CorePath, string(raw))
})

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

// BaseView renders the effective base block — the attributes and relations
// every type inherits. Since the ontology/policy/storage split the base is
// embedded in the binary and no workspace file carries (or may declare) it,
// so this view is the one place to read it.
func BaseView(resolved *schema.ResolvedSchema) *omap.Map {
	v := omap.New()
	fields := []any{}
	for _, name := range resolved.BaseAttributes.Keys() {
		a, _ := resolved.BaseAttributes.Get(name)
		fields = append(fields, attrView(a))
	}
	v.Set("fields", fields)
	rels := []any{}
	for _, name := range resolved.BaseRelations.Keys() {
		r, _ := resolved.BaseRelations.Get(name)
		rels = append(rels, relationView(r))
	}
	v.Set("relations", rels)
	return v
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
// Keying by predicate name alone is lossy in a way that misleads (a preset
// may declare `supersedes` on two types with different targets, as the old
// build-hub did on adr and pdr), so a row is keyed by the whole declaration.
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
	// storing the forward edge. Aggregating matters — a preset declaring one
	// inverse on several types (the pre-0.6.0 build-hub carried `supersedes`
	// on adr and pdr; today's declares it on adr alone) would otherwise
	// report only whichever was seen first and understate the surface.
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
	// The id scheme, as declared and as rendered: an agent building an `add`
	// reads `id_shape` to know what the write will be named, and `id_prefix`
	// to know which field decides it.
	v.Set("id_prefix", idPrefixView(rtype.IdPrefix))
	v.Set("id_date", rtype.IdDate)
	v.Set("id_shape", nullIfEmpty(rtype.IdShape()))
	v.Set("required", rtype.Required)
	v.Set("orphan", rtype.Orphan)
	v.Set("when", strPtr(rtype.When))
	fields := []any{}
	for _, name := range rtype.Attributes.Keys() {
		a, _ := rtype.Attributes.Get(name)
		fields = append(fields, attrView(a))
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

// attrView renders one resolved attribute — the per-field JSON contract both
// `schema show` (typeView) and `schema base` (BaseView) emit, kept in one
// place the way relationView already is for relations: key order is contract,
// and a second copy is how the two surfaces drift.
func attrView(a *schema.ResolvedAttribute) *omap.Map {
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
	return f
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

// nullIfEmpty renders "" as null: a singleton has no id shape, and an agent
// should read the absence, not an empty string it might interpolate.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// idPrefixView renders id_prefix the way it was authored — a literal token,
// or `{by, map}` in declaration order — and null when the type declares none.
func idPrefixView(p *schema.IdPrefix) any {
	if p == nil {
		return nil
	}
	if p.Literal != nil {
		return *p.Literal
	}
	m := omap.New()
	for _, member := range p.Members {
		m.Set(member.Value, member.Prefix)
	}
	v := omap.New()
	v.Set("by", strPtr(p.By))
	v.Set("map", m)
	return v
}
