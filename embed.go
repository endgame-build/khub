// Package khub carries khub's embedded data trees and nothing else.
//
// It exists only because `//go:embed` patterns may not contain "..": a pattern
// resolves against the package's own directory, so a package under internal/
// cannot reach a tree at the module root. Both trees the binary ships live at
// the root — presets/ and skills/ — which is why this file sits here rather
// than inside internal/presets.
//
// The pre-cutover note here said the presets would move to /presets and this
// file would collapse into internal/presets. Half of that happened: the presets
// did move off the retired Python package, but skills/ stays at the root where
// `npx skills` expects to find it, so one root-level embed still has to serve
// both trees.
//
// No logic lives here. internal/presets and internal/skills sub-root these
// filesystems and are the only importers.
package khub

import "embed"

// PresetsData embeds presets/: the core base block plus one directory per
// preset (schema.yaml + optional templates/). Patterns stay explicit so a
// stray file dropped into the tree cannot silently enter the binary.
//
//go:embed presets/core.yaml
//go:embed presets/*/schema.yaml
//go:embed presets/*/templates
var PresetsData embed.FS

// SkillsData embeds the repo-root skills tree (one directory per skill, each
// holding a SKILL.md). This is a build-time copy: the binary carries the
// skills, so `khub install-skills` needs nothing on disk beside it.
//
//go:embed skills
var SkillsData embed.FS
