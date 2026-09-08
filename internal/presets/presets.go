// Package presets ports the preset-registry half of src/khub/core/workspace.py
// (PRESETS_DIR, known_presets, resolve_preset) over an fs.FS rather than a
// directory path: the packaged tree is embedded, and `--preset-source <dir>`
// swaps in os.DirFS with identical discovery logic.
//
// A preset is a DIRECTORY: <name>/ontology.yaml (the domain model, which also
// declares the preset), optional <name>/policy.yaml and <name>/storage.yaml,
// and an optional <name>/templates/*.yaml. core/ (the base block) is never
// taken from a --preset-source and is never an installable preset; it always
// comes from the packaged tree, supplied to every resolve by LoadSchema.
package presets

import (
	"io/fs"
	"os"
	"path"
	"sort"

	khub "github.com/endgame-build/khub"
	"github.com/endgame-build/khub/internal/errs"
)

// coreDir is the reserved directory holding the base block. It matches the
// same */ontology.yaml glob the presets do, so discovery must skip it by name
// — otherwise the base block would list as an installable preset.
const coreDir = "core"

// OntologyFile is the per-preset domain model, relative to the preset dir. Its
// presence is what makes a directory a preset, and it carries the preset
// `version:`.
const OntologyFile = "ontology.yaml"

// CorePath is the base-block document in the preset tree — embedded in the
// binary and supplied to every resolve, never copied into a workspace. It is
// a path, not a file name, and is built from the two constants rather than
// spelled out, so the reserved directory is named in exactly one place. The
// separator is a literal "/" because this addresses an io/fs tree, where the
// separator is always "/" whatever the host. `//go:embed` in embed.go cannot
// read it (patterns are literals), so that file spells `presets/core/` itself.
const CorePath = coreDir + "/" + OntologyFile

// PolicyFile and StorageFile are the per-preset gate and storage layers,
// relative to the preset dir. Both optional: absent reads as empty.
const (
	PolicyFile  = "policy.yaml"
	StorageFile = "storage.yaml"
)

// TemplatesDir is the per-preset body-template directory, relative to the
// preset dir.
const TemplatesDir = "templates"

// aliases maps a retired preset name to the directory that replaced it.
// build-lite became build-hub in 0.6.0, when the two build presets merged:
// kb's graduation command (`khub init build-lite`) and every workspace whose
// config.yaml still says build-lite resolve through here. Known() never lists
// an alias — it is a name that resolves, not a preset anyone is offered.
var aliases = map[string]string{"build-lite": "build-hub"}

// Canonical is the directory name a preset name resolves to in source: the
// name itself unless it is a retired alias AND the replacement exists there.
// A --preset-source carrying its own build-lite/ keeps that name — the alias
// exists for the packaged tree, not to shadow a directory a user wrote. init
// records the canonical name in config.yaml and upgrade restamps it there, so
// a workspace carries the canonical name after either.
func Canonical(name string, source fs.FS) string {
	canonical, ok := aliases[name]
	if !ok {
		return name
	}
	if _, err := fs.Stat(source, path.Join(canonical, OntologyFile)); err != nil {
		return name
	}
	return canonical
}

// Embedded is the packaged preset tree — PRESETS_DIR. Paths are relative to
// the tree root ("core/ontology.yaml", "build-hub/ontology.yaml", …).
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
// Discovery globs "*/ontology.yaml" and takes the parent name, skipping the
// reserved core directory — the base block matches the same glob but is not a
// preset anyone can init from.
func Known(source fs.FS) []string {
	matches, err := fs.Glob(source, "*/"+OntologyFile)
	if err != nil {
		return nil // only a malformed pattern reaches here; ours is a literal
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		if name := path.Dir(m); name != coreDir {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Resolve is resolve_preset: the FS-relative path of <name>/ontology.yaml, or
// the unknown_preset error listing what source does offer. The reserved core
// directory is not resolvable — it is the base block, not a preset.
func Resolve(name string, source fs.FS) (string, error) {
	name = Canonical(name, source)
	// The reserved name first: core/ontology.yaml exists in the tree, so the
	// stat would succeed — its result cannot matter here.
	if name == coreDir {
		return "", errs.UnknownPreset(name, Known(source))
	}
	candidate := path.Join(name, OntologyFile)
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
