// Package backfill ports src/khub/core/backfill.py — `khub backfill`, the
// write path for cutover: read first- and last-commit dates from git and write
// created/updated where missing, and with a type filter add that type's missing
// required scaffolding.
//
// Every write is additive and missing-only: a field is written only when
// absent, so an authored value is never overwritten (PRJ-003). A collection
// type is skipped and reported — a shared file's commit dates are not per-row
// dates.
//
// With inferType, a stray whose frontmatter lacks `type` is stamped with the
// one type whose layout holds it, and then backfilled like any other entity.
// Not a port: Python had no such option.
package backfill

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// Sources a change can carry (BackfillChange.source literals).
const (
	SourceGit      = "git log"
	SourceScaffold = "type schema"
	SourceLayout   = "storage layout"
)

// Stray is a stray file --infer-type left alone, and why.
type Stray struct {
	Path   string // workspace-relative, slash-separated
	Reason string
}

// BackfillChange is backfill.BackfillChange: one additive write — which
// entity, which field, the value, and where it came from.
//
// Value is Python's `"" if value is None else str(value)`, so a scaffolded
// placeholder reads as the empty string and a git date as its ISO form.
type BackfillChange struct {
	Type   string
	Slug   string
	Field  string
	Value  string
	Source string
}

// ID is BackfillChange.id.
func (c BackfillChange) ID() string { return c.Type + "/" + c.Slug }

// BackfillReport is backfill.BackfillReport: the changes plus whether git was
// available and whether anything was written. SkippedCollections names the
// collection types date-backfill passed over.
type BackfillReport struct {
	Changes            []BackfillChange
	GitAvailable       bool
	DryRun             bool
	SkippedCollections []string
	// InferType is whether strays were considered at all; Strays the ones
	// still strays after the run, in path order.
	InferType bool
	Strays    []Stray
}

// DatedEntities is BackfillReport.dated_entities: distinct entities that
// received a git-sourced date — the N in the CLI's message.
func (r *BackfillReport) DatedEntities() int { return r.distinct(SourceGit) }

// ScaffoldedEntities is BackfillReport.scaffolded_entities: distinct entities
// that received type scaffolding.
func (r *BackfillReport) ScaffoldedEntities() int { return r.distinct(SourceScaffold) }

// StampedEntities is the number of strays that received an inferred `type`.
func (r *BackfillReport) StampedEntities() int { return r.distinct(SourceLayout) }

func (r *BackfillReport) distinct(source string) int {
	seen := map[index.Node]bool{}
	for _, c := range r.Changes {
		if c.Source == source {
			seen[index.Node{Type: c.Type, Slug: c.Slug}] = true
		}
	}
	return len(seen)
}

