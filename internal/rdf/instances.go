// instances.go is the ABox (`--instances`): every entity as an individual of
// its type's class, the scalar attributes it carries as literals, and its
// stored forward edges as object properties. Derived inverses are not
// emitted — the graph is derived, and an RDF consumer derives them the same
// way khub does. Values are what the file holds: an absent attribute emits
// nothing, and the shape's khub:default says what it reads as.
//
// A literal's datatype follows the attribute's declared kind, never the
// value's YAML shape: the shape says `sh:datatype` from the kind, so a
// literal typed any other way would fail the file's own shapes where
// `validate` passes the entity. A date in a datetime field becomes midnight,
// a datetime in a date field keeps its time (the date shape admits both),
// and anything in a text field is its string form.

package rdf

import (
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// Instances appends one subject per entity to doc, in scan order (schema
// declaration order, then the type's sorted entity order) — the order every
// projection walks — then one khub:File subject per workspace file a path
// reference named, in first-reference order.
func Instances(doc *Doc, resolved *schema.ResolvedSchema, idx *index.Index, g *graph.Graph, ns Namespaces) int {
	m := &mapper{ns: ns, resolved: resolved}
	count := 0
	var files []string
	seen := map[string]bool{}
	for _, node := range g.Nodes() {
		rtype, ok := resolved.Types.Get(node.Type)
		if !ok {
			continue
		}
		s := &Subject{IRI: m.entityIRI(node)}
		s.Add(rdfType, m.typeIRI(node.Type))
		meta := idx.Meta[node]
		for _, an := range rtype.Attributes.Keys() {
			if an == TypeKey {
				continue
			}
			a, _ := rtype.Attributes.Get(an)
			m.values(&s.Pairs, meta, a, m.fieldIRI(an), resolved.BaseAttributes.Has(an))
		}
		out := g.OutEdges(node)
		for _, rn := range rtype.Relations.Keys() {
			rel, _ := rtype.Relations.Get(rn)
			pred := m.fieldIRI(rn)
			// An object is asserted once per predicate, as the graph keeps
			// one edge per target: RDF holds a triple once however often the
			// file repeats the value.
			emitted := map[string]bool{}
			add := func(o IRI) {
				if !emitted[o.Full] {
					emitted[o.Full] = true
					s.Add(pred, o)
				}
			}
			for _, e := range out {
				if e.Predicate == rn {
					add(m.entityIRI(e.To))
				}
			}
			// Two stored values satisfy the relation without an edge in the
			// graph: a self-reference, which `check` counts but the graph
			// drops, and a path reference to a plain file, a node of its own
			// here.
			raw, _ := meta.Get(rn)
			for _, target := range targets(raw) {
				if hits := idx.ResolveTarget(rel, target); len(hits) > 0 {
					if len(hits) == 1 && hits[node] {
						add(m.entityIRI(node))
					}
					continue
				}
				if path, ok := idx.FileRef(rel, target); ok {
					add(m.fileIRI(path))
					if !seen[path] {
						seen[path] = true
						files = append(files, path)
					}
				}
			}
		}
		doc.Subjects = append(doc.Subjects, s)
		count++
	}
	for _, rel := range files {
		s := &Subject{IRI: m.fileIRI(rel)}
		s.Add(rdfType, khubFile)
		s.Add(khubPath, Str(rel))
		doc.Subjects = append(doc.Subjects, s)
	}
	return count
}

// values appends one pair per value the attribute holds in meta: a list
// contributes one pair per item, an absent or null attribute none. base
// says whether the attribute is the base block's, which decides where the
// fields of an object it holds mint.
func (m *mapper) values(pairs *[]Pair, meta *omap.Map, a *schema.ResolvedAttribute, pred IRI, base bool) {
	raw, has := meta.Get(a.Name)
	if !has || raw == nil {
		return
	}
	valued := a
	if a.BaseType == "list" && a.Items != nil {
		valued = a.Items
	}
	if items, isList := raw.([]any); isList && a.BaseType == "list" {
		for _, item := range items {
			if item != nil {
				*pairs = append(*pairs, Pair{Pred: pred, Obj: m.value(item, valued, base)})
			}
		}
		return
	}
	*pairs = append(*pairs, Pair{Pred: pred, Obj: m.value(raw, valued, base)})
}

// value renders one value under its declaration: an object as a blank node
// carrying its declared fields that are present, in declaration order (an
// undeclared key stays out, as an extension key does on the entity), any
// other kind as the literal the declared kind asks for. A value that is not
// the mapping its kind declares is its string form, so the render never
// fails on what validate reports.
func (m *mapper) value(v any, a *schema.ResolvedAttribute, base bool) Term {
	if a.BaseType != "object" {
		return literal(v, a.BaseType)
	}
	obj, ok := v.(*omap.Map)
	if !ok || a.Fields == nil {
		return Str(stringForm(v))
	}
	node := Blank{}
	for _, fn := range a.Fields.Keys() {
		f, _ := a.Fields.Get(fn)
		m.values(&node.Pairs, obj, f, m.nestedFieldIRI(base, fn), base)
	}
	return node
}

// targets reads a relation's stored value as the list of targets it names.
func targets(raw any) []string {
	switch x := raw.(type) {
	case nil:
		return nil
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if item != nil {
				out = append(out, values.Str(item))
			}
		}
		return out
	case string:
		if x == "" {
			return nil
		}
		return []string{x}
	}
	return []string{values.Str(raw)}
}

