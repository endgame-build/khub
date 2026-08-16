// Package fsio ports the durability helpers core/entity.py writes through: the
// exclusive create that gates slug uniqueness (`path.open("x")`), the
// temp-sibling + fsync + rename swap `_mutate_collection` lands with
// (`os.replace`), and the collection lock it holds across the in-lock re-read
// (`fcntl.flock` on `.khub/generated/locks/<type>.lock`).
//
// Only collection writes are atomic. A per-item entity is rewritten in place by
// `_write_doc`'s plain `path.write_text` — porting that as an atomic swap would
// change inode behavior git and editors observe, so it stays a plain write.
package fsio

import "os"

// AtomicWrite replaces path's contents via a temp sibling: write, flush, fsync,
// rename. The temp name is Python's `path.with_name(path.name + ".tmp")`, and
// os.Rename is os.replace (both overwrite atomically within a filesystem).
func AtomicWrite(path string, data []byte) (err error) {
	tmp := path + ".tmp"
	fh, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	// Every failure below must take the temp file with it: a half-written
	// <name>.tmp left in the tree is a stray `check` reports and a human has
	// to reason about.
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	if _, err = fh.Write(data); err != nil {
		_ = fh.Close()
		return err
	}
	if err = fh.Sync(); err != nil {
		_ = fh.Close()
		return err
	}
	if err = fh.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WriteNew creates path exclusively — Python's `path.open("x")`. The O_EXCL
// create IS the slug-uniqueness gate (the index check is only a hint), so a
// concurrent add racing to the same path gets fs.ErrExist here instead of
// silently overwriting the first writer.
func WriteNew(path string, data []byte) (err error) {
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	// The exclusive create already claimed the slug, so a failed write would
	// leave a truncated file holding a name no retry can reuse. Remove it.
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if _, err = fh.Write(data); err != nil {
		_ = fh.Close()
		return err
	}
	return fh.Close()
}
