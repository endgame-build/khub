package schema

// Ports src/khub/core/model.py — the resolved-schema in-memory model (post
// base-merge, WPK-000-1). The single contract every surface reads: attributes
// (scalars/enums) and relations (typed/union/any edges), plus khub storage
// config (layout/path/format). Populated by Resolve; consumed by
// introspection, validation, and the graph/query layers.
//
// Pointer fields (*string) carry Python's None; nil slices carry an absent
// enum/target list. All *Ordered fields iterate in declaration order.

import (
	"fmt"

	"github.com/endgame-build/khub/internal/omap"
)

// Relation kinds (model.ResolvedRelation.kind literals).
const (
	KindTyped = "typed"
	KindUnion = "union"
	KindAny   = "any"
)

// Layouts (schema_model.TypeDecl.layout / model.StorageConfig.layout literals).
const (
	LayoutFile       = "file"
	LayoutFolder     = "folder"
	LayoutCollection = "collection"
	LayoutSingleton  = "singleton"
)

// StorageConfig is khub-only storage metadata — never enters LinkML validation.
//
// Collection layout: one file holds every entity of the type as a row; Path
// names that file (default "{type}.{fmt}") instead of a directory.
type StorageConfig struct {
	Layout string
	Path   *string // nil = not declared
	Fmt    string  // effective format (post storage-matrix derivation)
}

// ResolvedAttribute is a scalar or enum attribute on a resolved type (base
// merged, overrides applied).
type ResolvedAttribute struct {
	Name               string
	BaseType           string // "text" when undeclared
	Required           bool
	Pattern            *string
	Enum               []string // nil = no enum
	Default            any      // nil = no default (Python None)
	OverriddenFromBase bool
}

// ResolvedRelation is a typed / union / any edge. The predicate is the
// authored field name.
type ResolvedRelation struct {
	Predicate string
	Targets   []string
	Kind      string // KindTyped | KindUnion | KindAny
	Many      bool
	Required  bool
	Inverse   *string
	// Cycle-checked by `check`. A hierarchy predicate (depends_on, supersedes)
	// is acyclic by contract; a plain association (related, affects) is not.
	Acyclic bool
}

// PrefixMember is one (enum value, prefix) pair of a by-value IdPrefix.
type PrefixMember struct {
	Value  string
	Prefix string
}

// IdPrefix is a type's enumerated-id policy: a literal prefix, or one per enum
// member. See TypeDecl.IdPrefix. By/Members are empty for the literal form.
type IdPrefix struct {
	Literal *string
	By      *string
	Members []PrefixMember
}

// Resolve returns the prefix for one entity's attributes; ok is false when its
// `by` value is absent or maps to no member (Python returns None).
func (p *IdPrefix) Resolve(attributes *omap.Map) (string, bool) {
	if p.Literal != nil {
		return *p.Literal, true
	}
	by := ""
	if p.By != nil {
		by = *p.By
	}
	value, _ := attributes.Get(by)
	s, ok := value.(string)
	if !ok {
		return "", false
	}
	for _, m := range p.Members {
		if m.Value == s {
			return m.Prefix, true
		}
	}
	return "", false
}

// All returns every prefix this policy can mint, deduplicated, first
// occurrence first (Python dict.fromkeys order).
func (p *IdPrefix) All() []string {
	if p.Literal != nil {
		return []string{*p.Literal}
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range p.Members {
		if !seen[m.Prefix] {
			seen[m.Prefix] = true
			out = append(out, m.Prefix)
		}
	}
	return out
}

// ResolvedType is one resolved entity type: base merged in, overrides applied,
// predicates resolved.
type ResolvedType struct {
	Name       string
	Storage    StorageConfig
	Attributes *Ordered[*ResolvedAttribute]
	Relations  *Ordered[*ResolvedRelation]
	// Singleton-only (see TypeDecl.Required): a missing required singleton is
	// a `check` finding.
	Required bool
	// See TypeDecl.Orphan: this type's instances are exempt from the orphan sweep.
	Orphan bool
	// See TypeDecl.IdPrefix: `add` mints `<prefix>-NNN-<slug>` when this is set.
	IdPrefix *IdPrefix
	// See TypeDecl.Template / TemplateOff: the declared template stem and the
	// explicit opt-out. Read through TemplateName.
	Template    *string
	TemplateOff bool
	// See TypeDecl.When: the moment to capture this type, in domain language.
	When *string
}

// TemplateName is the template file stem this type reads
// (.khub/templates/<stem>.yaml), "" when the type is explicitly untemplated.
// Undeclared falls back to the type's own name — the standing convention, so
// a schema declaring nothing behaves exactly as before the key existed.
func (t *ResolvedType) TemplateName() string {
	if t.TemplateOff {
		return ""
	}
	if t.Template != nil {
		return *t.Template
	}
	return t.Name
}

// ReadsTemplate reports whether this type consults a body template at all.
// Body templates exist only where a body does: per-item and singleton md.
// The resolver already rejects a DECLARED `template:` on any other type
// (the storage matrix), so this is what constrains the undeclared case —
// without it every type claims its conventional stem, and a template file
// sitting beside a collection type reads as claimed while no verb consults
// it. `add`, `validate` and `check` all gate on this one predicate.
func (t *ResolvedType) ReadsTemplate() bool {
	return t.Storage.Fmt == "md" && t.Storage.Layout != LayoutCollection
}

// CollectionRelpath is the one workspace-relative path of a collection type's
// inventory file. The single source of the default-path rule (path else
// "{name}.{fmt}") — scan, write, integrity, and gitlog all address the file
// through here.
func (t *ResolvedType) CollectionRelpath() string {
	if t.Storage.Path != nil && *t.Storage.Path != "" {
		return *t.Storage.Path
	}
	return fmt.Sprintf("%s.%s", t.Name, t.Storage.Fmt)
}

// ResolvedSchema is all declared types, base merged in — ready for compile.
//
// BaseAttributes/BaseRelations are the resolved base block, retained so the
// compiler can emit them once on an abstract base and emit only each type's
// delta.
type ResolvedSchema struct {
	Types          *Ordered[*ResolvedType]
	BaseAttributes *Ordered[*ResolvedAttribute]
	BaseRelations  *Ordered[*ResolvedRelation]
}
