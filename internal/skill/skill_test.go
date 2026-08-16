// Library-level assertions ported from tests/test_skill_cmd.py (the
// core.install_skills half; the CLI half is covered by fixtures). The source
// tree is an in-memory fs.FS, matching the Go engine's contract of taking
// the skills FS from the caller.
package skill

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/endgame-build/khub/internal/errs"
)

// projectDirs: every project dir the default install writes.
var projectDirs = []string{".claude/skills", ".agents/skills", ".opencode/skills"}

func sourceFS() fstest.MapFS {
	return fstest.MapFS{
		"khub/SKILL.md":  &fstest.MapFile{Data: []byte("---\nname: khub\n---\nuse khub\n")},
		"setup/SKILL.md": &fstest.MapFile{Data: []byte("---\nname: setup\n---\nbootstrap\n")},
	}
}

func mustInstall(t *testing.T, root string, opt Options) *Report {
	t.Helper()
	report, err := Install(sourceFS(), root, opt)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestInstallsEverySkillIntoEveryProjectDir(t *testing.T) {
	// Default: both skills into all three agent dirs, reported per file, in
	// target-then-skill order.
	root := t.TempDir()
	report := mustInstall(t, root, Options{})

	if report.Scope != "project" {
		t.Errorf("scope = %s", report.Scope)
	}
	if !reflect.DeepEqual(report.Skills, []string{"khub", "setup"}) {
		t.Errorf("skills = %v", report.Skills)
	}
	var got []Write
	for _, target := range projectDirs {
		for _, name := range []string{"khub", "setup"} {
			got = append(got, Write{Path: filepath.Join(target, name, "SKILL.md"), Action: "created"})
			if !exists(filepath.Join(root, target, name, "SKILL.md")) {
				t.Errorf("missing %s/%s/SKILL.md", target, name)
			}
		}
	}
	if !reflect.DeepEqual(report.Writes, got) {
		t.Errorf("writes = %v, want %v", report.Writes, got)
	}
}

func TestSecondRunIsUnchangedAndRewritesNothing(t *testing.T) {
	root := t.TempDir()
	mustInstall(t, root, Options{})
	written := filepath.Join(root, ".agents/skills/khub/SKILL.md")
	before, err := os.Stat(written)
	if err != nil {
		t.Fatal(err)
	}

	report := mustInstall(t, root, Options{})
	for _, w := range report.Writes {
		if w.Action != "unchanged" {
			t.Errorf("%s action = %s, want unchanged", w.Path, w.Action)
		}
	}
	after, err := os.Stat(written)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("mtime changed on an unchanged re-install")
	}
}

func TestDriftedFileIsUpdated(t *testing.T) {
	// An installed copy is managed: edited content is re-synced, not preserved.
	root := t.TempDir()
	mustInstall(t, root, Options{})
	drifted := filepath.Join(root, ".agents/skills/khub/SKILL.md")
	if err := os.WriteFile(drifted, []byte("stale content"), 0o666); err != nil {
		t.Fatal(err)
	}

	report := mustInstall(t, root, Options{})
	actions := map[string]string{}
	for _, w := range report.Writes {
		actions[w.Path] = w.Action
	}
	if actions[".agents/skills/khub/SKILL.md"] != "updated" {
		t.Errorf("actions = %v", actions)
	}
	b, err := os.ReadFile(drifted)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "stale content" {
		t.Error("drifted content was preserved")
	}
}

func TestDryRunReportsAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	report := mustInstall(t, root, Options{DryRun: true})

	if !report.DryRun || len(report.Writes) == 0 {
		t.Fatalf("report = %+v", report)
	}
	for _, w := range report.Writes {
		if w.Action != "created" {
			t.Errorf("%s action = %s, want created", w.Path, w.Action)
		}
	}
	if exists(filepath.Join(root, ".agents")) {
		t.Error(".agents was created under dry run")
	}
	if exists(filepath.Join(root, ".gitignore")) {
		t.Error(".gitignore was written under dry run")
	}
}

func TestTargetAndSkillNarrowTheInstall(t *testing.T) {
	root := t.TempDir()
	mustInstall(t, root, Options{Targets: []string{"agents"}, Skills: []string{"khub"}})

	if !exists(filepath.Join(root, ".agents/skills/khub/SKILL.md")) {
		t.Error("narrowed skill missing")
	}
	if exists(filepath.Join(root, ".agents/skills/setup")) {
		t.Error("unselected skill installed")
	}
	if exists(filepath.Join(root, ".claude")) {
		t.Error("unselected target written")
	}
}

func TestProjectScopeGitignoresOnlyWhatKhubOwns(t *testing.T) {
	// Ignore only khub's own skill directories, never the whole skills dir.
	root := t.TempDir()
	mustInstall(t, root, Options{})

	b, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	has := func(line string) bool {
		for _, l := range lines {
			if l == line {
				return true
			}
		}
		return false
	}
	for _, target := range projectDirs {
		if !has(target + "/khub/") {
			t.Errorf("missing gitignore line %s/khub/", target)
		}
		if !has(target + "/setup/") {
			t.Errorf("missing gitignore line %s/setup/", target)
		}
		if has(target + "/") {
			t.Errorf("gitignore swallows the whole %s/", target)
		}
	}
}

