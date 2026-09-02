// Package schema ports khub's ontology layer: the authored-vocabulary
// meta-schema (core/schema_model.py), the resolver (core/resolve.py), and the
// resolved in-memory model (core/model.py).
package schema

// This file ports src/khub/core/schema_model.py — the khub meta-schema
// (WPK-000-1) — declaration structs plus the whole pydantic validation layer:
// extra="forbid" everywhere (unknown keys become raw_linkml_smuggled), the
// Literal value sets, the id-prefix pattern, and the storage matrix
// (@model_validator _storage_matrix). Every ValueError message is byte-exact;
// pydantic-core catalog messages ("Input should be …", "Field required") are
// reproduced for the shapes that arise from YAML input.
//
// Tri-state pydantic semantics map as: *bool / *string nil = "field absent"
// (an override inherits the base), a non-nil pointer = "field authored" (an
// explicit `required: false` beats an inherited true). TypeDecl.FormatSet is
// the model_fields_set analog for `format` — the storage matrix must tell an
// authored `format: md` (rejected on a collection) from the field default
// (derivable from the path suffix).

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/omap"
)

// Scalar attribute types (schema_model.ScalarType).
var scalarTypes = []string{"text", "number", "date", "datetime", "bool", "list"}

// Layout literal set (schema_model.TypeDecl.layout).
var layoutLiterals = []string{"file", "folder", "collection", "singleton"}

// The format/layout compatibility matrix's two sides (core/formats.py
// PER_ITEM / COLLECTION). jsonl is collection-only; md is per-item only;
// gjson is named in the grammar but undefined — rejected everywhere.
var (
	PerItem    = map[string]bool{"md": true, "json": true, "yaml": true}
	Collection = map[string]bool{"json": true, "jsonl": true, "yaml": true}
)

// IDPrefixPattern is schema_model.ID_PREFIX_RE: a prefix is a slug token — it
// is concatenated with the date (when `id_date`) and the slugified title. An
// empty one would mint a leading hyphen; a hyphenated one would make the
// prefix unreadable back out of the id.
const IDPrefixPattern = `^[a-z][a-z0-9]*$`

var idPrefixRE = regexp.MustCompile(IDPrefixPattern)

// TemplateNamePattern constrains a declared template to a bare file stem —
// resolved inside .khub/templates/, so a separator would escape the directory.
const TemplateNamePattern = `^[a-z][a-z0-9-]*$`

var templateNameRE = regexp.MustCompile(TemplateNamePattern)

// AttrDecl is a scalar or enum attribute declaration. Type may be omitted on
// an override (it is inherited from the base) or when Enum is given.
type AttrDecl struct {
	Type *string // nil = not declared
	// nil = not declared: an override inherits the base's required, while an
	// explicit `required: false` is distinguishable and wins over the base.
	Required *bool
	Default  any      // nil = not declared (Python cannot tell `default: null` from absent either)
	Enum     []string // nil/empty = not declared (Python truthiness: [] inherits the base's)
	Pattern  *string
}

// ToDecl is the `to:` union — a single type name (or the literal "any"), or a
// list of type names.
type ToDecl struct {
	One    string
	List   []string
	IsList bool
}

// RelationDecl is a relation declaration. The field name (the map key) is the
// predicate. Inverse names the read-time derived edge on the target (never
// stored); firm-ops declares none.
type RelationDecl struct {
	To       ToDecl
	Many     bool
	Required bool
	Inverse  *string
	// `check` reports elementary and self cycles over every acyclic predicate.
	Acyclic bool
}

// IdPrefixDecl is a prefix chosen by the value of another attribute (By), one
// per enum member. Map iterates in authored order.
type IdPrefixDecl struct {
	By  string
	Map *Ordered[string]
}

// IdPrefixSpec is the authored id_prefix union: a literal slug token or a
// by-value decl. Exactly one field is non-nil.
type IdPrefixSpec struct {
	Literal *string
	Decl    *IdPrefixDecl
}