// entityIRI mints an individual's IRI: the workspace namespace plus the
// qualified id, type and slug each a segment, always as a full IRI so a slug
// never has to fit a prefixed name.
func (m *mapper) entityIRI(n index.Node) IRI {
	return IRI{Full: m.ns.Entity + segment(n.Type) + "/" + segment(n.Slug)}
}

// fileIRI mints a path-referenced file's IRI under the workspace namespace,
// each path segment encoded on its own so the slashes survive.
func (m *mapper) fileIRI(rel string) IRI {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = segment(p)
	}
	return IRI{Full: m.ns.Entity + "file/" + strings.Join(parts, "/")}
}

// literal renders one value as the literal the attribute's kind asks for.
func literal(v any, kind string) Term {
	switch kind {
	case "text":
		return Str(textForm(v))
	case "number":
		return Typed(decimalForm(v), xsdDecimal)
	case "bool":
		if values.IsBool(v) {
			return Bool(values.AsBool(v))
		}
		return Typed(stringForm(v), xsdBoolean)
	case "date":
		s := stringForm(v)
		if isDateTime(v, s) {
			return Typed(xsdDateTimeLexical(s), xsdDateTime)
		}
		return Typed(s, xsdDate)
	case "datetime":
		s := stringForm(v)
		if !isDateTime(v, s) && len(s) == 10 {
			return Typed(s+"T00:00:00", xsdDateTime)
		}
		return Typed(xsdDateTimeLexical(s), xsdDateTime)
	}
	// An untyped list item, or a kind the schema does not know: typed by the
	// value.
	switch x := v.(type) {
	case bool:
		return Bool(x)
	case int64, int, float64, canon.BigInt:
		return Typed(stringForm(x), xsdDecimal)
	case canon.Date:
		return Typed(x.ISO, xsdDate)
	case canon.DateTime:
		return Typed(xsdDateTimeLexical(x.ISO), xsdDateTime)
	}
	return Str(stringForm(v))
}

// textForm is a text attribute's literal: the string `validate` matches its
// enum and pattern against (`True`, `1.0`), so the file's own sh:in and
// sh:pattern decide as validate does. A structured value is its JSON text.
func textForm(v any) string {
	switch v.(type) {
	case []any, *omap.Map:
		return stringForm(v)
	}
	return values.Str(v)
}

// decimalForm is a number attribute's literal: the form `validate` compares
// (`1.0` for a whole float), which is also an xsd:decimal, unless that form
// is an exponent or a non-finite value, which xsd:decimal has no spelling for.
func decimalForm(v any) string {
	if f, ok := v.(float64); ok {
		if s := canon.PyFloatRepr(f); !strings.ContainsAny(s, "eanif") {
			return s
		}
	}
	return stringForm(v)
}

// stringForm is a value's text: the lexical form a literal of the declared
// kind carries. Booleans are RDF's `true`/`false`, numbers their decimal
// text, dates their ISO form, and a structured value (a mapping, a list) its
// JSON text, so a text attribute holding one still conforms to xsd:string.
func stringForm(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		return strconv.FormatBool(x)
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case canon.BigInt:
		return x.Literal
	case canon.Date:
		return x.ISO
	case canon.DateTime:
		return x.ISO
	case []any, *omap.Map:
		doc, err := canon.EncodeCLI(x)
		if err != nil {
			// Loaded YAML always encodes; a failure here is a bug, not input.
			panic(err)
		}
		return doc
	}
	return values.Str(v)
}

// isDateTime reports whether a date-kind value carries a time of day.
func isDateTime(v any, s string) bool {
	if _, ok := v.(canon.DateTime); ok {
		return true
	}
	if _, ok := v.(canon.Date); ok {
		return false
	}
	return len(s) > 10 && (s[10] == 'T' || s[10] == ' ')
}

// xsdDateTimeLexical turns khub's ISO datetime, which separates the date
// from the time with a space as Python's str(datetime) does, into the XSD
// lexical form, which requires the `T`. The same instant either way.
func xsdDateTimeLexical(s string) string {
	if len(s) > 10 && s[10] == ' ' {
		return s[:10] + "T" + s[11:]
	}
	return s
}
