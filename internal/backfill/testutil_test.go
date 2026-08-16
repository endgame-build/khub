package backfill

// Test scaffolding for the cutover write path, mirroring tests/conftest.py's
// fresh_ws / seed fixtures and tests/test_projection.py's git helpers. Git
// fixtures commit with controlled author dates so the first/last-commit reads
// have real history.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/workspace"
)

type kv struct {
	K string
	V any
}

func freshWS(t *testing.T) string { return wsFor(t, "firm-ops") }

func wsFor(t *testing.T, preset string) string {
	t.Helper()
	root := t.TempDir()
	if _, err := workspace.Init(preset, root, workspace.InitOptions{}); err != nil {
		t.Fatalf("init %s: %v", preset, err)
	}
	return root
}

func fields(pairs ...string) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		m.Set(pairs[i], pairs[i+1])
	}
	return m
}

func create(t *testing.T, root, typeName, id string, pairs ...string) {
	t.Helper()
	_, err := entity.Create(root, typeName, entity.CreateOpts{
		Fields: fields(pairs...), ID: id, UseTemplate: true,
	})
	if err != nil {
		t.Fatalf("create %s/%s: %v", typeName, id, err)
	}
}

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
	writeRaw(t, root, relpath, "---\n"+text+"---\n")
}

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

func readRaw(t *testing.T, root, relpath string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relpath)))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// metaOf reads one entity's frontmatter back off disk.
func metaOf(t *testing.T, root, relpath string) *omap.Map {
	t.Helper()
	meta, _, err := canon.Parse(readRaw(t, root, relpath), "md")
	if err != nil {
		t.Fatalf("parse %s: %v", relpath, err)
	}
	return meta
}

func field(t *testing.T, root, relpath, key string) any {
	t.Helper()
	v, _ := metaOf(t, root, relpath).Get(key)
	return v
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

// datedGitWS is test_projection.py's dated_git_ws: `undated` committed then
// edited (distinct first/last dates); `dated` hand-kept with an inline comment.
func datedGitWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	seed(t, root, "fragments/undated.md", kv{"type", "fragment"}, kv{"stage", "raw"})
	writeRaw(t, root, "fragments/dated.md",
		"---\ntype: fragment\nstage: raw\ncreated: 2025-01-01  # hand-kept, do not touch\n---\nbody\n")
	gitInit(t, root)
	commit(t, root, "seed", "2026-01-01T12:00:00") // first commit
	seed(t, root, "fragments/undated.md", kv{"type", "fragment"}, kv{"stage", "mature"})
	commit(t, root, "edit undated", "2026-03-01T12:00:00") // last commit (distinct)
	return root
}

// scaffoldWS is test_projection.py's scaffold_ws: a complete opportunity beside
// an incomplete legacy one written directly.
func scaffoldWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	create(t, root, "person", "noor", "name", "Noor", "role", "partner")
	create(t, root, "client", "initech", "name", "Initech")
	create(t, root, "opportunity", "initech-deal",
		"name", "Initech Deal", "stage", "prospect", "client", "initech", "owner", "noor")
	seed(t, root, "opportunities/legacy-deal/_index.md",
		kv{"type", "opportunity"}, kv{"client", "initech"})
	return root
}

func mustBackfill(t *testing.T, root string, typeName *string, dryRun bool) *BackfillReport {
	t.Helper()
	report, err := Backfill(root, typeName, dryRun)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	return report
}

// changeKeys renders the (slug, field) pairs the Python rows assert on.
func changeKeys(report *BackfillReport) map[[2]string]BackfillChange {
	out := map[[2]string]BackfillChange{}
	for _, c := range report.Changes {
		out[[2]string{c.Slug, c.Field}] = c
	}
	return out
}

// snapshot records every file under dir so a dry run can be proved byte-inert.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out[e.Name()] = string(raw)
	}
	return out
}

func locatedCode(err error) string {
	for e := err; e != nil; {
		if l, ok := e.(*errs.Located); ok {
			return l.Code
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			return ""
		}
		e = u.Unwrap()
	}
	return ""
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }
