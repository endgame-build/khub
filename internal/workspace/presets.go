// Ports the preset-reading half of src/khub/core/workspace.py: loading
// core.yaml + <preset>/schema.yaml off a preset tree and flattening them into
// the one document init writes to .khub/schema.yaml. The registry itself
// (known_presets / resolve_preset, PRESETS_DIR) lives in internal/presets so
// --preset-source can swap the fs.FS; this file is the consumer.

package workspace

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/presets"
)

// flattened is the merged schema document plus the provenance init stamps on
// it: core's base block, core's entities overlaid by the preset's, and the
// preset version that goes into the header and config.yaml.
type flattened struct {
	Doc     *omap.Map // {"base": …, "entities": …} — the bytes init writes
	Version string
	// Entities is Doc["entities"], kept for the tree/singleton passes.
	Entities *omap.Map
}

// flatten is the core+preset merge: `{**core.entities, **preset.entities}`
// under a doc that leads with core's base block. An entity-less preset is
// rejected here, before anything is written.
func flatten(name string, source fs.FS, schemaPath string) (*flattened, error) {
	core, err := loadYAMLFS(presets.Embedded(), presets.CoreFile)
	if err != nil {
		return nil, err
	}
	presetData, err := loadYAMLFS(source, schemaPath)
	if err != nil {
		return nil, err
	}

	version := "0.0.0"
	if raw, ok := presetData.Get("version"); ok && !pyFalsy(raw) {
		version = pyScalarString(raw)
	}

	presetEntities, err := mappingOrNil(presetData, "entities")
	if err != nil {
		return nil, err
	}
	if presetEntities == nil || presetEntities.Len() == 0 {
		return nil, errs.New("empty_preset", fmt.Sprintf("Preset '%s' declares no entities", name))
	}

	// core.yaml is base-only in v1, so its `entities` is legitimately absent.
	coreEntities, err := mappingOrNil(core, "entities")
	if err != nil {
		return nil, err
	}
	entities := omap.New()
	if coreEntities != nil {
		copyInto(entities, coreEntities)
	}
	copyInto(entities, presetEntities)

	doc := omap.New()
	base, _ := core.Get("base") // absent reads as null, exactly like core.get("base")
	doc.Set("base", base)
	doc.Set("entities", entities)
	return &flattened{Doc: doc, Version: version, Entities: entities}, nil
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

func copyInto(dst, src *omap.Map) {
	for _, k := range src.Keys() {
		v, _ := src.Get(k)
		dst.Set(k, v)
	}
}

// declOf reads one entity declaration as a mapping. A non-mapping declaration
// has no keys to read, which is how Python's `decl.get(...)` chain behaves for
// the paths init walks (it would raise; nothing in a shipped preset does this).
func declOf(entities *omap.Map, name string) *omap.Map {
	v, _ := entities.Get(name)
	if m, ok := v.(*omap.Map); ok {
		return m
	}
	return omap.New()
}

// declStr is decl.get(key) narrowed to the string the vocabulary declares:
// absent/null read as "". Non-string scalars render as their Python str()
// form (pyScalarString, shared with locate.go).
func declStr(decl *omap.Map, key string) string {
	v, ok := decl.Get(key)
	if !ok || v == nil {
		return ""
	}
	return pyScalarString(v)
}
