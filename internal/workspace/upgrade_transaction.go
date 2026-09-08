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
	"github.com/endgame-build/khub/internal/template"
)

type upgradeFile struct {
	data []byte
	mode fs.FileMode
}
type upgradeWrite struct {
	name   string
	before *upgradeFile
	after  upgradeFile
}

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
	candidate, err := os.MkdirTemp("", "khub-upgrade-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(candidate)
	// ponytail: copy the workspace (minus fsio.SkipDirs and non-regular files)
	// for an exact candidate scan; an unreadable regular file elsewhere still
	// aborts. Narrow the copy to schema paths, or use a read overlay,
	// if corpus size makes the temporary disk footprint material.
	before, dirs, err := copyUpgradeWorkspace(root, candidate)
	if err != nil {
		return nil, err
	}
	result, err := upgradeCandidate(candidate, opt, source)
	if err != nil {
		return nil, err
	}
	result.Path = osPath(pyPath(filepath.ToSlash(root)))
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
		if _, err := readUpgradeFile(root, w.name); err != nil {
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
	if err := publishUpgrade(root, writes, newDirs); err != nil {
		return nil, err
	}
	if opt.Tails != nil {
		opt.Tails(root)
	}
	return result, nil
}

func copyUpgradeWorkspace(root, dest string) (map[string]upgradeFile, map[string]bool, error) {
	files := map[string]upgradeFile{}
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
			// Sockets, fifos and devices are not khub files; readUpgradeFile
			// still refuses one at any path the upgrade writes.
			return nil
		}
		f, err := readUpgradeFile(root, rel)
		if err != nil {
			return err
		}
		if f == nil {
			return fs.ErrNotExist
		}
		files[rel] = *f
		return os.WriteFile(to, f.data, 0o666)
	})
	return files, dirs, err
}
func readUpgradeFile(root, name string) (*upgradeFile, error) {
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
	return &upgradeFile{data, info.Mode().Perm()}, nil
}
func upgradeDiff(root string, before map[string]upgradeFile, dirs map[string]bool) ([]upgradeWrite, []string, error) {
	writes := []upgradeWrite{}
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
		after, err := readUpgradeFile(root, rel)
		if err != nil {
			return err
		}
		prev, ok := before[rel]
		if ok && bytes.Equal(prev.data, after.data) {
			return nil
		}
		w := upgradeWrite{name: rel, after: *after}
		if ok {
			w.before = &prev
			w.after.mode = prev.mode
		}
		writes = append(writes, w)
		return nil
	})
	// config.yaml carries the version and is always published last.
	slices.SortStableFunc(writes, func(a, b upgradeWrite) int {
		if a.name == filepath.Join(".khub", "config.yaml") {
			return 1
		}
		if b.name == filepath.Join(".khub", "config.yaml") {
			return -1
		}
		return strings.Compare(a.name, b.name)
	})
	return writes, newDirs, err
}
func sameUpgradeFile(a, b *upgradeFile) bool {
	return a == nil && b == nil || a != nil && b != nil && a.mode == b.mode && bytes.Equal(a.data, b.data)
}