// Backfill is backfill.backfill: write missing dates (from git) and, with
// typeName non-nil, that type's missing required scaffolding.
//
// typeName is Python's `type_: str | None` — a nil pointer is the absent
// option, and an explicitly empty string is a type name no schema declares
// (which fails loudly, exactly as in Python). inferType stamps `type` on the
// strays whose layout names one type (see inferTypes).
func Backfill(root string, typeName *string, inferType, dryRun bool) (*BackfillReport, error) {
	if dryRun {
		return backfill(root, typeName, inferType, true)
	}
	return fsio.Locked(root, func() (*BackfillReport, error) { return backfill(root, typeName, inferType, false) })
}
func backfill(root string, typeName *string, inferType, dryRun bool) (*BackfillReport, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	if typeName != nil && !resolved.Types.Has(*typeName) {
		// A misspelled --type must fail loudly, not scaffold nothing and report success.
		return nil, unknownType(root, resolved, *typeName)
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	var stamped map[index.Node]bool
	var left []Stray
	if inferType {
		if stamped, left, err = inferTypes(root, scanned); err != nil {
			return nil, err
		}
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities
	gitOK, err := gitlog.HasGitHistory(root)
	if err != nil {
		return nil, err
	}

	nodes := sortedNodes(idx)
	paths := map[index.Node]string{}
	var requests []gitlog.DateRequest
	if gitOK {
		for _, node := range nodes {
			rtype, _ := resolved.Types.Get(node.Type)
			meta := idx.Meta[node]
			if rtype.Storage.Layout == schema.LayoutCollection || (!absent(meta, "created") && !absent(meta, "updated")) {
				continue
			}
			rel, err := filepath.Rel(root, entity.EntityPath(root, idx, node))
			if err != nil {
				return nil, err
			}
			paths[node] = filepath.ToSlash(rel)
			requests = append(requests, gitlog.DateRequest{Path: paths[node], First: absent(meta, "created"), Last: absent(meta, "updated")})
		}
	}
	dates, err := gitlog.CommitDates(root, requests)
	if err != nil {
		return nil, err
	}

	changes := []BackfillChange{}
	skipped := map[string]bool{}
	for _, node := range nodes {
		rtype, _ := resolved.Types.Get(node.Type)
		if rtype.Storage.Layout == schema.LayoutCollection {
			skipped[node.Type] = true // a file date is not a row date; reported, never silent
			continue
		}
		meta := idx.Meta[node]
		path := entity.EntityPath(root, idx, node)
		scaffold := typeName != nil && *typeName == node.Type
		adds := additions(rtype, meta, scaffold, stamped[node], dates[paths[node]])
		if len(adds.order) == 0 {
			continue
		}
		for _, field := range adds.order {
			a := adds.byField[field]
			changes = append(changes, BackfillChange{
				Type: node.Type, Slug: node.Slug, Field: field,
				Value: changeValue(a.value), Source: a.source,
			})
		}
		if !dryRun {
			if err := apply(root, path, adds); err != nil {
				return nil, err
			}
		}
	}
	return &BackfillReport{
		Changes:            changes,
		GitAvailable:       gitOK,
		DryRun:             dryRun,
		SkippedCollections: sortedKeys(skipped),
		InferType:          inferType,
		Strays:             left,
	}, nil
}

// inferTypes decides, for every stray in the scan, whether its `type` can be
// inferred, and stamps the inferable ones in the scan's frontmatter so they
// are entities for the rest of the run. The file is written later, with the
// dates, by apply.
//
// A stray is inferable when exactly one type's layout holds its file and the
// file's frontmatter has no `type` (or a null one). It is left, and reported,
// when its `type` names something else (a disagreement is not a gap to fill),
// when it has no frontmatter to stamp (adopting a foreign file is a different
// verb), when more than one type claims its path, or when it is a collection
// row. A row without `type` already reads as its collection's type, so only a
// row with another type, or an explicit null one, is a stray at all.
func inferTypes(root string, scanned *index.Index) (map[index.Node]bool, []Stray, error) {
	strays := index.StrayNodes(scanned)
	claims := map[string][]string{} // path -> the types whose scan read it
	paths := map[index.Node]string{}
	for _, node := range scanned.Order {
		rel, err := filepath.Rel(root, entity.EntityPath(root, scanned, node))
		if err != nil {
			return nil, nil, err
		}
		rel = filepath.ToSlash(rel)
		paths[node] = rel
		if rtype, _ := scanned.Resolved.Types.Get(node.Type); rtype.Storage.Layout != schema.LayoutCollection {
			claims[rel] = append(claims[rel], node.Type)
		}
	}
	stamped := map[index.Node]bool{}
	left := []Stray{}
	for _, node := range sortedStrays(strays) {
		rel := paths[node]
		rtype, _ := scanned.Resolved.Types.Get(node.Type)
		meta := scanned.Meta[node]
		if rtype.Storage.Layout == schema.LayoutCollection {
			rel += "#" + node.Slug
		}
		reason, err := strayReason(root, rel, rtype, meta, claims[paths[node]])
		if err != nil {
			return nil, nil, err
		}
		if reason != "" {
			left = append(left, Stray{Path: rel, Reason: reason})
			continue
		}
		meta.Set("type", node.Type)
		stamped[node] = true
	}
	sort.SliceStable(left, func(i, j int) bool { return left[i].Path < left[j].Path })
	return stamped, left, nil
}

// strayReason is why one stray cannot be stamped, or "" when it can.
//
// The decision reads the file at the EDIT altitude, the one apply writes
// from, never the scan's meta: the two disagree on a byte-order mark (the
// scan sees no frontmatter, the edit read strips the mark and finds it), and
// a stamp judged at the scan altitude would overwrite the author's `type`.
func strayReason(root, rel string, rtype *schema.ResolvedType, meta *omap.Map, claimants []string) (string, error) {
	if rtype.Storage.Layout == schema.LayoutCollection {
		// A row with no `type` reads as its collection's, so a row stray has
		// another type or a null one. Date backfill skips collections, and so
		// does the stamp.
		if t, _ := meta.Get("type"); values.Present(t) {
			return typeReason(t, rtype), nil
		}
		return "a collection row; write its type by hand", nil
	}
	if len(claimants) > 1 {
		return "claimed by more than one type (" + strings.Join(claimants, ", ") + ")", nil
	}
	text, err := canon.ReadTextIn(root, filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(text, "\uFEFF") {
		// The scan cannot read past the mark and the writer would drop it.
		return "starts with a byte-order mark; remove it and run again", nil
	}
	edit, _, err := canon.ReadDoc(text, rtype.Storage.Fmt)
	if err != nil {
		// Fenceless, or a fence only the loose scan accepts: no frontmatter
		// to add a key to. Adopting a foreign file is not a stamp.
		return "no frontmatter to stamp", nil
	}
	if t, _ := edit.Get("type"); values.Present(t) {
		return typeReason(t, rtype), nil
	}
	return "", nil // absent, null or ''
}

// typeReason is the reason for a stray whose `type` names something else. A
// non-string value is described, never rendered in another language's repr.
func typeReason(t any, rtype *schema.ResolvedType) string {
	if s, ok := t.(string); ok {
		return fmt.Sprintf("type is %s, the layout holds %s", pyRepr(s), pyRepr(rtype.Name))
	}
	return fmt.Sprintf("type is not a type name, the layout holds %s", pyRepr(rtype.Name))
}

// pyRepr quotes a name the way khub's other messages do: 'x'.
func pyRepr(s string) string { return "'" + s + "'" }

func sortedStrays(set map[index.Node]bool) []index.Node {
	out := make([]index.Node, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	index.SortNodes(out)
	return out
}

// addition is one entry of `_additions`' `field -> (value, source)` map.
type addition struct {
	value  any
	source string
}

// additionSet keeps Python dict insertion order: created, updated, then the
// scaffolding keys still absent.
type additionSet struct {
	order   []string
	byField map[string]addition
}

func (s *additionSet) set(field string, value any, source string) {
	if _, has := s.byField[field]; !has {
		s.order = append(s.order, field)
	}
	s.byField[field] = addition{value: value, source: source}
}

func (s *additionSet) has(field string) bool { _, ok := s.byField[field]; return ok }

// additions is backfill._additions: the absent fields to write for one entity.
// Dates come first from git; scaffolding then covers any required key still
// absent (including created/updated when git could not supply them).
func additions(rtype *schema.ResolvedType, meta *omap.Map, scaffold, stamp bool, dates gitlog.Dates) *additionSet {
	adds := &additionSet{byField: map[string]addition{}}
	if stamp {
		adds.set("type", rtype.Name, SourceLayout)
	}
	if absent(meta, "created") && !dates.First.IsZero() {
		adds.set("created", canon.Date{ISO: dates.First.Format("2006-01-02")}, SourceGit)
	}
	if absent(meta, "updated") && !dates.Last.IsZero() {
		adds.set("updated", canon.Date{ISO: dates.Last.Format("2006-01-02")}, SourceGit)
	}
	if scaffold {
		for _, key := range missingRequired(rtype, meta) {
			if !adds.has(key) {
				// The placeholder is null, not "": null keeps the field missing to
				// `check` (so the gap is still surfaced) AND passes `validate`,
				// whose attribute check short-circuits on a None value. An empty
				// string would satisfy neither.
				adds.set(key, nil, SourceScaffold)
			}
		}
	}
	return adds
}

// missingRequired is backfill._missing_required: the required attributes and
// relations absent on an entity, in declaration order — the same
// required-field knowledge validate/check use.
func missingRequired(rtype *schema.ResolvedType, meta *omap.Map) []string {
	var keys []string
	for _, name := range rtype.Attributes.Keys() {
		attr, _ := rtype.Attributes.Get(name)
		if attr.Required && !present(meta, attr.Name) {
			keys = append(keys, attr.Name)
		}
	}
	for _, predicate := range rtype.Relations.Keys() {
		rel, _ := rtype.Relations.Get(predicate)
		if rel.Required && !present(meta, predicate) {
			keys = append(keys, predicate)
		}
	}
	return keys
}

// apply is backfill._apply: re-serialize the document with the absent fields
// added. Only ever called with fields confirmed absent, so an authored value is
// never overwritten (PRJ-003).
//
// The write goes through canon.SpliceDoc, which appends the absent keys and
// leaves every other byte — a hand-kept `created: … # do not touch` above all —
// alone, matching ruamel's round-trip. canon.ErrNoSplice means the shape is not
// expressible in place; the whole-document emitter takes over and the comments
// go with it (canon/splice.go states the trade).
func apply(root, path string, adds *additionSet) error {
	text, err := canon.ReadTextIn(root, path)
	if err != nil {
		return err
	}
	fmtName := canon.FmtOf(path)
	// EDIT altitude, matching Python's _apply -> _read_doc. Parse (the scan
	// altitude) strips the body's trailing newline and resolves frontmatter
	// under YAML 1.1, so backfilling a date silently rewrote the body.
	meta, body, err := canon.ReadDoc(text, fmtName)
	if err != nil {
		return err
	}
	for _, field := range adds.order {
		// Every addition was judged absent; a value at the edit altitude
		// means the judgment read something else, and the write stops before
		// it overwrites an authored value.
		if v, _ := meta.Get(field); values.Present(v) {
			return errs.New("would_overwrite", fmt.Sprintf("Refusing to overwrite '%s' in %s", field, filepath.ToSlash(relTo(root, path))))
		}
		meta.Set(field, adds.byField[field].value)
	}
	if fmtName == "md" && emptyFrontmatter(text) {
		// Nothing in an empty block to preserve; the splice would keep its
		// blank line above the new key.
		text, err = canon.Render(meta, body, fmtName)
	} else {
		text, err = canon.SpliceDoc(text, meta, body, fmtName, readDocMode(fmtName))
	}
	if errors.Is(err, canon.ErrNoSplice) {
		text, err = canon.Render(meta, body, fmtName)
	}
	if err != nil {
		return err
	}
	return fsio.AtomicWriteIn(root, path, []byte(text))
}

// emptyFrontmatter is whether an md document's frontmatter block holds only
// whitespace.
func emptyFrontmatter(text string) bool {
	yamlText, _, err := canon.SplitFrontmatter(text)
	return err == nil && strings.TrimSpace(yamlText) == ""
}

func relTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

// readDocMode is the resolver canon.ReadDoc read `meta` under, so an untouched
// exotic scalar is not mistaken for a change. read_doc loads every format under
// ruamel's 1.2 — unlike the scan altitude, where md goes through
// python-frontmatter's 1.1 loader.
func readDocMode(string) canon.ResolveMode { return canon.Mode12 }

// changeValue is Python's `"" if value is None else str(value)`: the scaffold
// placeholder reads as the empty string (never "None"), a date as its ISO form.
func changeValue(v any) string {
	if v == nil {
		return ""
	}
	return values.Str(v)
}

// unknownType is LocatedError.unknown_type with the workspace's preset name.
func unknownType(root string, resolved *schema.ResolvedSchema, name string) error {
	prov, err := workspace.Provenance(root)
	if err != nil {
		return err
	}
	preset, _ := prov.Get("preset")
	presetName, _ := preset.(string)
	known := append([]string{}, resolved.Types.Keys()...)
	sort.Strings(known)
	return errs.UnknownType(name, presetName, known)
}

// present is values.present over one key — the shared predicate the write gate
// and the integrity gate use (backfill imported it from integrity in Python).
func present(meta *omap.Map, key string) bool {
	v, _ := meta.Get(key)
	return values.Present(v)
}

// absent is `meta.get(key) is None`, which is NOT `not present(...)`: an empty
// string is absent to `present` but not None, and only None reaches for git.
func absent(meta *omap.Map, key string) bool {
	v, _ := meta.Get(key)
	return v == nil
}

func sortedNodes(idx *index.Index) []index.Node {
	out := make([]index.Node, 0, len(idx.Nodes))
	for n := range idx.Nodes {
		out = append(out, n)
	}
	index.SortNodes(out)
	return out
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
