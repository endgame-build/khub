package workspace

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
)

// FileState is one file's bytes and permission bits, as staged in a journal.
type FileState struct {
	Data []byte
	Mode fs.FileMode
}

// FileChange is one path's transition inside a journaled publication. Path is
// workspace-relative in the OS's separators. A nil Before is a file that did
// not exist; a nil After is a removal.
type FileChange struct {
	Path   string
	Before *FileState
	After  *FileState
}

// Journal names the verb a publication belongs to: the prefix of its
// recovery directory under .khub/generated/, the noun its messages open
// with, and the code a failed recovery is reported under.
type Journal struct {
	Prefix string
	Noun   string
	Code   string
}

// UpgradeJournal is the journal `khub upgrade` publishes under.
var UpgradeJournal = Journal{Prefix: "upgrade-", Noun: "Upgrade", Code: "upgrade_recovery_failed"}

// Upgrade preflights in isolation; writers recompute under the workspace lock.
// A crash leaves a journal for manual inspection, never an automatic recovery claim.
func Upgrade(root string, opt UpgradeOptions) (*UpgradeResult, error) {
	if opt.DryRun {
		return prepareUpgrade(root, opt)
	}
	return fsio.Locked(root, func() (*UpgradeResult, error) { return prepareUpgrade(root, opt) })
}

func prepareUpgrade(root string, opt UpgradeOptions) (*UpgradeResult, error) {
	prov, err := Provenance(root)
	if err != nil {
		return nil, err
	}
	if configString(prov, "preset") == "" {
		return nil, errs.NoPreset(root)
	}
	source, err := PresetSource(root)
	if err != nil {
		return nil, err
	}
	// Replacement upgrades can repair invalid current schemas. --no-schema
	// still validates the preserved schema when preparing the candidate below.
	old, _ := introspect.LoadSchema(root)
	candidate, before, dirs, cleanup, err := Candidate(root, old)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	result, err := upgradeCandidate(candidate, opt, source)
	if err != nil {
		return nil, err
	}
	result.Path = pyPath(filepath.ToSlash(root))
	result.DryRun = opt.DryRun
	result.RemovedTypes = []string{}
	resolved, err := introspect.LoadSchema(candidate)
	if err != nil {
		return nil, err
	}
	for _, name := range resolved.Types.Keys() {
		rt, _ := resolved.Types.Get(name)
		if stem := rt.TemplateName(); rt.ReadsTemplate() && stem != "" {
			if _, err := template.LoadTemplate(candidate, stem, rt.FieldNames()); err != nil {
				return nil, err
			}
		}
	}
	if old != nil {
		for _, name := range old.Types.Keys() {
			if _, ok := resolved.Types.Get(name); !ok {
				result.RemovedTypes = append(result.RemovedTypes, name)
			}
		}
	}
	writes, newDirs, err := upgradeDiff(candidate, before, dirs)
	if err != nil {
		return nil, err
	}
	// Check real paths even for dry runs: a copied candidate must not hide an
	// unsafe symlink or a non-directory destination in the live workspace.
	for _, w := range writes {
		if _, err := ReadFileState(root, w.Path); err != nil {
			return nil, err
		}
	}
	for _, dir := range newDirs {
		r, _, err := fsio.OpenPath(root, filepath.Join(root, dir))
		if err != nil {
			return nil, err
		}
		r.Close()
	}
	if opt.DryRun {
		if opt.Preview != nil {
			if err := opt.Preview(candidate); err != nil {
				return nil, err
			}
		}
		return result, nil
	}
	if err := PublishChanges(root, writes, newDirs, UpgradeJournal); err != nil {
		return nil, err
	}
	if opt.Tails != nil {
		opt.Tails(root)
	}
	return result, nil
}

// Candidate copies the workspace at root into a temporary directory an
// operator verb can rewrite and scan in isolation, before anything is
// published. The copy leaves out .git/, .khub/generated/, vendored trees and
// non-regular files (see copyCandidate). It returns the copy's path, every
// regular file it carried (workspace-relative, OS separators), every
// directory, and the function that removes the copy. old is the schema the
// live workspace resolves under, or nil when it does not resolve.
func Candidate(root string, old *schema.ResolvedSchema) (dir string, files map[string]FileState, dirs map[string]bool, cleanup func(), err error) {
	dir, err = os.MkdirTemp("", "khub-candidate-")
	if err != nil {
		return "", nil, nil, nil, err
	}
	cleanup = func() { os.RemoveAll(dir) }
	// ponytail: copy the workspace (minus fsio.SkipDirs and non-regular files)
	// for an exact candidate scan; an unreadable regular file elsewhere still
	// aborts. Narrow the copy to schema paths, or use a read overlay,
	// if corpus size makes the temporary disk footprint material.
	files, dirs, err = copyCandidate(root, dir, old)
	if err != nil {
		cleanup()
		return "", nil, nil, nil, err
	}
	return dir, files, dirs, cleanup, nil
}

