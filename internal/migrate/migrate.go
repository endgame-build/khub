// Package migrate rewrites legacy entity shapes into the current base
// vocabulary. `khub upgrade` runs it over the candidate copy of the workspace,
// so every rewrite rides the upgrade's journal, rollback and --dry-run.
//
// Each op detects its own shape, so a second run finds nothing to do and a
// crashed upgrade re-runs cleanly; no ledger records what already ran. The ops
// read the candidate's resolved schema: under --no-schema the old layer files
// still declare a type-level status, and the status ops find nothing to move.
// LegacyStatus and LegacySources are the detection validate shares, so the
// gate names `khub upgrade` only for what upgrade migrates.
//
// The package reads through index and writes through canon. It never imports
// entity, which imports workspace, and it takes no lock: the caller owns the
// tree it hands over.
package migrate

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// Op names one kind of rewrite.
type Op string

const (
	OpRenameKey   Op = "rename_key"   // From and To are key names
	OpMapValue    Op = "map_value"    // From and To are values
	OpDeleteKey   Op = "delete_key"   // From is the dropped value
	OpMoveEntries Op = "move_entries" // From lists the moved entries, To names their field
)

// Change is one rewrite of one field.
type Change struct {
	Op       Op
	Field    string
	From, To any
}

// Entity is one migrated entity. Reemitted reports that its md or yaml file
// could not be spliced in place and was written whole, which drops the file's
// comments.
type Entity struct {
	ID, Path  string
	Changes   []Change
	Reemitted bool
}

// Conflict is an entity left unchanged because its fields disagree and no op
// may guess which one holds, or a file migrate could not read. ID is empty for
// a file whose entities could not be told apart.
type Conflict struct {
	ID, Path, Field, Reason string
}

// Result lists what a run changed and what it refused, in scan order. Both
// slices are empty, never nil.
type Result struct {
	Migrated  []Entity
	Conflicts []Conflict
}

// legacyStatus maps the values the retired type-level statuses used onto the
// base enum. It is keyed by value, not by type, so it holds for any preset:
// a value moves only when the type's status enum lacks it and holds its
// target. `deprecated` was already a base value and stays.
var legacyStatus = map[string]string{
	"proposed": values.StatusDraft,
	"active":   values.StatusStable,
	"archived": values.StatusDeprecated,
}

// LegacyStatus reports how upgrade migrates a `status` value the type's base
// enum lacks. homes are the type's other fields whose enum holds the value
// (one home: the value moves there; several: a conflict); to is the base value
// it maps onto when no field holds it. Both are empty for a value upgrade
// cannot migrate, a type that redeclares `status`, or a value already valid.
func LegacyStatus(rtype *schema.ResolvedType, value any) (homes []string, to string) {
	attr, ok := rtype.Attributes.Get("status")
	s, isStr := value.(string)
	if !ok || attr.OverriddenFromBase || attr.Enum == nil || !isStr || slices.Contains(attr.Enum, s) {
		return nil, ""
	}
	for _, name := range rtype.Attributes.Keys() {
		if a, _ := rtype.Attributes.Get(name); name != "status" && slices.Contains(a.Enum, s) {
			homes = append(homes, name)
		}
	}
	if len(homes) == 0 && slices.Contains(attr.Enum, legacyStatus[s]) {
		to = legacyStatus[s]
	}
	return homes, to
}

// LegacySources reports whether the type keeps the base `sources` object list
// beside a `references` relation, so upgrade can move bare entries across.
func LegacySources(rtype *schema.ResolvedType) bool {
	attr, ok := rtype.Attributes.Get("sources")
	return ok && !attr.OverriddenFromBase && attr.HoldsObjects() && rtype.Relations.Has("references")
}

// SplitSources splits a `sources` value into its bare string entries, which
// upgrade moves to `references`, and the indices of every other entry, which
// stay. A scalar is a one-entry list.
func SplitSources(value any) (bare []any, kept []int) {
	for i, e := range values.Items(value) {
		if _, isStr := e.(string); isStr {
			bare = append(bare, e)
		} else {
			kept = append(kept, i)
		}
	}
	return bare, kept
}