func publishUpgrade(root string, writes []upgradeWrite, dirs []string) error {
	if len(writes) == 0 && len(dirs) == 0 {
		return nil
	}
	journal := filepath.Join(root, ".khub", "generated", "upgrade-"+rand.Text())
	if err := fsio.MkdirAll(root, journal); err != nil {
		return err
	}
	// Save both generations before publishing anything. A durable manifest maps
	// numbered blobs to workspace paths and preserves modes and absence.
	r, rel, err := fsio.OpenPath(root, journal)
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
	for i, w := range writes {
		rec := omap.New()
		rec.Set("path", w.name)
		rec.Set("existed", w.before != nil)
		rec.Set("mode", int(w.after.mode))
		if w.before != nil {
			rec.Set("original_mode", int(w.before.mode))
			if err := fsio.WriteNewIn(root, filepath.Join(journal, fmt.Sprintf("%d.original", i)), w.before.data); err != nil {
				return err
			}
		}
		if err := fsio.WriteNewIn(root, filepath.Join(journal, fmt.Sprintf("%d.replacement", i)), w.after.data); err != nil {
			return err
		}
		records = append(records, rec)
	}
	manifest := omap.New()
	manifest.Set("files", records)
	dirRecords := make([]any, len(dirs))
	for i, dir := range dirs {
		dirRecords[i] = dir
	}
	manifest.Set("directories", dirRecords)
	text, err := canon.DumpWide(manifest)
	if err != nil {
		return err
	}
	if err = fsio.WriteNewIn(root, filepath.Join(journal, "manifest.yaml"), []byte(text)); err != nil {
		return err
	}
	created := []string{}
	applied := []upgradeWrite{}
	recoverFailure := func(cause error) error {
		failed := []string{}
		var located *errs.Located
		if errors.As(cause, &located) && located.Code == "upgrade_recovery_failed" {
			failed = append(failed, "concurrent workspace edit")
		}
		for i := len(applied) - 1; i >= 0; i-- {
			w := applied[i]
			current, err := readUpgradeFile(root, w.name)
			if err != nil || !sameUpgradeFile(current, &w.after) {
				failed = append(failed, w.name)
				continue
			}
			if w.before == nil {
				err = fsio.Remove(root, filepath.Join(root, w.name), false)
			} else {
				err = fsio.AtomicWriteIn(root, filepath.Join(root, w.name), w.before.data)
			}
			if err != nil {
				failed = append(failed, w.name)
			}
		}
		for i := len(created) - 1; i >= 0; i-- {
			if err := fsio.Remove(root, filepath.Join(root, created[i]), false); err != nil {
				failed = append(failed, created[i])
			}
		}
		if len(failed) > 0 {
			return errs.New("upgrade_recovery_failed", fmt.Sprintf("Upgrade failed (%s); recovery incomplete at %s. Originals and replacements retained in %s", cause, strings.Join(failed, ", "), journal))
		}
		if err := fsio.Remove(root, journal, true); err != nil {
			return errs.New("upgrade_recovery_failed", fmt.Sprintf("Upgrade failed (%s); recovery evidence could not be removed at %s: %s", cause, journal, err))
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
	for _, w := range writes {
		current, err := readUpgradeFile(root, w.name)
		if err != nil {
			return recoverFailure(errs.New("upgrade_recovery_failed", fmt.Sprintf("Workspace could not be compared with its staged original at %s (%s); evidence in %s", w.name, err, journal)))
		}
		if !sameUpgradeFile(current, w.before) {
			return recoverFailure(errs.New("upgrade_recovery_failed", fmt.Sprintf("Workspace changed during upgrade at %s; staged evidence in %s", w.name, journal)))
		}
		if w.before == nil {
			err = fsio.WriteNewIn(root, filepath.Join(root, w.name), w.after.data)
		} else {
			err = fsio.AtomicWriteIn(root, filepath.Join(root, w.name), w.after.data)
		}
		// A failed fsync may still have published. Inspect before deciding rollback.
		now, readErr := readUpgradeFile(root, w.name)
		published := readErr == nil && now != nil && bytes.Equal(now.data, w.after.data)
		if published {
			w.after.mode = now.mode
			applied = append(applied, w)
		} else if readErr != nil || err == nil || !sameUpgradeFile(now, w.before) {
			return recoverFailure(errs.New("upgrade_recovery_failed", fmt.Sprintf("Publication state changed or could not be read at %s; staged evidence in %s", w.name, journal)))
		}
		if err != nil {
			return recoverFailure(err)
		}
	}
	if err := fsio.Remove(root, journal, true); err != nil {
		return errs.New("upgrade_recovery_failed", fmt.Sprintf("Upgrade committed; recovery material remains at %s: %s", journal, err))
	}
	return nil
}
