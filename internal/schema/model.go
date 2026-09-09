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
	"github.com/dlclark/regexp2"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

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

// IsOrphan is khub's orphan rule, stated once. A node no edge reaches or leaves
// is an orphan unless its type declares `orphan: true` — edge-less is that
// type's normal condition, and a health count that can never reach zero is not
// a health count.
//
// The receiver must be a type the schema declares — every caller reaches it via
// Types.Get on a node's own type, which the scan produced from that same
// schema, so a nil receiver here means the index and the schema disagree and a
// panic is the correct report.
//
// It takes the two booleans rather than a graph because its callers derive them
// differently and cannot share that part: query and viz read degrees off a
// built graph, while project counts them during the same traversal it uses to
// find broken references, which no Graph can report. What must not diverge is
// the rule — in particular the exemption — so only the rule lives here.
func (t *ResolvedType) IsOrphan(hasOut, hasIn bool) bool {
	return !hasOut && !hasIn && !t.Orphan
}

// ResolvedAttribute is a scalar or enum attribute on a resolved type (base
// merged, overrides applied).
type ResolvedAttribute struct {
	patternOnce        sync.Once
	patternRE          *regexp2.Regexp
	patternErr         error
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

// PrefixMember is one (enum value, prefix) pair of a by-value IDPrefix.
type PrefixMember struct {
	Value  string
	Prefix string
}

// IDPrefix is a type's prefixed-id policy: a literal prefix, or one per enum
// member. See TypeDecl.IDPrefix. By/Members are empty for the literal form.
type IDPrefix struct {
	Literal *string
	By      *string
	Members []PrefixMember
}

// Resolve returns the prefix for one entity's attributes; ok is false when its
// `by` value is absent or maps to no member (Python returns None).
func (p *IDPrefix) Resolve(attributes *omap.Map) (string, bool) {
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
func (p *IDPrefix) All() []string {
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
	// See TypeDecl.IDPrefix: `add` mints `<prefix>-<slug>` when this is set.
	IDPrefix *IDPrefix
	// See TypeDecl.IDDate: the minted id carries its mint date,
	// `<prefix>-<YYYY-MM-DD>-<slug>`.
	IDDate bool
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

// IDShape renders the id pattern this type declares — `fr|cst|br-slug`,
// `ad-YYYY-MM-DD-slug`, `slug` — once, for every message that shows it. The
// `bad_id` finding and `schema show` both name it, and a reader fixing a slug
// against one while reading the other must not be told two things. A
// singleton mints nothing and renders "".
func (t *ResolvedType) IDShape() string {
	if t.Storage.Layout == LayoutSingleton {
		return ""
	}
	var parts []string
	if t.IDPrefix != nil {
		parts = append(parts, strings.Join(t.IDPrefix.All(), "|"))
	}
	if t.IDDate {
		parts = append(parts, "YYYY-MM-DD")
	}
	parts = append(parts, "slug")
	return strings.Join(parts, "-")
}

// FieldNames lists the type's declared field names — attributes, then
// relations, each in declaration order. A template's lens `when` clauses are
// validated against this set (template.LoadTemplate's fields parameter).
func (t *ResolvedType) FieldNames() []string {
	return slices.Concat(t.Attributes.Keys(), t.Relations.Keys())
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

// StorageRelpath is the effective path, shared by scanning and ownership checks.
func (t *ResolvedType) StorageRelpath() string {
	if t.Storage.Path != nil && *t.Storage.Path != "" {
		return *t.Storage.Path
	}
	if t.Storage.Layout == LayoutCollection || t.Storage.Layout == LayoutSingleton {
		return t.Name + "." + t.Storage.Fmt
	}
	return t.Name
}

// AcceptsEntityPath describes exactly the paths ScanType reads, including depth.
func (t *ResolvedType) AcceptsEntityPath(rel string) bool {
	rel = filepath.ToSlash(filepath.Clean(rel))
	base := t.StorageRelpath()
	if t.Storage.Layout == LayoutCollection || t.Storage.Layout == LayoutSingleton {
		return rel == base
	}
	sub, ok := strings.CutPrefix(rel, base+"/")
	if !ok {
		return false
	}
	parts := strings.Split(sub, "/")
	suffix := "." + t.Storage.Fmt
	if t.Storage.Layout == LayoutFolder {
		return len(parts) == 2 && parts[1] == "_index"+suffix
	}
	return len(parts) == 1 && sub != "_index"+suffix && strings.HasSuffix(sub, suffix)
}

// MatchPattern is the shared write/integrity full-match gate. Cache belongs to
// the resolved schema, so serving edited schemas never retains stale patterns.
func (a *ResolvedAttribute) MatchPattern(value string) (bool, error) {
	if a.Pattern == nil {
		return true, nil
	}
	if err := a.CompilePattern(); err != nil {
		return false, err
	}
	return a.patternRE.MatchString(value)
}

// CompilePattern compiles the attribute's pattern once, with the shared match
// timeout; MatchPattern calls it, callers rarely need to.
func (a *ResolvedAttribute) CompilePattern() error {
	a.patternOnce.Do(func() {
		if a.Pattern == nil {
			return
		}
		a.patternRE, a.patternErr = regexp2.Compile(`\A(?:`+*a.Pattern+`)\z`, regexp2.None)
		if a.patternErr == nil {
			a.patternRE.MatchTimeout = time.Second
		}
	})
	return a.patternErr
}
