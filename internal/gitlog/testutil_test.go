package gitlog

// Test scaffolding for the git-derived reads, mirroring tests/conftest.py's
// fresh_ws / seed fixtures and tests/test_gitlog.py's _git_init / _commit
// helpers. Running git inside a throwaway fixture repo is the only way to
// exercise the subprocess path the module exists for.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/workspace"
)

// now is tests/test_gitlog.py's NOW.
var now = time.Date(2026, 6, 27, 0, 0, 0, 0, time.UTC)

type kv struct {
	K string
	V any
}

func freshWS(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	initPreset(t, root, "firm-ops")
	return root
}

func initPreset(t *testing.T, root, preset string) {
	t.Helper()
	if _, err := workspace.Init(preset, root, workspace.InitOptions{}); err != nil {
		t.Fatalf("init %s: %v", preset, err)
	}
}

// writeRaw writes a file verbatim (a collection inventory, a hand-authored doc).
func writeRaw(t *testing.T, root, relpath, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(relpath))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seed writes an md entity's frontmatter, like conftest.py's seed fixture.
func seed(t *testing.T, root, relpath string, f ...kv) {
	t.Helper()
	meta := omap.New()
	for _, x := range f {
		meta.Set(x.K, x.V)
	}
	text, err := canon.DumpWide(meta)
	if err != nil {
		t.Fatalf("dump %s: %v", relpath, err)
	}
	p := filepath.Join(root, filepath.FromSlash(relpath))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("---\n"+text+"---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, root, when string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if when != "" {
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+when, "GIT_COMMITTER_DATE="+when)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitInit(t *testing.T, root string) {
	t.Helper()
	git(t, root, "", "init")
	git(t, root, "", "config", "user.email", "t@t")
	git(t, root, "", "config", "user.name", "t")
}

func commit(t *testing.T, root, message, when string) {
	t.Helper()
	git(t, root, "", "add", "-A")
	git(t, root, when, "commit", "-m", message)
}

func mustStale(t *testing.T, root string, days *int) *StaleReport {
	t.Helper()
	report, err := Stale(root, days, now)
	if err != nil {
		t.Fatalf("stale: %v", err)
	}
	return report
}

func slugSet(report *StaleReport) map[string]bool {
	out := map[string]bool{}
	for _, e := range report.Entries {
		out[e.Slug] = true
	}
	return out
}

func slugOrder(report *StaleReport) []string {
	out := make([]string, len(report.Entries))
	for i, e := range report.Entries {
		out[i] = e.Slug
	}
	return out
}

func ptr[T any](v T) *T { return &v }
