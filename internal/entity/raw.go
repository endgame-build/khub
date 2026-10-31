package entity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// RawEntity is one entity written with its frontmatter as given, for
// `khub import`: the fields, their values and their order are the caller's,
// not rebuilt from the schema. Stem is the name it came under; WriteRaw mints
// its id from it. Source, when set, is the concept's md text: an md type
// splices the new frontmatter into it, renaming the keys Renames names in
// place, so its comments and formatting survive. Quarantine, when set, is
// the key a relation value no entity answers to moves to, as a mapping of
// field to values; when empty, a real write refuses such a value.
type RawEntity struct {
	Type, Stem string
	Meta       *omap.Map
	Body       string
	Source     string
	Renames    []canon.Rename
	Quarantine string
}

// RawResult is one item's outcome. Slug is its id; Alias is the stem, when
// the id differs from it and the stem is in `aliases`; AliasDropped is the
// stem when it was left out because another entity already answers to it.
// Unresolved lists, per relation field, the values no entity answers to. Err
// is the refusal that kept the item out.
type RawResult struct {
	Slug, Path, Alias, AliasDropped string
	Unresolved                      *omap.Map // field -> []string
	Err                             error
}

// WriteRawHeld plans every item by add's rules and, unless dryRun, writes
// the batch. The caller holds the workspace lock for a real run, so the plan
// and the writes see one workspace. Callers' Meta maps are never changed.
//
//   - Its id is the type's scheme around the stem, minted as add mints one:
//     a stem that already carries the type's prefix (and date) is kept, any
//     other gets them. The stem is added to `aliases` when the id differs,
//     unless an entity of the workspace or the batch already answers to it.
//   - placeNew gates the id and the file, as for add.
//   - Every declared relation value must resolve, against the workspace and
//     the rest of the batch. A value that does not moves to the item's
//     Quarantine key, or refuses a real write when it has none. Values naming
//     one target twice keep their first spelling.
//
// The plan runs to a fixed point: an item refused leaves the batch, so a
// value naming it resolves no longer, until no refusal is left. One item's
// refusal is its result and never stops the others. The writes follow only
// once the plan is final, all or none: every file is rendered first, and a
// write that still fails removes or restores every file the run wrote and
// returns its error. Under the lock a write fails only
// on the filesystem, never on another item, and a partial batch would leave
// edges to concepts that never landed. `generated` is stamped only when it is
// absent.
func WriteRawHeld(root string, items []RawEntity, actor string, dryRun bool) ([]RawResult, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	idx = index.Filter(idx, index.StrayNodes(idx))
	out := make([]RawResult, len(items))
	metas := make([]*omap.Map, len(items))
	planned := map[index.Node]bool{}
	for i, it := range items {
		metas[i] = shallowCopy(it.Meta)
		out[i] = placeRaw(root, resolved, idx, it.Type, it.Stem, metas[i], planned)
		if out[i].Err == nil {
			planned[index.Node{Type: it.Type, Slug: out[i].Slug}] = true
		}
	}
	grantAliases(resolved, idx, items, metas, out)
	gated := gateBatch(root, resolved, idx, items, metas, out, dryRun)
	if dryRun {
		return out, nil
	}
	writes, err := prepare(root, resolved, items, gated, out, actor)
	if err != nil {
		return nil, err
	}
	return out, publishAll(root, writes)
}

// placeRaw mints the item's id and gates it. The stem's alias is decided
// later, once every item has its id.
func placeRaw(
	root string, resolved *schema.ResolvedSchema, idx *index.Index, typeName, stem string, meta *omap.Map,
	planned map[index.Node]bool,
) RawResult {
	rtype, ok := resolved.Types.Get(typeName)
	if !ok {
		known := append([]string{}, resolved.Types.Keys()...)
		sort.Strings(known)
		preset, err := presetName(root)
		if err != nil {
			return RawResult{Err: err}
		}
		return RawResult{Err: errs.UnknownType(typeName, preset, known)}
	}
	slug, alias, err := importSlug(rtype, meta, stem)
	if err != nil {
		return RawResult{Err: err}
	}
	if planned[index.Node{Type: typeName, Slug: slug}] {
		return RawResult{Slug: slug, Err: errs.SlugTaken(slug, typeName, errs.Imported)}
	}
	path, err := placeNew(root, resolved, idx, rtype, slug, meta, errs.Imported)
	if err != nil {
		return RawResult{Slug: slug, Err: err}
	}
	if !rtype.Attributes.Has("aliases") {
		alias = ""
	}
	return RawResult{Slug: slug, Path: path, Alias: alias}
}

