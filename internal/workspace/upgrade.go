// upgrade.go ports kb's cmd_upgrade / _replace_config / _schema_drift into
// the workspace core: bring an existing workspace up to the khub binary now
// on PATH.
//
// Deliberately not a re-init. Init never writes over a .khub/ file the
// workspace already owns — editing it IS the override mechanism. Upgrade must,
// because provision reads the workspace's own layer files and templates:
// leave old files in place and a type — or a section — shipped in a new
// preset can never reach an existing workspace, which makes the verb an
// upgrade in name only. So .khub/ goes FIRST: replace, re-read, then
// provision.
//
// An edited file is copied to <name>.bak before it is replaced. Overwriting
// is the point; doing it silently and unrecoverably to someone's own ontology
// is not. Publication stages originals and replacements for rollback and crash inspection.

package workspace

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/presets"
	"github.com/endgame-build/khub/internal/schema"
)

// UpgradeOptions controls planning and schema refresh. NoSchema keeps
// the workspace's .khub/ files — layers, templates and the provenance stamp —
// exactly as they are and reports what the shipped ontology has that they do
// not.
type UpgradeOptions struct {
	NoSchema bool
	DryRun   bool
	// Preview runs dry-run tails against the prepared temporary workspace.
	Preview func(string) error
	// Tails runs after a real upgrade has published, still under the workspace
	// lock, so the wire and index tails see exactly the tree the core
	// committed. The callee must use the Held variants (wire.WireHeld,
	// reindex.ReindexHeld): fsio.Locked is not re-entrant.
	// Tails are non-fatal; the hook returns nothing.
	Tails func(root string)
}

// ConfigChange is one workspace-owned file upgrade touched. Name and Backup
// are workspace-relative (.khub/storage.yaml, .khub/storage.yaml.bak); Action
// is "created" (the workspace had none — nothing to lose, no backup) or
// "replaced". An unchanged file is not reported at all. Backup is "" when
// nothing was at risk: a replaced file whose only difference was the
// provenance header collects no .bak.
type ConfigChange struct {
	Name   string
	Action string
	Backup string
}

// UpgradeResult is the outcome of one upgrade. Field order is the order the
// CLI's JSON payload preserves. Every slice is initialised empty, never nil,
// so the payload renders [] rather than null.
type UpgradeResult struct {
	Path        string
	Preset      string
	VersionFrom string
	VersionTo   string
	Config      []ConfigChange
	// SingletonsCreated names the singleton types this run minted — creations
	// only, the same rule init follows.
	SingletonsCreated []string
	// SchemaDrift lists the shipped types the workspace ontology lacks. Filled
	// only under NoSchema: after a refresh the files are never stale.
	SchemaDrift  []string
	DryRun       bool
	RemovedTypes []string
}

// Upgrade brings the workspace at root up to the preset the binary ships:
// the layer files and templates are replaced from the embedded (or recorded
// --preset-source) preset named in .khub/config.yaml, the provenance version
// is restamped, the schema is re-read, and directories and singletons the
// ontology gained are provisioned. Under NoSchema the first two steps are
// skipped and SchemaDrift says what was left behind.
//
// root must be a workspace (the CLI resolves it through FindWorkspace); a
// config.yaml recording no preset is the no_preset refusal.
func upgradeCandidate(root string, opt UpgradeOptions, sourceDir string) (*UpgradeResult, error) {
	prov, err := Provenance(root)
	if err != nil {
		return nil, err
	}
	recorded := configString(prov, "preset")
	if recorded == "" {
		return nil, errs.NoPreset(root)
	}
	versionFrom := configString(prov, "version")

	source := presets.Source(sourceDir)
	// A workspace recorded under a retired name upgrades onto the preset that
	// replaced it, and config.yaml is restamped with the canonical name below.
	preset := presets.Canonical(recorded, source)
	schemaPath, err := presets.Resolve(preset, source)
	if err != nil {
		return nil, err
	}
	merged, err := flatten(preset, source, schemaPath)
	if err != nil {
		return nil, err
	}

	target := pyPath(filepath.ToSlash(root))
	khubDir := pyJoin(target, ".khub")
	config := []ConfigChange{}
	versionTo := versionFrom
	if !opt.NoSchema {
		config, err = replaceConfig(target, khubDir, preset, merged, source)
		if err != nil {
			return nil, err
		}
		if err := restampConfig(pyJoin(khubDir, "config.yaml"), "version", merged.Version); err != nil {
			return nil, err
		}
		if preset != recorded {
			if err := restampConfig(pyJoin(khubDir, "config.yaml"), "preset", preset); err != nil {
				return nil, err
			}
		}
		versionTo = merged.Version
	}

	// Re-read what is now on disk and provision off the RESOLVED types, exactly
	// as init does — a type the ontology gained gets its directory, a templated
	// singleton the workspace lacks gets its file.
	resolved, err := introspect.LoadSchema(osPath(target))
	if err != nil {
		return nil, err
	}
	var createdSingletons []singleton
	if err := provision(target, resolved, &createdSingletons); err != nil {
		return nil, err
	}
	created := make([]string, 0, len(createdSingletons))
	for _, s := range createdSingletons {
		created = append(created, s.typeName)
	}

	drift := []string{}
	if opt.NoSchema {
		drift = schemaDrift(merged, resolved)
	}
	return &UpgradeResult{
		Path:              osPath(target),
		Preset:            preset,
		VersionFrom:       versionFrom,
		VersionTo:         versionTo,
		Config:            config,
		SingletonsCreated: created,
		SchemaDrift:       drift,
	}, nil
}

