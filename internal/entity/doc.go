// Port of the storage half of src/khub/core/entity.py: entity_path,
// _collection_file, _locator, _mutate_collection, _write_new, _read_doc,
// _write_doc and _md_normalized.

package entity

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// entityPath is the on-disk path for slug of rtype: `_index` under a folder,
// flat for a file layout, the one fixed file for a singleton, the shared
// inventory file for a collection (rows have no path of their own — `path#slug`
// is the row locator).
func entityPath(root string, idx *index.Index, node index.Node) string {
	rtype, _ := idx.Resolved.Types.Get(node.Type)
	switch rtype.Storage.Layout {
	case schema.LayoutCollection:
		return collectionFile(root, rtype)
	case schema.LayoutSingleton:
		// One fixed file regardless of slug (the slug IS the type name).
		return filepath.Join(root, storagePath(rtype, rtype.Name+"."+rtype.Storage.Fmt))
	}
	if rtype.Templated() {
		// Where this scan found it. Creation supplies the final templated path
		// separately because the new node has no scan entry yet.
		if rel, ok := idx.Paths[node]; ok {
			return filepath.Join(root, filepath.FromSlash(rel))
		}
		return filepath.Join(root, filepath.FromSlash(rtype.StorageRelpath()), node.Slug+"."+rtype.Storage.Fmt)
	}
	base := filepath.Join(root, storagePath(rtype, rtype.Name))
	if rtype.Storage.Layout == schema.LayoutFolder {
		return filepath.Join(base, node.Slug, "_index."+rtype.Storage.Fmt)
	}
	return filepath.Join(base, node.Slug+"."+rtype.Storage.Fmt)
}

// storagePath is Python's `rtype.storage.path or <fallback>`: an absent OR
// empty declared path falls back.
func storagePath(rtype *schema.ResolvedType, fallback string) string {
	if rtype.Storage.Path != nil && *rtype.Storage.Path != "" {
		return *rtype.Storage.Path
	}
	return fallback
}

// collectionFile is a collection type's one file (the shared default-path rule
// lives on the model).
func collectionFile(root string, rtype *schema.ResolvedType) string {
	return filepath.Join(root, filepath.FromSlash(rtype.CollectionRelpath()))
}

// locator is the row address `path#slug` for a collection row; "" for per-item
// layouts (Python's None).
func locator(rtype *schema.ResolvedType, slug string) string {
	if rtype.Storage.Layout != schema.LayoutCollection {
		return ""
	}
	return rtype.CollectionRelpath() + "#" + slug
}

// sink is where a write verb's publication goes. The real one writes through
// fsio. A dry one runs fsio's checks — workspace confinement, the symlink and
// reparse-point refusals, an existing file where a new one must go — and
// writes nothing, so a dry run refuses where the write would and leaves the
// tree as it found it.
type sink struct{ dry bool }

func (s sink) mkdirAll(root, dir string) error {
	if s.dry {
		return fsio.CheckMkdirAll(root, dir)
	}
	return fsio.MkdirAll(root, dir)
}

func (s sink) publish(root, path string, data []byte, exclusive bool) error {
	switch {
	case s.dry:
		return fsio.CheckPublish(root, path, exclusive)
	case exclusive:
		return fsio.WriteNewIn(root, path, data)
	}
	return fsio.AtomicWriteIn(root, path, data)
}

func (s sink) remove(root, path string, recursive bool) error {
	if s.dry {
		return fsio.CheckRemove(root, path)
	}
	return fsio.Remove(root, path, recursive)
}

// locked runs fn under the workspace lock. A dry run takes no lock — it is a
// read, and reads create none — but first runs the lock path's checks, so a
// lock directory the write could not make refuses the dry run too.
func locked[T any](root string, out sink, fn func() (T, error)) (T, error) {
	if out.dry {
		if err := fsio.CheckLock(root); err != nil {
			var zero T
			return zero, err
		}
		return fn()
	}
	return fsio.Locked(root, fn)
}

// mutateCollection is the one write path for every collection mutation.
//
// Caller holds the workspace lock: fresh in-lock read (the O_EXCL replacement;
// the pre-built index is only a hint), mutate, then write-temp + fsync + rename
// — a crash never leaves a torn file. mutate reports whether to write: false
// skips the rewrite (idempotent no-op). A malformed collection refuses the
// write: khub never rewrites a file it cannot fully round-trip.
//
// The bool reports whether the file's bytes change. A dry run (out.dry) runs
// all of it but the publication.
func mutateCollection(
	root string, rtype *schema.ResolvedType, out sink, mutate func(rows *omap.Map) (bool, error),
) (bool, error) {
	path := collectionFile(root, rtype)
	changed := false
	err := func() error {
		text, readErr := canon.ReadTextIn(root, path)
		switch {
		case readErr == nil:
		case errors.Is(readErr, fs.ErrNotExist):
			text = "" // a missing collection file is zero rows
		default:
			return readErr
		}
		rows, loadErr := canon.LoadCollection(text, rtype.Storage.Fmt)
		if loadErr != nil {
			return errs.New("malformed_entity", fmt.Sprintf(
				"Refusing to write %s: cannot round-trip it (%s)",
				filepath.Base(path), loadErr.Error()))
		}
		write, err := mutate(rows)
		if err != nil {
			return err
		}
		if !write { // no-op: leave the file untouched (no spurious rewrite)
			return nil
		}
		// Comment-preserving write first: a hand-maintained yaml collection
		// carries a header, inter-row and trailing comments that ruamel keeps
		// across an edit (docs/collections-design.md). canon.ErrNoSplice means
		// this particular change is not expressible in place — re-emit the whole
		// file, which is byte-correct but drops the comments.
		dumped, dumpErr := canon.SpliceCollection(text, rows, rtype.Storage.Fmt)
		if errors.Is(dumpErr, canon.ErrNoSplice) {
			dumped, dumpErr = canon.DumpCollection(rows, rtype.Storage.Fmt)
		}
		if dumpErr != nil {
			return dumpErr
		}
		changed = dumped != text
		if err := out.mkdirAll(root, filepath.Dir(path)); err != nil {
			return err
		}
		return out.publish(root, path, []byte(dumped), false)
	}()
	return changed, err
}

