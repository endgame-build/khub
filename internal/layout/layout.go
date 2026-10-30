// Package layout changes a type's storage layout and format on a live
// corpus — `khub layout migrate`. Per-type `layout`/`format`/`path` is
// config in .khub/storage.yaml; this is the verb that moves the entities
// with it, so file → collection, md → json or folder → file is one planned,
// validated, journaled change instead of a hand-run of git mv and a schema
// edit that leave `check` to find what went wrong.
//
// The shape follows bwrb's `identity migrate`: the whole set is validated
// before anything is written, a dry run is the default, and the execution
// runs under the workspace lock through the same journaled publication
// `khub upgrade` uses (workspace.PublishChanges), so a failure restores the
// tree and a crash leaves a journal to read.
//
// Everything here is schema-generic: the renderers read the resolved type's
// storage and attributes, and the destination config is checked by resolving
// it, so every rule the schema already enforces (the layout × format matrix,
// overlapping paths, control paths, device names) applies to a migration
// without a second copy of it.
package layout

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// Storage is one type's storage config as the plan reports it. Path is the
// workspace-relative directory or file (a templated type lists its templates,
// joined by ", ").
type Storage struct {
	Layout string
	Format string
	Path   string
}

// Target is what the caller asked for. An empty Layout or Format keeps the
// type's current value; an empty Path derives the destination from the
// current one (see derivePath).
type Target struct {
	Layout string
	Format string
	Path   string
}

// Options controls execution. Without Execute the plan is computed and
// nothing is written or locked. Tails runs after a real migration has
// published, still under the workspace lock, so the index tail sees the tree
// the migration committed; the callee must use reindex.ReindexHeld, because
// fsio.Locked is not re-entrant. Tails are non-fatal.
type Options struct {
	Execute bool
	Tails   func(root string)
}

// Plan is what a migration will do, or did. Create, Rewrite and Remove are
// workspace-relative slash paths in entity order: files the plan writes that
// do not exist, files it overwrites (an existing destination holding the
// same bytes, or an empty collection file), and the entity files it removes.
// StorageYAML reports that .khub/storage.yaml is rewritten (always, once a
// plan exists; a plan that would change nothing is refused).
type Plan struct {
	Type        string
	From        Storage
	To          Storage
	Entities    int
	Create      []string
	Rewrite     []string
	Remove      []string
	StorageYAML bool
	Executed    bool

	changes  []workspace.FileChange
	dirs     []string
	emptied  []string // directories to drop once they hold nothing, deepest first
	entities []index.Node
}

// Journal is the recovery journal a migration publishes under.
var Journal = workspace.Journal{Prefix: "migrate-", Noun: "Migration", Code: "migrate_recovery_failed"}

// StorageYAML is the layer file a migration rewrites.
const StorageYAML = ".khub/storage.yaml"

// Migrate plans, and with opt.Execute performs, the move of typeName's
// entities to target. A real run recomputes the plan under the workspace lock
// — the dry run's plan is a preview, never the input of the write.
func Migrate(root, typeName string, target Target, opt Options) (*Plan, error) {
	if !opt.Execute {
		return plan(root, typeName, target)
	}
	return fsio.Locked(root, func() (*Plan, error) {
		p, err := plan(root, typeName, target)
		if err != nil {
			return nil, err
		}
		if err := workspace.PublishChanges(root, p.changes, p.dirs, Journal); err != nil {
			return nil, err
		}
		p.Executed = true
		dropEmptied(root, p.emptied)
		if opt.Tails != nil {
			opt.Tails(root)
		}
		return p, nil
	})
}

// Row is one line of `khub layout`: a type and where its bytes land.
type Row struct {
	Type string
	Storage
}

// Show lists every declared type's storage, in declaration order.
func Show(root string) ([]Row, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, 0, resolved.Types.Len())
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		rows = append(rows, Row{Type: name, Storage: describe(rtype)})
	}
	return rows, nil
}

// describe is the type's storage as the plan and the table report it.
func describe(rtype *schema.ResolvedType) Storage {
	return Storage{Layout: rtype.Storage.Layout, Format: rtype.Storage.Fmt, Path: rtype.StorageShape()}
}

var (
	layouts = []string{schema.LayoutFile, schema.LayoutFolder, schema.LayoutCollection}
	formats = []string{"md", "json", "yaml", "jsonl"}
)

