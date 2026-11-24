package serve

// The explorer's routes: /api/rows/<type> and the fields /api/entity/<id>
// appends after the `get` record. Shapes are pinned here; the numbers themselves are
// held against the CLI in internal/explore/agree_test.go.

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
)

func status(t *testing.T, method, url, host string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, readAll(t, resp)
}

func errorCode(t *testing.T, body string) string {
	t.Helper()
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("not an error envelope: %q", body)
	}
	return env.Error.Code
}

func TestRowsEndpointShape(t *testing.T) {
	srv := httptest.NewServer(Handler(wsWithEntities(t), nil))
	defer srv.Close()
	var set struct {
		Type       string           `json:"type"`
		Total      int              `json:"total"`
		Attributes []map[string]any `json:"attributes"`
		Relations  []map[string]any `json:"relations"`
		Rows       []map[string]any `json:"rows"`
	}
	getJSON(t, srv.URL+"/api/rows/opportunity", &set)
	if set.Type != "opportunity" || set.Total != 1 || len(set.Rows) != 1 {
		t.Fatalf("rowset = %+v", set)
	}
	for _, c := range set.Relations {
		for _, k := range []string{"key", "label", "direction", "predicate", "peer_types", "base"} {
			if _, ok := c[k]; !ok {
				t.Errorf("column missing %q: %v", k, c)
			}
		}
	}
	row := set.Rows[0]
	for _, k := range []string{"id", "slug", "title", "draft", "stale", "orphan", "incomplete", "trust", "attrs", "counts", "links"} {
		if _, ok := row[k]; !ok {
			t.Errorf("row missing %q", k)
		}
	}
	if row["id"] != "opportunity/acme-deal" {
		t.Fatalf("row id = %v", row["id"])
	}
	counts := row["counts"].(map[string]any)
	if counts["out:client"] != float64(1) {
		t.Fatalf("out:client = %v", counts["out:client"])
	}
	links := row["links"].(map[string]any)
	if l, ok := links["out:client"].([]any); !ok || len(l) != 1 || l[0] != "client/acme" {
		t.Fatalf("links out:client = %v (must be an array)", links["out:client"])
	}
}

func TestRowsEndpointRefusals(t *testing.T) {
	root := wsWithEntities(t)
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()

	// An undeclared type: the same refusal /api/type gives.
	code, body := status(t, "GET", srv.URL+"/api/rows/nosuchtype", "")
	if code != http.StatusBadRequest || errorCode(t, body) != "unknown_type" {
		t.Errorf("unknown type = %d %s", code, body)
	}
	// Still read-only, still loopback-only.
	if code, _ := status(t, "POST", srv.URL+"/api/rows/client", ""); code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", code)
	}
	if code, _ := status(t, "GET", srv.URL+"/api/rows/client", "evil.example.com"); code != http.StatusForbidden {
		t.Errorf("foreign Host = %d, want 403", code)
	}
	if code, body := status(t, "HEAD", srv.URL+"/api/rows/client", ""); code != http.StatusOK || body != "" {
		t.Errorf("HEAD = %d with body %q", code, body)
	}
	if code, _ := status(t, "GET", srv.URL+"/api/rows/", ""); code != http.StatusNotFound {
		t.Errorf("no type = %d, want 404", code)
	}
}

