// Package entity ports core/entity.py — the write verbs and the entity read.
// This file pins the package's public surface so the rest of the port can
// compile against it while the bodies land; every function here is a direct
// analogue of the Python function of the same name.
package entity

import (
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// CreateResult is the outcome of a create: where it landed and whether it is a
// draft. Locator ("path#slug") is set only for a collection row.
type CreateResult struct {
	Type    string
	Slug    string
	Path    string
	Draft   bool
	Locator string // "" when not a collection row
}

// UpdateResult is the outcome of an edit.
type UpdateResult struct {
	Type    string
	Slug    string
	Path    string
	Draft   bool
	Locator string
}

// LinkResult is the outcome of a link/unlink. Changed is false when the edge
// already existed (link) or was already absent (unlink) — the CLI reads it to
// tell a real mutation from a no-op.
type LinkResult struct {
	Type      string
	Slug      string
	Predicate string
	Target    string
	Changed   bool
}

// Inbound is an edge pointing at an entity.
type Inbound struct {
	SourceType string
	SourceSlug string
	Predicate  string
}

// DeleteResult is the outcome of a remove: deleted, or refused with the
// inbound edges listed.
type DeleteResult struct {
	Type    string
	Slug    string
	Removed bool
	Inbound []Inbound
}

// Edge is one relation on an entity, stored (forward) or derived (inverse).
type Edge struct {
	Predicate string
	Target    string
	Derived   bool
}

// EntityView is a read of one entity. For a collection row, Raw is the row's
// stored serialization (never the whole file) and Locator addresses it.
type EntityView struct {
	Type    string
	Slug    string
	Path    string
	Meta    *omap.Map
	Body    string
	Raw     string
	Edges   []Edge // nil unless --edges was requested
	Locator string
}

// CreateOpts carries create's keyword arguments.
type CreateOpts struct {
	Fields      *omap.Map // parsed --<field> values, in CLI input order
	ID          string    // explicit --id, used verbatim
	Draft       bool
	Body        string
	Strict      bool
	UseTemplate bool // false = --no-template
}

// UpdateOpts carries update's keyword arguments. Body is nil to leave the body
// unchanged; a pointer to "" clears it.
type UpdateOpts struct {
	Fields *omap.Map
	Body   *string
	Strict bool
}

// EntityPath is the file backing one entity — for a collection type, the
// shared inventory file.
func EntityPath(root string, rtype *schema.ResolvedType, slug string) string {
	return entityPath(root, rtype, slug)
}

// ResolveID resolves a bare or qualified id against the index, honoring the
// case-folding rule and raising on ambiguity.
func ResolveID(idx *index.Index, id string) (index.Node, error) { return resolveID(idx, id) }

// Slugify mints a slug from free text (Python slugify).
func Slugify(text string) string { return slugify(text) }