func copyCandidate(root, dest string, old *schema.ResolvedSchema) (map[string]FileState, map[string]bool, error) {
	files := map[string]FileState{}
	dirs := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() && (fsio.SkipDirs[d.Name()] || rel == filepath.Join(".khub", "generated")) {
			return filepath.SkipDir
		}
		to := filepath.Join(dest, rel)
		if d.IsDir() {
			dirs[rel] = true
			return os.MkdirAll(to, 0o777)
		}
		if d.Type()&os.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, to)
		}
		if !d.Type().IsRegular() {
			// Sockets, fifos and devices are not khub files, so the copy skips
			// them. A Windows junction or cloud placeholder lists here too and
			// cannot be copied as a link. Inside storage it is refused, as the
			// candidate scan refuses a symlink there; elsewhere it is skipped.
			if inStorage(old, rel) {
				return fsio.RefuseLinkEntry(d, path)
			}
			return nil
		}
		f, err := ReadFileState(root, rel)
		if err != nil {
			return err
		}
		if f == nil {
			return fs.ErrNotExist
		}
		files[rel] = *f
		return os.WriteFile(to, f.Data, 0o666)
	})
	return files, dirs, err
}

// inStorage reports whether rel lies in a storage path old declares. An
// unreadable current schema (old is nil) declares none.
func inStorage(old *schema.ResolvedSchema, rel string) bool {
	if old == nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, name := range old.Types.Keys() {
		t, _ := old.Types.Get(name)
		for _, sp := range t.StorageRoots() {
			if rel == sp || strings.HasPrefix(rel, sp+"/") {
				return true
			}
		}
	}
	return false
}

// ReadFileState reads the regular file at name (workspace-relative) through
// workspace-confined I/O: nil for a path that does not exist, an error for a
// symlink on the way or a non-regular entry.
func ReadFileState(root, name string) (*FileState, error) {
	path := filepath.Join(root, name)
	info, err := fsio.Stat(root, path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrInvalid}
	}
	data, err := fsio.ReadFile(root, path)
	if err != nil {
		return nil, err
	}
	return &FileState{data, info.Mode().Perm()}, nil
}

func upgradeDiff(root string, before map[string]FileState, dirs map[string]bool) ([]FileChange, []string, error) {
	writes := []FileChange{}
	newDirs := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if !dirs[rel] {
				newDirs = append(newDirs, rel)
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		after, err := ReadFileState(root, rel)
		if err != nil {
			return err
		}
		prev, ok := before[rel]
		if ok && bytes.Equal(prev.Data, after.Data) {
			return nil
		}
		w := FileChange{Path: rel, After: after}
		if ok {
			w.Before = &prev
			w.After.Mode = prev.Mode
		}
		writes = append(writes, w)
		return nil
	})
	// config.yaml carries the version and is always published last.
	slices.SortStableFunc(writes, func(a, b FileChange) int {
		if a.Path == filepath.Join(".khub", "config.yaml") {
			return 1
		}
		if b.Path == filepath.Join(".khub", "config.yaml") {
			return -1
		}
		return strings.Compare(a.Path, b.Path)
	})
	return writes, newDirs, err
}

// sameFileState is state equality where nil stands for an absent file.
func sameFileState(a, b *FileState) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Mode == b.Mode && bytes.Equal(a.Data, b.Data)
}

