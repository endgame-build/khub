package retrieve

// Shared fixtures for the retrieve tests: a workspace built from an inline
// schema, exactly as the graph and search tests build theirs, so the walk
// under test is pinned by a graph this file draws rather than by a preset
// that may move.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

type kv struct {
	K string
	V any
}

// preset is a small typed graph: a `need` is realized in a `part`, parts
// depend on parts, and a `note` may affect anything.
const preset = `
version: "0.1.0"
ontology:
  entities:
    need:
      attributes: { title: {} }
      relations:
        realized_in: { to: part, many: true, inverse: realizes }
    part:
      attributes: { title: {} }
      relations:
        depends_on: { to: part, many: true, acyclic: true }
    note:
      attributes: { title: {} }
      relations:
        affects: { to: any, many: true }
storage:
  need: { layout: file, path: needs }
  part: { layout: file, path: parts }
  note: { layout: file, path: notes }
`

func wsFromSchema(t *testing.T, presetYAML string) string {
	t.Helper()
	root := t.TempDir()
	khub := filepath.Join(root, ".khub")
	mkdirAll(t, khub)
	writeFile(t, filepath.Join(khub, "ontology.yaml"), presetYAML)
	cfg := omap.New()
	cfg.Set("name", "ws")
	cfg.Set("preset", "fixture")
	cfg.Set("version", "0.0.0")
	cfg.Set("source", nil)
	defaults := omap.New()
	defaults.Set("stale_days", int64(90))
	cfg.Set("defaults", defaults)
	cfgText, err := canon.DumpWide(cfg)
	if err != nil {
		t.Fatalf("dump config: %v", err)
	}
	writeFile(t, filepath.Join(khub, "config.yaml"), cfgText)
	return root
}

// seed writes one md entity file with the given frontmatter and body.
func seed(t *testing.T, root, relpath, body string, fields ...kv) {
	t.Helper()
	meta := omap.New()
	for _, f := range fields {
		meta.Set(f.K, f.V)
	}
	text, err := canon.DumpWide(meta)
	if err != nil {
		t.Fatalf("dump %s: %v", relpath, err)
	}
	p := filepath.Join(root, filepath.FromSlash(relpath))
	mkdirAll(t, filepath.Dir(p))
	writeFile(t, p, "---\n"+text+"---\n"+body)
}

func mkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ptr[T any](v T) *T { return &v }

// graphWS is the fixture every walk test reads:
//
//	need/auth --realized_in--> part/api --depends_on--> part/db
//	part/web  --depends_on--> part/api
//	note/jwt  --affects--> part/api, part/web
//	need/lonely (no edges)
//
// Bodies carry the words the search tests look for.
func graphWS(t *testing.T) string {
	t.Helper()
	ws := wsFromSchema(t, preset)
	seed(t, ws, "needs/auth.md", "Every call carries a token.\n",
		kv{"type", "need"}, kv{"title", "Auth required"}, kv{"realized_in", []any{"api"}})
	seed(t, ws, "needs/lonely.md", "Nothing links here, token or not.\n",
		kv{"type", "need"}, kv{"title", "Lonely need"})
	seed(t, ws, "parts/api.md", "The API service.\n",
		kv{"type", "part"}, kv{"title", "API"}, kv{"depends_on", []any{"db"}})
	seed(t, ws, "parts/web.md", "The web front.\n",
		kv{"type", "part"}, kv{"title", "Web"}, kv{"depends_on", []any{"api"}})
	seed(t, ws, "parts/db.md", "The database.\n",
		kv{"type", "part"}, kv{"title", "Database"})
	seed(t, ws, "notes/jwt.md", "Use JWT for the token.\n",
		kv{"type", "note"}, kv{"title", "Use JWT"}, kv{"affects", []any{"api", "web"}})
	return ws
}

func mustRetrieve(t *testing.T, root string, opts Options) *Result {
	t.Helper()
	res, err := Retrieve(root, opts)
	if err != nil {
		t.Fatalf("Retrieve(%+v): %v", opts, err)
	}
	return res
}

func ids(res *Result) []string {
	out := make([]string, 0, len(res.Records))
	for _, r := range res.Records {
		out = append(out, r.Node.ID())
	}
	return out
}

func eqSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
