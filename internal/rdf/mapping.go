// mapping.go walks the resolved schema into the document docs/rdf-mapping.md
// specifies: one rdfs:Class and one sh:NodeShape per type, one shared
// rdf:Property per predicate name, SHACL constraints for what the ontology
// layer says, and khub: annotations for what it cannot say (storage, policy,
// acyclic, inverse). Zero per-type code: every triple comes from a field of
// schema.ResolvedSchema.

package rdf

import (
	"strings"

	"github.com/endgame-build/khub/internal/schema"
)

// Namespaces fixes where minted IRIs live. Vocab is khub's own annotation
// vocabulary and never moves; the other three hang off the --base root and
// the preset name, so two workspaces of one preset share class and predicate
// IRIs (the federation property) while each keeps its own entity IRIs.
type Namespaces struct {
	Vocab    string // urn:khub:vocab/
	Ontology string // <base>ontology/<preset>/
	Type     string // <ontology>type/
	Field    string // <ontology>field/
	Shape    string // <ontology>shape/
	Entity   string // <base>workspace/<name>/
}

// DefaultBase is the root every minted IRI hangs off unless --base says
// otherwise. A URN: it claims no host, and RFC 8141 lets the NSS carry the
// slash-separated path the layout below needs.
const DefaultBase = "urn:khub:"

// VocabIRI is the khub annotation vocabulary's namespace, fixed across
// presets, workspaces and --base: a consumer reads khub:acyclic the same way
// from every export.
const VocabIRI = "urn:khub:vocab/"

// TypeKey is khub's reserved discriminator field. The base block declares it
// as an attribute, but in RDF the class membership is rdf:type, so the walk
// maps it there instead of minting a property for it.
const TypeKey = "type"

// Mint builds the namespaces for one export. ontology is the preset name,
// falling back to the workspace name when the workspace records none.
func Mint(base, preset, workspace string) Namespaces {
	if base == "" {
		base = DefaultBase
	}
	key := preset
	if key == "" {
		key = workspace
	}
	ont := base + "ontology/" + segment(key) + "/"
	return Namespaces{
		Vocab:    VocabIRI,
		Ontology: ont,
		Type:     ont + "type/",
		Field:    ont + "field/",
		Shape:    ont + "shape/",
		Entity:   base + "workspace/" + segment(workspace) + "/",
	}
}

// The prefix block, in the order every export declares it.
var standardPrefixes = []Prefix{
	{"rdf", "http://www.w3.org/1999/02/22-rdf-syntax-ns#"},
	{"rdfs", "http://www.w3.org/2000/01/rdf-schema#"},
	{"owl", "http://www.w3.org/2002/07/owl#"},
	{"xsd", "http://www.w3.org/2001/XMLSchema#"},
	{"sh", "http://www.w3.org/ns/shacl#"},
}

func pn(prefix, local string) IRI { return IRI{Prefixed: prefix + ":" + local} }

// Well-known terms.
var (
	rdfType          = pn("rdf", "type")
	rdfProperty      = pn("rdf", "Property")
	rdfJSON          = pn("rdf", "JSON")
	rdfsClass        = pn("rdfs", "Class")
	rdfsSubClassOf   = pn("rdfs", "subClassOf")
	rdfsLabel        = pn("rdfs", "label")
	rdfsComment      = pn("rdfs", "comment")
	owlOntology      = pn("owl", "Ontology")
	owlDatatypeProp  = pn("owl", "DatatypeProperty")
	owlObjectProp    = pn("owl", "ObjectProperty")
	shNodeShape      = pn("sh", "NodeShape")
	shTargetClass    = pn("sh", "targetClass")
	shProperty       = pn("sh", "property")
	shPath           = pn("sh", "path")
	shName           = pn("sh", "name")
	shOrder          = pn("sh", "order")
	shDatatype       = pn("sh", "datatype")
	shMinCount       = pn("sh", "minCount")
	shMaxCount       = pn("sh", "maxCount")
	shIn             = pn("sh", "in")
	shPattern        = pn("sh", "pattern")
	shNodeKind       = pn("sh", "nodeKind")
	shIRI            = pn("sh", "IRI")
	shClass          = pn("sh", "class")
	shOr             = pn("sh", "or")
	xsdString        = pn("xsd", "string")
	xsdBoolean       = pn("xsd", "boolean")
	xsdDecimal       = pn("xsd", "decimal")
	xsdDate          = pn("xsd", "date")
	xsdDateTime      = pn("xsd", "dateTime")
	khubEntity       = pn("khub", "Entity")
	khubPreset       = pn("khub", "preset")
	khubPresetVer    = pn("khub", "presetVersion")
	khubWorkspace    = pn("khub", "workspace")
	khubRequiredScop = pn("khub", "requiredScope")
	khubLayout       = pn("khub", "layout")
	khubFormat       = pn("khub", "format")
	khubPath         = pn("khub", "path")
	khubIDShape      = pn("khub", "idShape")
	khubTemplate     = pn("khub", "template")
	khubReqSingleton = pn("khub", "requiredSingleton")
	khubOrphanExempt = pn("khub", "orphanExempt")
	khubWhen         = pn("khub", "when")
	khubType         = pn("khub", "type")
	khubDefault      = pn("khub", "default")
	khubPattern      = pn("khub", "pattern")
	khubKind         = pn("khub", "kind")
	khubAcyclic      = pn("khub", "acyclic")
	khubInverse      = pn("khub", "inverse")
	khubPathRefs     = pn("khub", "pathRefs")
)

