package schema

// Ports src/khub/core/resolve.py — the schema resolver (WPK-000-1). Resolve
// turns authored khub-vocabulary YAML into a ResolvedSchema: parse via the
// vocab meta-schema (which rejects smuggled raw LinkML), merge the base block
// into every type, apply per-type overrides, treat each relation's field name
// as its predicate, and reject malformed declarations (unknown target, missing
// base) with located errors — before any artifact is written.
//
// Merge semantics (contract): attributes facet-merge (an override inherits
// every facet it does not redeclare); relations whole-replace (a redeclared
// predicate is the type's declaration, not a merge). Both keep the base's
// declaration position on override, exactly like a Python dict assignment.

import (
	"fmt"
	"os"
	"strings"

	"github.com/endgame-build/khub/internal/canon"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// Resolve resolves authored schema files into a ResolvedSchema (WPK-000-1).
// Later files override: the last base block wins whole; entity declarations
// merge by name with last-wins (keeping the first file's declaration position,
// like a Python dict update).
func Resolve(schemaFiles []string) (*ResolvedSchema, error) {
	var baseRaw any
	entitiesRaw := omap.New()
	for _, f := range schemaFiles {
		data, err := LoadYAML(f)
		if err != nil {
			return nil, err
		}
		if v, ok := data.Get("base"); ok && v != nil {
			baseRaw = v
		}
		if v, ok := data.Get("entities"); ok && v != nil && !pyFalsy(v) {
			m, isMap := v.(*omap.Map)
			if !isMap {
				// Python crashes with an AttributeError here; Go surfaces a
				// plain (uncoded) error instead.
				return nil, fmt.Errorf("schema file %s: 'entities' is not a mapping", f)
			}
			for _, name := range m.Keys() {
				dv, _ := m.Get(name)
				entitiesRaw.Set(name, dv)
			}
		}
	}

	raw := omap.New()
	raw.Set("entities", entitiesRaw)
	if baseRaw != nil {
		raw.Set("base", baseRaw)
	}

	schema, verrs := validateSchemaFile(raw)
	if len(verrs) > 0 {
		return nil, smuggledError(verrs)
	}

	if schema.Entities.Len() > 0 && schema.Base == nil {
		return nil, errs.MissingBase()
	}

	declared := map[string]bool{}
	for _, name := range schema.Entities.Keys() {
		declared[name] = true
	}
	base := schema.Base
	if base == nil {
		base = &BaseBlock{Attributes: NewOrdered[*AttrDecl](), Relations: NewOrdered[*RelationDecl]()}
	}

	types := NewOrdered[*ResolvedType]()
	for _, name := range schema.Entities.Keys() {
		decl, _ := schema.Entities.Get(name)
		rt, err := resolveType(name, decl, base, declared)
		if err != nil {
			return nil, err
		}
		types.Set(name, rt)
	}

	baseAttributes := NewOrdered[*ResolvedAttribute]()
	for _, an := range base.Attributes.Keys() {
		ad, _ := base.Attributes.Get(an)
		baseAttributes.Set(an, resolveAttr(an, ad, false))
	}
	baseRelations := NewOrdered[*ResolvedRelation]()
	for _, rn := range base.Relations.Keys() {
		rd, _ := base.Relations.Get(rn)
		rr, err := resolveRelation("(base)", rn, rd, declared)
		if err != nil {
			return nil, err
		}
		baseRelations.Set(rn, rr)
	}
	return &ResolvedSchema{Types: types, BaseAttributes: baseAttributes, BaseRelations: baseRelations}, nil
}

// LoadYAML is resolve.load_yaml: a safe-load of one YAML document into an
// insertion-ordered map; an empty (or falsy-rooted) document is an empty map.
// Duplicate keys are a hard error, matching ruamel's DuplicateKeyError rather
// than last-wins. Loading goes through internal/canon so schema scalars
// resolve by ruamel 1.2 rules (the choke-point rule: only canon may reach for
// a YAML library).
func LoadYAML(path string) (*omap.Map, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return omap.New(), nil
	}
	v, err := canon.LoadDocMode(string(data), canon.Mode12)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	root := fromYAML(v)
	if pyFalsy(root) {
		return omap.New(), nil
	}
	m, ok := root.(*omap.Map)
	if !ok {
		// Python returns the non-mapping root and crashes on the first .get;
		// Go names the problem instead.
		return nil, fmt.Errorf("schema file %s: top level is not a mapping", path)
	}
	return m, nil
}

