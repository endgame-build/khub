package doctor_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/doctor"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/workspace"
)

func newWorkspace(t *testing.T) string {
	t.Helper()
	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workspace.Init("build-hub", ws, workspace.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	return ws
}

func find(t *testing.T, r *doctor.Report, name string) doctor.Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s check in %+v", name, r.Checks)
	return doctor.Check{}
}

// tree lists every path under root, so a test can prove doctor wrote nothing.
func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		info, err := d.Info()
		if err != nil {
			return err
		}
		// The mtime too: a probe created and removed in a directory moves it.
		out = append(out, rel+" "+info.ModTime().String())
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHealthyWorkspacePassesAndWritesNothing(t *testing.T) {
	ws := newWorkspace(t)
	for _, generated := range []bool{true, false} {
		if !generated {
			// The lock does not exist yet: the probe must create and remove
			// its file, leaving the tree as it found it.
			if err := os.RemoveAll(filepath.Join(ws, ".khub", "generated")); err != nil {
				t.Fatal(err)
			}
		}
		before := tree(t, ws)
		r := doctor.Run(ws, nil)
		if !r.Passed() {
			t.Fatalf("healthy workspace failed: %+v", r.Checks)
		}
		names := make([]string, 0, len(r.Checks))
		for _, c := range r.Checks {
			names = append(names, c.Name)
		}
		if got := strings.Join(names, ","); got != strings.Join(doctor.Names, ",") {
			t.Fatalf("check order: %s", got)
		}
		if after := tree(t, ws); strings.Join(before, "\n") != strings.Join(after, "\n") {
			t.Fatalf("doctor changed the tree:\nbefore %v\nafter  %v", before, after)
		}
	}
}

func TestNoWorkspaceSkipsTheRest(t *testing.T) {
	r := doctor.Run("", errs.NoWorkspace())
	if r.Passed() || r.Checks[0].Status != doctor.Fail {
		t.Fatalf("want a failed workspace check: %+v", r.Checks)
	}
	for _, c := range r.Checks[1:] {
		if c.Status != doctor.Skip {
			t.Fatalf("%s ran without a workspace: %+v", c.Name, c)
		}
	}
}

func TestGitMissingFails(t *testing.T) {
	ws := newWorkspace(t)
	t.Setenv("PATH", "")
	c := find(t, doctor.Run(ws, nil), "git")
	if c.Status != doctor.Fail || c.Code != "git_unavailable" {
		t.Fatalf("git with no PATH: %+v", c)
	}
}

func TestGitStates(t *testing.T) {
	ws := newWorkspace(t)
	// The temp dir may sit inside a repository on a developer machine; only
	// assert the outcome where it cannot.
	r := doctor.Run(ws, nil)
	if c := find(t, r, "git"); c.Status == doctor.Warn && (c.Code != "not_a_git_repository" || !r.Passed()) {
		t.Fatalf("outside git: %+v", c)
	}
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", ws}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", ".")
	if c := find(t, doctor.Run(ws, nil), "git"); c.Status != doctor.Warn || c.Code != "no_git_history" {
		t.Fatalf("unborn HEAD: %+v", c)
	}
	git("-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "x")
	if c := find(t, doctor.Run(ws, nil), "git"); c.Status != doctor.OK {
		t.Fatalf("committed: %+v", c)
	}
}

func TestGitThatAlwaysFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell-script git needs a unix shell")
	}
	ws := newWorkspace(t)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	if c := find(t, doctor.Run(ws, nil), "git"); c.Status != doctor.Fail || c.Code != "git_unavailable" {
		t.Fatalf("broken git: %+v", c)
	}
}

func TestBrokenSchemaSkipsLayout(t *testing.T) {
	ws := newWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws, ".khub", "ontology.yaml"), []byte("types: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := doctor.Run(ws, nil)
	if c := find(t, r, "schema"); c.Status != doctor.Fail || c.Err == nil {
		t.Fatalf("schema: %+v", c)
	}
	if c := find(t, r, "layout"); c.Status != doctor.Skip {
		t.Fatalf("layout ran over a broken schema: %+v", c)
	}
}

func TestProvenance(t *testing.T) {
	ws := newWorkspace(t)
	cfg := filepath.Join(ws, ".khub", "config.yaml")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(line, "version:") {
			kept = append(kept, line)
		}
	}
	if err := os.WriteFile(cfg, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := find(t, doctor.Run(ws, nil), "provenance"); c.Status != doctor.Warn || c.Code != "provenance_incomplete" {
		t.Fatalf("no version: %+v", c)
	}
	if err := os.Remove(cfg); err != nil {
		t.Fatal(err)
	}
	if c := find(t, doctor.Run(ws, nil), "provenance"); c.Status != doctor.Fail || c.Err == nil {
		t.Fatalf("no config: %+v", c)
	}
}

