// The embedded skills tree is data, so its test is a shape-and-bytes check:
// the FS must be rooted where skill.Install expects (one directory per skill),
// and the files it hands over must match the bytes the parity fixture
// recorded from the Python install (parity/cases/init-wire-skills/install-skills).
package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/endgame-build/khub/internal/skill"
)

func TestFSIsRootedAtTheSkillDirectories(t *testing.T) {
	// skill.AvailableSkills globs "*/SKILL.md" — the FS must already be
	// sub-rooted past the embed's "skills/" prefix for that to match.
	names, err := skill.AvailableSkills(FS())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(names, []string{"khub", "setup"}) {
		t.Fatalf("AvailableSkills = %v", names)
	}
}

func TestEmbeddedBytesMatchTheCheckout(t *testing.T) {
	// The embed is a build-time copy of the repo-root skills/ tree, so an edit
	// to a SKILL.md that never gets rebuilt would ship stale bytes. This is also
	// what replaced the "wheel carries the skills" CI step: a go:embed pattern
	// that matched nothing used to fail silently at runtime, and the sibling
	// test above pins the two skill names the binary must carry.
	root := filepath.Join("..", "..", "skills")
	err := fs.WalkDir(FS(), ".", func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return werr
		}
		got, rerr := fs.ReadFile(FS(), p)
		if rerr != nil {
			return rerr
		}
		want, rerr := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if rerr != nil {
			return rerr
		}
		if string(got) != string(want) {
			t.Errorf("%s: embedded bytes differ from the checkout", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSkillDigestsMatchTheRecordedInstall(t *testing.T) {
	// The digests the install fixture records. These move only when a SKILL.md is
	// deliberately edited — and when they do, init-wire-skills/install-skills has
	// to be re-recorded in the same commit, because its tree manifest hashes the
	// same bytes.
	for name, digest := range map[string]string{
		"khub/SKILL.md":  "9204fa2fd3fd37ac0e48001462210e04fcf159c0895a7b892736e241c9ebdb6f",
		"setup/SKILL.md": "e16c725ac93732f1767291f10f0f91cc3c3aa159149cde7a7ed0c4e75b124fdd",
	} {
		raw, err := fs.ReadFile(FS(), name)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != digest {
			t.Errorf("%s digest drifted from the recorded fixture", name)
		}
	}
}

func TestInstallFromTheEmbeddedTree(t *testing.T) {
	// End to end through the engine that consumes this package: every skill
	// into every project dir, then a re-install reporting "unchanged".
	root := t.TempDir()
	report, err := skill.Install(FS(), root, skill.Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []skill.Write{
		{Path: filepath.FromSlash(".claude/skills/khub/SKILL.md"), Action: "created"},
		{Path: filepath.FromSlash(".claude/skills/setup/SKILL.md"), Action: "created"},
		{Path: filepath.FromSlash(".agents/skills/khub/SKILL.md"), Action: "created"},
		{Path: filepath.FromSlash(".agents/skills/setup/SKILL.md"), Action: "created"},
		{Path: filepath.FromSlash(".opencode/skills/khub/SKILL.md"), Action: "created"},
		{Path: filepath.FromSlash(".opencode/skills/setup/SKILL.md"), Action: "created"},
	}
	if !reflect.DeepEqual(report.Writes, want) {
		t.Fatalf("writes = %v", report.Writes)
	}
	again, err := skill.Install(FS(), root, skill.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range again.Writes {
		if w.Action != "unchanged" {
			t.Errorf("re-install %s = %s", w.Path, w.Action)
		}
	}
}
