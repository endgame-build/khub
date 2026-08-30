// Ports the preset-reading half of src/khub/core/workspace.py, post the
// ontology/policy/storage split: loading a preset's three layer documents off
// a preset tree for init to write into .khub/{ontology,policy,storage}.yaml.
// The base block is NOT read here — it stays embedded in the binary and is
// supplied to every resolve by introspect.LoadSchema, never copied into a
// workspace. The registry itself (known_presets / resolve_preset, PRESETS_DIR)
// lives in internal/presets so --preset-source can swap the fs.FS; this file
// is the consumer.

package workspace

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/presets"
)

// flattened is the preset's three layer documents plus the provenance init
// stamps on them. No base: the base block is embedded, not copied. No derived
// views either — the tree/singleton passes read the RESOLVED schema of the
// workspace init just scaffolded, so no field here can disagree with what the
// binary will actually scan.
type flattened struct {
	Ontology *omap.Map // {"ontology": …} — the bytes behind .khub/ontology.yaml
	Policy   *omap.Map // {"policy": …} — always written, empty when the preset ships none
	Storage  *omap.Map // {"storage": …} — always written, empty when the preset ships none
	Version  string
}

// flatten reads a preset's three layer documents. An entity-less preset is
// rejected here, before anything is written — and so is a preset declaring
// `ontology.base`: the base block is khub-owned and embedded, and copying an
// authored one into the workspace would scaffold a schema no command can load
// (the resolver rejects it). The base is deliberately not loaded here: it is
// supplied at resolve time.
func flatten(name string, source fs.FS, ontologyPath string) (*flattened, error) {
	ontDoc, err := loadYAMLFS(source, ontologyPath)
	if err != nil {
		return nil, err
	}
	// The same top-level vocabulary the resolver enforces on workspace files:
	// flatten regenerates the workspace copy from the `ontology:` block alone,
	// so any other authored key would be dropped silently rather than caught.
	for _, k := range ontDoc.Keys() {
		if k != "ontology" && k != "version" {
			return nil, errs.New("invalid_schema", fmt.Sprintf(
				"Preset '%s' ontology.yaml: unknown top-level key '%s'", name, k))
		}
	}

	version := "0.0.0"
	if raw, ok := ontDoc.Get("version"); ok && !pyFalsy(raw) {
		version = pyScalarString(raw)
	}

	ontology, err := mappingOrNil(ontDoc, "ontology")
	if err != nil {
		return nil, err
	}
	var presetEntities *omap.Map
	if ontology != nil {
		if _, has := ontology.Get("base"); has {
			return nil, errs.New("invalid_schema", fmt.Sprintf(
				"Preset '%s' declares ontology.base; the base block is khub-owned and embedded "+
					"(see `khub schema base`) — override a base attribute by redeclaring it on the type", name))
		}
		presetEntities, err = mappingOrNil(ontology, "entities")
		if err != nil {
			return nil, err
		}
	}
	if presetEntities == nil || presetEntities.Len() == 0 {
		return nil, errs.New("empty_preset", fmt.Sprintf("Preset '%s' declares no entities", name))
	}

	// Policy and storage are optional layer files: absent reads as empty, and
	// the workspace copy is written either way so every scaffold has the same
	// three-file shape.
	policyMap, err := layerMap(source, path.Join(name, presets.PolicyFile), "policy")
	if err != nil {
		return nil, err
	}
	storageMap, err := layerMap(source, path.Join(name, presets.StorageFile), "storage")
	if err != nil {
		return nil, err
	}

	ontologyOut := omap.New()
	ontologyOut.Set("ontology", ontology)
	policyOut := omap.New()
	policyOut.Set("policy", policyMap)
	storageOut := omap.New()
	storageOut.Set("storage", storageMap)
	return &flattened{
		Ontology: ontologyOut,
		Policy:   policyOut,
		Storage:  storageOut,
		Version:  version,
	}, nil
}

// layerMap reads one optional layer file's top-level mapping: an absent file,
// an empty document, or a falsy `<key>:` value all read as an empty map.
//
// Only genuine absence is tolerant. A stat failure that is not "does not
// exist" (a broken symlink, a permission error under --preset-source)
// propagates rather than silently dropping the layer, and a non-empty
// document that never says `<key>:` — the flat-authoring mistake, per-type
// entries at the top level — is an error: writing `policy: {}` over it would
// discard every gate the preset author declared, with no diagnostic anywhere.
func layerMap(source fs.FS, relpath, key string) (*omap.Map, error) {
	if _, err := fs.Stat(source, relpath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return omap.New(), nil
		}
		return nil, err
	}
	doc, err := loadYAMLFS(source, relpath)
	if err != nil {
		return nil, err
	}
	for _, k := range doc.Keys() {
		if k != key && k != "version" {
			return nil, errs.New("invalid_schema", fmt.Sprintf(
				"Preset layer %s: unknown top-level key '%s' (the file carries a single '%s:' block)",
				relpath, k, key))
		}
	}
	m, err := mappingOrNil(doc, key)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return omap.New(), nil
	}
	return m, nil
}

// loadYAMLFS is resolve.load_yaml against an fs.FS instead of a path: a safe
// load into an insertion-ordered map, with the `data or {}` falsy collapse.
func loadYAMLFS(fsys fs.FS, name string) (*omap.Map, error) {
	raw, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(raw)) == "" {
		return omap.New(), nil
	}
	v, err := canon.LoadDocMode(string(raw), canon.Mode12)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if pyFalsy(v) {
		return omap.New(), nil
	}
	m, ok := v.(*omap.Map)
	if !ok {
		return nil, fmt.Errorf("schema file %s: top level is not a mapping", name)
	}
	return m, nil
}

// mappingOrNil reads a mapping-valued key: nil for absent/null/empty (the
// `or {}` idiom), an error for a value Python would crash on.
func mappingOrNil(m *omap.Map, key string) (*omap.Map, error) {
	v, ok := m.Get(key)
	if !ok || pyFalsy(v) {
		return nil, nil
	}
	sub, isMap := v.(*omap.Map)
	if !isMap {
		return nil, fmt.Errorf("'%s' is not a mapping (got %T)", key, v)
	}
	return sub, nil
}
