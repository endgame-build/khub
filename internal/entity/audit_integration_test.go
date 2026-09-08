package entity_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/query"
)

func auditWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	auditWrite(t, root, ".khub/ontology.yaml", `ontology:
  entities:
    a:
      attributes:
        code: {type: text, pattern: '(?=OK)OK'}
      relations:
        owns: {to: c, many: true, inverse: owned_by}
        one: {to: c}
    b:
      relations:
        owns: {to: c, many: true}
    c: {}
`)
	auditWrite(t, root, ".khub/config.yaml", "preset: audit\nversion: 1\n")
	return root
}
func auditWrite(t *testing.T, root, rel, text string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}
func auditFields(pairs ...string) *omap.Map {
	m := omap.New()
	for i := 0; i < len(pairs); i += 2 {
		m.Set(pairs[i], pairs[i+1])
	}
	return m
}
func auditCreate(t *testing.T, root, typ, id string, fields ...string) {
	t.Helper()
	if _, err := entity.Create(root, typ, entity.CreateOpts{ID: id, Fields: auditFields(fields...)}); err != nil {
		t.Fatal(err)
	}
}
func TestAuditRelationshipIdentity(t *testing.T) {
	root := auditWorkspace(t)
	auditCreate(t, root, "c", "target")
	auditCreate(t, root, "a", "source", "owns", "target,c/target", "one", "target,c/target")
	linked, err := entity.Link(root, "a/source", "owns", "c/target")
	if err != nil || linked.Changed {
		t.Fatalf("link=%v err=%v", linked, err)
	}
	linked, err = entity.Link(root, "a/source", "one", "c/target")
	if err != nil || linked.Changed {
		t.Fatalf("single link=%v err=%v", linked, err)
	}
	view, err := entity.Get(root, "a/source", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Edges) != 2 || len(view.Edges[0].ResolvedTargets) != 1 || view.Edges[0].ResolvedTargets[0] != "c/target" {
		t.Fatalf("edges=%+v", view.Edges)
	}
	removed, err := entity.Unlink(root, "a/source", "owns", "C/TARGET")
	if err != nil || !removed.Changed {
		t.Fatalf("unlink=%v err=%v", removed, err)
	}
	auditWrite(t, root, "c/stray.md", "---\ntype: a\n---\n")
	if _, err := entity.Link(root, "a/source", "owns", "stray"); err == nil {
		t.Fatal("stray accepted as target")
	}
	auditWrite(t, root, "a/dangling.md", "---\ntype: a\nowns: [missing]\n---\n")
	removed, err = entity.Unlink(root, "a/dangling", "owns", "MISSING")
	if err != nil || !removed.Changed {
		t.Fatalf("dangling unlink=%v %v", removed, err)
	}
}
func TestAuditInverseSourceTypeAndBatchGet(t *testing.T) {
	root := auditWorkspace(t)
	auditCreate(t, root, "c", "target")
	auditCreate(t, root, "a", "source")
	auditCreate(t, root, "b", "source", "owns", "target")
	inv := "owned_by"
	typ := "c"
	matches, err := query.Query(root, query.Filters{Type: &typ, Has: &inv}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("unrelated source acquired inverse: %v", matches)
	}
	views, err := entity.GetMany(root, []string{"b/source", "c/target", "b/source"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 3 || views[0].Type != "b" || views[1].Type != "c" || views[2].Type != "b" {
		t.Fatalf("views=%v", views)
	}
	if views, err := entity.GetMany(root, []string{"c/target", "source"}, false); err == nil || views != nil {
		t.Fatal("ambiguous batch partially succeeded")
	}
}
func TestAuditValidationAndScanFailures(t *testing.T) {
	root := auditWorkspace(t)
	if _, err := entity.Create(root, "a", entity.CreateOpts{ID: "bad", Fields: auditFields("code", "NO")}); err == nil {
		t.Fatal("bad pattern accepted on write")
	}
	auditWrite(t, root, "a/bad.md", "---\ntype: a\ncode: NO\n---\n")
	report, err := integrity.Validate(root, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range report.Errors {
		if e.Field == "code" {
			found = true
		}
	}
	if !found {
		t.Fatal("lookaround pattern skipped by validate")
	}
	auditWrite(t, root, "a/nested/hidden.yaml", "type: a\n")
	check, err := integrity.Check(root, false)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, m := range check.Misplaced {
		if m.Path == "a/nested/hidden.yaml" {
			found = true
		}
	}
	if !found {
		t.Fatalf("misplaced=%v", check.Misplaced)
	}
	if err := os.Chmod(filepath.Join(root, "a"), 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(filepath.Join(root, "a"), 0o755)
	if os.Geteuid() != 0 {
		if _, err := integrity.Validate(root, nil, false); err == nil {
			t.Fatal("unreadable scan succeeded")
		}
	}
}
func TestAuditRejectsStorageAndControlSymlinks(t *testing.T) {
	for _, rel := range []string{"a", "a/linked.md", ".khub/policy.yaml"} {
		t.Run(rel, func(t *testing.T) {
			root := auditWorkspace(t)
			outside := t.TempDir()
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(root, rel)); err != nil {
				t.Fatal(err)
			}
			schema, err := introspect.LoadSchema(root)
			if err == nil {
				_, err = index.Build(root, schema)
			}
			if err == nil {
				t.Fatal("symlink accepted")
			}
		})
	}
}
func TestAuditConcurrentEdits(t *testing.T) {
	if root := os.Getenv("KHUB_AUDIT_CHILD_ROOT"); root != "" {
		_, err := entity.Update(root, "a/source", entity.UpdateOpts{Fields: auditFields(os.Getenv("KHUB_AUDIT_CHILD_FIELD"), "retained")})
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, collection := range []bool{false, true} {
		t.Run(fmt.Sprintf("collection=%v", collection), func(t *testing.T) {
			root := auditWorkspace(t)
			if collection {
				auditWrite(t, root, ".khub/storage.yaml", "storage:\n  a: {layout: collection, path: rows.json}\n")
			}
			auditCreate(t, root, "a", "source")
			body := strings.Repeat("data ", 200000)
			if _, err := entity.Update(root, "a/source", entity.UpdateOpts{Body: &body}); err != nil {
				t.Fatal(err)
			}
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmds := make([]*exec.Cmd, 8)
			for i := range cmds {
				cmds[i] = exec.Command(exe, "-test.run=^TestAuditConcurrentEdits$")
				cmds[i].Env = append(os.Environ(), "KHUB_AUDIT_CHILD_ROOT="+root, fmt.Sprintf("KHUB_AUDIT_CHILD_FIELD=field%d", i))
				if err := cmds[i].Start(); err != nil {
					t.Fatal(err)
				}
			}
			for _, cmd := range cmds {
				if err := cmd.Wait(); err != nil {
					t.Fatal(err)
				}
			}
			view, err := entity.Get(root, "a/source", false)
			if err != nil {
				t.Fatal(err)
			}
			for i := range cmds {
				if value, _ := view.Meta.Get(fmt.Sprintf("field%d", i)); value != "retained" {
					t.Fatalf("successful field%d edit lost", i)
				}
			}
		})
	}
}

func TestAuditUnlinkDeduplicatesRemainingAliases(t *testing.T) {
	root := auditWorkspace(t)
	auditCreate(t, root, "c", "first")
	auditCreate(t, root, "c", "second")
	auditWrite(t, root, "a/source.md", "---\ntype: a\nowns: [first, c/first, second]\n---\n")
	if _, err := entity.Unlink(root, "a/source", "owns", "second"); err != nil {
		t.Fatal(err)
	}
	view, err := entity.Get(root, "a/source", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Edges) != 1 || view.Edges[0].Target != "first" {
		t.Fatalf("duplicates survived: %v", view.Edges)
	}
}
