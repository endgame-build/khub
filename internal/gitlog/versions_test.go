package gitlog

import (
	"os"
	"path/filepath"
	"testing"
)

// The path is a file's name, never a pattern, and a rename starts anew whatever
// the config says.
func TestFileVersionsTakesThePathAsAName(t *testing.T) {
	root := t.TempDir()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git := func(args ...string) {
		t.Helper()
		args = append([]string{"-c", "user.name=Ana", "-c", "user.email=ana@example.com", "-c", "commit.gpgsign=false", "-c", "core.autocrlf=false"}, args...)
		if _, code, err := run(root, args...); err != nil || code != 0 {
			t.Fatalf("git %v: %d %v", args, code, err)
		}
	}
	git("init", "-q")
	git("config", "log.follow", "true")
	write("cap-one.md", "one\n")
	git("add", "-A")
	git("commit", "-qm", "one")
	write("cap-[o]ne.md", "odd\n")
	git("add", "-A")
	git("commit", "-qm", "odd")
	git("mv", "cap-one.md", "cap-two.md")
	git("commit", "-qm", "moved")

	subjects := func(name string) (out []string) {
		t.Helper()
		versions, more, err := FileVersions(root, name, 10)
		if err != nil || more {
			t.Fatalf("%s: %v, more %v", name, err, more)
		}
		for _, v := range versions {
			out = append(out, v.Subject)
		}
		return out
	}
	for name, want := range map[string]string{"cap-[o]ne.md": "odd", "cap-two.md": "moved"} {
		if got := subjects(name); len(got) != 1 || got[0] != want {
			t.Errorf("%s = %q, want only %q", name, got, want)
		}
	}
	versions, _, err := FileVersions(root, "cap-one.md", 10)
	if err != nil || len(versions) != 2 {
		t.Fatalf("%d versions, %v", len(versions), err)
	}
	if versions[0].After.State != BlobAbsent || versions[0].Before.Text != "one\n" {
		t.Errorf("the move left %+v", versions[0])
	}
	if versions[1].Before.State != BlobAbsent || versions[1].After.Text != "one\n" {
		t.Errorf("the creation left %+v", versions[1])
	}
	if _, more, _ := FileVersions(root, "cap-one.md", 1); !more {
		t.Error("a cut history says it is whole")
	}
	if v, more, err := FileVersions(t.TempDir(), "x.md", 1); len(v) != 0 || more || err != nil {
		t.Errorf("outside git: %v %v %v", v, more, err)
	}
}