// writeNew writes a brand-new entity file exclusively (O_EXCL): fs.ErrExist if
// the slug is taken.
func writeNew(root, path string, out sink, meta *omap.Map, body string) error {
	text, err := canon.Render(meta, body, canon.FmtOf(path))
	if err != nil {
		return err
	}
	return out.publish(root, path, []byte(text), true)
}

// readDoc is the edit-altitude round-trip load: (meta, body) with formatting
// preserved. md fence-splits and loads the frontmatter under ruamel's YAML 1.2
// resolution (the read path's python-frontmatter uses 1.1 — the one place the
// two loaders genuinely differ); yaml/json go through the shared per-item
// parser, which pops the reserved `body` key.
func readDoc(root, path string) (*omap.Map, string, error) {
	text, err := canon.ReadTextIn(root, path)
	if err != nil {
		return nil, "", err
	}
	if canon.FmtOf(path) != "md" {
		return canon.Parse(text, canon.FmtOf(path))
	}
	yamlText, body, splitErr := canon.SplitFrontmatter(text)
	if splitErr != nil {
		return nil, "", errs.New("malformed_entity", splitErr.Error())
	}
	meta, nonMap, loadErr := canon.LoadDocMap(yamlText, canon.Mode12)
	if loadErr != nil {
		return nil, "", errs.New("malformed_entity", loadErr.Error())
	}
	if nonMap != nil {
		// Python would TypeError on the first `cmap[key] = value`; unreachable
		// behind the scan (a non-mapping frontmatter never enters the index).
		return nil, "", errs.New("malformed_entity",
			"A md entity must carry mapping frontmatter")
	}
	return meta, body, nil
}

// writeDoc re-serializes a round-trip map and the (unchanged) body — a minimal
// diff, published atomically while the workspace lock is held.
//
// The document already exists here (writeDoc is only ever reached through
// readDoc), so its current bytes are the comment channel a plain re-emit would
// destroy: canon.SpliceDoc rewrites just the values that moved and leaves every
// other byte — comments included — alone. canon.ErrNoSplice means the change
// cannot be expressed in place; the whole-document emitter takes over, exactly
// as before. The bool reports whether the file's bytes change.
func writeDoc(root, path string, out sink, meta *omap.Map, body string) (bool, error) {
	fmtName := canon.FmtOf(path)
	old, readErr := canon.ReadTextIn(root, path)
	if readErr != nil {
		return false, readErr
	}
	text, err := canon.SpliceDoc(old, meta, body, fmtName, canon.Mode12)
	if errors.Is(err, canon.ErrNoSplice) {
		text, err = canon.Render(meta, body, fmtName)
	}
	if err != nil {
		return false, err
	}
	return text != old, out.publish(root, path, []byte(text), false)
}

// mdNormalized ends newly supplied md prose in a newline (file-format nicety).
// Applied only where NEW body text enters (create, edit --body) and only for md
// — a round-tripped body is written byte-for-byte, and a non-md `body` field
// stores the string verbatim.
func mdNormalized(body string, rtype *schema.ResolvedType) string {
	if rtype.Storage.Fmt == "md" && body != "" && !hasSuffixNewline(body) {
		return body + "\n"
	}
	return body
}

func hasSuffixNewline(s string) bool { return len(s) > 0 && s[len(s)-1] == '\n' }

// templatedPath is the workspace-relative file a new entity of a templated
// type is written to. Of the templates every field placeholder of which meta
// supplies, the one naming the most fields wins (the most specific place),
// the first declared on a tie. A relation value qualified as `type/slug`
// places its slug.
func templatedPath(rtype *schema.ResolvedType, slug string, meta *omap.Map) (string, error) {
	best, bestFields := "", -1
	var firstMissing []string
	for _, tp := range rtype.Storage.Templates {
		rel, missing, err := tp.Fill(slug, func(name string) (string, bool) {
			return rtype.PathField(meta, name)
		})
		var bad *schema.PathValueError
		if errors.As(err, &bad) {
			return "", errs.InvalidPathValue(rtype.Name, bad.Field, bad.Value)
		}
		if err != nil {
			return "", err
		}
		if len(missing) > 0 {
			if firstMissing == nil {
				firstMissing = missing
			}
			continue
		}
		if n := len(tp.Fields()); n > bestFields {
			best, bestFields = rel, n
		}
	}
	if bestFields < 0 {
		return "", errs.PathFieldMissing(rtype.Name, rtype.Storage.Templates[0].String(), firstMissing)
	}
	return best, nil
}

// refuseIgnored refuses to write a new entity where `.khub/ignore` excludes:
// the scan would never read it back, so the write would vanish.
func refuseIgnored(root, path string, ignore *schema.IgnoreList, typeName string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || !ignore.Ignored(rel, false) {
		return nil
	}
	return errs.PathIgnored(typeName, filepath.ToSlash(rel))
}
