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
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// entityPath is the on-disk path for slug of rtype: `_index` under a folder,
// flat for a file layout, the one fixed file for a singleton, the shared
// inventory file for a collection (rows have no path of their own — `path#slug`
// is the row locator).
func entityPath(root string, rtype *schema.ResolvedType, slug string) string {
	switch rtype.Storage.Layout {
	case schema.LayoutCollection:
		return collectionFile(root, rtype)
	case schema.LayoutSingleton:
		// One fixed file regardless of slug (the slug IS the type name).
		return filepath.Join(root, storagePath(rtype, rtype.Name+"."+rtype.Storage.Fmt))
	}
	base := filepath.Join(root, storagePath(rtype, rtype.Name))
	if rtype.Storage.Layout == schema.LayoutFolder {
		return filepath.Join(base, slug, "_index."+rtype.Storage.Fmt)
	}
	return filepath.Join(base, slug+"."+rtype.Storage.Fmt)
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

// mutateCollection is the one write path for every collection mutation.
//
// Caller holds the workspace lock: fresh in-lock read (the O_EXCL replacement;
// the pre-built index is only a hint), mutate, then write-temp + fsync + rename
// — a crash never leaves a torn file. mutate reports whether to write: false
// skips the rewrite (idempotent no-op). A malformed collection refuses the
// write: khub never rewrites a file it cannot fully round-trip.
func mutateCollection(
	root string, rtype *schema.ResolvedType, mutate func(rows *omap.Map) (bool, error),
) error {
	path := collectionFile(root, rtype)
	return func() error {
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
		out, dumpErr := canon.SpliceCollection(text, rows, rtype.Storage.Fmt)
		if errors.Is(dumpErr, canon.ErrNoSplice) {
			out, dumpErr = canon.DumpCollection(rows, rtype.Storage.Fmt)
		}
		if dumpErr != nil {
			return dumpErr
		}
		if err := fsio.MkdirAll(root, filepath.Dir(path)); err != nil {
			return err
		}
		return fsio.AtomicWriteIn(root, path, []byte(out))
	}()
}

// writeNew writes a brand-new entity file exclusively (O_EXCL): fs.ErrExist if
// the slug is taken.
func writeNew(root, path string, meta *omap.Map, body string) error {
	text, err := canon.Render(meta, body, canon.FmtOf(path))
	if err != nil {
		return err
	}
	return fsio.WriteNewIn(root, path, []byte(text))
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
// as before.
func writeDoc(root, path string, meta *omap.Map, body string) error {
	fmtName := canon.FmtOf(path)
	old, readErr := canon.ReadTextIn(root, path)
	if readErr != nil {
		return readErr
	}
	text, err := canon.SpliceDoc(old, meta, body, fmtName, canon.Mode12)
	if errors.Is(err, canon.ErrNoSplice) {
		text, err = canon.Render(meta, body, fmtName)
	}
	if err != nil {
		return err
	}
	return fsio.AtomicWriteIn(root, path, []byte(text))
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
