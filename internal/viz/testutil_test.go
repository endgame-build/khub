package viz

// Test scaffolding for the Cytoscape projection, mirroring tests/conftest.py's
// fresh_ws / seed fixtures and tests/test_projection.py's seed_firm_ops.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
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

func firmOpsWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	create(t, root, "person", "noor", "name", "Noor", "role", "partner", "title", "Noor P")
	create(t, root, "client", "initech", "name", "Initech", "title", "Initech")
	create(t, root, "opportunity", "initech-deal", "stage", "prospect",
		"client", "initech", "owner", "noor", "title", "Initech Deal")
	create(t, root, "project", "initech-pov", "client", "initech", "owner", "noor",
		"active", "true", "title", "Initech PoV")
	create(t, root, "meeting", "kickoff", "date", "2026-06-01T10:00:00",
		"call_type", "client", "source", "recording", "engagement", "initech-pov",
		"title", "Kickoff")
	return root
}

// liveGraph is test_projection.py's _live, narrowed to the graph.
func liveGraph(t *testing.T, root string) *graph.Graph {
	t.Helper()
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	return graph.BuildGraph(index.Filter(scanned, index.StrayNodes(scanned)))
}

func mustViz(t *testing.T, root, out string, typeFilter *string) *Result {
	t.Helper()
	result, err := Viz(root, out, typeFilter)
	if err != nil {
		t.Fatalf("viz: %v", err)
	}
	return result
}

// noExternalAssets is test_projection.py's _no_external_assets: no <script src>
// or <link href> points at a host (PRJ-005).
func noExternalAssets(t *testing.T, html string) {
	t.Helper()
	for _, scheme := range []string{"http", "//"} {
		for _, attr := range []string{"src=\"", "src='", "href=\"", "href='"} {
			if tag := attr + scheme; strings.Contains(html, tag) {
				t.Errorf("external asset reference %q in output", tag)
			}
		}
	}
}

func dataOf(t *testing.T, el *omap.Map) *omap.Map {
	t.Helper()
	v, ok := el.Get("data")
	if !ok {
		t.Fatal("element carries no data key")
	}
	m, ok := v.(*omap.Map)
	if !ok {
		t.Fatalf("data is %T, want a mapping", v)
	}
	return m
}

func str(t *testing.T, m *omap.Map, key string) string {
	t.Helper()
	v, _ := m.Get(key)
	s, _ := v.(string)
	return s
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

func hasNode(nodes []index.Node, want index.Node) bool {
	for _, n := range nodes {
		if n == want {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }
