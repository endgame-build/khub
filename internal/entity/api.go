// Package entity ports core/entity.py — the write verbs and the entity read.
// This file pins the package's public surface so the rest of the port can
// compile against it while the bodies land; every function here is a direct
// analogue of the Python function of the same name.
package entity

import (
	"github.com/endgame-build/khub/internal/errs"
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

// UpdateResult is the outcome of an edit. Changed is whether the stored bytes
// moved; an edit that sets what is already there changes nothing.
type UpdateResult struct {
	Type    string
	Slug    string
	Path    string
	Draft   bool
	Locator string
	Changed bool
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
	Predicate       string
	Target          string
	Derived         bool
	ResolvedTargets []string // qualified IDs for HTTP navigation; CLI serialization unchanged
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
	Trust   string // the review tier (trust.Tier), derived on read
}

// CreateOpts carries create's keyword arguments.
type CreateOpts struct {
	Fields      *omap.Map // parsed --<field> values, in CLI input order
	ID          string    // explicit --id, used verbatim
	Draft       bool
	Body        string
	Strict      bool
	UseTemplate bool   // false = --no-template
	Actor       string // --by; "" walks the Actor ladder
	DryRun      bool   // run every check, take no lock, write nothing
}

// UpdateOpts carries update's keyword arguments. Body is nil to leave the body
// unchanged; a pointer to "" clears it.
type UpdateOpts struct {
	Fields *omap.Map
	Body   *string
	Strict bool
	Actor  string // --by; "" walks the Actor ladder
	DryRun bool   // run every check, take no lock, write nothing
}

// EntityPath is the file backing one entity — for a collection type, the
// shared inventory file.
func EntityPath(root string, idx *index.Index, node index.Node) string {
	return entityPath(root, idx, node)
}

// ResolveID resolves a bare or qualified id against the index, honoring the
// case-folding rule and raising on ambiguity.
func ResolveID(idx *index.Index, id string) (index.Node, error) { return resolveID(idx, id) }

// View reads one entity of an index the caller already scanned — the record
// `get` prints, with its stored and derived edges when edges is set. The
// index must have its strays filtered, as every read verb's has, so a derived
// edge never comes from a file that is not an entity.
func View(root string, idx *index.Index, node index.Node, edges bool) (*EntityView, error) {
	view, err := getView(root, idx, node)
	if err != nil {
		return nil, err
	}
	if edges {
		view.Edges = entityEdges(idx, idx.Resolved, node, view.Meta)
	}
	return view, nil
}

// Slugify mints a slug from free text (Python slugify).
func Slugify(text string) string { return slugify(text) }

// WriteSupplied names the base attributes create fills itself — the
// discriminator, the two dates and the OKF `generated` stamp — in the order
// create writes them. An extractor never fills these, so the extraction
// contract (internal/extract) lists them as supplied and leaves them out of
// the fields it asks for, and conform does not count one as a gap. `status`
// is not here: `--draft` writes it, but an explicit `--status` is a value the
// caller states, so an extractor may fill it. The list lives beside create so
// the two cannot drift; TestCreateWritesSuppliedFirst pins it.
func WriteSupplied() []string { return []string{"type", "created", "updated", "generated"} }

// ResolveWriteValue is the write path's one relation-value judgment, for a
// caller that wants the verdict without the write (conform): the value an
// edge would store — an alias rewritten to its owner's slug, qualified when
// the bare slug would be ambiguous — or the refusal create would raise for it
// (referential_integrity, ambiguity_error). predicate names the relation in
// the message.
func ResolveWriteValue(idx *index.Index, rel *schema.ResolvedRelation, predicate, target string) (string, error) {
	value, err := aliasTarget(idx, rel, target)
	if err != nil {
		return "", err
	}
	if _, err := resolveWriteTarget(rel, value, idx, predicate, "relation", nil); err != nil {
		return "", err
	}
	return value, nil
}

// UniqueTargets is the write path's duplicate fold over a relation's values:
// two spellings that resolve to one node keep the first.
func UniqueTargets(idx *index.Index, rel *schema.ResolvedRelation, list []string) []string {
	return uniqueTargets(idx, rel, list)
}

// PlanSlug is the id create would write meta under, or the refusal it would
// raise before writing: minting from name then title in the type's scheme
// (no_slug_source, invalid_slug, id_prefix_undecided), then every gate
// placeNew holds (a taken singleton, slug or alias, a Windows device name, an
// OKF navigation name, an ignored path). It runs placeNew itself against the
// index instead of the exclusive write, so `schema conform` promises only
// what add keeps. meta is the record as create assembles it; today's date
// dates an id_date type, as it does on add.
func PlanSlug(rtype *schema.ResolvedType, meta *omap.Map, idx *index.Index) (string, error) {
	slug := rtype.Name
	if rtype.Storage.Layout != schema.LayoutSingleton {
		var err error
		if slug, err = chooseSlug(rtype, meta, ""); err != nil {
			return "", err
		}
	}
	if _, err := placeNew(idx.Root, idx.Resolved, idx, rtype, slug, meta, errs.Minted); err != nil {
		return "", err
	}
	return slug, nil
}