// replaceConfig puts every shipped file back over the workspace copy: the
// three layer files in write order, then the templates by name. Only what
// changed is reported.
func replaceConfig(target, khubDir, preset string, merged *flattened, source fs.FS) ([]ConfigChange, error) {
	changes := []ConfigChange{}
	for _, layer := range merged.layers() {
		incoming, err := layerBytes(preset, merged.Version, layer.Doc)
		if err != nil {
			return nil, err
		}
		change, reported, err := replaceFile(target, pyJoin(khubDir, layer.File), incoming, true)
		if err != nil {
			return nil, err
		}
		if reported {
			changes = append(changes, change)
		}
	}
	if !presets.HasTemplates(preset, source) {
		return changes, nil
	}
	tplDir := pyJoin(khubDir, "templates")
	if err := fsio.MkdirAll(osPath(target), osPath(tplDir)); err != nil {
		return nil, err
	}
	for _, tpl := range presets.Templates(preset, source) {
		payload, err := fs.ReadFile(source, tpl)
		if err != nil {
			return nil, err
		}
		change, reported, err := replaceFile(target, pyJoin(tplDir, pyName(tpl)), string(payload), false)
		if err != nil {
			return nil, err
		}
		if reported {
			changes = append(changes, change)
		}
	}
	return changes, nil
}

// replaceFile is kb's _replace_config for one file. A workspace predating the
// file has nothing to lose, so it is created rather than replaced and collects
// no .bak. A file equal to what khub ships is left alone and unreported. With
// headed set, the layer provenance line is stripped before comparing: a file
// whose only difference is the header — the version bump itself — is replaced
// without a backup, since there is no edit to preserve. Anything else is
// copied to <name>.bak first.
func replaceFile(target, path, incoming string, headed bool) (ConfigChange, bool, error) {
	name := relTo(target, path)
	raw, err := fsio.ReadFile(osPath(target), osPath(path))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return ConfigChange{}, false, err
		}
		if merr := fsio.MkdirAll(osPath(target), osPath(pyParent(path))); merr != nil {
			return ConfigChange{}, false, merr
		}
		if werr := fsio.AtomicWriteIn(osPath(target), osPath(path), []byte(incoming)); werr != nil {
			return ConfigChange{}, false, werr
		}
		return ConfigChange{Name: name, Action: "created"}, true, nil
	}
	current := string(raw)
	if current == incoming {
		return ConfigChange{}, false, nil
	}
	backup := ""
	if !headed || stripHeader(current) != stripHeader(incoming) {
		backupPath := path + ".bak"
		if werr := fsio.AtomicWriteIn(osPath(target), osPath(backupPath), []byte(current)); werr != nil {
			return ConfigChange{}, false, werr
		}
		backup = relTo(target, backupPath)
	}
	if werr := fsio.AtomicWriteIn(osPath(target), osPath(path), []byte(incoming)); werr != nil {
		return ConfigChange{}, false, werr
	}
	return ConfigChange{Name: name, Action: "replaced", Backup: backup}, true, nil
}

// stripHeader drops the layer provenance line when text opens with one, so
// two layer files compare on their bodies.
func stripHeader(text string) string {
	if !strings.HasPrefix(text, layerHeaderPrefix) {
		return text
	}
	if _, rest, found := strings.Cut(text, "\n"); found {
		return rest
	}
	return ""
}

// relTo renders a slash path under target the way the reports print it —
// ".khub/storage.yaml", never the absolute form. A "." target (the CLI at
// the workspace root, as init's target is) joins away in pyJoin, so path is
// already relative and there is no prefix to strip.
func relTo(target, path string) string {
	if target == "." {
		return path
	}
	return strings.TrimPrefix(strings.TrimPrefix(path, target), "/")
}

// restampConfig writes one scalar into config.yaml — the shipped `version`,
// or the canonical `preset` when the workspace was recorded under a retired
// name — touching only that scalar (the workspace may have edited stale_days,
// and a comment beside it should survive). A change the splicer cannot
// express re-emits the document through the same wide dump init wrote it with.
func restampConfig(configPath, key, value string) error {
	raw, err := fsio.ReadFile(filepath.Dir(filepath.Dir(osPath(configPath))), osPath(configPath))
	if err != nil {
		return err
	}
	v, err := canon.LoadDocMode(string(raw), canon.Mode12)
	if err != nil {
		return err
	}
	cfg, err := asMapOrEmpty(v, "config.yaml")
	if err != nil {
		return err
	}
	if configString(cfg, key) == value {
		return nil
	}
	cfg.Set(key, value)
	out, err := canon.SpliceMapping(raw, cfg, canon.Mode12)
	if errors.Is(err, canon.ErrNoSplice) {
		text, derr := canon.DumpWide(cfg)
		if derr != nil {
			return derr
		}
		out = []byte(text)
	} else if err != nil {
		return err
	}
	return fsio.AtomicWriteIn(filepath.Dir(filepath.Dir(osPath(configPath))), osPath(configPath), out)
}

// schemaDrift is kb's _schema_drift: the types the shipped ontology declares
// that the workspace's resolved schema does not, in the preset's declaration
// order. Reported, never merged — the copy is the workspace's, and guessing at
// a three-way merge of someone's ontology is a worse failure than telling
// them.
func schemaDrift(merged *flattened, resolved *schema.ResolvedSchema) []string {
	drift := []string{}
	ontology, ok := merged.Ontology.Get("ontology")
	if !ok {
		return drift
	}
	ont, ok := ontology.(*omap.Map)
	if !ok {
		return drift
	}
	entities, ok := ont.Get("entities")
	if !ok {
		return drift
	}
	shipped, ok := entities.(*omap.Map)
	if !ok {
		return drift
	}
	for _, name := range shipped.Keys() {
		if _, declared := resolved.Types.Get(name); !declared {
			drift = append(drift, name)
		}
	}
	return drift
}
