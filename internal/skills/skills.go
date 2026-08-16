// Package skills exposes khub's shipped agent skills as an fs.FS. It replaces
// the wheel force-include ("skills" -> "khub/skills") that src/khub/core/skill.py
// resolves at runtime: the Go binary carries the tree instead of looking for it.
//
// internal/skill takes its source tree as a parameter, so this package holds
// only the embed and the sub-root — the FS returned here is already rooted at
// the skill directories, so fs.Glob(source, "*/SKILL.md") matches.
package skills

import (
	"io/fs"

	khub "github.com/endgame-build/khub"
)

// FS is the shipped skills tree, rooted so that each entry is one skill
// directory ("khub/SKILL.md", "setup/SKILL.md"). Pass it straight to
// skill.Install.
func FS() fs.FS {
	sub, err := fs.Sub(khub.SkillsData, "skills")
	if err != nil {
		// Unreachable: the embed directive pins the prefix at build time.
		panic(err)
	}
	return sub
}
