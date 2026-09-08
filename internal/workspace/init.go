// Ports src/khub/core/workspace.py — the scaffolder (WPK-001-1).
//
// Init scaffolds a workspace from a named preset (a DIRECTORY:
// <name>/ontology.yaml plus optional policy.yaml / storage.yaml /
// templates/*.yaml) into editable .khub/{ontology,policy,storage}.yaml (+
// .khub/templates/), stamps provenance, writes config.yaml, gitignores
// .khub/generated/, and lays down the entity tree — including CREATING each md
// singleton that has a template and does not exist yet. It never MODIFIES an
// existing entity file (WS-003, amended for singletons: may create, never
// overwrite), so one command green-fields a fresh workspace and force-seeds
// over a live corpus.
//
// The YAML written here uses the WIDE dump profile (canon.DumpWide, width
// 4096, insertion order) — workspace.py's own YAML instance, not formats.py's
// round-trip one. That choice is byte-visible in every scaffolded schema.

package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/presets"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/template"
)

// InitResult is the outcome of a scaffold: resolved provenance and the cutover
// guarantee. Field order is the dataclass order the CLI's JSON payload
// preserves (path, preset, version, name, source, entity_files_modified,
// seeded_over_corpus, singletons_created, preserved).
type InitResult struct {
	Path    string
	Preset  string
	Version string
	Name    string
	// Source is str(preset_source) or "" for Python's None (JSON null).
	Source string
	// EntityFilesModified is measured, not assumed: how many pre-existing
	// entity files init changed or removed. The cutover guarantee (AC-004) is
	// that this is 0; a non-zero value is a loud signal the non-destructive
	// guarantee was violated.
	EntityFilesModified int
	SeededOverCorpus    bool
	// SingletonsCreated names the singleton types this run minted — creations,
	// never overwrites.
	SingletonsCreated []string
	// Preserved lists workspace-owned files a re-init left alone
	// (.khub/{ontology,policy,storage}.yaml, .khub/config.yaml,
	// .khub/templates/*.yaml). The engagement owns these outright — editing
	// them IS the override mechanism — so a re-scaffold reports them instead
	// of silently restoring the preset's copy.
	Preserved []string
}

// InitOptions carries init_workspace's keyword arguments. PresetSource "" is
// Python's None (the packaged presets); Name "" falls back to the target
// directory name.
type InitOptions struct {
	PresetSource string
	Name         string
	Force        bool
	// Tails runs after a successful scaffold, under the workspace lock, so the
	// wire and index tails see the tree the scaffold committed and nothing a
	// concurrent writer slipped in between. The callee must use the Held
	// variants (wire.WireHeld, reindex.ReindexHeld): fsio.Locked is not
	// re-entrant. Tails are non-fatal; the hook returns nothing.
	Tails func(root string)
}

// The gitignore line init appends: the runtime collection locks live there.
const generatedIgnore = ".khub/generated/"