// vocabulary is the khub: block every export opens with, so the file is
// self-describing: each term declared once, in this order, with the one-line
// meaning docs/rdf-mapping.md gives it.
var vocabulary = []struct {
	term    IRI
	class   bool
	comment string
}{
	{khubEntity, true, "Every khub entity. Each type is a subclass."},
	{khubPreset, false, "On the ontology: the preset the workspace was initialized from."},
	{khubPresetVer, false, "On the ontology: that preset's version."},
	{khubWorkspace, false, "On the ontology: the workspace this export was rendered from."},
	{khubRequiredScop, false, "On the ontology: sh:minCount is khub's required gate, enforced on this subset of entities (drafts are exempt)."},
	{khubLayout, false, "Storage: file, folder, collection or singleton."},
	{khubFormat, false, "Storage: md, json, yaml or jsonl."},
	{khubPath, false, "Storage: where the type's entities live, one value per path template."},
	{khubIDShape, false, "Storage: the id a new entity is minted with."},
	{khubTemplate, false, "Storage: the body template stem the type declares."},
	{khubReqSingleton, false, "Policy: a singleton `check` requires to exist."},
	{khubOrphanExempt, false, "Policy: instances are exempt from the orphan sweep."},
	{khubWhen, false, "The moment to capture an entity of this type, in domain language."},
	{khubType, false, "The khub scalar kind of an attribute: text, number, date, datetime, bool or list."},
	{khubDefault, false, "The value an absent attribute reads as."},
	{khubPattern, false, "The authoritative RE2 pattern; sh:pattern is its portable rendering when one exists."},
	{khubKind, false, "A relation's target form: typed, union or any."},
	{khubAcyclic, false, "`check` reports a cycle over this relation."},
	{khubInverse, false, "The derived inverse predicate the target answers to; never stored."},
	{khubPathRefs, false, "A value may name a workspace file by path instead of an entity."},
}

// Provenance is the header the ontology carries.
type Provenance struct{ Preset, Version, Workspace string }

// Schema renders the TBox: ontology header, the khub vocabulary, one class
// per type, one property per predicate, one shape per type.
func Schema(resolved *schema.ResolvedSchema, ns Namespaces, prov Provenance) *Doc {
	doc := &Doc{
		Comment: []string{
			"khub RDF projection: RDFS + SHACL over the resolved schema. Derived, never",
			"edited by hand; `khub schema rdf` regenerates it. See docs/rdf-mapping.md.",
		},
	}
	doc.Prefixes = append(doc.Prefixes, standardPrefixes...)
	doc.Prefixes = append(doc.Prefixes,
		Prefix{"khub", ns.Vocab},
		Prefix{"type", ns.Type},
		Prefix{"field", ns.Field},
		Prefix{"shape", ns.Shape},
	)

	ont := &Subject{IRI: IRI{Full: ns.Ontology}}
	ont.Add(rdfType, owlOntology)
	ont.Add(rdfsLabel, Str(labelOf(prov)))
	ont.Add(khubPreset, Str(prov.Preset))
	ont.Add(khubPresetVer, Str(prov.Version))
	ont.Add(khubWorkspace, Str(prov.Workspace))
	ont.Add(khubRequiredScop, Str("active"))
	doc.Subjects = append(doc.Subjects, ont)

	for _, v := range vocabulary {
		s := &Subject{IRI: v.term}
		if v.class {
			s.Add(rdfType, rdfsClass)
		} else {
			s.Add(rdfType, rdfProperty)
		}
		s.Add(rdfsComment, Str(v.comment))
		doc.Subjects = append(doc.Subjects, s)
	}

	m := &mapper{ns: ns, resolved: resolved}
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		doc.Subjects = append(doc.Subjects, m.class(rtype))
	}
	doc.Subjects = append(doc.Subjects, m.properties()...)
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		doc.Subjects = append(doc.Subjects, m.shape(rtype))
	}
	return doc
}

func labelOf(prov Provenance) string {
	if prov.Preset != "" {
		return prov.Preset
	}
	return prov.Workspace
}