// TypeDecl is one entity type's storage config plus its attribute/relation
// deltas. See schema_model.TypeDecl for the full storage-matrix commentary.
type TypeDecl struct {
	Layout string
	Path   *string
	Format string // post-storageMatrix: the effective format
	// FormatSet is the model_fields_set analog: format was authored, not
	// defaulted — an authored `format: md` on a collection is rejected where
	// the default would derive from the path suffix.
	FormatSet bool
	// Singleton-only: `check` reports a missing required singleton.
	Required bool
	// Opt out of the orphan sweep (see schema_model.TypeDecl.orphan).
	Orphan bool
	// Prefixed ids: `add` mints `<prefix>-<slug>` instead of a bare slug.
	IdPrefix *IdPrefixSpec
	// Dated ids: the minted id carries the day it was minted on
	// (`<prefix>-<YYYY-MM-DD>-<slug>`). A flat sibling of IdPrefix, not part of
	// it: the two resolve independently, so a by-value prefix and a date
	// compose. Default false.
	IdDate bool
	// The declared template stem (.khub/templates/<name>.yaml). nil = the
	// standing convention (the type's own name); see TemplateOff for the
	// explicit opt-out. A NAME, never a path: templates live in one directory,
	// which keeps init's copy and the per-file preserve trivial, and lets two
	// types share one template.
	Template *string
	// `template: false` — explicitly untemplated even if a conventionally
	// named file exists.
	TemplateOff bool
	// The moment this type should be captured, in one line of domain language.
	When       *string
	Attributes *Ordered[*AttrDecl]
	Relations  *Ordered[*RelationDecl]
}

// BaseBlock is the base block: attributes and relations every entity inherits.
type BaseBlock struct {
	Attributes *Ordered[*AttrDecl]
	Relations  *Ordered[*RelationDecl]
}

// SchemaFile is a whole authored schema input, post-merge: the base block plus
// one merged TypeDecl per type — each type's ontology delta annotated with its
// policy gates and storage config, joined by type name.
type SchemaFile struct {
	Base     *BaseBlock
	Entities *Ordered[*TypeDecl]
	// Names annotated by policy/storage that ontology never declared. Ontology
	// is the layer that says which types EXIST; the other two only annotate. A
	// name here is a typo or a stale entry, and Resolve turns it into a located
	// error naming the layer — collected rather than raised inline so the whole
	// document is walked first, as the vocabulary walk does everywhere else.
	UnknownPolicy  []string
	UnknownStorage []string
}

// --- the storage matrix (schema_model.TypeDecl._storage_matrix) --------------

func declHas[V any](m *Ordered[V], k string) bool { return m != nil && m.Has(k) }

// storageMatrix is the @model_validator(mode="after") of TypeDecl: it derives
// the effective format (mutating Format) and rejects illegal layout×format
// cells. Error messages are byte-exact ValueError strings; the vocabulary walk
// wraps them with pydantic's "Value error, " prefix.
func (t *TypeDecl) storageMatrix() error {
	switch t.Layout {
	case "singleton":
		if t.Path == nil || *t.Path == "" {
			return errors.New("a singleton type needs path: the exact file it lives at")
		}
		suffix := strings.TrimLeft(pySuffix(*t.Path), ".")
		fmtv := t.Format
		if !t.FormatSet {
			if suffix != "" {
				fmtv = suffix
			} else {
				fmtv = "md"
			}
		}
		if !PerItem[fmtv] {
			return fmt.Errorf("format '%s' is not supported for a singleton; use md, json, or yaml", fmtv)
		}
		if suffix != "" && suffix != fmtv {
			return fmt.Errorf("path suffix '.%s' disagrees with format '%s'", suffix, fmtv)
		}
		t.Format = fmtv
	case "collection":
		suffix := ""
		if t.Path != nil && *t.Path != "" {
			suffix = strings.TrimLeft(pySuffix(*t.Path), ".")
		}
		// FormatSet distinguishes an authored `format: md` (rejected — md is
		// never a collection format) from the field default (derivable from
		// the path suffix).
		fmtv := t.Format
		if !t.FormatSet {
			fmtv = suffix
		}
		if fmtv == "" {
			return errors.New("a collection type needs format: json|jsonl|yaml " +
				"(or a path carrying that extension)")
		}
		if !Collection[fmtv] {
			return fmt.Errorf("format '%s' is not a collection format; use json, jsonl, or yaml "+
				"(md is per-item only)", fmtv)
		}
		if t.FormatSet && suffix != "" && suffix != fmtv {
			return fmt.Errorf("path suffix '.%s' disagrees with format '%s'", suffix, fmtv)
		}
		t.Format = fmtv
		for _, reserved := range []string{"slug", "type"} {
			if declHas(t.Attributes, reserved) || declHas(t.Relations, reserved) {
				return fmt.Errorf("'%s' is a reserved row key on a collection type "+
					"(row identity / the schema binding); rename the field", reserved)
			}
		}
	default:
		if !PerItem[t.Format] {
			return fmt.Errorf("format '%s' is not supported for a file/folder layout; "+
				"use md, json, or yaml (jsonl is collection-only, gjson is deferred)", t.Format)
		}
	}
	// On any non-md type the `body` key is the prose channel: a field so named
	// would be popped out of meta on every read and clobbered on write.
	if t.Format != "md" && (declHas(t.Attributes, "body") || declHas(t.Relations, "body")) {
		return fmt.Errorf("'body' is reserved on a %s type (it is the prose channel); "+
			"rename the field or use format: md", t.Format)
	}
	// Body templates exist only where a body does: per-item/singleton md. A
	// `template:` key (the opt-out included) on a collection or non-md type
	// configures something nothing ever reads — add seeds md only, validate
	// skips non-md and collections — so it is a category error here, not a
	// permanent `check` finding for a file no verb would consult.
	if (t.Template != nil || t.TemplateOff) && (t.Layout == "collection" || t.Format != "md") {
		return errors.New("'template' applies only to per-item and singleton md types " +
			"(a collection or non-md type never reads a body template); drop the key")
	}
	// A singleton's id IS its type name, so an id scheme on one declares a
	// prefix or a date no verb would ever mint — a category error here, not a
	// permanent `check` finding.
	if (t.IdPrefix != nil || t.IdDate) && t.Layout == "singleton" {
		return errors.New("'id_prefix' and 'id_date' apply only to minting types " +
			"(a singleton's id is its type name, so it mints nothing); drop the key")
	}
	return nil
}