func plan(root, typeName string, target Target) (*Plan, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	rtype, ok := resolved.Types.Get(typeName)
	if !ok {
		prov, perr := workspace.Provenance(root)
		if perr != nil {
			return nil, perr
		}
		preset, _ := prov.Get("preset")
		known := append([]string{}, resolved.Types.Keys()...)
		sort.Strings(known)
		return nil, errs.UnknownType(typeName, values.Str(preset), known)
	}
	from := describe(rtype)
	if rtype.Storage.Layout == schema.LayoutSingleton {
		return nil, errs.New("migrate_singleton", fmt.Sprintf(
			"'%s' is a singleton: one fixed file at %s, which has no layout to migrate. "+
				"Move the file by hand and edit storage.%s.path", typeName, from.Path, typeName))
	}
	to, err := resolveTarget(rtype, target)
	if err != nil {
		return nil, err
	}
	if to == from {
		return nil, errs.New("migrate_unchanged", fmt.Sprintf(
			"'%s' is already stored as %s/%s at %s; pass --layout, --to-format or --path for a different destination",
			typeName, to.Layout, to.Format, to.Path))
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		return nil, err
	}
	if err := refuseUnclean(rtype, idx); err != nil {
		return nil, err
	}
	p := &Plan{Type: typeName, From: from, To: to, Create: []string{}, Rewrite: []string{}, Remove: []string{}, StorageYAML: true}
	for _, node := range idx.Order {
		if node.Type == typeName {
			p.entities = append(p.entities, node)
		}
	}
	p.Entities = len(p.entities)

	// Every entity, read at the edit altitude (the round-trip load, so a
	// value reaches the new file exactly as `edit` would have seen it), then
	// rendered under the destination.
	rows := omap.New() // the destination collection, in entity order
	var creates []workspace.FileChange
	removed := map[string]bool{}
	for _, node := range p.entities {
		meta, body, before, rel, err := readSource(root, rtype, idx, node)
		if err != nil {
			return nil, err
		}
		if rel != "" && !removed[rel] {
			removed[rel] = true
			p.Remove = append(p.Remove, rel)
			p.changes = append(p.changes, workspace.FileChange{Path: filepath.FromSlash(rel), Before: before})
			p.emptied = appendEmptied(p.emptied, rel, rtype)
		}
		meta, err = reshape(rtype, node.Slug, meta, body, from, to)
		if err != nil {
			return nil, err
		}
		if to.Layout == schema.LayoutCollection {
			if body != "" {
				meta.Set("body", body)
			}
			rows.Set(node.Slug, meta)
			continue
		}
		if schema.ReservedDeviceName(node.Slug) {
			return nil, errs.New("migrate_lossy", fmt.Sprintf(
				"'%s/%s' cannot become a %s: '%s' is a reserved device name on Windows, so no file or folder may carry it",
				typeName, node.Slug, to.Layout, node.Slug))
		}
		text, err := canon.Render(meta, mdNormalized(body, to.Format), to.Format)
		if err != nil {
			return nil, err
		}
		change, err := destination(root, rtype, destRel(to, node.Slug), []byte(text), to)
		if err != nil {
			return nil, err
		}
		creates = append(creates, change)
	}
	if to.Layout == schema.LayoutCollection {
		text, err := canon.DumpCollection(rows, to.Format)
		if err != nil {
			return nil, err
		}
		change, err := destination(root, rtype, to.Path, []byte(text), to)
		if err != nil {
			return nil, err
		}
		creates = append(creates, change)
	}
	// Destinations first, the schema next, the sources last: an interrupted
	// run read under the old schema still sees every entity, and one read
	// under the new schema sees the moved ones.
	for _, c := range creates {
		rel := filepath.ToSlash(c.Path)
		if c.Before == nil {
			p.Create = append(p.Create, rel)
		} else {
			p.Rewrite = append(p.Rewrite, rel)
		}
	}
	for _, c := range creates {
		p.dirs = newDirs(root, p.dirs, filepath.Dir(c.Path))
	}
	storageChange, err := storageEntry(root, typeName, to)
	if err != nil {
		return nil, err
	}
	p.changes = append(append(creates, storageChange), p.changes...)
	if err := validateCandidate(root, resolved, p); err != nil {
		return nil, err
	}
	return p, nil
}

