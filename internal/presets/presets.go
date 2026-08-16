// Package presets ports the preset-registry half of src/khub/core/workspace.py
// (PRESETS_DIR, known_presets, resolve_preset) over an fs.FS rather than a
// directory path: the packaged tree is embedded, and `--preset-source <dir>`
// swaps in os.DirFS with identical discovery logic.
//
// A preset is a DIRECTORY: <name>/schema.yaml plus an optional
// <name>/templates/*.yaml. core.yaml (the base block) is never taken from a
// --preset-source; it always comes from the packaged tree, exactly as Python
// reads PRESETS_DIR / "core.yaml" unconditionally.
package presets

import (
	"io/fs"
	"os"
	"path"
	"sort"

	khub "github.com/endgame-build/khub"
	"github.com/endgame-build/khub/internal/errs"
)

// CoreFile is the base-block document at the root of the preset tree.
const CoreFile = "core.yaml"

// SchemaFile is the per-preset schema document, relative to the preset dir.
const SchemaFile = "schema.yaml"

// TemplatesDir is the per-preset body-template directory, relative to the
// preset dir.
const TemplatesDir = "templates"

// Embedded is the packaged preset tree — PRESETS_DIR. Paths are relative to
// the tree root ("core.yaml", "build-lite/schema.yaml", …).
func Embedded() fs.FS {
	sub, err := fs.Sub(khub.PresetsData, "presets")
	if err != nil {
		// Unreachable: the embed directive pins the prefix at build time.
		panic(err)
	}
	return sub
}

// DirSource is the --preset-source adapter: the same registry logic over a
// directory on disk. A missing directory is not an error here — it simply
// resolves no presets, matching pathlib's glob over a nonexistent path.
func DirSource(dir string) fs.FS { return os.DirFS(dir) }

// Source picks the tree a call should read: the packaged presets when dir is
// empty (Python's `source or PRESETS_DIR`), the directory otherwise.
func Source(dir string) fs.FS {
	if dir == "" {
		return Embedded()
	}
	return DirSource(dir)
}

// Known is known_presets: the preset names resolvable from source, sorted.
// Discovery is the same glob Python runs — "*/schema.yaml", parent name.
func Known(source fs.FS) []string {
	matches, err := fs.Glob(source, "*/"+SchemaFile)
	if err != nil {
		return nil // only a malformed pattern reaches here; ours is a literal
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, path.Dir(m))
	}
	sort.Strings(names)
	return names
}

// Resolve is resolve_preset: the FS-relative path of <name>/schema.yaml, or
// the unknown_preset error listing what source does offer.
func Resolve(name string, source fs.FS) (string, error) {
	candidate := path.Join(name, SchemaFile)
	if _, err := fs.Stat(source, candidate); err != nil {
		return "", errs.UnknownPreset(name, Known(source))
	}
	return candidate, nil
}

// HasTemplates is `preset_templates.is_dir()`: whether the preset ships a
// templates/ directory at all. init creates .khub/templates/ on that answer
// alone, so an empty directory still produces an empty workspace copy.
func HasTemplates(name string, source fs.FS) bool {
	fi, err := fs.Stat(source, path.Join(name, TemplatesDir))
	return err == nil && fi.IsDir()
}

// Templates lists a preset's template files, sorted by name, as FS-relative
// paths — `sorted(preset_templates.glob("*.yaml"))`. A preset shipping no
// templates/ directory yields none.
func Templates(name string, source fs.FS) []string {
	if !HasTemplates(name, source) {
		return nil
	}
	matches, err := fs.Glob(source, path.Join(name, TemplatesDir, "*.yaml"))
	if err != nil {
		return nil
	}
	sort.Strings(matches)
	return matches
}