// dateOnly is the OKF 0.1 `stale_after` form: a calendar date, no time.
var dateOnly = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// LegacyStaleAfter reports the datetime upgrade writes for a date-only base
// `stale_after`, unquoted (a date) or quoted (a string): midnight UTC that day.
func LegacyStaleAfter(rtype *schema.ResolvedType, value any) (canon.DateTime, bool) {
	attr, ok := rtype.Attributes.Get("stale_after")
	if !ok || attr.OverriddenFromBase {
		return canon.DateTime{}, false
	}
	var day string
	switch x := value.(type) {
	case canon.Date:
		day = x.ISO
	case string:
		day = x
	}
	if !dateOnly.MatchString(day) {
		return canon.DateTime{}, false
	}
	return canon.DateTime{ISO: day + "T00:00:00Z"}, true
}

// Writer writes one migrated file under root. fsio.AtomicWriteIn is the
// durable choice; upgrade, which migrates a private temporary candidate,
// passes a plain write, since syncing thousands of throwaway files cost it
// tens of seconds and bought nothing.
type Writer func(root, path string, data []byte) error

// Run migrates every entity the index of root sees and writes each changed
// file in place through write. A file it cannot read is a conflict, not a
// failure.
func Run(root string, write Writer) (*Result, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	res := &Result{Migrated: []Entity{}, Conflicts: []Conflict{}}
	done := map[string]bool{}
	for _, node := range idx.Order {
		rtype, _ := resolved.Types.Get(node.Type)
		if rtype.Storage.Layout == schema.LayoutCollection {
			if rel := rtype.CollectionRelpath(); !done[rel] {
				done[rel] = true
				if err := migrateCollection(root, rel, rtype, idx, res, write); err != nil {
					return nil, err
				}
			}
			continue
		}
		if rel, ok := idx.Paths[node]; ok {
			if err := migrateDoc(root, rel, rtype, node, idx.Meta[node], res, write); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

func unreadable(id, rel string, err error) Conflict {
	return Conflict{ID: id, Path: rel, Reason: "unreadable: " + err.Error()}
}

func migrateDoc(root, rel string, rtype *schema.ResolvedType, node index.Node, scan *omap.Map, res *Result, write Writer) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	text, err := canon.ReadTextIn(root, path)
	if err != nil {
		res.Conflicts = append(res.Conflicts, unreadable(node.ID(), rel, err))
		return nil
	}
	format := canon.FmtOf(path)
	meta, body, err := canon.ReadDoc(text, format)
	if err != nil {
		// The scan's lenient read took it; the edit-altitude read refuses it.
		res.Conflicts = append(res.Conflicts, unreadable(node.ID(), rel, err))
		return nil
	}
	p := planEntity(rtype, meta, scan, nil)
	if p.conflict != nil {
		res.Conflicts = append(res.Conflicts, Conflict{node.ID(), rel, p.conflict.Field, p.conflict.Reason})
		return nil
	}
	if len(p.changes) == 0 {
		return nil
	}
	out, err := canon.SpliceDoc(text, p.meta, body, format, canon.Mode12, p.renames...)
	reemitted := errors.Is(err, canon.ErrNoSplice)
	if reemitted {
		out, err = canon.Render(p.meta, body, format)
	}
	if err != nil {
		return err
	}
	if err := write(root, path, []byte(out)); err != nil {
		return err
	}
	// json carries no comments, so its re-emit loses nothing.
	res.Migrated = append(res.Migrated, Entity{node.ID(), rel, p.changes, reemitted && format != "json"})
	return nil
}

func migrateCollection(root, rel string, rtype *schema.ResolvedType, idx *index.Index, res *Result, write Writer) error {
	path := filepath.Join(root, filepath.FromSlash(rel))
	text, err := canon.ReadTextIn(root, path)
	if err == nil {
		var rows *omap.Map
		if rows, err = canon.LoadCollection(text, rtype.Storage.Fmt); err == nil {
			return migrateRows(root, rel, path, text, rows, rtype, idx, res, write)
		}
	}
	res.Conflicts = append(res.Conflicts, unreadable("", rel, err))
	return nil
}

func migrateRows(root, rel, path, text string, rows *omap.Map, rtype *schema.ResolvedType, idx *index.Index, res *Result, write Writer) error {
	var migrated []Entity
	var renames []canon.Rename
	for _, slug := range rows.Keys() {
		node := index.Node{Type: rtype.Name, Slug: slug}
		rowAny, _ := rows.Get(slug)
		row, ok := rowAny.(*omap.Map)
		if !ok || idx.Meta[node] == nil {
			continue
		}
		p := planEntity(rtype, row, idx.Meta[node], []string{slug})
		if p.conflict != nil {
			res.Conflicts = append(res.Conflicts, Conflict{node.ID(), rel, p.conflict.Field, p.conflict.Reason})
			continue
		}
		if len(p.changes) == 0 {
			continue
		}
		rows.Set(slug, p.meta)
		renames = append(renames, p.renames...)
		migrated = append(migrated, Entity{node.ID(), rel, p.changes, false})
	}
	if len(migrated) == 0 {
		return nil
	}
	out, err := canon.SpliceCollection(text, rows, rtype.Storage.Fmt, renames...)
	reemitted := errors.Is(err, canon.ErrNoSplice)
	if reemitted {
		out, err = canon.DumpCollection(rows, rtype.Storage.Fmt)
	}
	if err != nil {
		return err
	}
	if err := write(root, path, []byte(out)); err != nil {
		return err
	}
	for i := range migrated {
		// json and jsonl carry no comments, so their re-emit loses nothing.
		migrated[i].Reemitted = reemitted && rtype.Storage.Fmt == "yaml"
	}
	res.Migrated = append(res.Migrated, migrated...)
	return nil
}

// plan is one entity's rewrite: the wanted mapping, the key renames that
// produce it in place, and the changes to report. A conflict voids the rest.
type plan struct {
	meta     *omap.Map
	renames  []canon.Rename
	changes  []Change
	conflict *Conflict
}

func (p *plan) change(op Op, field string, from, to any) {
	p.changes = append(p.changes, Change{op, field, from, to})
}

func (p *plan) rename(prefix []string, from, to string) {
	p.meta = renamed(p.meta, from, to)
	p.renames = append(p.renames, canon.Rename{Path: append(slices.Clone(prefix), from), To: to})
	p.change(OpRenameKey, from, from, to)
}

func (p *plan) refuse(field, reason string) {
	p.conflict = &Conflict{Field: field, Reason: reason}
}

// planEntity runs the ops in order over a copy of meta: status first, so a
// status that moves to another field frees `status` for the draft flag. scan
// is the entity as the index read it, at the altitude the retired `draft`
// flag was read at. prefix addresses meta inside its file (a row's slug).
func planEntity(rtype *schema.ResolvedType, meta, scan *omap.Map, prefix []string) *plan {
	p := &plan{meta: renamed(meta, "", "")}
	draftRaw, _ := scan.Get("draft")
	draft := draftApplies(rtype, p.meta) && values.AsBool(draftRaw)
	migrateStatus(p, rtype, draft, prefix)
	if p.conflict == nil {
		migrateDraft(p, rtype, draft, prefix)
	}
	if p.conflict == nil {
		migrateSources(p, rtype, prefix)
	}
	if p.conflict == nil {
		migrateStaleAfter(p, rtype)
	}
	return p
}

// migrateStaleAfter widens a date-only `stale_after` to the OKF 0.2 datetime.
func migrateStaleAfter(p *plan, rtype *schema.ResolvedType) {
	v, _ := p.meta.Get("stale_after")
	if to, ok := LegacyStaleAfter(rtype, v); ok {
		p.meta.Set("stale_after", to)
		p.change(OpMapValue, "stale_after", v, to)
	}
}

// draftApplies reports whether the entity carries the retired publish flag: a
// `draft` key on a type that declares no `draft` of its own.
func draftApplies(rtype *schema.ResolvedType, meta *omap.Map) bool {
	_, has := meta.Get("draft")
	return has && !rtype.OwnsDraft()
}

// acceptsDraft reports whether the type's status can hold `draft`. A type
// that still redeclares a retired status (old layer files kept by
// --no-schema) cannot.
func acceptsDraft(rtype *schema.ResolvedType) bool {
	attr, ok := rtype.Attributes.Get("status")
	return ok && (attr.Enum == nil || slices.Contains(attr.Enum, values.StatusDraft))
}

// migrateStatus moves a `status` value the base enum lacks, as LegacyStatus
// reads it. A retired value that maps onto the lifecycle gives way to a true
// draft flag: the entity was unpublished, so it becomes `status: draft`.
func migrateStatus(p *plan, rtype *schema.ResolvedType, draft bool, prefix []string) {
	v, _ := p.meta.Get("status")
	homes, to := LegacyStatus(rtype, v)
	switch {
	case len(homes) > 1:
		p.refuse("status", fmt.Sprintf("status '%s' is a value of %s; move it by hand", v, strings.Join(homes, " and ")))
	case len(homes) == 1:
		if _, taken := p.meta.Get(homes[0]); taken {
			p.refuse("status", fmt.Sprintf("status '%s' belongs in %s, which is already set; settle it by hand", v, homes[0]))
			return
		}
		p.rename(prefix, "status", homes[0])
	case to != "":
		if draft && acceptsDraft(rtype) {
			to = values.StatusDraft
		}
		p.meta.Set("status", to)
		p.change(OpMapValue, "status", v, to)
	}
}

// migrateDraft folds the retired `draft` flag into `status`.
func migrateDraft(p *plan, rtype *schema.ResolvedType, draft bool, prefix []string) {
	if !draftApplies(rtype, p.meta) {
		return
	}
	from, _ := p.meta.Get("draft")
	status, hasStatus := p.meta.Get("status")
	switch {
	case !draft || status == values.StatusDraft:
		p.meta.Delete("draft")
		p.change(OpDeleteKey, "draft", from, nil)
	case !acceptsDraft(rtype):
		p.refuse("draft", "draft is true but this schema's status cannot hold draft; "+
			"run `khub upgrade` without --no-schema")
	case !hasStatus:
		p.rename(prefix, "draft", "status")
		p.meta.Set("status", values.StatusDraft)
		p.change(OpMapValue, "status", from, values.StatusDraft)
	default:
		p.refuse("draft", fmt.Sprintf("draft is true but status is '%s'; settle it by hand", values.Str(status)))
	}
}

// migrateSources moves the bare entries of `sources`, once a relation and now
// a list of OKF source objects, to the `references` relation. Object entries
// are sources already and stay.
func migrateSources(p *plan, rtype *schema.ResolvedType, prefix []string) {
	v, has := p.meta.Get("sources")
	if !has || !LegacySources(rtype) {
		return
	}
	bare, kept := SplitSources(v)
	if len(bare) == 0 {
		return
	}
	refs, hasRefs := p.meta.Get("references")
	merged := slices.Clone(values.Items(refs))
	for _, e := range bare {
		if !slices.Contains(merged, e) {
			merged = append(merged, e)
		}
	}
	switch {
	case len(kept) == 0 && !hasRefs:
		// The whole field moves, so its key is renamed and keeps its comments.
		p.meta = renamed(p.meta, "sources", "references")
		p.renames = append(p.renames, canon.Rename{Path: append(slices.Clone(prefix), "sources"), To: "references"})
	case len(kept) == 0:
		p.meta.Delete("sources")
	default:
		all := values.Items(v)
		keep := make([]any, len(kept))
		for i, k := range kept {
			keep[i] = all[k]
		}
		p.meta.Set("sources", keep)
	}
	p.meta.Set("references", merged)
	p.change(OpMoveEntries, "sources", bare, "references")
}

// renamed copies m with key from renamed to, in place; an empty from copies.
func renamed(m *omap.Map, from, to string) *omap.Map {
	out := omap.New()
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		if k == from {
			k = to
		}
		out.Set(k, v)
	}
	return out
}