// resolveTarget is the destination storage: the caller's values over the
// type's current ones, with the derived path, checked against what a layout
// can carry before the resolver sees it so the refusal names the flag.
func resolveTarget(rtype *schema.ResolvedType, target Target) (Storage, error) {
	to := Storage{Layout: rtype.Storage.Layout, Format: rtype.Storage.Fmt}
	if target.Layout != "" {
		if !contains(layouts, target.Layout) {
			return to, errs.New("migrate_target", fmt.Sprintf(
				"--layout must be one of %s, got '%s'", strings.Join(layouts, ", "), target.Layout))
		}
		to.Layout = target.Layout
	}
	if target.Format != "" {
		if !contains(formats, target.Format) {
			return to, errs.New("migrate_target", fmt.Sprintf(
				"--to-format must be one of %s, got '%s'", strings.Join(formats, ", "), target.Format))
		}
		to.Format = target.Format
	}
	if to.Layout == schema.LayoutCollection && !canon.Collection[to.Format] {
		return to, errs.New("migrate_target", fmt.Sprintf(
			"A collection stores json, jsonl or yaml rows, not %s; pass --to-format", to.Format))
	}
	if to.Layout != schema.LayoutCollection && !canon.PerItem[to.Format] {
		return to, errs.New("migrate_target", fmt.Sprintf(
			"A %s layout stores md, json or yaml files; %s is a collection format", to.Layout, to.Format))
	}
	if target.Path != "" {
		if schema.IsPathTemplate(target.Path) {
			return to, errs.New("migrate_target", fmt.Sprintf(
				"--path '%s' is a path template; a migration writes a plain directory or file. "+
					"Set a template by hand in %s after migrating", target.Path, StorageYAML))
		}
		to.Path = cleanPath(target.Path)
		if !filepath.IsLocal(filepath.FromSlash(to.Path)) {
			return to, errs.New("migrate_target", fmt.Sprintf(
				"--path '%s' leaves the workspace; a storage path is workspace-relative", target.Path))
		}
		if to.Layout == schema.LayoutCollection && strings.TrimPrefix(path.Ext(to.Path), ".") != to.Format {
			return to, errs.New("migrate_target", fmt.Sprintf(
				"--path '%s' must end in .%s: a collection file's extension names its format", target.Path, to.Format))
		}
	} else {
		if rtype.Templated() && to.Layout != schema.LayoutCollection {
			return to, errs.New("migrate_target", fmt.Sprintf(
				"'%s' stores its entities by path template (%s), which a migration cannot rewrite in place; "+
					"pass --path <directory> for a plain %s layout, or --layout collection",
				rtype.Name, rtype.StorageShape(), to.Layout))
		}
		to.Path = derivePath(rtype, to)
	}
	return to, nil
}

// derivePath keeps the type where it is: a directory layout keeps its
// directory, a collection becomes `<directory>.<format>`, and a collection
// unpacked into a directory drops its extension.
func derivePath(rtype *schema.ResolvedType, to Storage) string {
	base := rtype.StorageRelpath()
	if rtype.Storage.Layout == schema.LayoutCollection {
		base = strings.TrimSuffix(base, path.Ext(base))
	}
	if to.Layout == schema.LayoutCollection {
		return base + "." + to.Format
	}
	return base
}

// cleanPath is the caller's --path as the schema will store it: slash
// separated, cleaned, no `./` prefix. Escapes and control paths are left for
// the resolver, whose refusal names them.
func cleanPath(p string) string {
	return path.Clean(strings.TrimPrefix(filepath.ToSlash(p), "./"))
}

