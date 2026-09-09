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
	"github.com/endgame-build/khub/internal/fsio"
	"os"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/canon"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// schemaDoc is one authored document plus the file it came from. The name
// travels with the mapping because every shape error names its file.
type schemaDoc struct {
	name string
	m    *omap.Map
}

// authoredLayers is the three layer mappings, merged across every document and
// each keeping its first declaration position.
type authoredLayers struct {
	entities *omap.Map
	policy   *omap.Map
	storage  *omap.Map
}

// mergeLayers folds every authored document into the three layer accumulators.
// The merge dispatches on TOP-LEVEL KEY, never on filename, so the same three
// layers resolve identically whether they arrive as three files or as three
// blocks of one document.
//
// This is also the one altitude that still sees the authored shape, so it is
// where the vocabulary's extra="forbid" is restored: the re-nest downstream
// rebuilds the input from the three layer keys alone, and an unknown authored
// key would otherwise vanish silently — a typo'd `entitles:` resolving to a
// zero-type schema with no complaint. `version` is the one non-layer key an
// authored document may carry (presets stamp it; workspace copies carry it in
// the provenance comment instead).
func mergeLayers(docs []schemaDoc) (*authoredLayers, error) {
	out := &authoredLayers{entities: omap.New(), policy: omap.New(), storage: omap.New()}

	// mergeInto folds one layer mapping into its accumulator, last-wins by name
	// and keeping the first document's declaration position.
	mergeInto := func(dst *omap.Map, v any, file, label string) error {
		m, isMap := v.(*omap.Map)
		if !isMap {
			// Python crashes with an AttributeError here; Go surfaces a
			// plain (uncoded) error instead.
			return fmt.Errorf("schema file %s: '%s' is not a mapping", file, label)
		}
		for _, name := range m.Keys() {
			dv, _ := m.Get(name)
			dst.Set(name, dv)
		}
		return nil
	}

	pre := &vocabCollector{}
	for _, d := range docs {
		for _, k := range d.m.Keys() {
			switch k {
			case "ontology", "policy", "storage", "version":
			default:
				pre.add("extra_forbidden", []string{k}, msgExtra)
			}
		}
		// ontology (entities) / policy / storage — the one authored shape.
		if v, ok := d.m.Get("ontology"); ok && v != nil && !pyFalsy(v) {
			om, isMap := v.(*omap.Map)
			if !isMap {
				return nil, fmt.Errorf("schema file %s: 'ontology' is not a mapping", d.name)
			}
			// The base block is khub-owned: it arrives as baseDoc (the embedded
			// core document), never from an authored file — redefining it whole
			// would silently change what every gate reads.
			if _, has := om.Get("base"); has {
				return nil, errs.New("invalid_schema",
					"Invalid schema at ontology.base: the base block is khub-owned; "+
						"override a base attribute by redeclaring it on the type "+
						"(see `khub schema base`)")
			}
			for _, k := range om.Keys() {
				if k != "entities" {
					pre.add("extra_forbidden", []string{"ontology", k}, msgExtra)
				}
			}
			if ev, has := om.Get("entities"); has && ev != nil && !pyFalsy(ev) {
				if err := mergeInto(out.entities, ev, d.name, "ontology.entities"); err != nil {
					return nil, err
				}
			}
		}
		for _, layer := range []struct {
			key string
			dst *omap.Map
		}{{"policy", out.policy}, {"storage", out.storage}} {
			if v, ok := d.m.Get(layer.key); ok && v != nil && !pyFalsy(v) {
				if err := mergeInto(layer.dst, v, d.name, layer.key); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(pre.list) > 0 {
		return nil, smuggledError(pre.list)
	}
	return out, nil
}

// ResolveWith is Resolve with a pre-read base document — the embedded base
// block, which khub owns and never copies into a workspace.
//
// baseDoc is the ONLY legal source of the base: an authored `ontology.base` is
// rejected with a located error, because the base is khub's own plumbing (the
// discriminator, the draft flag, the universal edges) and redefining it whole
// would silently change what every gate reads. The override mechanism is
// per-type: redeclare the attribute on the type (see `khub schema base`).
//
// It takes a document rather than a path so internal/schema never imports
// internal/presets — the resolver stays pure, and who supplies the documents is
// the caller's business (introspect.LoadSchema, which knows both the workspace
// and the embedded tree, is the one that joins them).
func ResolveWith(baseDoc *omap.Map, schemaFiles []string) (*ResolvedSchema, error) {
	return resolveIn("", baseDoc, schemaFiles)
}

// ResolveIn reads workspace layers through root-confined I/O.
func ResolveIn(root string, baseDoc *omap.Map, schemaFiles []string) (*ResolvedSchema, error) {
	return resolveIn(root, baseDoc, schemaFiles)
}

func resolveIn(root string, baseDoc *omap.Map, schemaFiles []string) (*ResolvedSchema, error) {
	docs := make([]schemaDoc, 0, len(schemaFiles))
	for _, f := range schemaFiles {
		var data *omap.Map
		var err error
		if root == "" {
			data, err = LoadYAML(f)
		} else {
			var raw []byte
			raw, err = fsio.ReadFile(root, f)
			if err == nil {
				data, err = ParseDoc(f, string(raw))
			}
		}
		if err != nil {
			return nil, err
		}
		docs = append(docs, schemaDoc{name: f, m: data})
	}

	layers, err := mergeLayers(docs)
	if err != nil {
		return nil, err
	}

	var baseRaw any

	// The base, from the one legal source. A supplied document that yields no
	// base is a loud error, not an empty base: the caller passing one is
	// promising khub's plumbing (the discriminator, the draft flag, the
	// universal edges), and a mis-nested embedded document must fail here
	// rather than let every gate silently change meaning.
	if baseDoc != nil {
		if ov, ok := baseDoc.Get("ontology"); ok && ov != nil {
			if om, isMap := ov.(*omap.Map); isMap {
				if bv, has := om.Get("base"); has && bv != nil {
					baseRaw = bv
				}
			}
		}
		if baseRaw == nil {
			return nil, errs.New("schema_error", "Base document carries no ontology.base block")
		}
	}

	// Re-nest for the vocabulary walk. The three layers are already merged
	// across every document, so the walk sees one block per layer regardless of
	// how many files supplied them.
	raw := omap.New()
	ontologyRaw := omap.New()
	if baseRaw != nil {
		ontologyRaw.Set("base", baseRaw)
	}
	ontologyRaw.Set("entities", layers.entities)
	raw.Set("ontology", ontologyRaw)
	raw.Set("policy", layers.policy)
	raw.Set("storage", layers.storage)

	schema, verrs := validateSchemaFile(raw)
	if len(verrs) > 0 {
		return nil, smuggledError(verrs)
	}
	if err := finishLayered(schema); err != nil {
		return nil, err
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
	resolved := &ResolvedSchema{Types: types, BaseAttributes: baseAttributes, BaseRelations: baseRelations}
	if err := validateResolved(resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

// finishLayered completes the layered merge: the cross-layer checks that can
// only run once all three halves are in hand.
//
// The pre-split walk ran storageMatrix inline, because one subtree carried
// every field it cross-checks. Layered, `required` arrives from policy,
// `layout`/`path`/`format` from storage, and `attributes`/`relations` from
// ontology — so the check has to wait for the merge. Storage defaults land
// first, so a type that no storage layer named is still a complete declaration
// by the time the matrix sees it.
func finishLayered(sf *SchemaFile) error {
	// Ontology declares which types exist; policy and storage only annotate.
	// A name they carry that ontology never declared is a typo or a stale
	// entry, and silently ignoring it would mean a gate or a path quietly not
	// applying.
	for _, layer := range []struct {
		name  string
		names []string
	}{{"policy", sf.UnknownPolicy}, {"storage", sf.UnknownStorage}} {
		if len(layer.names) > 0 {
			return errs.New("invalid_schema", fmt.Sprintf(
				"Invalid schema at %s.%s: no type '%s' is declared in ontology",
				layer.name, layer.names[0], layer.names[0]))
		}
	}
	for _, name := range sf.Entities.Keys() {
		td, _ := sf.Entities.Get(name)
		td.storageDefaults(name)
		// `required` is the one cross-layer fact whose author sits in POLICY,
		// so its violation is located there — a storage.<type> loc would send
		// the user to a layer file that may not even mention the type.
		if td.Required && td.Layout != LayoutSingleton {
			return errs.New("invalid_schema", fmt.Sprintf(
				"Invalid schema at policy.%s.required: Value error, "+
					"'required' is singleton-only (a required file/folder/collection "+
					"type has no single artifact to require)", name))
		}
		if err := td.storageMatrix(); err != nil {
			return errs.New("invalid_schema", fmt.Sprintf(
				"Invalid schema at storage.%s: Value error, %s", name, err.Error()))
		}
	}
	return nil
}

// validateResolved checks compiled patterns and normalized storage ownership
// before any caller can scan or write through the resolved schema.
func validateResolved(resolved *ResolvedSchema) error {
	types := []*ResolvedType{}
	for _, name := range resolved.Types.Keys() {
		t, _ := resolved.Types.Get(name)
		for _, attrName := range t.Attributes.Keys() {
			attr, _ := t.Attributes.Get(attrName)
			if err := attr.CompilePattern(); err != nil {
				return errs.New("invalid_schema", fmt.Sprintf("Invalid pattern at ontology.entities.%s.attributes.%s: %s", name, attrName, err))
			}
		}
		rel := filepath.Clean(t.StorageRelpath())
		first := strings.Split(filepath.ToSlash(rel), "/")[0]
		if !filepath.IsLocal(rel) || rel == "." || first == ".khub" || first == ".git" || first == ".claude" || first == ".agents" || first == ".opencode" || rel == "index.md" || rel == "AGENTS.md" || rel == "CLAUDE.md" {
			return errs.New("invalid_schema", fmt.Sprintf("Invalid schema at storage.%s.path: path must stay in workspace storage and cannot own khub control files (%s)", name, t.StorageRelpath()))
		}
		normalized := filepath.ToSlash(rel)
		if t.Storage.Path != nil {
			t.Storage.Path = &normalized
		}
		for _, other := range types {
			if storageOverlap(t, other) {
				if t.Storage.Layout == LayoutCollection && other.Storage.Layout == LayoutCollection {
					return errs.CollectionPathCollision(normalized, []string{other.Name, t.Name})
				}
				return errs.New("invalid_schema", fmt.Sprintf("Storage paths for '%s' and '%s' overlap: %s, %s", other.Name, t.Name, other.StorageRelpath(), normalized))
			}
		}
		types = append(types, t)
	}
	return nil
}

func storageOverlap(a, b *ResolvedType) bool {
	ap, bp := a.StorageRelpath(), b.StorageRelpath()
	if ap == bp {
		return true
	}
	if a.Storage.Layout == LayoutFolder && strings.HasPrefix(bp, ap+"/") {
		return true
	}
	if b.Storage.Layout == LayoutFolder && strings.HasPrefix(ap, bp+"/") {
		return true
	}
	aFile := a.Storage.Layout == LayoutSingleton || a.Storage.Layout == LayoutCollection
	bFile := b.Storage.Layout == LayoutSingleton || b.Storage.Layout == LayoutCollection
	if aFile && (b.AcceptsEntityPath(ap) || strings.HasPrefix(bp, ap+"/")) {
		return true
	}
	if bFile && (a.AcceptsEntityPath(bp) || strings.HasPrefix(ap, bp+"/")) {
		return true
	}
	return false
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
	return ParseDoc(path, string(data))
}

// ParseDoc is LoadYAML over in-memory bytes — the one schema-document parse
// path, so the embedded base inherits the same 1.2 resolution, falsy
// collapse, duplicate-key rejection, and scalar normalization as every
// authored layer file. name labels errors.
func ParseDoc(name, text string) (*omap.Map, error) {
	if len(strings.TrimSpace(text)) == 0 {
		return omap.New(), nil
	}
	v, err := canon.LoadDocMode(text, canon.Mode12)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	root := fromYAML(v)
	if pyFalsy(root) {
		return omap.New(), nil
	}
	m, ok := root.(*omap.Map)
	if !ok {
		// Python returns the non-mapping root and crashes on the first .get;
		// Go names the problem instead.
		return nil, fmt.Errorf("schema file %s: top level is not a mapping", name)
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

	if err := checkIDPrefix(name, decl, attributes); err != nil {
		return nil, err
	}
	storage := StorageConfig{Layout: decl.Layout, Path: decl.Path, Fmt: decl.Format}
	return &ResolvedType{
		Name:        name,
		Storage:     storage,
		Attributes:  attributes,
		Relations:   relations,
		Required:    decl.Required,
		Orphan:      decl.Orphan,
		IDPrefix:    idPrefixOf(decl.IDPrefix),
		IDDate:      decl.IDDate,
		Template:    decl.Template,
		TemplateOff: decl.TemplateOff,
		When:        decl.When,
	}, nil
}

// checkIDPrefix: a by-value prefix must name an enum attribute and cover every
// member. Checked here rather than on TypeDecl because the deciding attribute
// may come from the base block, or be an override that tightens only
// `required` — both invisible until the base has been merged in.
func checkIDPrefix(name string, decl *TypeDecl, attributes *Ordered[*ResolvedAttribute]) error {
	if decl.IDPrefix == nil || decl.IDPrefix.Decl == nil {
		return nil
	}
	spec := decl.IDPrefix.Decl
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

func idPrefixOf(spec *IDPrefixSpec) *IDPrefix {
	if spec == nil {
		return nil
	}
	if spec.Literal != nil {
		return &IDPrefix{Literal: spec.Literal}
	}
	members := make([]PrefixMember, 0, spec.Decl.Map.Len())
	for _, k := range spec.Decl.Map.Keys() {
		prefix, _ := spec.Decl.Map.Get(k)
		members = append(members, PrefixMember{Value: k, Prefix: prefix})
	}
	by := spec.Decl.By
	return &IDPrefix{By: &by, Members: members}
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
			// The offending type's name: ontology.entities.<type>.… for the
			// declaring layer, <layer>.<type>.… for the annotating ones.
			switch {
			case len(e.loc) > 2 && e.loc[0] == "ontology" && e.loc[1] == "entities":
				typeName = e.loc[2]
			case len(e.loc) > 1 && (e.loc[0] == "policy" || e.loc[0] == "storage"):
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
// checkIDPrefix's message interpolates (`missing ['constraint'], unknown []`).
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