type mapper struct {
	ns       Namespaces
	resolved *schema.ResolvedSchema
}

// typeIRI is the class of a type.
func (m *mapper) typeIRI(name string) IRI {
	if localOK(name) {
		return pn("type", name)
	}
	return IRI{Full: m.ns.Type + segment(name)}
}

func (m *mapper) shapeIRI(name string) IRI {
	if localOK(name) {
		return pn("shape", name)
	}
	return IRI{Full: m.ns.Shape + segment(name)}
}

// fieldIRI is the one predicate IRI a name mints, shared by every type that
// declares it. A base predicate lives in the khub vocabulary, because the
// base block is khub's own and the same across presets; a domain predicate
// lives in the preset's namespace.
func (m *mapper) fieldIRI(name string) IRI {
	if m.resolved.BaseAttributes.Has(name) || m.resolved.BaseRelations.Has(name) {
		if localOK(name) {
			return pn("khub", name)
		}
		return IRI{Full: m.ns.Vocab + segment(name)}
	}
	if localOK(name) {
		return pn("field", name)
	}
	return IRI{Full: m.ns.Field + segment(name)}
}

func (m *mapper) class(t *schema.ResolvedType) *Subject {
	s := &Subject{IRI: m.typeIRI(t.Name)}
	s.Add(rdfType, rdfsClass)
	s.Add(rdfsSubClassOf, khubEntity)
	s.Add(rdfsLabel, Str(t.Name))
	if t.When != nil {
		s.Add(khubWhen, Str(*t.When))
	}
	s.Add(khubLayout, Str(t.Storage.Layout))
	s.Add(khubFormat, Str(t.Storage.Fmt))
	if t.Templated() {
		for _, tp := range t.Storage.Templates {
			s.Add(khubPath, Str(tp.String()))
		}
	} else {
		s.Add(khubPath, Str(t.StorageRelpath()))
	}
	if shape := t.IDShape(); shape != "" {
		s.Add(khubIDShape, Str(shape))
	}
	if t.Template != nil {
		s.Add(khubTemplate, Str(*t.Template))
	}
	if t.Required {
		s.Add(khubReqSingleton, Bool(true))
	}
	if t.Orphan {
		s.Add(khubOrphanExempt, Bool(true))
	}
	return s
}

// properties declares each predicate name once, in first-declaration order
// across the types: a datatype property when every declaration is an
// attribute, an object property when every one is a relation, and a bare
// rdf:Property when the name is both on different types (RDFS allows it;
// each shape says which it is there).
func (m *mapper) properties() []*Subject {
	type use struct{ attr, rel bool }
	uses := map[string]*use{}
	var order []string
	note := func(name string, attr bool) {
		u, ok := uses[name]
		if !ok {
			u = &use{}
			uses[name] = u
			order = append(order, name)
		}
		if attr {
			u.attr = true
		} else {
			u.rel = true
		}
	}
	for _, tn := range m.resolved.Types.Keys() {
		t, _ := m.resolved.Types.Get(tn)
		for _, an := range t.Attributes.Keys() {
			if an != TypeKey {
				note(an, true)
			}
		}
		for _, rn := range t.Relations.Keys() {
			note(rn, false)
		}
	}
	out := make([]*Subject, 0, len(order))
	for _, name := range order {
		u := uses[name]
		s := &Subject{IRI: m.fieldIRI(name)}
		s.Add(rdfType, rdfProperty)
		switch {
		case u.attr && u.rel:
			s.Add(rdfsLabel, Str(name))
			s.Add(rdfsComment, Str("An attribute on some types and a relation on others; each shape says which."))
			out = append(out, s)
			continue
		case u.attr:
			s.Add(rdfType, owlDatatypeProp)
		default:
			s.Add(rdfType, owlObjectProp)
		}
		s.Add(rdfsLabel, Str(name))
		out = append(out, s)
	}
	return out
}

func (m *mapper) shape(t *schema.ResolvedType) *Subject {
	s := &Subject{IRI: m.shapeIRI(t.Name)}
	s.Add(rdfType, shNodeShape)
	s.Add(shTargetClass, m.typeIRI(t.Name))
	order := 0
	for _, an := range t.Attributes.Keys() {
		if an == TypeKey {
			continue
		}
		a, _ := t.Attributes.Get(an)
		s.Add(shProperty, m.attributeShape(a, order))
		order++
	}
	for _, rn := range t.Relations.Keys() {
		r, _ := t.Relations.Get(rn)
		s.Add(shProperty, m.relationShape(r, order))
		order++
	}
	return s
}

// datatypes maps the khub scalar kinds onto XSD. `list` has no datatype: its
// items are whatever the author wrote.
var datatypes = map[string]IRI{
	"text":     xsdString,
	"number":   xsdDecimal,
	"date":     xsdDate,
	"datetime": xsdDateTime,
	"bool":     xsdBoolean,
}

