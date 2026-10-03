package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// seedLegacySkills lays out what `khub install-skills` wrote: one SKILL.md per
// skill under each of the three agent directories, and a .gitignore line each.
func seedLegacySkills(t *testing.T, root string) {
	t.Helper()
	gitignore := "node_modules/\n"
	for _, dir := range legacySkillDirs {
		for _, name := range legacySkillNames {
			folder := filepath.Join(root, filepath.FromSlash(dir), name)
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(folder, legacySkillFile), []byte("# skill\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			gitignore += dir + "/" + name + "/\n"
		}
	}
	gitignore += ".khub/generated/\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatal(err)
	}
}

var allLegacySkills = []string{
	".claude/skills/khub", ".claude/skills/setup",
	".agents/skills/khub", ".agents/skills/setup",
	".opencode/skills/khub", ".opencode/skills/setup",
}

func TestRemoveLegacySkillsRemovesInstalledCopiesAndTheirGitignoreLines(t *testing.T) {
	root := t.TempDir()
	seedLegacySkills(t, root)

	removed, err := RemoveLegacySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, allLegacySkills) {
		t.Errorf("removed = %v, want %v", removed, allLegacySkills)
	}
	for _, dir := range []string{".claude", ".agents", ".opencode"} {
		if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
			t.Errorf("%s still exists after its only content was removed", dir)
		}
	}
	got, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "node_modules/\n.khub/generated/\n"; string(got) != want {
		t.Errorf(".gitignore = %q, want %q", got, want)
	}

	again, err := RemoveLegacySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 || again == nil {
		t.Errorf("a second run removed %v, want an empty list", again)
	}
}

func TestRemoveLegacySkillsLeavesWhatTheInstallerDidNotWrite(t *testing.T) {
	root := t.TempDir()
	seedLegacySkills(t, root)
	// The user added a file to one copy, and keeps a skill of their own beside another.
	mine := filepath.Join(root, ".claude", "skills", "khub", "notes.md")
	if err := os.WriteFile(mine, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	own := filepath.Join(root, ".agents", "skills", "review")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveLegacySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".claude/skills/setup", ".agents/skills/khub", ".agents/skills/setup",
		".opencode/skills/khub", ".opencode/skills/setup"}
	if !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if _, err := os.Stat(mine); err != nil {
		t.Errorf("the user's file was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".claude", "skills", "khub", legacySkillFile)); err != nil {
		t.Errorf("a folder holding the user's file lost its SKILL.md: %v", err)
	}
	if _, err := os.Stat(own); err != nil {
		t.Errorf("the user's own skill folder was removed: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if want := "node_modules/\n.claude/skills/khub/\n.khub/generated/\n"; string(got) != want {
		t.Errorf(".gitignore = %q, want %q", got, want)
	}
}

func TestRemoveLegacySkillsLeavesSymlinks(t *testing.T) {
	root := t.TempDir()
	elsewhere := t.TempDir()
	if err := os.WriteFile(filepath.Join(elsewhere, legacySkillFile), []byte("# skill\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, ".claude", "skills", "khub")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	// A real folder whose SKILL.md is a link is not the installer's either.
	linked := filepath.Join(root, ".agents", "skills", "khub")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(elsewhere, legacySkillFile), filepath.Join(linked, legacySkillFile)); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveLegacySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want nothing", removed)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the symlinked folder was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(elsewhere, legacySkillFile)); err != nil {
		t.Errorf("the link's target was removed: %v", err)
	}
}

func TestRemoveLegacySkillsDryRunWritesNothing(t *testing.T) {
	root := t.TempDir()
	seedLegacySkills(t, root)
	before, _ := os.ReadFile(filepath.Join(root, ".gitignore"))

	removed, err := RemoveLegacySkills(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(removed, allLegacySkills) {
		t.Errorf("removed = %v, want %v", removed, allLegacySkills)
	}
	for _, rel := range allLegacySkills {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel), legacySkillFile)); err != nil {
			t.Errorf("a dry run removed %s: %v", rel, err)
		}
	}
	after, _ := os.ReadFile(filepath.Join(root, ".gitignore"))
	if string(after) != string(before) {
		t.Error("a dry run rewrote .gitignore")
	}
}

func TestRemoveLegacySkillsWithNoneReportsAnEmptyList(t *testing.T) {
	root := t.TempDir()
	// An empty skills directory was not the installer's, and stays.
	empty := filepath.Join(root, ".claude", "skills")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveLegacySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if removed == nil || len(removed) != 0 {
		t.Errorf("removed = %#v, want an empty, non-nil list", removed)
	}
	if _, err := os.Stat(empty); err != nil {
		t.Errorf("an empty skills directory was removed: %v", err)
	}
}

// A copy `npx skills add` installed has the installer's shape and no .gitignore
// line, which is how it is told apart and left alone.
func TestRemoveLegacySkillsLeavesACopyWithNoGitignoreLine(t *testing.T) {
	root := t.TempDir()
	seedLegacySkills(t, root)
	gitignore := "node_modules/\n.claude/skills/khub/\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(gitignore), 0o644); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveLegacySkills(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{".claude/skills/khub"}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if _, err := os.Stat(filepath.Join(root, ".agents", "skills", "khub", legacySkillFile)); err != nil {
		t.Errorf("a copy with no .gitignore line was removed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if want := "node_modules/\n"; string(got) != want {
		t.Errorf(".gitignore = %q, want %q", got, want)
	}
}

// A symlinked .gitignore is not followed, so nothing is taken for khub's and
// the cleanup stays quiet.
func TestRemoveLegacySkillsWithASymlinkedGitignoreRemovesNothing(t *testing.T) {
	root := t.TempDir()
	seedLegacySkills(t, root)
	real := filepath.Join(t.TempDir(), "gitignore")
	if err := os.Rename(filepath.Join(root, ".gitignore"), real); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal(err)
	}

	removed, err := RemoveLegacySkills(root, false)
	if err != nil {
		t.Fatalf("err = %v, want none", err)
	}
	if len(removed) != 0 {
		t.Errorf("removed = %v, want none", removed)
	}
}

// A failure part-way still reports the folders that went and drops their
// .gitignore lines, so a later copy at those paths is never taken for khub's.
func TestRemoveLegacySkillsReportsWhatWentBeforeAFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	seedLegacySkills(t, root)
	locked := filepath.Join(root, ".agents", "skills", "khub")
	if err := os.Chmod(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	removed, err := RemoveLegacySkills(root, false)
	if err == nil {
		t.Fatal("err = nil, want the failed removal")
	}
	if want := []string{".claude/skills/khub", ".claude/skills/setup"}; !reflect.DeepEqual(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	got, rerr := os.ReadFile(filepath.Join(root, ".gitignore"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	want := "node_modules/\n.agents/skills/khub/\n.agents/skills/setup/\n.opencode/skills/khub/\n.opencode/skills/setup/\n.khub/generated/\n"
	if string(got) != want {
		t.Errorf(".gitignore = %q, want %q", got, want)
	}
}