// PublishChanges applies changes in order, creating dirs first, under a
// journal in .khub/generated/<Prefix><random>/ that stages every original and
// replacement before the first byte lands. Each path is compared with its
// staged original before it is touched, so a concurrent edit aborts the
// publication; a failure restores what was applied and removes only what it
// created. The caller holds the workspace lock. Recovery that cannot be
// completed is reported under the journal's Code with the journal retained
// for manual inspection; khub makes no automatic crash-recovery guarantee.
func PublishChanges(root string, changes []FileChange, dirs []string, journal Journal) error {
	if len(changes) == 0 && len(dirs) == 0 {
		return nil
	}
	jdir := filepath.Join(root, ".khub", "generated", journal.Prefix+rand.Text())
	if err := fsio.MkdirAll(root, jdir); err != nil {
		return err
	}
	// Save both generations before publishing anything. A durable manifest maps
	// numbered blobs to workspace paths and preserves modes and absence.
	r, rel, err := fsio.OpenPath(root, jdir)
	if err != nil {
		return err
	}
	dir, err := r.Open(rel)
	r.Close()
	if err != nil {
		return err
	}
	err = dir.Chmod(0o700)
	dir.Close()
	if err != nil {
		return err
	}
	records := []any{}
	for i, w := range changes {
		rec := omap.New()
		rec.Set("path", filepath.ToSlash(w.Path))
		rec.Set("existed", w.Before != nil)
		if w.After != nil {
			rec.Set("mode", int(w.After.Mode))
		} else {
			rec.Set("removed", true)
		}
		if w.Before != nil {
			rec.Set("original_mode", int(w.Before.Mode))
			if err := fsio.WriteNewIn(root, filepath.Join(jdir, fmt.Sprintf("%d.original", i)), w.Before.Data); err != nil {
				return err
			}
		}
		if w.After != nil {
			if err := fsio.WriteNewIn(root, filepath.Join(jdir, fmt.Sprintf("%d.replacement", i)), w.After.Data); err != nil {
				return err
			}
		}
		records = append(records, rec)
	}
	manifest := omap.New()
	manifest.Set("files", records)
	dirRecords := make([]any, len(dirs))
	for i, dir := range dirs {
		dirRecords[i] = filepath.ToSlash(dir)
	}
	manifest.Set("directories", dirRecords)
	text, err := canon.DumpWide(manifest)
	if err != nil {
		return err
	}
	if err = fsio.WriteNewIn(root, filepath.Join(jdir, "manifest.yaml"), []byte(text)); err != nil {
		return err
	}
	created := []string{}
	applied := []FileChange{}
	shown := filepath.ToSlash(jdir)
	recoverFailure := func(cause error) error {
		failed := []string{}
		var located *errs.Located
		if errors.As(cause, &located) && located.Code == journal.Code {
			failed = append(failed, "concurrent workspace edit")
		}
		for i := len(applied) - 1; i >= 0; i-- {
			w := applied[i]
			current, err := ReadFileState(root, w.Path)
			if err != nil || !sameFileState(current, w.After) {
				failed = append(failed, filepath.ToSlash(w.Path))
				continue
			}
			if w.Before == nil {
				err = fsio.Remove(root, filepath.Join(root, w.Path), false)
			} else {
				err = fsio.AtomicWriteIn(root, filepath.Join(root, w.Path), w.Before.Data)
			}
			if err != nil {
				failed = append(failed, filepath.ToSlash(w.Path))
			}
		}
		for i := len(created) - 1; i >= 0; i-- {
			if err := fsio.Remove(root, filepath.Join(root, created[i]), false); err != nil {
				failed = append(failed, filepath.ToSlash(created[i]))
			}
		}
		if len(failed) > 0 {
			return errs.New(journal.Code, fmt.Sprintf("%s failed (%s); recovery incomplete at %s. Originals and replacements retained in %s", journal.Noun, cause, strings.Join(failed, ", "), shown))
		}
		if err := fsio.Remove(root, jdir, true); err != nil {
			return errs.New(journal.Code, fmt.Sprintf("%s failed (%s); recovery evidence could not be removed at %s: %s", journal.Noun, cause, shown, err))
		}
		return cause
	}
	for _, dir := range dirs {
		path := filepath.Join(root, dir)
		if _, err := fsio.Stat(root, path); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return recoverFailure(err)
		}
		if err := fsio.MkdirAll(root, path); err != nil {
			return recoverFailure(err)
		}
		created = append(created, dir)
	}
	for _, w := range changes {
		current, err := ReadFileState(root, w.Path)
		if err != nil {
			return recoverFailure(errs.New(journal.Code, fmt.Sprintf("Workspace could not be compared with its staged original at %s (%s); evidence in %s", filepath.ToSlash(w.Path), err, shown)))
		}
		if !sameFileState(current, w.Before) {
			return recoverFailure(errs.New(journal.Code, fmt.Sprintf("Workspace changed during %s at %s; staged evidence in %s", strings.ToLower(journal.Noun), filepath.ToSlash(w.Path), shown)))
		}
		switch {
		case w.After == nil:
			err = fsio.Remove(root, filepath.Join(root, w.Path), false)
		case w.Before == nil:
			err = fsio.WriteNewIn(root, filepath.Join(root, w.Path), w.After.Data)
		default:
			err = fsio.AtomicWriteIn(root, filepath.Join(root, w.Path), w.After.Data)
		}
		// A failed fsync may still have published. Inspect before deciding rollback.
		now, readErr := ReadFileState(root, w.Path)
		published := readErr == nil && (now == nil) == (w.After == nil) && (now == nil || bytes.Equal(now.Data, w.After.Data))
		if published {
			if now != nil {
				w.After.Mode = now.Mode
			}
			applied = append(applied, w)
		} else if readErr != nil || err == nil || !sameFileState(now, w.Before) {
			return recoverFailure(errs.New(journal.Code, fmt.Sprintf("Publication state changed or could not be read at %s; staged evidence in %s", filepath.ToSlash(w.Path), shown)))
		}
		if err != nil {
			return recoverFailure(err)
		}
	}
	if err := fsio.Remove(root, jdir, true); err != nil {
		return errs.New(journal.Code, fmt.Sprintf("%s committed; recovery material remains at %s: %s", journal.Noun, shown, err))
	}
	return nil
}
