// instances.go is the ABox (`--instances`): every entity as an individual of
// its type's class, the scalar attributes it carries as literals, and its
// stored forward edges as object properties. Derived inverses are not
// emitted — the graph is derived, and an RDF consumer derives them the same
// way khub does. Values are what the file holds: an absent attribute emits
// nothing, and the shape's khub:default says what it reads as.

package rdf

import (
	"strconv"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// Instances appends one subject per entity to doc, in scan order (schema
// declaration order, then the type's sorted entity order) — the order every
// projection walks.
func Instances(doc *Doc, resolved *schema.ResolvedSchema, idx *index.Index, g *graph.Graph, ns Namespaces) int {
	m := &mapper{ns: ns, resolved: resolved}
	count := 0
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
			raw, has := meta.Get(an)
			if !has || raw == nil {
				continue
			}
			a, _ := rtype.Attributes.Get(an)
			pred := m.fieldIRI(an)
			if items, isList := raw.([]any); isList {
				for _, item := range items {
					s.Add(pred, literal(item, a.BaseType))
				}
				continue
			}
			s.Add(pred, literal(raw, a.BaseType))
		}
		out := g.OutEdges(node)
		for _, rn := range rtype.Relations.Keys() {
			pred := m.fieldIRI(rn)
			for _, e := range out {
				if e.Predicate == rn {
					s.Add(pred, m.entityIRI(e.To))
				}
			}
		}
		doc.Subjects = append(doc.Subjects, s)
		count++
	}
	return count
}

// entityIRI mints an individual's IRI: the workspace namespace plus the
// qualified id, type and slug each a segment, always as a full IRI so a slug
// never has to fit a prefixed name.
func (m *mapper) entityIRI(n index.Node) IRI {
	return IRI{Full: m.ns.Entity + segment(n.Type) + "/" + segment(n.Slug)}
}

// literal renders one scalar as the literal the attribute's kind asks for.
// The kind decides the datatype where the value is a string the schema types
// (a date, a number written as text); a value that is already typed keeps
// its own shape. Anything structured — a mapping, a nested list, a list
// item that is itself a list — goes out as an rdf:JSON literal, which loses
// nothing.
func literal(v any, kind string) Term {
	switch x := v.(type) {
	case bool:
		return Bool(x)
	case int64:
		return Typed(strconv.FormatInt(x, 10), xsdDecimal)
	case int:
		return Typed(strconv.Itoa(x), xsdDecimal)
	case float64:
		return Typed(strconv.FormatFloat(x, 'f', -1, 64), xsdDecimal)
	case canon.BigInt:
		return Typed(x.Literal, xsdDecimal)
	case canon.Date:
		return Typed(x.ISO, xsdDate)
	case canon.DateTime:
		return Typed(xsdDateTimeLexical(x.ISO), xsdDateTime)
	case string:
		switch kind {
		case "date":
			return Typed(x, xsdDate)
		case "datetime":
			return Typed(xsdDateTimeLexical(x), xsdDateTime)
		case "number":
			return Typed(x, xsdDecimal)
		case "bool":
			return Typed(x, xsdBoolean)
		}
		return Str(x)
	case []any, *omap.Map:
		doc, err := canon.EncodeCLI(x)
		if err != nil {
			// Loaded YAML always encodes; a failure here is a bug, not input.
			panic(err)
		}
		return Typed(doc, rdfJSON)
	case nil:
		return Str("")
	}
	return Str(values.Str(v))
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
