// schema.go is the schema aspect of viz: the resolved ontology drawn as a
// graph — one box per declared type listing its attributes, one edge per
// declared relation carrying its cardinality.
//
// The notation borrows from Graffoo/VOWL only what khub has. A type is a class
// box; `any` (VOWL's owl:Thing) is the one box that stands for every type, and
// carries the base block — the attributes and relations every type inherits —
// so they are drawn once rather than on every box. A union relation is one
// edge per member type. Cardinality is UML multiplicity on the edge and on
// each attribute line. khub has no subclassing, so there is no is-a edge.
//
// Everything is read from the ResolvedSchema; nothing here knows a type name.

package viz

import (
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// SchemaAny is the id of the `any` box. A type's box is `schema:<name>` — the
// namespace serve's page uses — and an entity is `type/slug`, so neither can
// be `schema-any` whatever a type is called: the seventh byte of every type
// box id is `:`, and every entity id holds a `/`. (`schema:*` collided with a
// type named `*`, which the loader accepts.) serve's page uses the same id.
const SchemaAny = "schema-any"

// SchemaID is the node id of a declared type's box.
func SchemaID(typeName string) string { return "schema:" + typeName }

// SchemaTypes is the set of type boxes a schema render draws, in declaration
// order: every declared type, or under a type filter that type and the types
// its own relations can reach — the same outbound frontier --type keeps in the
// instance aspect.
func SchemaTypes(resolved *schema.ResolvedSchema, typeFilter *string) []string {
	if typeFilter == nil {
		return append([]string{}, resolved.Types.Keys()...)
	}
	out := []string{*typeFilter}
	seen := map[string]bool{*typeFilter: true}
	rtype, _ := resolved.Types.Get(*typeFilter)
	for _, rel := range ownRelations(resolved, rtype) {
		if rel.Kind == schema.KindAny {
			continue
		}
		for _, target := range rel.Targets {
			if !seen[target] && resolved.Types.Has(target) {
				seen[target] = true
				out = append(out, target)
			}
		}
	}
	return out
}

// SchemaElements draws the boxes for types and the relations that leave them.
//
// Unfiltered, every type's own relations are drawn and the `any` box carries
// the base block: its attributes, and its relations as `any` -> `any`. Under a
// type filter only that type's relations are drawn — any other box in types is
// there as a target (or, in the combined view, as the type of a drawn entity).
// The base relations then leave the filtered type for `any`, because they are
// part of that type's outbound frontier: the instance half draws its
// `depends_on` edges, and the schema half has to say they are legal.
func SchemaElements(resolved *schema.ResolvedSchema, types []string, typeFilter *string) *Elements {
	els := &Elements{Nodes: []*omap.Map{}, Edges: []*omap.Map{}}
	drawn := map[string]bool{}
	for _, t := range types {
		drawn[t] = true
	}
	sources := types
	if typeFilter != nil {
		sources = []string{*typeFilter}
	}

	needAny := typeFilter == nil
	for _, t := range types {
		rtype, _ := resolved.Types.Get(t)
		var attrs []*schema.ResolvedAttribute
		for _, name := range rtype.Attributes.Keys() {
			a, _ := rtype.Attributes.Get(name)
			// A base attribute lives on the `any` box. One the type redeclares
			// stays here, because the redeclaration is what the type says.
			if resolved.BaseAttributes.Has(name) && !a.OverriddenFromBase {
				continue
			}
			attrs = append(attrs, a)
		}
		els.Nodes = append(els.Nodes, schemaNode(SchemaID(t), t, t, attrs))
	}
	for _, t := range sources {
		rtype, _ := resolved.Types.Get(t)
		for _, rel := range ownRelations(resolved, rtype) {
			if rel.Kind == schema.KindAny {
				needAny = true
				els.Edges = append(els.Edges, schemaEdge(SchemaID(t), SchemaAny, rel))
				continue
			}
			for _, target := range rel.Targets {
				if drawn[target] {
					els.Edges = append(els.Edges, schemaEdge(SchemaID(t), SchemaID(target), rel))
				}
			}
		}
	}
	// The base relations are declared on every type and point at any type,
	// so from `any` to `any` is exactly their claim — or, under a filter,
	// from the filtered type to `any`.
	baseFrom := SchemaAny
	if typeFilter != nil {
		baseFrom = SchemaID(*typeFilter)
	}
	for _, p := range resolved.BaseRelations.Keys() {
		rel, _ := resolved.BaseRelations.Get(p)
		needAny = true
		els.Edges = append(els.Edges, schemaEdge(baseFrom, SchemaAny, rel))
	}
	if needAny {
		var attrs []*schema.ResolvedAttribute
		for _, name := range resolved.BaseAttributes.Keys() {
			a, _ := resolved.BaseAttributes.Get(name)
			attrs = append(attrs, a)
		}
		els.Nodes = append(els.Nodes, schemaNode(SchemaAny, "any", "", attrs))
	}
	return els
}

// ownRelations is a type's relations minus the base block's, in declaration
// order — what the type itself declares, as `khub schema edges` counts it.
func ownRelations(resolved *schema.ResolvedSchema, rtype *schema.ResolvedType) []*schema.ResolvedRelation {
	var out []*schema.ResolvedRelation
	for _, p := range rtype.Relations.Keys() {
		if resolved.BaseRelations.Has(p) {
			continue
		}
		rel, _ := rtype.Relations.Get(p)
		out = append(out, rel)
	}
	return out
}

// schemaNode is one class box. The label is the type name over one line per
// attribute; the box is sized from the label here, because the style's
// `width: label` is deprecated in Cytoscape and the label is monospace, so its
// extent is a character count.
func schemaNode(id, name, typeName string, attrs []*schema.ResolvedAttribute) *omap.Map {
	lines := []string{name}
	for _, a := range attrs {
		lines = append(lines, attrLine(a))
	}
	widest := 0
	for _, l := range lines {
		if n := len([]rune(l)); n > widest {
			widest = n
		}
	}
	data := omap.New()
	data.Set("id", id)
	data.Set("label", strings.Join(lines, "\n"))
	data.Set("type", typeName)
	data.Set("aspect", AspectSchema)
	data.Set("width", widest*schemaCharWidth+schemaPad)
	data.Set("height", len(lines)*schemaLineHeight+schemaPad)
	el := omap.New()
	el.Set("data", data)
	return el
}

// The box geometry for a 7px monospace label: 0.6em per glyph, 1.2em per line.
const (
	schemaFontSize   = 7
	schemaCharWidth  = 5 // 4.2 rounded up, so a long line never touches the edge
	schemaLineHeight = 9
	schemaPad        = 14
)

func schemaEdge(source, target string, rel *schema.ResolvedRelation) *omap.Map {
	data := omap.New()
	data.Set("source", source)
	data.Set("target", target)
	label := rel.Predicate + " " + multiplicity(rel.Required, rel.Many)
	if rel.Inverse != nil {
		// The inverse is not a second edge: khub derives it at read time from
		// this one, so it is named on the edge it is read from.
		label += " / " + *rel.Inverse
	}
	data.Set("label", label)
	data.Set("aspect", AspectSchema)
	data.Set("predicate", rel.Predicate)
	data.Set("required", rel.Required)
	data.Set("many", rel.Many)
	el := omap.New()
	el.Set("data", data)
	return el
}

// attrLine is a UML attribute line: `name: type [multiplicity]`, with an enum
// shown as its values, since the values are what an author has to write. A
// list holds many values, so its upper bound is `*`.
func attrLine(a *schema.ResolvedAttribute) string {
	kind := a.BaseType
	if len(a.Enum) > 0 {
		vals := make([]string, len(a.Enum))
		for i, v := range a.Enum {
			vals[i] = enumValue(v)
		}
		kind = "{" + strings.Join(vals, "|") + "}"
	}
	return a.Name + ": " + kind + " " + multiplicity(a.Required, a.BaseType == "list")
}

// enumValue quotes a value that would read as the notation's own syntax — a
// separator, a brace, a quote — or that is empty or padded, so `{a|b}` always
// means two values.
func enumValue(v string) string {
	if v == "" || strings.ContainsAny(v, "|{}\"\\") || strings.TrimSpace(v) != v {
		return strconv.Quote(v)
	}
	return v
}

// multiplicity is UML's: required sets the lower bound, many the upper.
func multiplicity(required, many bool) string {
	lower, upper := 0, "1"
	if required {
		lower = 1
	}
	if many {
		upper = "*"
	}
	if strconv.Itoa(lower) == upper {
		return "[" + upper + "]"
	}
	return "[" + strconv.Itoa(lower) + ".." + upper + "]"
}

// schemaStyle is appended after the per-type colours, so a box keeps its
// type's colour and changes only its shape. Required relations are solid and
// optional ones dashed — the reading Graffoo gives a mandatory property — and
// an instance-of edge is the dotted line UML draws for a realization.
func schemaStyle() []any {
	box := omap.New()
	box.Set("shape", "round-rectangle")
	box.Set("width", "data(width)")
	box.Set("height", "data(height)")
	box.Set("font-size", schemaFontSize)
	box.Set("font-family", "monospace")
	box.Set("text-wrap", "wrap")
	box.Set("text-valign", "center")
	box.Set("text-halign", "center")
	box.Set("text-justification", "left")

	anyBox := omap.New()
	anyBox.Set("background-color", "#666")

	rel := omap.New()
	rel.Set("line-color", "#888")
	rel.Set("target-arrow-color", "#888")
	rel.Set("color", "#444")
	rel.Set("font-size", 7)

	optional := omap.New()
	optional.Set("line-style", "dashed")

	instanceOf := omap.New()
	instanceOf.Set("line-style", "dotted")
	instanceOf.Set("line-color", "#ddd")
	instanceOf.Set("target-arrow-shape", "triangle-backcurve")
	instanceOf.Set("target-arrow-color", "#ddd")

	return []any{
		rule(`node[aspect="schema"]`, box),
		rule(`node[id="`+SchemaAny+`"]`, anyBox),
		rule(`edge[aspect="schema"]`, rel),
		rule(`edge[aspect="schema"][!required]`, optional),
		rule(`edge[aspect="instance_of"]`, instanceOf),
	}
}

// layoutFor is the Cytoscape layout option literal for an aspect.
//
// The instance aspect keeps the literal it always had, byte for byte. The
// schema aspect is a ring: serve's schema view tried force-directed and
// layered layouts on the same picture and kept the ring, because every type is
// equidistant and none is flung into a corner. The combined view is layered
// with the type boxes as roots, which is what puts the types above the
// entities that realize them.
func layoutFor(aspect string) string {
	switch aspect {
	case AspectSchema:
		return "{ name: 'circle', avoidOverlap: true, nodeDimensionsIncludeLabels: true }"
	case AspectBoth:
		return "{ name: 'breadthfirst', roots: 'node[aspect=\"schema\"]', avoidOverlap: true, nodeDimensionsIncludeLabels: true }"
	}
	return "{ name: 'cose' }"
}