// pySuffix ports pathlib.PurePath.suffix: the final component's extension —
// "" for dotfiles (".gitignore") and names ending in a dot ("repo.").
func pySuffix(p string) string {
	name := p
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		name = p[i+1:]
	}
	i := strings.LastIndexByte(name, '.')
	if 0 < i && i < len(name)-1 {
		return name[i:]
	}
	return ""
}

// --- the pydantic validation walk --------------------------------------------

// vocabErr is one pydantic-core error: type + loc + msg. The resolver's
// _smuggled_error scans the whole list for extra_forbidden before falling back
// to the first error, so the walk collects everything instead of failing fast.
type vocabErr struct {
	kind string // pydantic error type: "extra_forbidden", "value_error", …
	loc  []string
	msg  string
}

type vocabCollector struct{ list []vocabErr }

func (c *vocabCollector) add(kind string, loc []string, msg string) {
	c.list = append(c.list, vocabErr{kind: kind, loc: loc, msg: msg})
}

// at extends a loc path without aliasing the parent's backing array.
func at(loc []string, parts ...string) []string {
	out := make([]string, 0, len(loc)+len(parts))
	out = append(out, loc...)
	return append(out, parts...)
}

// pydantic-core message catalog (the subset reachable from YAML input).
const (
	msgString    = "Input should be a valid string"
	msgBoolType  = "Input should be a valid boolean"
	msgBoolParse = "Input should be a valid boolean, unable to interpret input"
	msgList      = "Input should be a valid list"
	msgDict      = "Input should be a valid dictionary"
	msgMissing   = "Field required"
	msgExtra     = "Extra inputs are not permitted"
)

func msgModel(name string) string {
	return "Input should be a valid dictionary or instance of " + name
}