// grantAliases adds each placed item's stem to its `aliases`, unless a slug
// or an alias of the workspace, or of another item of the batch, already
// answers to it: a lookup by the stem would then be ambiguous, so the stem is
// reported as dropped instead. A stem the item already lists stays.
func grantAliases(resolved *schema.ResolvedSchema, idx *index.Index, items []RawEntity, metas []*omap.Map, out []RawResult) {
	claims := map[string][]int{} // folded slug or alias -> batch items claiming it
	claim := func(name string, i int) {
		f := folder.String(name)
		if !slices.Contains(claims[f], i) {
			claims[f] = append(claims[f], i)
		}
	}
	for i := range items {
		if out[i].Err != nil {
			continue
		}
		claim(out[i].Slug, i)
		for _, a := range index.AliasesOf(metas[i]) {
			claim(a, i)
		}
	}
	for i := range items {
		alias := out[i].Alias
		if out[i].Err != nil || alias == "" {
			continue
		}
		list := values.Items(func() any { v, _ := metas[i].Get("aliases"); return v }())
		if slices.ContainsFunc(list, func(v any) bool { return foldEqual(values.Str(v), alias) }) {
			continue
		}
		_, slugTaken := idx.CanonicalSlug(alias)
		taken := slugTaken || len(idx.AliasOwners(alias)) > 0 ||
			slices.ContainsFunc(claims[folder.String(alias)], func(j int) bool { return j != i })
		if taken {
			out[i].Alias, out[i].AliasDropped = "", alias
			continue
		}
		metas[i].Set("aliases", append(slices.Clone(list), alias))
		claim(alias, i)
	}
}

// gateBatch resolves every placed item's relation values against the
// workspace and the batch, to a fixed point, and returns each item's
// frontmatter as it will be written.
func gateBatch(
	root string, resolved *schema.ResolvedSchema, idx *index.Index, items []RawEntity, metas []*omap.Map,
	out []RawResult, dryRun bool,
) []*omap.Map {
	gated := make([]*omap.Map, len(items))
	for changed := true; changed; {
		changed = false
		view := withPlanned(idx, root, items, metas, out)
		for i, it := range items {
			if out[i].Err != nil {
				continue
			}
			rtype, _ := resolved.Types.Get(it.Type)
			g, unresolved, err := gateRelations(view, rtype, metas[i])
			if err == nil {
				err = quarantine(rtype, it.Quarantine, g, unresolved, dryRun)
			}
			if err != nil {
				out[i] = RawResult{Slug: out[i].Slug, Err: err}
				changed = true
				continue
			}
			gated[i], out[i].Unresolved = g, unresolved
		}
	}
	return gated
}

// quarantine moves the values no entity answers to into the item's
// quarantine key, refusing an item that already carries that key. With no
// key, a real write refuses the item; a dry run reports the values.
func quarantine(rtype *schema.ResolvedType, key string, meta, unresolved *omap.Map, dryRun bool) error {
	if unresolved.Len() == 0 {
		return nil
	}
	if key == "" {
		if dryRun {
			return nil
		}
		field := unresolved.Keys()[0]
		v, _ := unresolved.Get(field)
		rel, _ := rtype.Relations.Get(field)
		return errs.ReferentialIntegrity(strings.Join(rel.Targets, "/"), v.([]string)[0], field, "relation")
	}
	if _, taken := meta.Get(key); taken {
		return errs.QuarantineTaken(key, "its unresolved relation values")
	}
	held := omap.New()
	for _, field := range unresolved.Keys() {
		lost, _ := unresolved.Get(field)
		held.Set(field, toAnyList(lost.([]string)))
	}
	meta.Set(key, held)
	return nil
}