// A schema that does not load is the CLI's own refusal, not a 500.
func TestRowsEndpointBrokenSchema(t *testing.T) {
	root := wsWithEntities(t)
	if err := os.WriteFile(filepath.Join(root, ".khub", "ontology.yaml"),
		[]byte("ontology:\n  entities:\n    client: [this, is, not, a, type]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()
	code, body := status(t, "GET", srv.URL+"/api/rows/client", "")
	if code != http.StatusBadRequest || errorCode(t, body) == "" || errorCode(t, body) == "internal_error" {
		t.Errorf("broken schema = %d %s", code, body)
	}
}

func TestRowsEndpointHostileTitle(t *testing.T) {
	root := wsFor(t)
	hostile := `</script><img src=x onerror=alert(1)>"&'`
	if _, err := entity.Create(root, "client", entity.CreateOpts{
		Fields: pairs("title", hostile), ID: "evil", UseTemplate: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()
	code, body := status(t, "GET", srv.URL+"/api/rows/client", "")
	if code != http.StatusOK {
		t.Fatalf("rows = %d", code)
	}
	// The title leaves as a JSON string (served as application/json with
	// nosniff, never as markup) and comes back byte for byte.
	var set struct {
		Rows []struct {
			Title string `json:"title"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(body), &set); err != nil || len(set.Rows) != 1 || set.Rows[0].Title != hostile {
		t.Fatalf("title round trip: %v %+v", err, set)
	}
}

func TestEntityEndpointExplorerFields(t *testing.T) {
	root := wsWithEntities(t)
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()
	_, body := status(t, "GET", srv.URL+"/api/entity/opportunity/acme-deal", "")
	// Existing fields first, unchanged in order; the explorer's appended.
	want := []string{`"id"`, `"type"`, `"slug"`, `"path"`, `"frontmatter"`, `"body"`, `"edges"`, `"markers"`, `"body_tree"`, `"neighbours"`}
	at := -1
	for _, k := range want {
		i := strings.Index(body, k+":")
		if i <= at {
			t.Fatalf("key %s out of order in %s", k, body)
		}
		at = i
	}
	var page struct {
		Markers    map[string]any   `json:"markers"`
		BodyTree   []map[string]any `json:"body_tree"`
		Neighbours []struct {
			Key   string           `json:"key"`
			Label string           `json:"label"`
			Items []map[string]any `json:"items"`
		} `json:"neighbours"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if page.Markers["trust"] == nil {
		t.Fatalf("markers = %v", page.Markers)
	}
	if len(page.Neighbours) == 0 || page.Neighbours[0].Key != "out:client" || page.Neighbours[0].Items[0]["id"] != "client/acme" {
		t.Fatalf("neighbours = %+v", page.Neighbours)
	}
}

func TestEntityEndpointRefusals(t *testing.T) {
	root := wsWithEntities(t)
	// The same bare slug on two types is ambiguous.
	if _, err := entity.Create(root, "person", entity.CreateOpts{
		Fields: pairs("title", "Acme Person", "name", "Acme Person", "role", "manager"), ID: "acme", UseTemplate: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()
	code, body := status(t, "GET", srv.URL+"/api/entity/acme", "")
	if code != http.StatusConflict || errorCode(t, body) != "ambiguity_error" {
		t.Errorf("ambiguous = %d %s", code, body)
	}
	code, body = status(t, "GET", srv.URL+"/api/entity/client/nobody", "")
	if code != http.StatusNotFound || errorCode(t, body) != "lookup_error" {
		t.Errorf("missing = %d %s", code, body)
	}
	if code, body := status(t, "HEAD", srv.URL+"/api/entity/client/acme", ""); code != http.StatusOK || body != "" {
		t.Errorf("HEAD = %d %q", code, body)
	}
}

// A body's markup reaches the page only as text blocks.
func TestEntityEndpointBodyIsText(t *testing.T) {
	root := wsFor(t)
	if _, err := entity.Create(root, "client", entity.CreateOpts{
		Fields: pairs("title", "Body"), ID: "body",
		Body: "Hi <script>alert(1)</script>\n\n[x](javascript:alert(2))\n",
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()
	var page struct {
		BodyTree []json.RawMessage `json:"body_tree"`
	}
	getJSON(t, srv.URL+"/api/entity/client/body", &page)
	tree := ""
	for _, b := range page.BodyTree {
		tree += string(b)
	}
	if !strings.Contains(tree, `"k": "text"`) || !strings.Contains(tree, `alert(1)`) {
		t.Fatalf("script must arrive as a text block: %s", tree)
	}
	if strings.Contains(tree, "javascript:") {
		t.Fatalf("a javascript: href must be dropped: %s", tree)
	}
}

// The page's modules revalidate on every load, so an upgraded binary never
// runs beside a cached module from the previous one.
func TestUIAssetsRevalidate(t *testing.T) {
	srv := httptest.NewServer(Handler(t.TempDir(), nil))
	defer srv.Close()
	for _, name := range []string{"app.js", "route.js", "app.css"} {
		resp, err := http.Get(srv.URL + "/static/" + name)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-cache" {
			t.Errorf("%s: %d, Cache-Control %q", name, resp.StatusCode, resp.Header.Get("Cache-Control"))
		}
	}
}

// A stray — a file in one type's folder declaring another type — is read by
// `get` but is no node of the graph. The page is told so outright, and is
// never handed another entity's connections in its place.
func TestEntityEndpointStrayIsExplicit(t *testing.T) {
	root := wsWithEntities(t)
	acme, err := entity.Get(root, "client/acme", false)
	if err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(filepath.Dir(acme.Path), "notes.md")
	if err := os.WriteFile(stray, []byte("---\ntype: person\ntitle: Notes\n---\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()
	var page struct {
		Stray      bool              `json:"stray"`
		Markers    map[string]any    `json:"markers"`
		Neighbours []json.RawMessage `json:"neighbours"`
	}
	getJSON(t, srv.URL+"/api/entity/client/notes", &page)
	if !page.Stray || page.Markers != nil || len(page.Neighbours) != 0 {
		t.Fatalf("stray page = %+v", page)
	}
	var real map[string]any
	getJSON(t, srv.URL+"/api/entity/client/acme", &real)
	if _, has := real["stray"]; has {
		t.Fatal("an entity of the graph carries no stray flag")
	}
}

// The page is a graph of ES modules the browser links at load. A renamed or
// removed export, or an import of a deleted file, leaves the page blank while
// every other test stays green — so every relative import is checked here:
// the file exists in the binary and exports each name imported from it.
func TestUIModuleImportsResolve(t *testing.T) {
	importRE := regexp.MustCompile(`(?m)^import\s+(\*\s+as\s+\w+|\{([^}]*)\})\s+from\s+'\./([^']+)';`)
	files, err := fs.Glob(uiFS, "ui/*.js")
	if err != nil || len(files) == 0 {
		t.Fatalf("no ui modules: %v", err)
	}
	for _, f := range files {
		src, err := uiFS.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range importRE.FindAllStringSubmatch(string(src), -1) {
			target, err := uiFS.ReadFile("ui/" + m[3])
			if err != nil {
				t.Errorf("%s imports ./%s, which is not in the binary", f, m[3])
				continue
			}
			for _, name := range strings.Split(m[2], ",") {
				name = strings.TrimSpace(name)
				if name == "" {
					continue
				}
				decl := regexp.MustCompile(`(?m)^export (function|var) ` + regexp.QuoteMeta(name) + `\b`)
				if !decl.Match(target) {
					t.Errorf("%s imports %s from ./%s, which does not export it", f, name, m[3])
				}
			}
		}
	}
	page, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), `src="/static/app.js"`) {
		t.Fatal("index.html no longer loads app.js")
	}
}

// A refusal carries the CLI envelope's machine fields — the page offers an
// ambiguous id's candidates and an unknown type's near miss from them — and a
// plain refusal carries neither.
func TestRefusalCarriesMachineFields(t *testing.T) {
	root := wsWithEntities(t)
	if _, err := entity.Create(root, "person", entity.CreateOpts{
		Fields: pairs("title", "Acme Person", "name", "Acme Person", "role", "manager"), ID: "acme", UseTemplate: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()
	type envelope struct {
		Error map[string]json.RawMessage `json:"error"`
	}
	decode := func(body string) map[string]json.RawMessage {
		var e envelope
		if err := json.Unmarshal([]byte(body), &e); err != nil {
			t.Fatalf("not an envelope: %s", body)
		}
		return e.Error
	}
	_, body := status(t, "GET", srv.URL+"/api/entity/acme", "")
	var expected []string
	if err := json.Unmarshal(decode(body)["expected"], &expected); err != nil ||
		len(expected) != 2 || expected[0] != "client/acme" || expected[1] != "person/acme" {
		t.Errorf("ambiguous expected = %s (%v)", decode(body)["expected"], err)
	}
	_, body = status(t, "GET", srv.URL+"/api/rows/clinet", "")
	e := decode(body)
	if err := json.Unmarshal(e["expected"], &expected); err != nil || !containsStr(expected, "client") {
		t.Errorf("unknown type expected = %s", e["expected"])
	}
	if string(e["suggestion"]) != `"client"` {
		t.Errorf("unknown type suggestion = %s", e["suggestion"])
	}
	_, body = status(t, "GET", srv.URL+"/api/rows/", "")
	e = decode(body)
	if _, has := e["expected"]; has {
		t.Errorf("a plain refusal has no expected: %s", body)
	}
	if _, has := e["suggestion"]; has {
		t.Errorf("a plain refusal has no suggestion: %s", body)
	}
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// Every API answer, success or refusal, is no-store: each is rebuilt from the
// tree, and a cached one is the stale view the design refuses to show. The
// vendored library is the deliberate exception that keeps browser caching.
func TestAPIResponsesAreNotCached(t *testing.T) {
	srv := httptest.NewServer(Handler(wsWithEntities(t), nil))
	defer srv.Close()
	for _, p := range []string{"/api/rows/client", "/api/entity/client/acme", "/api/overview", "/api/findings", "/api/recent", "/api/queries", "/api/search?q=acme", "/api/schema", "/api/rows/nosuchtype"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: Cache-Control %q, nosniff %q", p, resp.Header.Get("Cache-Control"), resp.Header.Get("X-Content-Type-Options"))
		}
	}
	resp, err := http.Get(srv.URL + "/static/cytoscape.min.js")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Cache-Control") != "" {
		t.Errorf("the vendored library keeps default caching, got %q", resp.Header.Get("Cache-Control"))
	}
}