func msgLiteral(allowed []string) string {
	quoted := make([]string, len(allowed))
	for i, a := range allowed {
		quoted[i] = "'" + a + "'"
	}
	if len(quoted) == 1 {
		return "Input should be " + quoted[0]
	}
	return "Input should be " + strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

func msgPattern(pattern string) string {
	return "String should match pattern '" + pattern + "'"
}

// validateSchemaFile is SchemaFile.model_validate: it walks the three-layer
// shape and MERGES it into one TypeDecl per type.
//
// Ontology is walked first because it is the layer that declares which types
// exist; policy and storage only annotate types already named there. The three
// arrive already collected per layer (Resolve merges across files before
// calling), so this sees one ontology/policy/storage block regardless of how
// many documents supplied them — the layers dispatch on TOP-LEVEL KEY, never
// on filename, which is what lets them arrive as three files or as three
// blocks of one document with identical results.
//
// storageMatrix does NOT run here. It cross-checks fields from all three layers
// (Required from policy, Layout/Path/Format from storage, Attributes/Relations
// from ontology), so it can only run once the merge is complete — Resolve does
// it, beside checkCollectionPaths.
func validateSchemaFile(raw *omap.Map) (*SchemaFile, []vocabErr) {
	c := &vocabCollector{}
	sf := &SchemaFile{Entities: NewOrdered[*TypeDecl]()}

	if v, ok := raw.Get("ontology"); ok && v != nil {
		if m, isMap := v.(*omap.Map); isMap {
			if bv, has := m.Get("base"); has && bv != nil {
				sf.Base = buildBaseBlock(bv, []string{"ontology", "base"}, c)
			}
			if ev, has := m.Get("entities"); has && ev != nil {
				if em, isEntMap := ev.(*omap.Map); isEntMap {
					for _, name := range em.Keys() {
						dv, _ := em.Get(name)
						td := buildOntologyDecl(dv, []string{"ontology", "entities", name}, c)
						if td != nil {
							sf.Entities.Set(name, td)
						}
					}
				} else {
					c.add("dict_type", []string{"ontology", "entities"}, msgDict)
				}
			}
			addExtras(m, []string{"ontology"}, c, "base", "entities")
		} else {
			c.add("dict_type", []string{"ontology"}, msgDict)
		}
	}

	sf.UnknownPolicy = applyLayer(raw, "policy", sf, c, applyPolicyDecl)
	sf.UnknownStorage = applyLayer(raw, "storage", sf, c, applyStorageDecl)

	for _, k := range raw.Keys() {
		if k != "ontology" && k != "policy" && k != "storage" {
			c.add("extra_forbidden", []string{k}, msgExtra)
		}
	}
	return sf, c.list
}

// applyLayer walks one annotating layer (policy or storage), folding each entry
// into the TypeDecl ontology already declared. Names with no ontology
// declaration are returned rather than reported here — Resolve raises them as
// located errors once it has seen every file.
func applyLayer(
	raw *omap.Map, layer string, sf *SchemaFile, c *vocabCollector,
	apply func(*omap.Map, *TypeDecl, []string, *vocabCollector),
) []string {
	v, ok := raw.Get(layer)
	if !ok || v == nil {
		return nil
	}
	m, isMap := v.(*omap.Map)
	if !isMap {
		c.add("dict_type", []string{layer}, msgDict)
		return nil
	}
	var unknown []string
	for _, name := range m.Keys() {
		dv, _ := m.Get(name)
		td, declared := sf.Entities.Get(name)
		if !declared {
			unknown = append(unknown, name)
			continue
		}
		dm, isDeclMap := dv.(*omap.Map)
		if !isDeclMap {
			c.add("model_type", []string{layer, name}, msgModel("TypeDecl"))
			continue
		}
		apply(dm, td, []string{layer, name}, c)
	}
	return unknown
}

func buildBaseBlock(v any, loc []string, c *vocabCollector) *BaseBlock {
	m, ok := v.(*omap.Map)
	if !ok {
		c.add("model_type", loc, msgModel("BaseBlock"))
		return nil
	}
	bb := &BaseBlock{}
	bb.Attributes = buildAttrMap(m, "attributes", loc, c)
	bb.Relations = buildRelationMap(m, "relations", loc, c)
	addExtras(m, loc, c, "attributes", "relations")
	return bb
}

// buildOntologyDecl walks a type's ONTOLOGY delta — what is true of the domain
// regardless of khub: its attributes, its relations, and the one line of domain
// language saying when to capture it.
//
// `acyclic` rides on the relation (see RelationDecl), not here and not in
// policy: it is a property of the predicate, sitting beside to/many/inverse,
// and true of the domain rather than of this workspace's gates.
//
// Format seeds to "md" so a type with no storage entry at all still resolves;
// applyStorageDecl overwrites it when one exists.
func buildOntologyDecl(v any, loc []string, c *vocabCollector) *TypeDecl {
	m, ok := v.(*omap.Map)
	if !ok {
		c.add("model_type", loc, msgModel("TypeDecl"))
		return nil
	}
	td := &TypeDecl{Format: "md"}
	td.When = takeStrOrNil(m, "when", loc, c)
	td.Attributes = buildAttrMap(m, "attributes", loc, c)
	td.Relations = buildRelationMap(m, "relations", loc, c)
	addExtras(m, loc, c, "when", "attributes", "relations")
	return td
}

// applyPolicyDecl folds a type's POLICY delta — the gates this workspace
// demands of it — onto the declaration ontology already made.
func applyPolicyDecl(m *omap.Map, td *TypeDecl, loc []string, c *vocabCollector) {
	td.Required = takeBool(m, "required", loc, c)
	td.Orphan = takeBool(m, "orphan", loc, c)
	addExtras(m, loc, c, "required", "orphan")
}

// applyStorageDecl folds a type's STORAGE delta — where and how its bytes
// materialize — onto the declaration ontology already made.
//
// `layout` is REQUIRED whenever a storage entry exists — the pre-split
// strictness. The file/<type> default (storageDefaults) applies only to a type
// with no storage entry at all: an entry that names a path but no layout is a
// half-statement, and silently defaulting it to `file` would turn a forgotten
// `layout: collection` into a directory of strays.
func applyStorageDecl(m *omap.Map, td *TypeDecl, loc []string, c *vocabCollector) {
	if lv, has := m.Get("layout"); has {
		td.Layout = takeLiteral(lv, layoutLiterals, at(loc, "layout"), c)
	} else {
		c.add("missing", at(loc, "layout"), msgMissing)
	}
	td.Path = takeStrOrNil(m, "path", loc, c)
	if fv, has := m.Get("format"); has {
		td.FormatSet = true
		if s, isStr := fv.(string); isStr {
			td.Format = s
		} else {
			c.add("string_type", at(loc, "format"), msgString)
		}
	}
	if pv, has := m.Get("id_prefix"); has && pv != nil {
		td.IdPrefix = buildIdPrefixSpec(pv, at(loc, "id_prefix"), c)
	}
	td.IdDate = takeBool(m, "id_date", loc, c)
	if tv, has := m.Get("template"); has && tv != nil {
		switch x := tv.(type) {
		case string:
			if templateNameRE.MatchString(x) {
				td.Template = &x
			} else {
				c.add("string_pattern_mismatch", at(loc, "template"), msgPattern(TemplateNamePattern))
			}
		case bool:
			if x {
				// `template: true` declares nothing the convention does not.
				c.add("string_type", at(loc, "template"), msgString)
			} else {
				td.TemplateOff = true
			}
		default:
			c.add("string_type", at(loc, "template"), msgString)
		}
	}
	addExtras(m, loc, c, "layout", "path", "format", "id_prefix", "id_date", "template")
}

// storageDefaults fills the storage config of a type no storage layer named:
// one file per entity under a directory named for the type. Applied post-merge
// so an ontology-only workspace resolves and runs.
//
// Only the per-item directory layouts default their path. A collection's or
// singleton's path names a FILE: the collection default ({name}.{fmt}) lives in
// CollectionRelpath and is applied at read time, and a singleton with no path
// is a storage-matrix error — pre-filling either here would overwrite both
// contracts.
func (t *TypeDecl) storageDefaults(name string) {
	if t.Layout == "" {
		t.Layout = LayoutFile
	}
	if t.Layout == LayoutFile || t.Layout == LayoutFolder {
		if t.Path == nil || *t.Path == "" {
			p := name
			t.Path = &p
		}
	}
}

func buildAttrDecl(v any, loc []string, c *vocabCollector) *AttrDecl {
	m, ok := v.(*omap.Map)
	if !ok {
		c.add("model_type", loc, msgModel("AttrDecl"))
		return nil
	}
	ad := &AttrDecl{}
	if tv, has := m.Get("type"); has && tv != nil {
		s := takeLiteral(tv, scalarTypes, at(loc, "type"), c)
		if s != "" {
			ad.Type = &s
		}
	}
	if rv, has := m.Get("required"); has && rv != nil {
		if b, ok2 := boolLax(rv, at(loc, "required"), c); ok2 {
			ad.Required = &b
		}
	}
	if dv, has := m.Get("default"); has {
		ad.Default = dv
	}
	if ev, has := m.Get("enum"); has && ev != nil {
		ad.Enum = takeStrList(ev, at(loc, "enum"), c)
	}
	ad.Pattern = takeStrOrNil(m, "pattern", loc, c)
	addExtras(m, loc, c, "type", "required", "default", "enum", "pattern")
	return ad
}

func buildRelationDecl(v any, loc []string, c *vocabCollector) *RelationDecl {
	m, ok := v.(*omap.Map)
	if !ok {
		c.add("model_type", loc, msgModel("RelationDecl"))
		return nil
	}
	rd := &RelationDecl{}
	if tv, has := m.Get("to"); has {
		rd.To = buildToDecl(tv, at(loc, "to"), c)
	} else {
		c.add("missing", at(loc, "to"), msgMissing)
	}
	rd.Many = takeBool(m, "many", loc, c)
	rd.Required = takeBool(m, "required", loc, c)
	rd.Inverse = takeStrOrNil(m, "inverse", loc, c)
	rd.Acyclic = takeBool(m, "acyclic", loc, c)
	addExtras(m, loc, c, "to", "many", "required", "inverse", "acyclic")
	return rd
}

// buildToDecl validates the `to: str | list[str]` union. When both branches
// fail, each branch's errors land under its pydantic union tag ("str",
// "list[str]"), in branch declaration order.
func buildToDecl(v any, loc []string, c *vocabCollector) ToDecl {
	switch x := v.(type) {
	case string:
		return ToDecl{One: x}
	case []any:
		items := make([]string, 0, len(x))
		var itemErrs []vocabErr
		for i, it := range x {
			if s, ok := it.(string); ok {
				items = append(items, s)
			} else {
				itemErrs = append(itemErrs, vocabErr{
					kind: "string_type", loc: at(loc, "list[str]", strconv.Itoa(i)), msg: msgString,
				})
			}
		}
		if len(itemErrs) == 0 {
			return ToDecl{List: items, IsList: true}
		}
		c.add("string_type", at(loc, "str"), msgString)
		c.list = append(c.list, itemErrs...)
		return ToDecl{}
	default:
		c.add("string_type", at(loc, "str"), msgString)
		c.add("list_type", at(loc, "list[str]"), msgList)
		return ToDecl{}
	}
}

// buildIdPrefixSpec validates the id_prefix union: a pattern-constrained
// string or an IdPrefixDecl mapping. Branch tags mirror pydantic's smart-union
// error locs ("constrained-str", "IdPrefixDecl").
func buildIdPrefixSpec(v any, loc []string, c *vocabCollector) *IdPrefixSpec {
	switch x := v.(type) {
	case string:
		if idPrefixRE.MatchString(x) {
			return &IdPrefixSpec{Literal: &x}
		}
		c.add("string_pattern_mismatch", at(loc, "constrained-str"), msgPattern(IDPrefixPattern))
		c.add("model_type", at(loc, "IdPrefixDecl"), msgModel("IdPrefixDecl"))
		return nil
	case *omap.Map:
		sub := &vocabCollector{}
		decl := buildIdPrefixDecl(x, at(loc, "IdPrefixDecl"), sub)
		if len(sub.list) == 0 {
			return &IdPrefixSpec{Decl: decl}
		}
		c.add("string_type", at(loc, "constrained-str"), msgString)
		c.list = append(c.list, sub.list...)
		return nil
	default:
		c.add("string_type", at(loc, "constrained-str"), msgString)
		c.add("model_type", at(loc, "IdPrefixDecl"), msgModel("IdPrefixDecl"))
		return nil
	}
}

func buildIdPrefixDecl(m *omap.Map, loc []string, c *vocabCollector) *IdPrefixDecl {
	d := &IdPrefixDecl{Map: NewOrdered[string]()}
	if bv, has := m.Get("by"); has {
		if s, ok := bv.(string); ok {
			d.By = s
		} else {
			c.add("string_type", at(loc, "by"), msgString)
		}
	} else {
		c.add("missing", at(loc, "by"), msgMissing)
	}
	if mv, has := m.Get("map"); has {
		if mm, ok := mv.(*omap.Map); ok {
			for _, k := range mm.Keys() {
				vv, _ := mm.Get(k)
				s, isStr := vv.(string)
				if !isStr {
					c.add("string_type", at(loc, "map", k), msgString)
					continue
				}
				if !idPrefixRE.MatchString(s) {
					c.add("string_pattern_mismatch", at(loc, "map", k), msgPattern(IDPrefixPattern))
					continue
				}
				d.Map.Set(k, s)
			}
		} else {
			c.add("dict_type", at(loc, "map"), msgDict)
		}
	} else {
		c.add("missing", at(loc, "map"), msgMissing)
	}
	addExtras(m, loc, c, "by", "map")
	return d
}

// --- dict-field and scalar helpers -------------------------------------------

func buildAttrMap(m *omap.Map, field string, loc []string, c *vocabCollector) *Ordered[*AttrDecl] {
	out := NewOrdered[*AttrDecl]()
	v, has := m.Get(field)
	if !has {
		return out
	}
	mm, ok := v.(*omap.Map)
	if !ok {
		c.add("dict_type", at(loc, field), msgDict)
		return out
	}
	for _, k := range mm.Keys() {
		dv, _ := mm.Get(k)
		if ad := buildAttrDecl(dv, at(loc, field, k), c); ad != nil {
			out.Set(k, ad)
		}
	}
	return out
}

func buildRelationMap(m *omap.Map, field string, loc []string, c *vocabCollector) *Ordered[*RelationDecl] {
	out := NewOrdered[*RelationDecl]()
	v, has := m.Get(field)
	if !has {
		return out
	}
	mm, ok := v.(*omap.Map)
	if !ok {
		c.add("dict_type", at(loc, field), msgDict)
		return out
	}
	for _, k := range mm.Keys() {
		dv, _ := mm.Get(k)
		if rd := buildRelationDecl(dv, at(loc, field, k), c); rd != nil {
			out.Set(k, rd)
		}
	}
	return out
}

// addExtras reports extra="forbid" violations: every input key that is not a
// declared field, in input order, after the declared fields (pydantic-core's
// order).
func addExtras(m *omap.Map, loc []string, c *vocabCollector, known ...string) {
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	for _, k := range m.Keys() {
		if !set[k] {
			c.add("extra_forbidden", at(loc, k), msgExtra)
		}
	}
}

func takeStrOrNil(m *omap.Map, field string, loc []string, c *vocabCollector) *string {
	v, has := m.Get(field)
	if !has || v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		return &s
	}
	c.add("string_type", at(loc, field), msgString)
	return nil
}