// refuseUnclean refuses a type whose storage holds what is not an entity of
// it: a malformed file, a stray (a file or row whose `type` disagrees), or a
// templated duplicate. A migration moves the type's entities and nothing
// else, and silently leaving those behind — or losing a stray row with the
// collection file — is the failure `check` exists to report first.
func refuseUnclean(rtype *schema.ResolvedType, idx *index.Index) error {
	var bad []string
	for _, rel := range idx.Malformed {
		if inRoots(rtype, rel) {
			bad = append(bad, rel+" (malformed)")
		}
	}
	for node := range index.StrayNodes(idx) {
		if node.Type == rtype.Name {
			loc := idx.Paths[node]
			if loc == "" {
				loc = rtype.CollectionRelpath() + "#" + node.Slug
			}
			bad = append(bad, loc+" (stray)")
		}
	}
	for _, d := range idx.Duplicates {
		if d.Type == rtype.Name {
			bad = append(bad, d.Path+" (duplicate)")
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return errs.New("migrate_unclean", fmt.Sprintf(
		"Refusing to migrate '%s': %d file(s) or row(s) in its storage are not entities of it — %s. "+
			"Fix them first; `khub check` reports each", rtype.Name, len(bad), strings.Join(bad, ", ")))
}

func inRoots(rtype *schema.ResolvedType, rel string) bool {
	for _, sp := range rtype.StorageRoots() {
		if rel == sp || strings.HasPrefix(rel, sp+"/") {
			return true
		}
	}
	return false
}

// readSource is one entity at the edit altitude: its meta and body, and for
// a file its staged state and workspace-relative path ("" for a row).
func readSource(root string, rtype *schema.ResolvedType, idx *index.Index, node index.Node) (*omap.Map, string, *workspace.FileState, string, error) {
	if rtype.Storage.Layout == schema.LayoutCollection {
		rows := idx.Collections[node.Type]
		rowAny, _ := rows.Get(node.Slug)
		row, _ := rowAny.(*omap.Map)
		if row == nil {
			return nil, "", nil, "", errs.LookupError(node.ID())
		}
		meta, body, err := canon.SplitRow(row, rtype.Storage.Fmt)
		if err != nil {
			return nil, "", nil, "", err
		}
		rel := rtype.CollectionRelpath()
		before, err := workspace.ReadFileState(root, filepath.FromSlash(rel))
		if err != nil {
			return nil, "", nil, "", err
		}
		return meta, body, before, rel, nil
	}
	rel := idx.Paths[node]
	before, err := workspace.ReadFileState(root, filepath.FromSlash(rel))
	if err != nil {
		return nil, "", nil, "", err
	}
	if before == nil {
		return nil, "", nil, "", errs.LookupError(node.ID())
	}
	meta, body, err := canon.ReadDoc(canon.NormalizeNewlines(string(before.Data)), rtype.Storage.Fmt)
	if err != nil {
		return nil, "", nil, "", errs.New("migrate_unclean", fmt.Sprintf(
			"Refusing to migrate '%s': %s cannot be round-tripped (%s)", rtype.Name, rel, err.Error()))
	}
	return meta, body, before, rel, nil
}

// reshape is the entity's meta as the destination stores it: `type` opens a
// file and leaves a row (the collection's binding supplies it); a row's
// redundant `slug` goes; a value the destination cannot carry refuses; and a
// declared date read back from json's strings becomes a date again on the
// way to md or yaml, so a round trip reproduces the file it started from.
func reshape(rtype *schema.ResolvedType, slug string, meta *omap.Map, body string, from, to Storage) (*omap.Map, error) {
	out := omap.New()
	if to.Layout != schema.LayoutCollection {
		if _, has := meta.Get("type"); !has {
			out.Set("type", rtype.Name)
		}
	}
	for _, k := range meta.Keys() {
		v, _ := meta.Get(k)
		switch {
		case k == "type" && to.Layout == schema.LayoutCollection:
			continue
		case k == "slug" && to.Layout == schema.LayoutCollection:
			if s, ok := v.(string); ok && s == slug {
				continue
			}
			return nil, errs.New("migrate_lossy", fmt.Sprintf(
				"'%s/%s' carries a 'slug' field (%s) that disagrees with its slug; a collection row's slug is its identity. "+
					"Drop the field first", rtype.Name, slug, values.Str(v)))
		case k == "body" && to.Format != "md":
			return nil, errs.New("migrate_lossy", fmt.Sprintf(
				"'%s/%s' carries a 'body' frontmatter field, which is the prose channel of a %s entity. "+
					"Rename the field first", rtype.Name, slug, to.Format))
		}
		out.Set(k, v)
	}
	if textual(from.Format) && !textual(to.Format) {
		for _, name := range rtype.Attributes.Keys() {
			attr, _ := rtype.Attributes.Get(name)
			v, has := out.Get(name)
			s, isStr := v.(string)
			if !has || !isStr || attr.BaseType != "date" {
				continue
			}
			if _, err := time.Parse("2006-01-02", s); err == nil && len(s) == 10 {
				out.Set(name, canon.Date{ISO: s})
			}
		}
	}
	return out, nil
}

// textual reports a format that carries dates as strings.
func textual(format string) bool { return format == "json" || format == "jsonl" }

// mdNormalized ends new md prose in a newline, as `add` does; every other
// format stores the string verbatim.
func mdNormalized(body, format string) string {
	if format == "md" && body != "" && !strings.HasSuffix(body, "\n") {
		return body + "\n"
	}
	return body
}

// destRel is the file one entity lands in under to.
func destRel(to Storage, slug string) string {
	switch to.Layout {
	case schema.LayoutFolder:
		return to.Path + "/" + slug + "/_index." + to.Format
	case schema.LayoutCollection:
		return to.Path
	}
	return to.Path + "/" + slug + "." + to.Format
}

// destination is the change that writes data at rel. A path that exists is
// taken only when it already holds these bytes or is an empty collection
// file; anything else is refused, since a migration never overwrites what it
// did not produce. An ignored path is refused too: no scan would read it back.
func destination(root string, rtype *schema.ResolvedType, rel string, data []byte, to Storage) (workspace.FileChange, error) {
	if ignore := currentIgnore(root); ignore.Ignored(rel, false) {
		return workspace.FileChange{}, errs.New("migrate_target", fmt.Sprintf(
			"Refusing to migrate '%s' to %s: .khub/ignore excludes that path, so no scan would read it; "+
				"remove the pattern or choose another --path", rtype.Name, rel))
	}
	existing, err := workspace.ReadFileState(root, filepath.FromSlash(rel))
	if err != nil {
		return workspace.FileChange{}, err
	}
	change := workspace.FileChange{Path: filepath.FromSlash(rel), After: &workspace.FileState{Data: data, Mode: 0o644}}
	if existing == nil {
		return change, nil
	}
	empty := false
	if to.Layout == schema.LayoutCollection && rel == to.Path {
		rows, lerr := canon.LoadCollection(canon.NormalizeNewlines(string(existing.Data)), to.Format)
		empty = lerr == nil && rows.Len() == 0
	}
	if !bytes.Equal(existing.Data, data) && !empty {
		return workspace.FileChange{}, errs.New("migrate_target_exists", fmt.Sprintf(
			"Refusing to migrate '%s': %s exists and is not what the migration would write. "+
				"Move it away, or choose another --path", rtype.Name, rel))
	}
	change.Before = existing
	change.After.Mode = existing.Mode
	return change, nil
}

// currentIgnore reads the workspace's ignore list; an unreadable one ignores
// nothing here, since the scan that produced the plan already refused it.
func currentIgnore(root string) *schema.IgnoreList {
	l, err := schema.LoadIgnore(root)
	if err != nil {
		return nil
	}
	return l
}

// newDirs appends the directories dir needs that the live tree lacks,
// shallowest first, once each.
func newDirs(root string, dirs []string, dir string) []string {
	var missing []string
	for d := dir; d != "." && d != "" && d != string(filepath.Separator); d = filepath.Dir(d) {
		if _, err := fsio.Stat(root, filepath.Join(root, d)); err == nil {
			break
		}
		missing = append(missing, d)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if !contains(dirs, missing[i]) {
			dirs = append(dirs, missing[i])
		}
	}
	return dirs
}

// appendEmptied records the directories a removed file may leave empty, up
// to and including the type's storage root, deepest first.
func appendEmptied(emptied []string, rel string, rtype *schema.ResolvedType) []string {
	for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
		if !contains(emptied, d) {
			emptied = append(emptied, d)
		}
		if !inRoots(rtype, d) || contains(rtype.StorageRoots(), d) {
			break
		}
	}
	return emptied
}

// dropEmptied removes the directories a migration emptied. A directory that
// still holds something stays, as does one that cannot be removed: nothing
// here is part of the contract the journal covers.
func dropEmptied(root string, emptied []string) {
	sort.Slice(emptied, func(i, j int) bool { return len(emptied[i]) > len(emptied[j]) })
	for _, d := range emptied {
		_ = fsio.Remove(root, filepath.Join(root, filepath.FromSlash(d)), false)
	}
}

// storageEntry is the change that records the destination in storage.yaml:
// the type's entry gains the new layout, path and — for a directory layout
// off the md default — format, keeps its other keys, and the file keeps every
// other byte through canon.SpliceEntry. A document the splice cannot address
// is re-emitted whole behind its leading comment lines.
func storageEntry(root, typeName string, to Storage) (workspace.FileChange, error) {
	rel := filepath.FromSlash(StorageYAML)
	before, err := workspace.ReadFileState(root, rel)
	if err != nil {
		return workspace.FileChange{}, err
	}
	raw := ""
	if before != nil {
		raw = string(before.Data)
	}
	doc, nonMap, err := canon.LoadDocMap(raw, canon.Mode12)
	if err != nil {
		return workspace.FileChange{}, errs.New("schema_error", fmt.Sprintf("Cannot parse schema %s: %s", StorageYAML, err.Error()))
	}
	if nonMap != nil {
		return workspace.FileChange{}, errs.New("schema_error", fmt.Sprintf("Cannot parse schema %s: the document is not a mapping", StorageYAML))
	}
	entry := omap.New()
	storage, _ := doc.Get("storage")
	if sm, ok := storage.(*omap.Map); ok {
		if ev, has := sm.Get(typeName); has {
			if em, ok := ev.(*omap.Map); ok {
				for _, k := range em.Keys() {
					v, _ := em.Get(k)
					entry.Set(k, v)
				}
			}
		}
	}
	entry.Set("layout", to.Layout)
	entry.Set("path", to.Path)
	// A collection's path carries its format in its extension; a directory
	// layout declares a non-default format.
	if to.Layout != schema.LayoutCollection && to.Format != "md" {
		entry.Set("format", to.Format)
	} else {
		entry.Delete("format")
	}
	out, err := canon.SpliceEntry([]byte(raw), []string{"storage", typeName}, entry, canon.Mode12)
	if errors.Is(err, canon.ErrNoSplice) {
		sm, _ := storage.(*omap.Map)
		if sm == nil {
			sm = omap.New()
			doc.Set("storage", sm)
		}
		sm.Set(typeName, entry)
		text, derr := canon.DumpWide(doc)
		if derr != nil {
			return workspace.FileChange{}, derr
		}
		out = []byte(leadingComments(raw) + text)
	} else if err != nil {
		return workspace.FileChange{}, err
	}
	after := &workspace.FileState{Data: out, Mode: 0o644}
	if before != nil {
		after.Mode = before.Mode
	}
	return workspace.FileChange{Path: rel, Before: before, After: after}, nil
}

// leadingComments is the comment and blank lines a document opens with — the
// layer provenance header a whole-document re-emit must keep.
func leadingComments(text string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		b.WriteString(line)
	}
	return b.String()
}