// Init is init_workspace: scaffold a workspace at path from preset.
//
// Guards run before any write, in order — unknown preset, preset mismatch
// against an existing workspace, non-empty target without Force. On failure
// the .khub/ it created and the singletons it minted are removed; directories
// and the .gitignore line may remain (harmless, idempotent on retry).
func Init(preset, path string, opt InitOptions) (*InitResult, error) {
	tails := func() (struct{}, error) {
		if opt.Tails != nil {
			opt.Tails(path)
		}
		return struct{}{}, nil
	}
	if _, err := os.Lstat(filepath.Join(path, ".khub")); err == nil {
		// Re-init over a workspace: one lock covers the scaffold and its tails.
		return fsio.Locked(path, func() (*InitResult, error) {
			result, err := initWorkspace(preset, path, opt)
			if err == nil {
				_, err = tails()
			}
			return result, err
		})
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// A fresh scaffold has no lock to hold (there is no .khub/ yet); the tails
	// take one once it exists, so they at least serialize against each other.
	result, err := initWorkspace(preset, path, opt)
	if err != nil {
		return nil, err
	}
	if _, err := fsio.Locked(path, tails); err != nil {
		return nil, err
	}
	return result, nil
}
func initWorkspace(preset, path string, opt InitOptions) (*InitResult, error) {
	// str(Path(...)) at the boundary: PresetSource is echoed into config.yaml
	// and InitResult.Source, so it carries pathlib's spelling, not the caller's.
	if opt.PresetSource != "" {
		opt.PresetSource = osPath(pyPath(filepath.ToSlash(opt.PresetSource)))
	}
	source := presets.Source(opt.PresetSource)
	// A retired name resolves to the directory that replaced it, and the
	// workspace records the canonical one.
	preset = presets.Canonical(preset, source)
	schemaPath, err := presets.Resolve(preset, source)
	if err != nil {
		return nil, err
	}
	target := pyPath(filepath.ToSlash(path))

	// Since 0.11.0 a re-init preserves the workspace's schema, so scaffolding a
	// DIFFERENT preset over it would lay down directories and singletons for
	// types the active schema does not declare — orphan files no verb can see.
	if existing, ok := existingPreset(target); ok && presets.Canonical(existing, source) != preset {
		return nil, errs.New("preset_mismatch", fmt.Sprintf(
			"%s is a '%s' workspace; refusing to scaffold '%s' over it. "+
				"Its .khub schema files are workspace-owned and would be kept, leaving files for "+
				"types the schema does not declare.", osPath(target), existing, preset))
	}
	empty, err := isEmptyDir(target)
	if err != nil {
		return nil, err
	}
	if !empty && !opt.Force {
		return nil, errs.TargetNotEmpty(osPath(target))
	}

	// Snapshot pre-existing entity files so the cutover guarantee is measured,
	// not assumed: a force-seed over a real corpus (pre-existing .md, not just
	// a prior .khub/ from a re-init) must modify zero of them.
	before, err := entityHashes(target)
	if err != nil {
		return nil, err
	}
	seededOverCorpus := len(before) > 0

	merged, err := flatten(preset, source, schemaPath)
	if err != nil {
		return nil, err
	}

	khubDir := pyJoin(target, ".khub")
	createdKhub := !pathExists(khubDir)
	var createdSingletons []singleton
	var writtenKhub []string
	wsName, preserved, err := scaffold(target, khubDir, preset, merged, source, opt, &createdSingletons, &writtenKhub)
	if err != nil {
		// Best-effort unwind (not full atomicity — dirs/.gitignore may remain):
		// drop the partial .khub/ we just created, any singleton files this run
		// minted outside it, and — on a re-init, where .khub/ predates us — the
		// individual files this run wrote into it, so a failed re-init leaves
		// the workspace's schema exactly as it was.
		for _, s := range createdSingletons {
			_ = fsio.Remove(osPath(target), osPath(s.path), false)
		}
		cleanupInitFiles(osPath(target), osPath(khubDir), writtenKhub, createdKhub)
		return nil, err
	}

	after, err := entityHashes(target)
	if err != nil {
		return nil, err
	}
	modified := 0
	for rel, digest := range before {
		if after[rel] != digest {
			modified++
		}
	}

	created := make([]string, 0, len(createdSingletons))
	for _, s := range createdSingletons {
		created = append(created, s.typeName)
	}
	return &InitResult{
		Path:                osPath(target),
		Preset:              preset,
		Version:             merged.Version,
		Name:                wsName,
		Source:              opt.PresetSource,
		EntityFilesModified: modified,
		SeededOverCorpus:    seededOverCorpus,
		SingletonsCreated:   created,
		Preserved:           preserved,
	}, nil
}

// Cleanup never recursively removes .khub: a writer may have created a stable
// lock or another file after fresh initialization began. Only empty dirs go.
func cleanupInitFiles(root, khubDir string, written []string, createdKhub bool) {
	for _, p := range written {
		_ = fsio.Remove(root, osPath(p), false)
	}
	if !createdKhub {
		return
	}
	dirs := map[string]bool{khubDir: true, filepath.Join(khubDir, "templates"): true}
	for _, p := range written {
		for d := filepath.Dir(osPath(p)); d != khubDir && d != filepath.Dir(d); d = filepath.Dir(d) {
			dirs[d] = true
		}
	}
	ordered := make([]string, 0, len(dirs))
	for d := range dirs {
		ordered = append(ordered, d)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, d := range ordered {
		_ = fsio.Remove(root, d, false)
	}
}

// singleton is one md singleton this run minted, kept so a mid-init failure
// can roll the creation back.
type singleton struct {
	typeName string
	path     string
}

// scaffold is the body of init_workspace's try block: every write, in order.
// Anything it returns an error from is unwound by the caller; written collects
// the .khub files THIS run created so a re-init failure can remove exactly
// them.
func scaffold(
	target, khubDir, preset string,
	merged *flattened,
	source fs.FS,
	opt InitOptions,
	created *[]singleton,
	written *[]string,
) (string, []string, error) {
	if err := os.MkdirAll(osPath(target), 0o777); err != nil {
		return "", nil, err
	}
	if err := fsio.MkdirAll(osPath(target), osPath(khubDir)); err != nil {
		return "", nil, err
	}
	preserved := []string{}

	// Creations only, the same rule entity files and singletons already follow:
	// the workspace owns its schema layer files, so a re-init must not restore
	// the preset over local edits. Refreshing from a newer preset is an
	// upgrade, not a scaffold. Each file carries the provenance header; the
	// base block is embedded in the binary and never written here.
	for _, layer := range merged.layers() {
		layerPath := pyJoin(khubDir, layer.File)
		if pathExists(layerPath) {
			preserved = append(preserved, ".khub/"+layer.File)
			continue
		}
		text, err := layerBytes(preset, merged.Version, layer.Doc)
		if err != nil {
			return "", nil, err
		}
		if err := fsio.WriteNewIn(osPath(target), osPath(layerPath), []byte(text)); err != nil {
			return "", nil, err
		}
		*written = append(*written, layerPath)
	}

	wsName := opt.Name
	if wsName == "" {
		wsName = resolvedName(target)
	}
	config := omap.New()
	config.Set("name", wsName)
	config.Set("preset", preset)
	config.Set("version", merged.Version)
	if opt.PresetSource == "" {
		config.Set("source", nil)
	} else {
		config.Set("source", opt.PresetSource)
	}
	// stale_days drives the status/query staleness flag; no `format` default is
	// written — nothing reads it (format is a per-type schema facet).
	defaults := omap.New()
	defaults.Set("stale_days", int64(DefaultStaleDays))
	config.Set("defaults", defaults)

	configPath := pyJoin(khubDir, "config.yaml")
	if pathExists(configPath) {
		preserved = append(preserved, ".khub/config.yaml") // carries edited defaults (stale_days)
	} else {
		body, err := canon.DumpWide(config)
		if err != nil {
			return "", nil, err
		}
		if err := fsio.WriteNewIn(osPath(target), osPath(configPath), []byte(body)); err != nil {
			return "", nil, err
		}
		*written = append(*written, configPath)
	}

	if err := appendGitignore(pyJoin(target, ".gitignore"), generatedIgnore); err != nil {
		return "", nil, err
	}

	// Flatten the preset's templates (if any) into the workspace-owned copy —
	// the same editable-copy relationship the three layer files have with the
	// preset.
	// The directory is created on `is_dir()` alone, so a preset shipping an
	// empty templates/ still lands an empty .khub/templates/.
	if presets.HasTemplates(preset, source) {
		tplDir := pyJoin(khubDir, "templates")
		if err := fsio.MkdirAll(osPath(target), osPath(tplDir)); err != nil {
			return "", nil, err
		}
		for _, tpl := range presets.Templates(preset, source) {
			base := pyName(tpl)
			dest := pyJoin(tplDir, base)
			if pathExists(dest) {
				preserved = append(preserved, ".khub/templates/"+base)
				continue
			}
			payload, err := fs.ReadFile(source, tpl)
			if err != nil {
				return "", nil, err
			}
			if err := fsio.WriteNewIn(osPath(target), osPath(dest), payload); err != nil {
				return "", nil, err
			}
			*written = append(*written, dest)
		}
	}

	// Resolve the workspace that now exists on disk — written and preserved
	// layer files alike, over the embedded base — and drive the tree and
	// singleton passes off the RESOLVED types. The scaffold then matches what
	// every later command scans by construction rather than by a hand-kept
	// mirror of the resolver's defaults; and a schema that cannot resolve (a
	// re-init preserving one generation's ontology beside another's storage)
	// fails HERE, loudly, instead of minting a green init over a workspace no
	// command can load.
	resolved, err := introspect.LoadSchema(osPath(target))
	if err != nil {
		return "", nil, err
	}
	if err := provision(target, resolved, created); err != nil {
		return "", nil, err
	}
	return wsName, preserved, nil
}

// layerDoc pairs one workspace layer file with the flattened document behind
// it.
type layerDoc struct {
	File string
	Doc  *omap.Map
}

// layers lists the three layer files in write order — the order init writes
// them, a re-init reports them preserved, and upgrade compares them.
func (f *flattened) layers() []layerDoc {
	return []layerDoc{
		{"ontology.yaml", f.Ontology},
		{"policy.yaml", f.Policy},
		{"storage.yaml", f.Storage},
	}
}

// layerHeaderPrefix opens every scaffolded layer file; the rest of the line
// is `<preset>@<version>`.
const layerHeaderPrefix = "# khub-preset: "

// layerBytes renders one layer file exactly as init writes it: the provenance
// header over the wide dump. Upgrade builds its incoming bytes through this
// same function, so the two verbs cannot disagree about what a shipped file
// looks like — an upgrade over a fresh init reports nothing to do.
func layerBytes(preset, version string, doc *omap.Map) (string, error) {
	body, err := canon.DumpWide(doc)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s@%s\n", layerHeaderPrefix, preset, version) + body, nil
}

// provision is scaffold's tail, shared with upgrade: one directory per type
// and each templated md singleton that is missing, both driven off the
// RESOLVED schema. That sharing is the point of the split — a type the
// ontology gained after the workspace was created still gets its directory.
func provision(target string, resolved *schema.ResolvedSchema, created *[]singleton) error {
	if err := layDownTree(target, resolved); err != nil {
		return err
	}
	return createSingletons(target, resolved, created)
}

// layDownTree creates one directory per type's storage path. File and folder
// layouts need the dir; a collection's or singleton's path names a FILE —
// create only its parent, never the file: a missing collection or singleton is
// legitimately zero entities.
//
// The pass reads the RESOLVED schema, so layouts, defaulted paths and the
// collection {type}.{format} fallback are the resolver's own — the scaffolded
// tree matches what every later command scans by construction.
func layDownTree(target string, resolved *schema.ResolvedSchema) error {
	for _, typeName := range resolved.Types.Keys() {
		rt, _ := resolved.Types.Get(typeName)
		switch rt.Storage.Layout {
		case schema.LayoutCollection, schema.LayoutSingleton:
			rel := rt.CollectionRelpath()
			if rt.Storage.Layout == schema.LayoutSingleton {
				rel = *rt.Storage.Path // the matrix guarantees a singleton's path
			}
			cpath := pyJoin(target, filepath.ToSlash(rel))
			if parent := pyParent(cpath); parent != target {
				if err := fsio.MkdirAll(osPath(target), osPath(parent)); err != nil {
					return err
				}
			}
		default:
			// file/folder: storageDefaults guarantees a non-empty path.
			if err := fsio.MkdirAll(osPath(target), osPath(pyJoin(target, *rt.Storage.Path))); err != nil {
				return err
			}
		}
	}
	return nil
}

// createSingletons creates each md singleton that has a template and does not
// exist yet — frontmatter + scaffolded body. Creations only: an existing file
// is never touched (WS-003 as amended). It appends to created as it goes so
// the caller can roll creations back on failure.
func createSingletons(target string, resolved *schema.ResolvedSchema, created *[]singleton) error {
	today := today()
	for _, name := range resolved.Types.Keys() {
		rt, _ := resolved.Types.Get(name)
		if rt.Storage.Layout != schema.LayoutSingleton {
			continue
		}
		spath := pyJoin(target, *rt.Storage.Path)
		if pySuffix(pyName(spath)) != ".md" || pathExists(spath) {
			continue
		}
		// The resolver's own template link: the declared stem, the type's name
		// by convention, or "" for the `template: false` opt-out.
		stem := rt.TemplateName()
		if stem == "" {
			continue
		}
		tpl, err := template.LoadTemplate(target, stem, rt.FieldNames())
		if err != nil {
			return err
		}
		if tpl == nil {
			continue
		}
		title := tpl.Title
		if title == "" {
			title = name
		}
		meta := omap.New()
		meta.Set("type", name)
		meta.Set("created", canon.Date{ISO: today})
		meta.Set("updated", canon.Date{ISO: today})
		meta.Set("draft", false)
		meta.Set("title", title)
		text, err := canon.Render(meta, tpl.Render(), "md")
		if err != nil {
			return err
		}
		if err := fsio.MkdirAll(osPath(target), osPath(pyParent(spath))); err != nil {
			return err
		}
		if err := fsio.WriteNewIn(osPath(target), osPath(spath), []byte(text)); err != nil {
			return err
		}
		*created = append(*created, singleton{typeName: name, path: spath})
	}
	return nil
}

// existingPreset is _existing_preset: the preset a workspace was scaffolded
// from. An unreadable or preset-less config must not block a re-init, so every
// failure reads as "not a workspace yet".
func existingPreset(target string) (string, bool) {
	configPath := pyJoin(target, ".khub", "config.yaml")
	fi, err := fsio.Stat(osPath(target), osPath(configPath))
	if err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	raw, err := fsio.ReadFile(osPath(target), osPath(configPath))
	if err != nil {
		return "", false
	}
	v, err := canon.LoadDocMode(string(raw), canon.Mode12)
	if err != nil {
		return "", false
	}
	data, ok := v.(*omap.Map)
	if !ok {
		return "", false
	}
	value, ok := data.Get("preset")
	if !ok || pyFalsy(value) {
		return "", false
	}
	return pyScalarString(value), true
}

// isEmptyDir answers `not (target.exists() and any(target.iterdir()))`: a
// missing target is empty; a target that is not a directory propagates the OS
// error, as Python's NotADirectoryError does.
func isEmptyDir(target string) (bool, error) {
	entries, err := os.ReadDir(osPath(target))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}

// Entity-capable suffixes (canon.PerItem + .jsonl ahead of collections).
// Vendor/VCS trees are skipped — hashing every package.json in a node_modules
// would turn the cutover snapshot into a full-tree sweep.
var (
	entitySuffixes = []string{".json", ".md", ".yaml", ".jsonl"}
	skipParts      = map[string]bool{".khub": true, ".git": true, ".venv": true, "node_modules": true}
)

// indexName is the generated OKF index at the workspace root
// (reindex.IndexName; not imported — reindex depends on entity, which depends
// on this package). It is a projection, never an entity: the CLI writes it as
// an init tail, so a re-init must not count it as a corpus seeded over.
const indexName = "index.md"

// entityHashes is _entity_hashes: content hashes of entity-suffixed files,
// keyed by relpath. The root index.md is skipped (see indexName).
//
// The skip test runs against the FULL path's components, target's own
// included — a workspace living under a directory named .git or node_modules
// hashes nothing, exactly as Python's `_SKIP_PARTS.intersection(p.parts)`
// behaves. Everything else is pruned during the walk, which is equivalent
// because any file below a skipped directory carries that component.
func entityHashes(target string) (map[string]string, error) {
	if !pathExists(target) {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	for _, part := range pyParts(target) {
		if skipParts[part] {
			return out, nil
		}
	}
	root := osPath(target)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if p != root && skipParts[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		// is_file(): rglob also matches directories named *.md (real corpora
		// have them), and symlinks resolve through Stat.
		if !d.Type().IsRegular() {
			fi, serr := os.Stat(p)
			if serr != nil || !fi.Mode().IsRegular() {
				return nil //nolint:nilerr // a broken link is not an entity file
			}
		}
		if !hasEntitySuffix(d.Name()) {
			return nil
		}
		if d.Name() == indexName && filepath.Dir(p) == root {
			return nil
		}
		payload, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		sum := sha256.Sum256(payload)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func hasEntitySuffix(name string) bool {
	for _, ext := range entitySuffixes {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

// appendGitignore is _append_gitignore: append one line unless it is already
// present, preserving the file's trailing-newline shape.
func appendGitignore(gitignore, line string) error {
	existing := ""
	if b, err := fsio.ReadFile(filepath.Dir(osPath(gitignore)), osPath(gitignore)); err == nil {
		existing = string(b)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, l := range strings.Split(existing, "\n") {
		if strings.TrimSuffix(l, "\r") == line {
			return nil
		}
	}
	sep := ""
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		sep = "\n"
	}
	return fsio.AtomicWriteIn(filepath.Dir(osPath(gitignore)), osPath(gitignore), []byte(existing+sep+line+"\n"))
}

// resolvedName is `target.resolve().name or "workspace"`: the target's own
// directory name, absolute-resolved so "." names the cwd rather than nothing.
func resolvedName(target string) string {
	abs, err := filepath.Abs(osPath(target))
	if err != nil {
		return "workspace"
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = resolved
	}
	name := pyName(filepath.ToSlash(abs))
	if name == "" {
		return "workspace"
	}
	return name
}

// today is date.today() through the parity clock seam: KHUB_PARITY_NOW freezes
// it when the recorder sets it, and only then.
func today() string {
	if frozen := os.Getenv("KHUB_PARITY_NOW"); frozen != "" {
		return frozen
	}
	return time.Now().Format("2006-01-02")
}

// pathExists is Path.exists(): symlinks are followed, so a dangling link
// reads as absent exactly as it does in Python.
func pathExists(path string) bool {
	_, err := os.Stat(osPath(path))
	return err == nil
}

// --- pathlib semantics --------------------------------------------------------
//
// Paths are carried in slash form and converted at every OS call. filepath.Clean
// is deliberately not used: it collapses "..", which pathlib keeps, and the
// difference is observable in InitResult.Path and the target_not_empty message.

func osPath(p string) string { return filepath.FromSlash(p) }

// pyPath is str(PurePosixPath(p)): empty and "." components dropped, ".."
// kept, trailing slash removed; "" and "." both render ".".
func pyPath(p string) string {
	abs := strings.HasPrefix(p, "/")
	parts := pySplit(p)
	if len(parts) == 0 {
		if abs {
			return "/"
		}
		return "."
	}
	joined := strings.Join(parts, "/")
	if abs {
		return "/" + joined
	}
	return joined
}

// pyJoin is str(PurePosixPath(base).joinpath(rest...)): an absolute component
// resets the path, exactly as pathlib's / operator does.
func pyJoin(base string, rest ...string) string {
	out := base
	for _, r := range rest {
		if strings.HasPrefix(r, "/") {
			out = r
			continue
		}
		out += "/" + r
	}
	return pyPath(out)
}

// pyParent is str(PurePosixPath(p).parent).
func pyParent(p string) string {
	abs := strings.HasPrefix(p, "/")
	parts := pySplit(p)
	if len(parts) == 0 {
		if abs {
			return "/"
		}
		return "."
	}
	parts = parts[:len(parts)-1]
	if len(parts) == 0 {
		if abs {
			return "/"
		}
		return "."
	}
	joined := strings.Join(parts, "/")
	if abs {
		return "/" + joined
	}
	return joined
}

// pyName is PurePosixPath(p).name — "" for "." and "/".
func pyName(p string) string {
	parts := pySplit(p)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// pyParts is PurePosixPath(p).parts, minus the "/" root element (nothing in
// _SKIP_PARTS can equal it).
func pyParts(p string) []string { return pySplit(p) }

// pySuffix is PurePosixPath(name).suffix: "" when the dot leads the name or
// ends it.
func pySuffix(name string) string {
	i := strings.LastIndex(name, ".")
	if i > 0 && i < len(name)-1 {
		return name[i:]
	}
	return ""
}

func pySplit(p string) []string {
	parts := []string{}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." {
			continue
		}
		parts = append(parts, seg)
	}
	return parts
}