func TestGitignoreAppendIsIdempotentAndAdditive(t *testing.T) {
	root := t.TempDir()
	seed := "# existing\n.khub/generated/\n"
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(seed), 0o666); err != nil {
		t.Fatal(err)
	}
	mustInstall(t, root, Options{})
	first, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(first), seed) {
		t.Errorf("existing gitignore content disturbed: %q", first)
	}
	mustInstall(t, root, Options{})
	second, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("re-install appended duplicate lines:\n%q\n%q", first, second)
	}
}

func TestGlobalScopeWritesHomeDirsAndNoGitignore(t *testing.T) {
	// --global installs per machine; a home directory is not a git repo.
	root := t.TempDir()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "") // the ~/.config fallback must hold without XDG

	report := mustInstall(t, root, Options{Global: true})
	if report.Scope != "global" {
		t.Errorf("scope = %s", report.Scope)
	}
	if !exists(filepath.Join(home, ".agents/skills/khub/SKILL.md")) {
		t.Error("missing ~/.agents install")
	}
	if !exists(filepath.Join(home, ".config/opencode/skills/khub/SKILL.md")) {
		t.Error("missing ~/.config/opencode install") // opencode's home path differs
	}
	if exists(filepath.Join(root, ".agents")) {
		t.Error("workspace written under --global")
	}
	if exists(filepath.Join(root, ".gitignore")) {
		t.Error(".gitignore written under --global")
	}
	// Absolute, so a reader can tell a machine install from a workspace one.
	for _, w := range report.Writes {
		if !strings.HasPrefix(w.Path, home) {
			t.Errorf("write path %q not under home", w.Path)
		}
	}
}

func TestGlobalScopeNeedsNoWorkspace(t *testing.T) {
	// A machine-wide install is what you run before any workspace exists.
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	report := mustInstall(t, "", Options{Global: true})
	if report.Scope != "global" {
		t.Errorf("scope = %s", report.Scope)
	}
	if !exists(filepath.Join(home, ".claude/skills/khub/SKILL.md")) {
		t.Error("missing ~/.claude install")
	}
}

func TestGlobalOpencodeHonoursXDGConfigHome(t *testing.T) {
	// opencode reads its skills under $XDG_CONFIG_HOME; writing to ~/.config
	// regardless would report `created` for files opencode never loads.
	base := t.TempDir()
	home := filepath.Join(base, "home")
	xdg := filepath.Join(base, "xdg")
	if err := os.MkdirAll(home, 0o777); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", xdg)

	mustInstall(t, "", Options{Global: true, Targets: []string{"opencode"}})
	if !exists(filepath.Join(xdg, "opencode/skills/khub/SKILL.md")) {
		t.Error("missing $XDG_CONFIG_HOME install")
	}
	if exists(filepath.Join(home, ".config")) {
		t.Error("~/.config written despite XDG_CONFIG_HOME")
	}
}

func TestUnknownSelectionIsALocatedError(t *testing.T) {
	cases := []struct {
		name    string
		root    string
		opt     Options
		code    string
		message string
	}{
		{"unknown skill", "root", Options{Skills: []string{"ghost"}}, "unknown_skill",
			"Unknown skill: ghost. Known skills: khub, setup"},
		{"unknown target", "root", Options{Targets: []string{"emacs"}}, "unknown_target",
			"Unknown target: emacs. Known targets: claude, agents, opencode"},
		{"missing root", "", Options{}, "missing_root",
			"A project-scope skill install needs a workspace root"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := tc.root
			if root != "" {
				root = t.TempDir()
			}
			_, err := Install(sourceFS(), root, tc.opt)
			var located *errs.Located
			if !errors.As(err, &located) {
				t.Fatalf("want *errs.Located, got %T (%v)", err, err)
			}
			if located.Code != tc.code || located.Message != tc.message {
				t.Errorf("got (%s, %q), want (%s, %q)",
					located.Code, located.Message, tc.code, tc.message)
			}
		})
	}
}

func TestMultiFileSkillSyncsInSortedWalkOrder(t *testing.T) {
	// sorted(rglob) parity: files land depth-first in lexical component order.
	source := fstest.MapFS{
		"deep/SKILL.md":     &fstest.MapFile{Data: []byte("s\n")},
		"deep/a.txt":        &fstest.MapFile{Data: []byte("a\n")},
		"deep/refs/deep.md": &fstest.MapFile{Data: []byte("d\n")},
	}
	root := t.TempDir()
	report, err := Install(source, root, Options{Targets: []string{"claude"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, w := range report.Writes {
		got = append(got, w.Path)
	}
	want := []string{
		".claude/skills/deep/SKILL.md",
		".claude/skills/deep/a.txt",
		".claude/skills/deep/refs/deep.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("write order = %v, want %v", got, want)
	}
}

func TestRepoSkillsTreeCarriesBothSkills(t *testing.T) {
	// The repo's skills/ tree is the install's source (embedded at release);
	// ports test_skills_dir_carries_both_skills against the checkout.
	repoSkills := os.DirFS(filepath.Join("..", "..", "skills"))
	names, err := AvailableSkills(repoSkills)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"khub", "setup"}) {
		t.Errorf("AvailableSkills = %v, want [khub setup]", names)
	}
}

func TestGlobalDirKnowsEveryTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")

	for target, want := range map[string]string{
		"claude":   filepath.Join(home, ".claude/skills"),
		"agents":   filepath.Join(home, ".agents/skills"),
		"opencode": filepath.Join(home, ".config/opencode/skills"),
	} {
		got, err := GlobalDir(target)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("GlobalDir(%s) = %s, want %s", target, got, want)
		}
	}
}
