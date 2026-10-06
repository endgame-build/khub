// Package khub carries khub's embedded preset tree and nothing else.
//
// It exists only because `//go:embed` patterns may not contain "..": a pattern
// resolves against the package's own directory, so a package under internal/
// cannot reach a tree at the module root. presets/ lives at the root, which is
// why this file sits here rather than inside internal/presets.
//
// No logic lives here. internal/presets sub-roots the filesystem and is the
// only importer.
package khub

import "embed"

// PresetsData embeds presets/: the core base block (core/ontology.yaml, never
// copied into a workspace) plus one directory per preset — ontology.yaml with
// optional policy.yaml / storage.yaml / templates/. Patterns stay explicit so a
// stray file dropped into the tree cannot silently enter the binary; the first
// also covers core/ontology.yaml.
//
//go:embed presets/*/ontology.yaml
//go:embed presets/*/policy.yaml
//go:embed presets/*/storage.yaml
//go:embed presets/*/templates
var PresetsData embed.FS