// validateCandidate applies the plan to a copy of the workspace and holds it
// to the contract the live one meets: the schema resolves, every entity of
// the type re-parses under the new storage as the same set of nodes, and
// `validate` reports no error it did not already report. Resolver refusals
// pass through as they are; the rest is migrate_invalid.
func validateCandidate(root string, resolved *schema.ResolvedSchema, p *Plan) error {
	cand, _, _, cleanup, err := workspace.Candidate(root, resolved)
	if err != nil {
		return err
	}
	defer cleanup()
	for _, c := range p.changes {
		target := filepath.Join(cand, c.Path)
		if c.After == nil {
			if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o777); err != nil {
			return err
		}
		if err := os.WriteFile(target, c.After.Data, 0o666); err != nil {
			return err
		}
	}
	after, err := introspect.LoadSchema(cand)
	if err != nil {
		return err
	}
	rtype, _ := after.Types.Get(p.Type)
	idx, err := index.Build(cand, after)
	if err != nil {
		return err
	}
	invalid := func(detail string) error {
		return errs.New("migrate_invalid", fmt.Sprintf(
			"Refusing to migrate '%s': under the new storage %s", p.Type, detail))
	}
	for _, rel := range idx.Malformed {
		if inRoots(rtype, rel) {
			return invalid(fmt.Sprintf("%s does not parse", rel))
		}
	}
	strays := index.StrayNodes(idx)
	var got []string
	for _, node := range idx.Order {
		if node.Type == p.Type && !strays[node] {
			got = append(got, node.Slug)
		}
	}
	want := make([]string, 0, len(p.entities))
	for _, node := range p.entities {
		want = append(want, node.Slug)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		return invalid(fmt.Sprintf("the scan finds %d entities where %d were migrated", len(got), len(want)))
	}
	liveReport, err := integrity.Validate(root, nil, false)
	if err != nil {
		return err
	}
	candReport, err := integrity.Validate(cand, nil, false)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, e := range liveReport.Errors {
		known[e.ID()+"\x00"+e.Field+"\x00"+e.Reason] = true
	}
	var fresh []string
	for _, e := range candReport.Errors {
		if !known[e.ID()+"\x00"+e.Field+"\x00"+e.Reason] {
			fresh = append(fresh, fmt.Sprintf("%s: %s: %s", e.ID(), e.Field, e.Reason))
		}
	}
	if len(fresh) > 0 {
		shown := fresh
		more := ""
		if len(shown) > 3 {
			more = fmt.Sprintf(" (+%d more)", len(shown)-3)
			shown = shown[:3]
		}
		return invalid(fmt.Sprintf("`khub validate` would report %d new error(s) — %s%s", len(fresh), strings.Join(shown, "; "), more))
	}
	return nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
