package serve

// The parity harness pins khub's CLI bytes; it cannot drive HTTP, and an HTTP
// body has neither of the two properties that make the harness worth it (a
// whole-tree manifest and an exact exit code). So the transport contract is
// tested here instead — the guards first, because they are the whole security
// posture.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/viz"
	"github.com/endgame-build/khub/internal/workspace"
)

func TestAllowedHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{"127.0.0.1:7777", true},
		{"localhost:7777", true},
		{"[::1]:7777", true},
		{"127.0.0.1", true},
		{"localhost", true},
		// The rebinding case: an attacker domain re-resolved to loopback still
		// announces its own name here, which is the only thing that separates
		// it from a real local request.
		{"evil.example.com:7777", false},
		{"khub.attacker.test", false},
		// A bare IP that is not loopback cannot be us, since we bind 127.0.0.1.
		{"192.168.1.10:7777", false},
		{"", false},
	}
	for _, c := range cases {
		if got := AllowedHost(c.host); got != c.want {
			t.Errorf("AllowedHost(%q) = %v, want %v", c.host, got, c.want)
		}
	}
}

func TestGuardRejectsWrites(t *testing.T) {
	srv := httptest.NewServer(Handler(t.TempDir(), nil))
	defer srv.Close()

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		req, err := http.NewRequest(method, srv.URL+"/api/overview", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/overview = %d, want 405", method, resp.StatusCode)
		}
	}
}

func TestGuardRejectsForeignHost(t *testing.T) {
	srv := httptest.NewServer(Handler(t.TempDir(), nil))
	defer srv.Close()

	req, err := http.NewRequest("GET", srv.URL+"/api/overview", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign Host = %d, want 403", resp.StatusCode)
	}
}

func TestNoCORSHeaders(t *testing.T) {
	// Same-origin policy is the second layer under the Host check: it stops a
	// local page from READING a response it is allowed to send. One
	// Access-Control-Allow-Origin header would undo that.
	srv := httptest.NewServer(Handler(wsWithEntities(t), nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/overview")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, h := range []string{
		"Access-Control-Allow-Origin",
		"Access-Control-Allow-Credentials",
		"Access-Control-Allow-Methods",
	} {
		if v := resp.Header.Get(h); v != "" {
			t.Errorf("%s present (%q); serve must stay same-origin", h, v)
		}
	}
}

func TestPageCarriesCSP(t *testing.T) {
	srv := httptest.NewServer(Handler(t.TempDir(), nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("page CSP = %q, want default-src 'none' with script-src 'self'", csp)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("page missing nosniff")
	}
}

// `khub serve --aspect` prints the URL with ?aspect=, and the page reads it
// client-side. The server must answer that URL with the page itself — same
// bytes, same headers — whatever the value, malformed escapes included.
func TestAspectQueryServesThePage(t *testing.T) {
	srv := httptest.NewServer(Handler(t.TempDir(), nil))
	defer srv.Close()

	get := func(path string) (*http.Response, string) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp, string(body)
	}
	_, want := get("/")
	for _, path := range []string{"/?aspect=both", "/?aspect=schema", "/?aspect=instances", "/?aspect=%E0", "/?aspect=bogus"} {
		resp, body := get(path)
		if resp.StatusCode != http.StatusOK || body != want {
			t.Errorf("GET %s = %d with %d bytes, want the page (%d bytes)", path, resp.StatusCode, len(body), len(want))
		}
		if resp.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("GET %s lost the CSP", path)
		}
	}
}