// fromYAML normalizes canon's value tree for the vocabulary layer: integers to
// int64, everything else passed through. Non-string mapping keys cannot occur
// (canon stringifies them on load, as Python would carry them into pydantic).
func fromYAML(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, it := range x {
			out[i] = fromYAML(it)
		}
		return out
	case *omap.Map:
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			x.Set(k, fromYAML(val))
		}
		return x
	case int:
		return int64(x)
	default:
		return v
	}
}

// pyFalsy mirrors Python truthiness for the `data or {}` / `or {}` guards.
func pyFalsy(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case bool:
		return !x
	case string:
		return x == ""
	case int64:
		return x == 0
	case uint64:
		return x == 0
	case float64:
		return x == 0
	case []any:
		return len(x) == 0
	case *omap.Map:
		return x.Len() == 0
	default:
		return false
	}
}

func resolveType(name string, decl *TypeDecl, base *BaseBlock, declared map[string]bool) (*ResolvedType, error) {
	// Attributes: base first, then the type's delta (an existing key is an
	// override that facet-merges; position stays at the base's slot).
	attributes := NewOrdered[*ResolvedAttribute]()
	for _, an := range base.Attributes.Keys() {
		ad, _ := base.Attributes.Get(an)
		attributes.Set(an, resolveAttr(an, ad, false))
	}
	for _, an := range decl.Attributes.Keys() {
		ad, _ := decl.Attributes.Get(an)
		if existing, ok := attributes.Get(an); ok {
			attributes.Set(an, overrideAttr(an, existing, ad))
		} else {
			attributes.Set(an, resolveAttr(an, ad, false))
		}
	}

	// Relations: universal edges from the base, then the type's own edges
	// (whole replacement — never a facet merge).
	relations := NewOrdered[*ResolvedRelation]()
	for _, rn := range base.Relations.Keys() {
		rd, _ := base.Relations.Get(rn)
		rr, err := resolveRelation(name, rn, rd, declared)
		if err != nil {
			return nil, err
		}
		relations.Set(rn, rr)
	}
	for _, rn := range decl.Relations.Keys() {
		rd, _ := decl.Relations.Get(rn)
		rr, err := resolveRelation(name, rn, rd, declared)
		if err != nil {
			return nil, err
		}
		relations.Set(rn, rr)
	}

	if err := checkIdPrefix(name, decl, attributes); err != nil {
		return nil, err
	}
	storage := StorageConfig{Layout: decl.Layout, Path: decl.Path, Fmt: decl.Format}
	return &ResolvedType{
		Name:       name,
		Storage:    storage,
		Attributes: attributes,
		Relations:  relations,
		Required:   decl.Required,
		Orphan:     decl.Orphan,
		IdPrefix:   idPrefixOf(decl.IdPrefix),
		When:       decl.When,
	}, nil
}

// checkIdPrefix: a by-value prefix must name an enum attribute and cover every
// member. Checked here rather than on TypeDecl because the deciding attribute
// may come from the base block, or be an override that tightens only
// `required` — both invisible until the base has been merged in.
func checkIdPrefix(name string, decl *TypeDecl, attributes *Ordered[*ResolvedAttribute]) error {
	if decl.IdPrefix == nil || decl.IdPrefix.Decl == nil {
		return nil
	}
	spec := decl.IdPrefix.Decl
	attr, ok := attributes.Get(spec.By)
	if !ok || len(attr.Enum) == 0 {
		return errs.New("schema_error", fmt.Sprintf(
			"%s.id_prefix.by '%s' must name an attribute of this type that declares an enum",
			name, spec.By))
	}
	var missing, unknown []string
	for _, member := range attr.Enum {
		if !spec.Map.Has(member) {
			missing = append(missing, member)
		}
	}
	enumSet := map[string]bool{}
	for _, member := range attr.Enum {
		enumSet[member] = true
	}
	for _, k := range spec.Map.Keys() {
		if !enumSet[k] {
			unknown = append(unknown, k)
		}
	}
	if len(missing) > 0 || len(unknown) > 0 {
		return errs.New("schema_error", fmt.Sprintf(
			"%s.id_prefix.map must cover exactly %s's enum; missing %s, unknown %s",
			name, spec.By, pyStrListRepr(missing), pyStrListRepr(unknown)))
	}
	return nil
}