// datedOpening is the `YYYY-MM-DD-` an id_date type's id opens with after its
// prefix.
var datedOpening = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}-`)

// importSlug is the id an imported concept takes: the stem when it already
// follows the type's scheme (its prefix, and its date for a dated type), else
// the scheme minted around the stem, without doubling a prefix or date the
// stem opens with. alias is the stem when the id differs from it. A
// singleton's id is its type name.
func importSlug(rtype *schema.ResolvedType, meta *omap.Map, stem string) (string, string, error) {
	if rtype.Storage.Layout == schema.LayoutSingleton {
		return rtype.Name, "", nil
	}
	base, err := slugBase(stem)
	if err != nil {
		// A stem with no usable name mints from the title, as add does.
		minted, merr := mintSlug(rtype, meta)
		return minted, "", merr
	}
	rest, conforms := base, true
	if rtype.IDPrefix != nil {
		prefix, ok := rtype.IDPrefix.Resolve(meta)
		if !ok {
			_, err := mintFrom(rtype, meta, base) // names the attribute that decides it
			return "", "", err
		}
		rest = strings.TrimPrefix(base, prefix+"-")
		conforms = rest != base
	}
	if rtype.IDDate {
		conforms = conforms && datedOpening.MatchString(rest)
		rest = datedOpening.ReplaceAllString(rest, "")
	}
	if conforms {
		return base, "", nil
	}
	slug, err := mintFrom(rtype, meta, rest)
	if err != nil {
		return "", "", err
	}
	return slug, base, nil
}

// withPlanned is idx with the batch's placed items added, so a value may name
// another item of the batch by its id, its alias, or its path.
func withPlanned(idx *index.Index, root string, items []RawEntity, metas []*omap.Map, results []RawResult) *index.Index {
	view := index.Copy(idx)
	for i, r := range results {
		if r.Err != nil {
			continue
		}
		node := index.Node{Type: items[i].Type, Slug: r.Slug}
		view.Nodes[node] = true
		view.Order = append(view.Order, node)
		if view.TypesBySlug[r.Slug] == nil {
			view.TypesBySlug[r.Slug] = map[string]bool{}
		}
		view.TypesBySlug[r.Slug][node.Type] = true
		view.Meta[node] = metas[i]
		if rel, err := filepath.Rel(root, r.Path); err == nil && r.Path != "" {
			view.Paths[node] = filepath.ToSlash(rel)
		}
	}
	return view
}

// gateRelations resolves every declared relation value against view, as add
// does: an alias becomes its owner's slug, and a value naming a target twice
// keeps its first spelling. It returns meta as it will be written, without
// the values nothing answers to, and those values per field; meta itself is
// not changed. A single-valued field left holding two targets is a
// cardinality refusal.
func gateRelations(view *index.Index, rtype *schema.ResolvedType, meta *omap.Map) (*omap.Map, *omap.Map, error) {
	out, unresolved := shallowCopy(meta), omap.New()
	for _, pred := range rtype.Relations.Keys() {
		raw, has := meta.Get(pred)
		if !has || !values.Truthy(raw) {
			continue
		}
		rel, _ := rtype.Relations.Get(pred)
		var kept, lost []string
		for _, v := range asTargetList(raw) {
			t, err := aliasTarget(view, rel, v)
			if err == nil {
				_, err = resolveWriteTarget(rel, t, view, pred, "relation")
			}
			if err != nil {
				lost = append(lost, v)
				continue
			}
			kept = append(kept, t)
		}
		kept = uniqueTargets(view, rel, kept)
		if len(lost) > 0 {
			unresolved.Set(pred, lost)
		}
		if !rel.Many && len(kept) > 1 {
			return nil, unresolved, errs.CardinalityViolation(pred)
		}
		if slices.Equal(kept, asTargetList(raw)) {
			continue // untouched: the value keeps its own shape
		}
		switch {
		case len(kept) == 0:
			out.Delete(pred)
		case !rel.Many:
			out.Set(pred, kept[0])
		default:
			out.Set(pred, toAnyList(kept))
		}
	}
	return out, unresolved, nil
}

// shallowCopy is m with its own key order, sharing the values.
func shallowCopy(m *omap.Map) *omap.Map {
	out := omap.New()
	for _, k := range m.Keys() {
		v, _ := m.Get(k)
		out.Set(k, v)
	}
	return out
}

// pending is one file the batch publishes: a new entity file, or a
// collection file rewritten with its new rows. before is a collection file's
// text as found, existed whether it was there at all, for rollback.
type pending struct {
	path, text, before  string
	collection, existed bool
	rows                *omap.Map
	format              string
}

// prepare renders every file the batch writes, before any is written.
func prepare(
	root string, resolved *schema.ResolvedSchema, items []RawEntity, gated []*omap.Map, out []RawResult, actor string,
) ([]pending, error) {
	var writes []pending
	byFile := map[string]int{}
	for i, it := range items {
		if out[i].Err != nil {
			continue
		}
		rtype, _ := resolved.Types.Get(it.Type)
		meta := gated[i]
		if _, has := meta.Get("generated"); !has {
			stamp(rtype, meta, actor)
		}
		if rtype.Storage.Layout == schema.LayoutCollection {
			k, seen := byFile[out[i].Path]
			if !seen {
				w, err := loadPending(root, out[i].Path, rtype.Storage.Fmt)
				if err != nil {
					return nil, err
				}
				writes = append(writes, w)
				k = len(writes) - 1
				byFile[out[i].Path] = k
			}
			if _, taken := writes[k].rows.Get(out[i].Slug); taken {
				return nil, errs.SlugTaken(out[i].Slug, it.Type, errs.Imported)
			}
			writes[k].rows.Set(out[i].Slug, newRow(meta, it.Body))
			continue
		}
		format := rtype.Storage.Fmt
		text, err := "", canon.ErrNoSplice
		if it.Source != "" && format == "md" {
			text, err = canon.SpliceDoc(it.Source, meta, it.Body, format, canon.Mode12, it.Renames...)
		}
		if errors.Is(err, canon.ErrNoSplice) {
			text, err = canon.Render(meta, it.Body, format)
		}
		if err != nil {
			return nil, err
		}
		writes = append(writes, pending{path: out[i].Path, text: text})
	}
	for k := range writes {
		w := &writes[k]
		if !w.collection {
			continue
		}
		// Comment-preserving first, as mutateCollection writes a row.
		text, err := canon.SpliceCollection(w.before, w.rows, w.format)
		if errors.Is(err, canon.ErrNoSplice) {
			text, err = canon.DumpCollection(w.rows, w.format)
		}
		if err != nil {
			return nil, err
		}
		w.text = text
	}
	return writes, nil
}

// loadPending reads a collection file the batch adds rows to.
func loadPending(root, path, format string) (pending, error) {
	w := pending{path: path, collection: true, existed: true, format: format}
	text, err := canon.ReadTextIn(root, path)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist):
		text, w.existed = "", false // a missing collection file is zero rows
	default:
		return w, err
	}
	rows, err := canon.LoadCollection(text, format)
	if err != nil {
		return w, errs.New("malformed_entity", fmt.Sprintf(
			"Refusing to write %s: cannot round-trip it (%s)", filepath.Base(path), err.Error()))
	}
	w.before, w.rows = text, rows
	return w, nil
}

// publishAll writes every pending file. A failure removes the new files,
// restores the collection files and removes the directories the run made,
// then returns the write's error; `import_write_failed` only when some of
// that could not be undone.
func publishAll(root string, writes []pending) error {
	var done []pending
	var made []string
	for _, w := range writes {
		dirs, err := mkdirs(root, filepath.Dir(w.path))
		made = append(made, dirs...)
		if err == nil {
			if w.collection {
				err = fsio.AtomicWriteIn(root, w.path, []byte(w.text))
			} else {
				err = fsio.WriteNewIn(root, w.path, []byte(w.text))
			}
		}
		if err != nil {
			if left := rollback(root, done, made); len(left) > 0 {
				return errs.ImportWriteFailed(relTo(root, w.path), err.Error(), left)
			}
			return err
		}
		done = append(done, w)
	}
	return nil
}

// mkdirs makes dir and returns the directories it created, outermost first.
func mkdirs(root, dir string) ([]string, error) {
	var missing []string
	for d := dir; len(d) > len(root); d = filepath.Dir(d) {
		if _, err := os.Lstat(d); err == nil {
			break
		}
		missing = append(missing, d)
	}
	slices.Reverse(missing)
	return missing, fsio.MkdirAll(root, dir)
}

// rollback undoes what publishAll wrote, newest first, and returns the
// workspace-relative paths it could not undo.
func rollback(root string, done []pending, made []string) []string {
	var left []string
	undo := func(path string, err error) {
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			left = append(left, relTo(root, path))
		}
	}
	for _, w := range slices.Backward(done) {
		if w.collection && w.existed {
			undo(w.path, fsio.AtomicWriteIn(root, w.path, []byte(w.before)))
		} else {
			undo(w.path, fsio.Remove(root, w.path, false))
		}
	}
	for _, d := range slices.Backward(made) {
		undo(d, fsio.Remove(root, d, false))
	}
	return left
}

func relTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