func TestUnknownPathIs404(t *testing.T) {
	srv := httptest.NewServer(Handler(t.TempDir(), nil))
	defer srv.Close()

	for _, path := range []string{"/nope", "/static/nope.js", "/api/nope"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestOverviewEndpoint(t *testing.T) {
	srv := httptest.NewServer(Handler(wsWithEntities(t), nil))
	defer srv.Close()

	var payload struct {
		Types []struct {
			Name  string
			Count int
		}
		Relations []struct {
			From, Predicate    string
			Links, Have, Total int
		}
	}
	getJSON(t, srv.URL+"/api/overview", &payload)
	counts := map[string]int{}
	for _, ty := range payload.Types {
		counts[ty.Name] = ty.Count
	}
	if counts["client"] != 1 || counts["opportunity"] != 1 {
		t.Errorf("counts = %v", counts)
	}
	for _, r := range payload.Relations {
		if r.From == "opportunity" && r.Predicate == "client" && (r.Links != 1 || r.Have != 1 || r.Total != 1) {
			t.Errorf("opportunity.client = %+v", r)
		}
	}
}

func TestOverviewRebuildsPerRequest(t *testing.T) {
	// The no-cache claim: an entity written after the server started must
	// appear without a restart, because nothing is held between requests.
	root := wsWithEntities(t)
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()

	people := func() int {
		var payload struct {
			Types []struct {
				Name  string
				Count int
			}
		}
		getJSON(t, srv.URL+"/api/overview", &payload)
		for _, ty := range payload.Types {
			if ty.Name == "person" {
				return ty.Count
			}
		}
		t.Fatal("no person type")
		return 0
	}
	before := people()
	if _, err := entity.Create(root, "person", entity.CreateOpts{
		Fields: pairs("title", "Added Later"), ID: "added-later", UseTemplate: true,
	}); err != nil {
		t.Fatal(err)
	}
	if after := people(); after != before+1 {
		t.Errorf("people %d -> %d, want one more", before, after)
	}
}

func TestRecentRefusesABadWindow(t *testing.T) {
	srv := httptest.NewServer(Handler(wsWithEntities(t), nil))
	defer srv.Close()
	for _, days := range []string{"-1", "x", "99999999999999999999"} {
		resp, err := http.Get(srv.URL + "/api/recent?days=" + days)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("days=%s = %d, want 400", days, resp.StatusCode)
		}
	}
}

// One file that does not parse must not take the page with it: every view
// answers, and names the file.
func TestViewsSurviveABrokenFile(t *testing.T) {
	root := wsWithEntities(t)
	if err := os.WriteFile(filepath.Join(root, "clients", "broken.md"), []byte("---\ntype: client\ntitle: [unclosed\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()
	for _, path := range []string{"/api/overview", "/api/rows/client", "/api/entity/client/acme", "/api/recent", "/api/search?q=acme"} {
		var payload struct{ Malformed []string }
		getJSON(t, srv.URL+path, &payload)
		if len(payload.Malformed) != 1 || !strings.HasSuffix(payload.Malformed[0], "broken.md") {
			t.Errorf("%s malformed = %v", path, payload.Malformed)
		}
	}
	var found struct {
		Passed  bool
		Buckets []struct{ Bucket string }
	}
	getJSON(t, srv.URL+"/api/findings", &found)
	if found.Passed || len(found.Buckets) == 0 || found.Buckets[0].Bucket != "malformed" {
		t.Errorf("findings = %+v", found)
	}
}

func TestSchemaEndpointNamesBaseRelations(t *testing.T) {
	srv := httptest.NewServer(Handler(wsWithEntities(t), nil))
	defer srv.Close()

	var payload struct {
		Workspace string `json:"workspace"`
		Types     []struct {
			Name      string `json:"name"`
			Relations []struct {
				Predicate string   `json:"predicate"`
				Targets   []string `json:"targets"`
			} `json:"relations"`
		} `json:"types"`
		BaseRelations []struct {
			Predicate string `json:"predicate"`
		} `json:"base_relations"`
	}
	getJSON(t, srv.URL+"/api/schema", &payload)

	if len(payload.Types) == 0 {
		t.Fatal("no types")
	}
	if payload.Workspace == "" || strings.ContainsAny(payload.Workspace, `/\\`) {
		t.Errorf("workspace = %q, want its name and never a path", payload.Workspace)
	}
	// The page hides these by default; if they stopped being reported it would
	// silently start drawing a complete graph instead.
	base := map[string]bool{}
	for _, r := range payload.BaseRelations {
		base[r.Predicate] = true
	}
	for _, want := range []string{"related", "references", "depends_on"} {
		if !base[want] {
			t.Errorf("base_relations missing %q", want)
		}
	}
}

func TestAdversarialSlugStaysEncoded(t *testing.T) {
	// Entity text is attacker-influenceable, and a list row carries a
	// title, so the response carries authored text verbatim.
	//
	// What is asserted here is what matters on THIS transport: the response is
	// valid JSON and the value round-trips exactly. It is deliberately NOT the
	// "no </script>" check that viz carries — serve answers
	// application/json + nosniff and the page consumes it through fetch and
	// JSON.parse, so the bytes never reach an HTML parser. viz inlines the same
	// document into a <script> element, which is a different hazard on a
	// different transport; TestRenderHTMLEscapesScriptClose covers that one.
	// Escaping here instead would be defending a threat this path does not have
	// while diverging serve's bytes from canon's.
	root := wsFor(t)
	hostile := `</script><img src=x onerror=alert(1)>`
	if _, err := entity.Create(root, "person", entity.CreateOpts{
		Fields: pairs("title", hostile),
		ID:     "xss-probe", UseTemplate: true,
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/rows/person")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body := readAll(t, resp)
	var payload struct {
		Rows []struct{ ID, Title string } `json:"rows"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	var got string
	for _, n := range payload.Rows {
		if n.ID == "person/xss-probe" {
			got = n.Title
		}
	}
	if got != hostile {
		t.Errorf("title round-tripped as %q, want the original %q", got, hostile)
	}
}

// --- helpers -----------------------------------------------------------------

func wsFor(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := workspace.Init("firm-ops", root, workspace.InitOptions{}); err != nil {
		t.Fatalf("init firm-ops: %v", err)
	}
	return root
}

// wsWithEntities is two entities and one edge between them — enough for the
// endpoint shapes without pinning any preset's own contents.
func wsWithEntities(t *testing.T) string {
	t.Helper()
	root := wsFor(t)
	if _, err := entity.Create(root, "client", entity.CreateOpts{
		Fields: pairs("title", "Acme"), ID: "acme", UseTemplate: true,
	}); err != nil {
		t.Fatalf("create client: %v", err)
	}
	if _, err := entity.Create(root, "opportunity", entity.CreateOpts{
		Fields: pairs("title", "Acme Deal", "client", "acme", "stage", "prospect"),
		ID:     "acme-deal", UseTemplate: true,
	}); err != nil {
		t.Fatalf("create opportunity: %v", err)
	}
	return root
}

func pairs(kv ...string) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(kv); i += 2 {
		m.Set(kv[i], kv[i+1])
	}
	return m
}

func getJSON(t *testing.T, url string, into any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, readAll(t, resp))
	}
	if err := json.Unmarshal([]byte(readAll(t, resp)), into); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return sb.String()
}

func TestUIHasNoMarkupSinks(t *testing.T) {
	// The page renders entity slugs, frontmatter and bodies, all of which are
	// attacker-influenceable by construction. dom.js is the only module that
	// creates elements and it assigns through textContent throughout; this is
	// what keeps that true as the tree grows.
	//
	// The CSP would stop an injected script at runtime. This stops it at build
	// time, which is the cheaper place. A strict literal scan with no comment
	// stripping — a gate that can be talked out of a match is not a gate, so
	// the UI's own comments are worded to name none of these.
	banned := []string{
		"innerHTML", "outerHTML", "insertAdjacentHTML",
		"document.write", "eval(", "new Function", "srcdoc",
	}
	err := fs.WalkDir(uiFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext != ".js" && ext != ".html" {
			return nil
		}
		body, err := uiFS.ReadFile(path)
		if err != nil {
			return err
		}
		for _, sink := range banned {
			if strings.Contains(string(body), sink) {
				t.Errorf("%s contains %q — build the DOM through dom.js instead", path, sink)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUIScriptsAreExternal(t *testing.T) {
	// Every <script> must carry a src. An inline script would be blocked by
	// script-src 'self' at runtime and silently do nothing, which is a much
	// worse way to find out than a red test.
	body, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range strings.Split(string(body), "<script")[1:] {
		open := strings.SplitN(tag, ">", 2)
		if len(open) < 2 {
			t.Fatalf("unterminated <script tag: %.40s", tag)
		}
		if !strings.Contains(open[0], "src=") {
			t.Errorf("inline <script%s> — the CSP forbids it; move it to a file under ui/", open[0])
		}
	}
}

func TestListenRefusesBusyPort(t *testing.T) {
	// The one refusal in serve the parity harness cannot reach: the CLI's TTY
	// gate fires before Listen, so `serve --port N` under a pipe never gets as
	// far as binding and the fixture records serve_needs_tty instead. Without
	// this test errs.PortInUse ships with no coverage at all.
	first, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	port := first.Addr().(*net.TCPAddr).Port

	_, err = Listen(port)
	var located *errs.Located
	if !errors.As(err, &located) {
		t.Fatalf("second Listen on %d = %v, want a located refusal", port, err)
	}
	if located.Code != "port_in_use" {
		t.Errorf("code = %q, want port_in_use", located.Code)
	}
	// An agent's next move has to be in the message: the cause is another
	// process, which the workspace cannot explain.
	if !strings.Contains(located.Message, "--port") {
		t.Errorf("message %q does not name the corrective flag", located.Message)
	}
}

func TestPaletteMatchesStylesheet(t *testing.T) {
	// viz.Palette (Go, for the static artifact) and app.css's --graph-N (CSS,
	// for the served page) are the same ten colours written twice, because a
	// stylesheet cannot read a Go slice. This is the pin that keeps them equal
	// — a comment asking two files to stay in step is how they stop being in
	// step, and a type being teal in one surface and purple in the other is
	// exactly the drift one palette was chosen to prevent.
	css, err := uiFS.ReadFile("ui/app.css")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range viz.Palette {
		decl := fmt.Sprintf("--graph-%d: %s;", i+1, want)
		if !strings.Contains(string(css), decl) {
			t.Errorf("app.css is missing %q (viz.Palette[%d])", decl, i)
		}
	}
}

func TestListenRefusesPrivilegedPort(t *testing.T) {
	// EACCES used to fall through to a generic os_error, which tells an
	// operator nothing about what to do next. Same class as a taken port: the
	// cause is the environment, not the workspace, so it names --port too.
	if os.Geteuid() == 0 {
		t.Skip("running as root: binding port 80 would succeed")
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no privileged ports: binding port 80 succeeds")
	}
	_, err := Listen(80)
	var located *errs.Located
	if !errors.As(err, &located) {
		t.Fatalf("Listen(80) = %v, want a located refusal", err)
	}
	if located.Code != "port_not_permitted" {
		t.Errorf("code = %q, want port_not_permitted", located.Code)
	}
	if !strings.Contains(located.Message, "--port") {
		t.Errorf("message %q does not name the corrective flag", located.Message)
	}
}

func TestSchemaEdgesMatchTheCLI(t *testing.T) {
	// /api/schema's `edges` is introspect.EdgesView — the same projection
	// `khub schema edges` prints. Serving it rather than re-deriving it on the
	// page is the whole reason the browser view and the CLI cannot disagree
	// about the ontology, so that equivalence is worth asserting rather than
	// assuming.
	root := wsWithEntities(t)
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()

	var payload struct {
		Edges []map[string]any `json:"edges"`
	}
	getJSON(t, srv.URL+"/api/schema", &payload)
	if len(payload.Edges) == 0 {
		t.Fatal("no ontology edges served")
	}

	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := canon.EncodeCLI(introspect.EdgesView(resolved))
	if err != nil {
		t.Fatal(err)
	}
	// Row count, not bytes: a JSON round trip through the test's map loses
	// omap's key order, which is the one thing a byte compare would catch and
	// the one thing canon already guarantees on the way out.
	if n := strings.Count(want, `"predicate"`); len(payload.Edges) != n {
		t.Errorf("served %d edges, EdgesView has %d", len(payload.Edges), n)
	}

	// Every row carries the keys the schema view draws from.
	for _, e := range payload.Edges {
		for _, k := range []string{"predicate", "from", "to", "kind", "many", "required", "inverse", "acyclic", "derived"} {
			if _, ok := e[k]; !ok {
				t.Errorf("edge row missing %q: %v", k, e)
				break
			}
		}
	}
}

func TestApiType(t *testing.T) {
	srv := httptest.NewServer(Handler(wsWithEntities(t), nil))
	defer srv.Close()

	var view map[string]any
	getJSON(t, srv.URL+"/api/type/client", &view)
	for _, k := range []string{"name", "layout", "fields", "relations"} {
		if _, ok := view[k]; !ok {
			t.Errorf("type view missing %q", k)
		}
	}

	// An undeclared name is a refusal that names what does exist.
	resp, err := http.Get(srv.URL + "/api/type/nosuchtype")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown type = %d, want 400", resp.StatusCode)
	}
}

func TestEntityEndpointSerializesResolvedTargetsAsArrays(t *testing.T) {
	srv := httptest.NewServer(Handler(wsWithEntities(t), nil))
	defer srv.Close()
	var payload struct {
		Edges []struct {
			Predicate       string   `json:"predicate"`
			ResolvedTargets []string `json:"resolved_targets"`
		} `json:"edges"`
	}
	getJSON(t, srv.URL+"/api/entity/opportunity/acme-deal", &payload)
	for _, edge := range payload.Edges {
		if edge.Predicate == "client" {
			if len(edge.ResolvedTargets) != 1 || edge.ResolvedTargets[0] != "client/acme" {
				t.Fatalf("resolved targets = %v", edge.ResolvedTargets)
			}
			return
		}
	}
	t.Fatal("client edge missing")
}

// One entity's history and reach and one saved query answer, and refuse what
// they cannot name in the CLI's envelope.
func TestEntityAndQueryEndpoints(t *testing.T) {
	root := wsWithEntities(t)
	if err := os.WriteFile(filepath.Join(root, ".khub", "queries.yaml"), []byte("deals:\n  type: opportunity\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Handler(root, nil))
	defer srv.Close()

	var history struct {
		ID      string
		Note    string
		Commits []any
	}
	getJSON(t, srv.URL+"/api/history/opportunity/acme-deal", &history)
	if history.ID != "opportunity/acme-deal" || history.Note == "" || len(history.Commits) != 0 {
		t.Errorf("history = %+v", history)
	}
	var reach struct {
		ID      string
		In, Out []any
	}
	getJSON(t, srv.URL+"/api/reach/client/acme", &reach)
	if reach.ID != "client/acme" {
		t.Errorf("reach = %+v", reach)
	}
	var query struct {
		Matched int
		Rows    []struct{ ID string }
	}
	getJSON(t, srv.URL+"/api/query/deals", &query)
	if query.Matched != 1 || len(query.Rows) != 1 || query.Rows[0].ID != "opportunity/acme-deal" {
		t.Errorf("query = %+v", query)
	}
	for _, path := range []string{"/api/history/", "/api/reach/", "/api/query/", "/api/history/client/nope", "/api/reach/client/nope", "/api/query/nope"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body := readAll(t, resp)
		resp.Body.Close()
		if resp.StatusCode < 400 || !strings.Contains(body, `"code"`) {
			t.Errorf("GET %s = %d %s", path, resp.StatusCode, body)
		}
	}
}

// The findings are check's, the ones only the command line can see included.
func TestFindingsCarryTheShadows(t *testing.T) {
	root := wsWithEntities(t)
	srv := httptest.NewServer(Handler(root, func(*schema.ResolvedSchema) []integrity.FlagShadow {
		return []integrity.FlagShadow{{Type: "client", Field: "by", Commands: []string{"add"}}}
	}))
	defer srv.Close()
	var payload struct {
		Passed  bool
		Buckets []struct{ Bucket string }
	}
	getJSON(t, srv.URL+"/api/findings", &payload)
	if payload.Passed || !slices.ContainsFunc(payload.Buckets, func(b struct{ Bucket string }) bool { return b.Bucket == "flag_shadowed" }) {
		t.Errorf("findings = %+v", payload)
	}
}