func idPrefixOf(spec *IdPrefixSpec) *IdPrefix {
	if spec == nil {
		return nil
	}
	if spec.Literal != nil {
		return &IdPrefix{Literal: spec.Literal}
	}
	members := make([]PrefixMember, 0, spec.Decl.Map.Len())
	for _, k := range spec.Decl.Map.Keys() {
		prefix, _ := spec.Decl.Map.Get(k)
		members = append(members, PrefixMember{Value: k, Prefix: prefix})
	}
	by := spec.Decl.By
	return &IdPrefix{By: &by, Members: members}
}

func resolveAttr(name string, ad *AttrDecl, overridden bool) *ResolvedAttribute {
	baseType := "text"
	if ad.Type != nil {
		baseType = *ad.Type
	}
	required := false // nil (undeclared) means not required here
	if ad.Required != nil {
		required = *ad.Required
	}
	var enum []string
	if len(ad.Enum) > 0 {
		enum = append([]string(nil), ad.Enum...)
	}
	return &ResolvedAttribute{
		Name:               name,
		BaseType:           baseType,
		Required:           required,
		Pattern:            ad.Pattern,
		Enum:               enum,
		Default:            ad.Default,
		OverriddenFromBase: overridden,
	}
}

// overrideAttr: the type-level declaration wins, but an override that only
// tightens one facet (e.g. `updated: { required: true }`) must not silently
// drop the base's required/pattern/enum/default — inherit each facet the
// override does not redeclare.
func overrideAttr(name string, baseAttr *ResolvedAttribute, ad *AttrDecl) *ResolvedAttribute {
	baseType := baseAttr.BaseType
	if ad.Type != nil {
		baseType = *ad.Type
	}
	required := baseAttr.Required
	if ad.Required != nil {
		required = *ad.Required
	}
	pattern := baseAttr.Pattern
	if ad.Pattern != nil {
		pattern = ad.Pattern
	}
	enum := baseAttr.Enum
	if len(ad.Enum) > 0 {
		enum = append([]string(nil), ad.Enum...)
	}
	def := baseAttr.Default
	if ad.Default != nil {
		def = ad.Default
	}
	return &ResolvedAttribute{
		Name:               name,
		BaseType:           baseType,
		Required:           required,
		Pattern:            pattern,
		Enum:               enum,
		Default:            def,
		OverriddenFromBase: true,
	}
}

func resolveRelation(typeName, predicate string, rd *RelationDecl, declared map[string]bool) (*ResolvedRelation, error) {
	common := func(targets []string, kind string) *ResolvedRelation {
		return &ResolvedRelation{
			Predicate: predicate, Targets: targets, Kind: kind,
			Many: rd.Many, Required: rd.Required, Inverse: rd.Inverse, Acyclic: rd.Acyclic,
		}
	}
	if !rd.To.IsList && rd.To.One == "any" {
		return common([]string{"any"}, KindAny), nil
	}
	if rd.To.IsList {
		for _, target := range rd.To.List {
			if !declared[target] {
				return nil, errs.UnknownTarget(typeName, predicate, target)
			}
		}
		return common(append([]string(nil), rd.To.List...), KindUnion), nil
	}
	// single typed target
	if !declared[rd.To.One] {
		return nil, errs.UnknownTarget(typeName, predicate, rd.To.One)
	}
	return common([]string{rd.To.One}, KindTyped), nil
}

// smuggledError is resolve._smuggled_error: a pydantic ValidationError becomes
// raw_linkml_smuggled when any extra_forbidden is present (the whole list is
// scanned), else invalid_schema locating the first error.
func smuggledError(list []vocabErr) *errs.Located {
	for _, e := range list {
		if e.kind == "extra_forbidden" {
			construct := e.loc[len(e.loc)-1]
			typeName := ""
			if len(e.loc) > 1 && e.loc[0] == "entities" {
				typeName = e.loc[1]
			}
			return errs.RawLinkMLSmuggled(typeName, construct, strings.Join(e.loc, "."))
		}
	}
	first := list[0]
	return errs.New("invalid_schema",
		fmt.Sprintf("Invalid schema at %s: %s", strings.Join(first.loc, "."), first.msg))
}

// pyStrListRepr renders a []string as Python's repr of a list[str] — the shape
// checkIdPrefix's message interpolates (`missing ['constraint'], unknown []`).
func pyStrListRepr(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = pyStrRepr(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// pyStrRepr approximates repr(str) for the enum-member tokens that reach it:
// single-quoted unless the string contains a single quote and no double quote,
// with backslash/quote/control escapes.
func pyStrRepr(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}