func takeBool(m *omap.Map, field string, loc []string, c *vocabCollector) bool {
	v, has := m.Get(field)
	if !has {
		return false
	}
	b, _ := boolLax(v, at(loc, field), c)
	return b
}

func takeLiteral(v any, allowed []string, loc []string, c *vocabCollector) string {
	if s, ok := v.(string); ok {
		for _, a := range allowed {
			if s == a {
				return s
			}
		}
	}
	c.add("literal_error", loc, msgLiteral(allowed))
	return ""
}

func takeStrList(v any, loc []string, c *vocabCollector) []string {
	items, ok := v.([]any)
	if !ok {
		c.add("list_type", loc, msgList)
		return nil
	}
	out := make([]string, 0, len(items))
	valid := true
	for i, it := range items {
		if s, isStr := it.(string); isStr {
			out = append(out, s)
		} else {
			c.add("string_type", at(loc, strconv.Itoa(i)), msgString)
			valid = false
		}
	}
	if !valid {
		return nil
	}
	return out
}

// boolLax is pydantic v2's lax bool: bool; int/float exactly 0 or 1; the
// documented string set, case-insensitive.
func boolLax(v any, loc []string, c *vocabCollector) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case int:
		if x == 0 || x == 1 {
			return x == 1, true
		}
		c.add("bool_parsing", loc, msgBoolParse)
	case int64:
		if x == 0 || x == 1 {
			return x == 1, true
		}
		c.add("bool_parsing", loc, msgBoolParse)
	case float64:
		if x == 0 || x == 1 {
			return x == 1, true
		}
		c.add("bool_parsing", loc, msgBoolParse)
	case string:
		switch strings.ToLower(x) {
		case "1", "true", "t", "yes", "y", "on":
			return true, true
		case "0", "false", "f", "no", "n", "off":
			return false, true
		}
		c.add("bool_parsing", loc, msgBoolParse)
	default:
		c.add("bool_type", loc, msgBoolType)
	}
	return false, false
}