func (m *mapper) attributeShape(a *schema.ResolvedAttribute, order int) Blank {
	b := Blank{}
	add := func(p IRI, o Term) { b.Pairs = append(b.Pairs, Pair{Pred: p, Obj: o}) }
	add(shPath, m.fieldIRI(a.Name))
	add(shName, Str(a.Name))
	add(shOrder, Int(order))
	if dt, ok := datatypes[a.BaseType]; ok {
		add(shDatatype, dt)
	}
	if a.Required {
		add(shMinCount, Int(1))
	}
	if a.BaseType != "list" {
		add(shMaxCount, Int(1))
	}
	if len(a.Enum) > 0 {
		items := make([]Term, len(a.Enum))
		for i, e := range a.Enum {
			items[i] = Str(e)
		}
		add(shIn, List{Items: items})
	}
	if a.Pattern != nil {
		if Portable(*a.Pattern) {
			add(shPattern, Str(Anchored(*a.Pattern)))
		}
		add(khubPattern, Str(*a.Pattern))
	}
	add(khubType, Str(a.BaseType))
	if a.Default != nil {
		add(khubDefault, literal(a.Default, a.BaseType))
	}
	return b
}

func (m *mapper) relationShape(r *schema.ResolvedRelation, order int) Blank {
	b := Blank{}
	add := func(p IRI, o Term) { b.Pairs = append(b.Pairs, Pair{Pred: p, Obj: o}) }
	add(shPath, m.fieldIRI(r.Predicate))
	add(shName, Str(r.Predicate))
	add(shOrder, Int(order))
	add(shNodeKind, shIRI)
	switch r.Kind {
	case schema.KindTyped:
		add(shClass, m.typeIRI(r.Targets[0]))
	case schema.KindUnion:
		items := make([]Term, len(r.Targets))
		for i, target := range r.Targets {
			items[i] = Blank{Pairs: []Pair{{Pred: shClass, Obj: m.typeIRI(target)}}}
		}
		add(shOr, List{Items: items})
	default:
		add(shClass, khubEntity)
	}
	if r.Required {
		add(shMinCount, Int(1))
	}
	if !r.Many {
		add(shMaxCount, Int(1))
	}
	add(khubKind, Str(r.Kind))
	if r.Acyclic {
		add(khubAcyclic, Bool(true))
	}
	if r.Inverse != nil {
		add(khubInverse, Str(*r.Inverse))
	}
	if r.ByPath {
		add(khubPathRefs, Bool(true))
	}
	return b
}

// Portable reports whether an RE2 pattern reads the same as an XPath/SPARQL
// regex, which is what sh:pattern is matched with. The test is a conservative
// syntactic one — it refuses every RE2 construct XPath lacks or reads
// differently — so a pattern it passes means the same thing on both sides,
// and a pattern it refuses is still carried as khub:pattern.
func Portable(p string) bool {
	if strings.Contains(p, "(?") || strings.Contains(p, "[:") || strings.Contains(p, "-[") {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] != '\\' {
			continue
		}
		if i+1 >= len(p) {
			return false
		}
		next := p[i+1]
		switch {
		case strings.IndexByte("afveAzZQECbBhHxX", next) >= 0:
			return false
		case next >= '0' && next <= '9':
			return false
		case next == 'p' || next == 'P':
			if i+2 >= len(p) || p[i+2] != '{' {
				return false
			}
		}
		i++
	}
	return true
}

// Anchored renders a pattern for sh:pattern, which a validator searches
// rather than full-matches as khub does. A pattern that already anchors
// itself at both ends with no top-level alternation (`^[0-9]+$`) goes out as
// written; any other is wrapped in `^(…)$`.
func Anchored(p string) string {
	if len(p) >= 2 && p[0] == '^' && p[len(p)-1] == '$' && !escapedEnd(p) && !topLevelBar(p) {
		return p
	}
	return "^(" + p + ")$"
}

// escapedEnd reports whether the pattern's final character is escaped
// (`\$`), in which case it is not an anchor.
func escapedEnd(p string) bool {
	slashes := 0
	for i := len(p) - 2; i >= 0 && p[i] == '\\'; i-- {
		slashes++
	}
	return slashes%2 == 1
}

// topLevelBar reports an alternation outside every group and class, which
// would make `^` and `$` anchor only the outer branches.
func topLevelBar(p string) bool {
	depth, inClass := 0, false
	for i := 0; i < len(p); i++ {
		switch c := p[i]; {
		case c == '\\':
			i++
		case inClass:
			inClass = c != ']'
		case c == '[':
			inClass = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == '|' && depth == 0:
			return true
		}
	}
	return false
}