func TestLockShapes(t *testing.T) {
	ws := newWorkspace(t)
	gen := filepath.Join(ws, ".khub", "generated")
	if err := os.RemoveAll(gen); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gen, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if c := find(t, doctor.Run(ws, nil), "lock"); c.Status != doctor.Fail || c.Code != "lock_unwritable" {
		t.Fatalf("generated is a file: %+v", c)
	}
	if err := os.Remove(gen); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gen, "locks", "workspace.lock"), 0o755); err != nil {
		t.Fatal(err)
	}
	if c := find(t, doctor.Run(ws, nil), "lock"); c.Status != doctor.Fail || c.Code != "lock_unwritable" {
		t.Fatalf("lock is a directory: %+v", c)
	}
}

func TestLockDirectoryDenied(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod does not deny writes here")
	}
	ws := newWorkspace(t)
	gen := filepath.Join(ws, ".khub", "generated")
	if err := os.RemoveAll(gen); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(gen, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(gen, 0o755) })
	c := find(t, doctor.Run(ws, nil), "lock")
	if c.Status != doctor.Fail || c.Err == nil {
		t.Fatalf("read-only generated: %+v", c)
	}
	if strings.Contains(c.Err.Error(), "doctor-probe") {
		t.Fatalf("the error names the probe file: %v", c.Err)
	}
}

func TestLayoutMismatchNamesEveryType(t *testing.T) {
	ws := newWorkspace(t)
	for _, dir := range []string{"requirements", "components"} {
		p := filepath.Join(ws, "knowledge", dir)
		if err := os.RemoveAll(p); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := doctor.Run(ws, nil)
	var failed []string
	for _, c := range r.Checks {
		if c.Name == "layout" {
			if c.Status != doctor.Fail || c.Code != "layout_mismatch" {
				t.Fatalf("layout: %+v", c)
			}
			failed = append(failed, c.Message)
		}
	}
	if len(failed) != 2 {
		t.Fatalf("want one row per broken type, got %v", failed)
	}
}

func setConfig(t *testing.T, ws, key, value string) {
	t.Helper()
	cfg := filepath.Join(ws, ".khub", "config.yaml")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(raw), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, key+":") {
			lines[i] = key + ": " + value
		}
	}
	if err := os.WriteFile(cfg, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProvenanceAgainstShippedPreset(t *testing.T) {
	ws := newWorkspace(t)
	setConfig(t, ws, "version", "99.0.0")
	if c := find(t, doctor.Run(ws, nil), "provenance"); c.Status != doctor.Warn || c.Code != "provenance_newer" {
		t.Fatalf("newer than shipped: %+v", c)
	}
	setConfig(t, ws, "preset", "nosuchpreset")
	c := find(t, doctor.Run(ws, nil), "provenance")
	if c.Status != doctor.Warn || c.Err == nil {
		t.Fatalf("unknown preset: %+v", c)
	}
}

func TestStorageAncestorIsAFile(t *testing.T) {
	ws := newWorkspace(t)
	p := filepath.Join(ws, "knowledge")
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := doctor.Run(ws, nil)
	c := find(t, r, "layout")
	if c.Status != doctor.Fail || c.Code != "layout_mismatch" || c.Err != nil ||
		!strings.Contains(c.Message, "knowledge is not a directory") {
		t.Fatalf("ancestor file: %+v", c)
	}
	if c := find(t, r, "read"); c.Status != doctor.Skip {
		t.Fatalf("read ran over a broken layout: %+v", c)
	}
}

func unixDenial(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod does not deny here")
	}
}

func TestReadOnlyStorageFails(t *testing.T) {
	unixDenial(t)
	ws := newWorkspace(t)
	p := filepath.Join(ws, "knowledge", "requirements")
	if err := os.Chmod(p, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o755) })
	c := find(t, doctor.Run(ws, nil), "layout")
	if c.Status != doctor.Fail || c.Code != "storage_unwritable" {
		t.Fatalf("read-only storage: %+v", c)
	}
}

func TestUnreadableDirectoryFailsRead(t *testing.T) {
	unixDenial(t)
	ws := newWorkspace(t)
	junk := filepath.Join(ws, "junk")
	if err := os.Mkdir(junk, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(junk, 0o755) })
	r := doctor.Run(ws, nil)
	if c := find(t, r, "read"); c.Status != doctor.Fail || c.Err == nil {
		t.Fatalf("unreadable dir: %+v", c)
	}
	if r.Passed() {
		t.Fatal("doctor passed over a workspace check cannot read")
	}
}
