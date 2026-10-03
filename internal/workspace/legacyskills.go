package workspace

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
)

// Earlier khub releases copied their agent skills into the workspace on every
// upgrade. The skills now ship through the plugin marketplace and `npx skills`,
// so those copies are stale: nothing refreshes them, and a project-level skill
// shadows the installed one.
var (
	legacySkillDirs  = []string{".claude/skills", ".agents/skills", ".opencode/skills"}
	legacySkillNames = []string{"khub", "setup"}
)

const legacySkillFile = "SKILL.md"

// RemoveLegacySkills deletes the skill copies an earlier `khub upgrade` or
// `khub install-skills` left in the workspace, with the .gitignore line each
// one came with, and returns the workspace-relative folders it removed (or
// would remove, under dryRun), in target-then-skill order.
//
// A folder goes only when .gitignore carries the line the installer wrote for
// it and every entry in it is a regular file named SKILL.md — all those
// commands ever wrote. The line is what tells khub's copy from one `npx skills`
// installed, which has the same shape and no line. A folder holding anything
// else is the user's and stays, as does one reached through a symlink. A real
// run is called with the workspace lock held. On a failure the folders removed
// before it are still returned.
func RemoveLegacySkills(root string, dryRun bool) ([]string, error) {
	removed := []string{}
	ignored, err := gitignoreLines(root)
	if err != nil {
		return removed, err
	}
	failure := removeLegacyFolders(root, ignored, dryRun, &removed)
	if dryRun || len(removed) == 0 {
		return removed, failure
	}
	// The lines of the folders that did go are dropped even after a failure,
	// so a later copy at the same path is never taken for khub's.
	if err := dropGitignoreLines(root, removed); err != nil && failure == nil {
		failure = err
	}
	return removed, failure
}

// removeLegacyFolders appends each folder it removes (or would remove) to
// removed and stops at the first failure.
func removeLegacyFolders(root string, ignored map[string]bool, dryRun bool, removed *[]string) error {
	for _, dir := range legacySkillDirs {
		before := len(*removed)
		for _, name := range legacySkillNames {
			rel := dir + "/" + name
			if !ignored[rel+"/"] {
				continue
			}
			folder := filepath.Join(root, filepath.FromSlash(rel))
			owned, err := isLegacySkill(root, folder)
			if err != nil {
				return err
			}
			if !owned {
				continue
			}
			if dryRun {
				*removed = append(*removed, rel)
				continue
			}
			if err := fsio.Remove(root, filepath.Join(folder, legacySkillFile), false); err != nil {
				return err
			}
			if err := fsio.Remove(root, folder, false); err != nil {
				return err
			}
			*removed = append(*removed, rel)
		}
		if dryRun || len(*removed) == before {
			continue
		}
		// The skills directory and its agent directory go only when emptied.
		for _, parent := range []string{dir, filepath.Dir(dir)} {
			emptied, err := removeIfEmpty(root, filepath.Join(root, filepath.FromSlash(parent)))
			if err != nil {
				return err
			}
			if !emptied {
				break
			}
		}
	}
	return nil
}

// isLegacySkill reports whether folder holds exactly what the installer wrote.
// A missing folder, a symlink on the way to it, and a folder with any other
// entry all answer false.
func isLegacySkill(root, folder string) (bool, error) {
	entries, err := fsio.ReadDir(root, folder)
	if err != nil {
		if isAbsentOrUnsafe(err) {
			return false, nil
		}
		// A file where the folder would be is not a skill folder either.
		if info, serr := fsio.Stat(root, folder); serr == nil && !info.IsDir() {
			return false, nil
		}
		return false, err
	}
	if len(entries) == 0 {
		return false, nil
	}
	for _, entry := range entries {
		if entry.Name() != legacySkillFile || !entry.Type().IsRegular() {
			return false, nil
		}
	}
	return true, nil
}

// isAbsentOrUnsafe reports a path that is not there, or that khub refuses to
// follow because a symlink is on the way to it.
func isAbsentOrUnsafe(err error) bool {
	var located *errs.Located
	return errors.Is(err, fs.ErrNotExist) || (errors.As(err, &located) && located.Code == "unsafe_path")
}

// gitignoreLines is the set of lines in the workspace's .gitignore, empty when
// there is none or it is a symlink khub does not follow.
func gitignoreLines(root string) (map[string]bool, error) {
	raw, err := fsio.ReadFile(root, filepath.Join(root, ".gitignore"))
	if err != nil {
		if isAbsentOrUnsafe(err) {
			return map[string]bool{}, nil
		}
		return nil, err
	}
	lines := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		lines[strings.TrimSuffix(line, "\r")] = true
	}
	return lines, nil
}

func removeIfEmpty(root, dir string) (bool, error) {
	entries, err := fsio.ReadDir(root, dir)
	if err != nil {
		if isAbsentOrUnsafe(err) {
			return false, nil
		}
		return false, err
	}
	if len(entries) > 0 {
		return false, nil
	}
	return true, fsio.Remove(root, dir, false)
}

// dropGitignoreLines removes the `<folder>/` line the installer appended for
// each removed folder and leaves every other byte of .gitignore as it was.
func dropGitignoreLines(root string, removed []string) error {
	gitignore := filepath.Join(root, ".gitignore")
	raw, err := fsio.ReadFile(root, gitignore)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	drop := make(map[string]bool, len(removed))
	for _, rel := range removed {
		drop[rel+"/"] = true
	}
	lines := strings.SplitAfter(string(raw), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !drop[strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")] {
			kept = append(kept, line)
		}
	}
	if len(kept) == len(lines) {
		return nil
	}
	return fsio.AtomicWriteIn(root, gitignore, []byte(strings.Join(kept, "")))
}
