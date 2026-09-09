package reindex

// Test scaffolding for the OKF index projection, mirroring tests/conftest.py's
// fresh_ws / seed fixtures and tests/test_projection.py's seed_firm_ops.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
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

// seedFirmOps is test_projection.py's seed_firm_ops: five entities across five
// types, each carrying a title.
func seedFirmOps(t *testing.T, root string) {
	t.Helper()
	create(t, root, "person", "noor", "name", "Noor", "role", "partner", "title", "Noor P")
	create(t, root, "client", "initech", "name", "Initech", "title", "Initech")
	create(t, root, "opportunity", "initech-deal", "stage", "prospect",
		"client", "initech", "owner", "noor", "title", "Initech Deal")
	create(t, root, "project", "initech-pov", "client", "initech", "owner", "noor",
		"active", "true", "title", "Initech PoV")
	create(t, root, "meeting", "kickoff", "date", "2026-06-01T10:00:00",
		"call_type", "client", "source", "recording", "engagement", "initech-pov",
		"title", "Kickoff")
}

func firmOpsWS(t *testing.T) string {
	t.Helper()
	root := freshWS(t)
	seedFirmOps(t, root)
	return root
}

// live is test_projection.py's _live: (resolved, valid index, graph) over the
// live tree — the projection's input.
func live(t *testing.T, root string) (*schema.ResolvedSchema, *index.Index, *graph.Graph) {
	t.Helper()
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatalf("load schema: %v", err)
	}
	scanned, err := index.Build(root, resolved)
	if err != nil {
		t.Fatalf("build index: %v", err)
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned))
	return resolved, idx, graph.BuildGraph(idx)
}

func mustReindex(t *testing.T, root string, dryRun bool) *Result {
	t.Helper()
	result, err := Reindex(root, dryRun)
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	return result
}

func locatedCode(err error) string {
	var l *errs.Located
	if errors.As(err, &l) {
		return l.Code
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

// collectionPresetOntology and collectionPresetStorage make a preset with one
// collection-layout type — the shape the pre-0.6.0 build-hub's repos.yaml
// gave the collection tests. build-hub 0.6.0 ships no collection (every type
// is a directory of md files), so the schema lives here.
const collectionPresetOntology = `
version: "0.1.0"
ontology:
  entities:
    repo:
      attributes:
        repo: { type: text, required: true }
        status: { enum: [active, archived], required: true }
`

const collectionPresetStorage = `
storage:
  repo: { layout: collection, format: yaml, path: knowledge/architecture/repos.yaml }
`

// collectionWS scaffolds a workspace from the collection preset above, the
// way `init --preset-source` does.
func collectionWS(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	writeRaw(t, src, "collections/ontology.yaml", collectionPresetOntology)
	writeRaw(t, src, "collections/storage.yaml", collectionPresetStorage)
	root := t.TempDir()
	if _, err := workspace.Init("collections", root, workspace.InitOptions{PresetSource: src}); err != nil {
		t.Fatalf("init collections: %v", err)
	}
	return root
}
