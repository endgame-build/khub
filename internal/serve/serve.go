// Package serve is `khub serve`: a read-only HTTP view of the workspace graph,
// bound to loopback, rebuilding from the live files on every request.
//
// It is the live counterpart to viz — same graph, same Cytoscape elements,
// same embedded library — where viz writes one self-contained file for sharing
// and printing. Both call viz.Graph; only the transport differs.
//
// Read-only is structural, not a policy: there are no write endpoints, and the
// guard rejects every method but GET and HEAD before routing, so adding one
// has to be a deliberate act. The page composes `khub link` / `khub unlink`
// commands for the operator to run, which keeps every mutation on khub's
// existing validated write path.
package serve

import (
	"context"
	"embed"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/endgame-build/khub/internal/assets"
	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/viz"
	"github.com/endgame-build/khub/internal/workspace"
)

//go:embed ui
var uiFS embed.FS

// DefaultPort is the port `khub serve` binds unless --port says otherwise.
// Fixed rather than ephemeral so the URL stays bookmarkable across runs; a
// collision is a refusal naming --port, not a silent hop to another number.
const DefaultPort = 7777

// contentSecurityPolicy is the page's CSP.
//
// Entity bodies are attacker-influenceable text (cli rules: `get`/`search`
// output is untrusted by construction) and the page renders them. 'self' for
// scripts and styles means an injected <script> in an entity body cannot
// execute even if a future edit renders content as HTML instead of text; the
// page's own code uses textContent throughout so it never gets that far.
const contentSecurityPolicy = "default-src 'none'; " +
	"script-src 'self'; style-src 'self'; connect-src 'self'; " +
	"img-src 'self' data:; base-uri 'none'; form-action 'none'; " +
	// frame-ancestors does NOT fall back to default-src, so without it any
	// local page could frame this one. Read-only makes that clickjacking at
	// worst, but the guard is meant to be the whole posture.
	"frame-ancestors 'none'"

type server struct{ root string }

// Handler builds the routed, guarded handler. Exported so tests drive the real
// routing and the real guards through httptest without binding a port.
func Handler(root string) http.Handler {
	s := &server{root: root}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.page)
	mux.HandleFunc("/static/", s.static)
	mux.HandleFunc("/api/graph", s.apiGraph)
	mux.HandleFunc("/api/schema", s.apiSchema)
	mux.HandleFunc("/api/entity/", s.apiEntity)
	mux.HandleFunc("/api/type/", s.apiType)
	return guard(mux)
}

// Listen binds loopback at port and reports the address. Split from Serve so
// the caller can print the real URL before the server blocks, and so a bind
// failure is a khub refusal rather than a log line.
func Listen(port int) (net.Listener, error) {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		switch {
		case errors.Is(err, syscall.EADDRINUSE):
			return nil, errs.PortInUse(port)
		case errors.Is(err, syscall.EACCES):
			// Below 1024 without privileges. Same class as a taken port — the
			// cause is the environment, which the workspace cannot explain —
			// so it gets the same corrective flag rather than a bare os_error.
			return nil, errs.PortNotPermitted(port)
		}
		return nil, err
	}
	return ln, nil
}

// Serve runs until ctx is cancelled, then drains in-flight requests.
func Serve(ctx context.Context, ln net.Listener, root string) error {
	srv := &http.Server{
		Handler:           Handler(root),
		ReadHeaderTimeout: 5 * time.Second,
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

// guard is the whole security posture, applied before routing.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fail(w, http.StatusMethodNotAllowed, "read_only",
				"khub serve is read-only; it exposes no write endpoint")
			return
		}
		if !AllowedHost(r.Host) {
			// DNS rebinding: an attacker domain re-resolved to 127.0.0.1 still
			// carries its own name in Host, so this is what stops any page on
			// the machine from reading the workspace. Without it, loopback
			// binding alone proves nothing.
			fail(w, http.StatusForbidden, "bad_host",
				"Unrecognized Host header; khub serve answers only on loopback names")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AllowedHost reports whether a Host header names this machine's loopback.
//
// The port is deliberately not checked: a rebound request carries the
// attacker's hostname whatever port it targets, so the hostname is the whole
// signal, and ignoring the port keeps the check honest under an ephemeral
// bind.
func AllowedHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	switch name {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		fail(w, http.StatusNotFound, "not_found", "No such path")
		return
	}
	body, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		fail(w, http.StatusInternalServerError, "asset_missing", "Page asset missing from the binary")
		return
	}
	w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

// static serves the page's own assets from the binary — never from disk, so
// there is no path to traverse — plus the Cytoscape library viz inlines.
// Serving it separately rather than inlining is the one deliberate divergence
// from viz: a viz file must open with no network, a served page has one, and
// the browser caches 424KB it would otherwise re-parse on every reload.
func (s *server) static(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/static/"):]
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if name == "cytoscape.min.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = io.WriteString(w, assets.CytoscapeJS)
		return
	}
	// embed.FS rejects any path failing fs.ValidPath, so "../" cannot traverse
	// out; an empty or malformed name simply fails to read.
	body, err := uiFS.ReadFile("ui/" + name)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found", "No such asset")
		return
	}
	switch path.Ext(name) {
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	_, _ = w.Write(body)
}

// apiGraph rebuilds the graph from the live files on every request.
//
// Measured at 80ms for 2000 entities including process start and a 3MB file
// write; in-process it is well under that. There is therefore no cache, which
// means there is nothing to invalidate and no way to answer with a stale
// graph — the property `search` has, for the same reason.
func (s *server) apiGraph(w http.ResponseWriter, r *http.Request) {
	// No type filter: the whole graph goes to the client once, which is what
	// makes every level change and every search a local operation. viz keeps
	// --type because it renders one static file per invocation.
	elements, types, err := viz.Graph(s.root, nil)
	if err != nil {
		failErr(w, err)
		return
	}
	nodes := make([]any, 0, len(elements.Nodes))
	for _, n := range elements.Nodes {
		nodes = append(nodes, n)
	}
	edges := make([]any, 0, len(elements.Edges))
	for _, e := range elements.Edges {
		edges = append(edges, e)
	}
	payload := omap.New()
	payload.Set("nodes", nodes)
	payload.Set("edges", edges)
	payload.Set("types", strList(types))
	writeJSON(w, payload)
}

// apiSchema serves the resolved schema the page needs to offer only legal
// predicates: a link the schema forbids can then never be composed, which is
// the schema doing the work rather than the page reimplementing it.
func (s *server) apiSchema(w http.ResponseWriter, r *http.Request) {
	resolved, err := introspect.LoadSchema(s.root)
	if err != nil {
		failErr(w, err)
		return
	}
	types := make([]any, 0, resolved.Types.Len())
	for _, name := range resolved.Types.Keys() {
		t, _ := resolved.Types.Get(name)
		record := omap.New()
		record.Set("name", name)
		record.Set("layout", t.Storage.Layout)
		record.Set("relations", relationRecords(t.Relations))
		types = append(types, record)
	}
	payload := omap.New()
	payload.Set("types", types)
	// The base block is already merged into every type's relations, so this
	// list is not extra edges — it names which of a type's predicates are the
	// universal ones. The page needs that to hide them by default: four edges
	// available on all N types is a complete graph, and drawing it is the
	// crowding OntoGraph's aspect split exists to avoid.
	payload.Set("base_relations", relationRecords(resolved.BaseRelations))
	// The ontology as a graph, straight from the projection `khub schema edges`
	// prints. Serving it rather than re-deriving it on the page is what keeps
	// the two from disagreeing — and it arrives already carrying the
	// derived-inverse half, which no client walk over `types` could produce.
	//
	// Its vocabulary is introspect's (`from`/`to`), not this file's `targets`.
	// Left as-is deliberately: renaming would fork the contract from the CLI's,
	// and `types[].relations` above stays untouched because the command
	// composer reads it.
	payload.Set("edges", introspect.EdgesView(resolved))
	writeJSON(w, payload)
}

// apiEntity serves one entity in full — the same record `khub get --edges`
// prints, so the app and the CLI cannot disagree about what an entity is.
//
// The id is resolved against the index by entity.Get, never joined onto a
// filesystem path, so a traversal attempt is simply a lookup miss.
//
// Deliberate divergence: entity.Get builds its index WITHOUT
// index.Filter(StrayNodes) and without RejectMalformed, unlike viz.Graph
// (internal/viz/viz.go), so a stray file is fetchable by id while not being a
// node in /api/graph. Left rather than forked — the page only ever links from
// graph nodes, so nothing in the UI can reach the difference.
func (s *server) apiEntity(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/entity/"):]
	if id == "" {
		fail(w, http.StatusNotFound, "not_found", "No entity id given")
		return
	}
	view, err := entity.Get(s.root, id, true)
	if err != nil {
		failErr(w, err)
		return
	}
	record := omap.New()
	record.Set("id", view.Type+"/"+view.Slug)
	record.Set("type", view.Type)
	record.Set("slug", view.Slug)
	record.Set("path", relPath(s.root, view.Path))
	record.Set("frontmatter", view.Meta)
	record.Set("body", view.Body)
	if view.Locator != "" {
		record.Set("locator", view.Locator)
	}
	// Always emitted, even empty — the CLI omits `edges` when there are none,
	// but a JSON endpoint with a shape that varies costs every caller a branch.
	edges := make([]any, 0, len(view.Edges))
	for _, e := range view.Edges {
		edge := omap.New()
		edge.Set("predicate", e.Predicate)
		edge.Set("target", e.Target)
		edge.Set("derived", e.Derived)
		// strList, not the []string itself: the CLI encoder serializes []any
		// as a JSON array and a bare []string as one string.
		edge.Set("resolved_targets", strList(e.ResolvedTargets))
		edges = append(edges, edge)
	}
	record.Set("edges", edges)
	writeJSON(w, record)
}

// relPath renders a workspace path the way the CLI's records do: relative to
// the root, forward slashes. internal/cli keeps its own unexported copy; this
// is a deliberate second two-liner rather than an export, because the
// alternative is exporting a formatting detail out of the CLI adapter.
//
// The fallback differs from the CLI's on purpose. The CLI returns the input
// path, which is fine in a terminal the operator already owns; a served page
// must not report an absolute path, because that hands the operator's
// directory layout to anything reading the response.
func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

// apiType serves one declared type in full — the record `khub schema show
// <type>` prints: storage, id shape, capture cue, every field, and every
// relation including the inverses other types derive against it.
//
// The schema counterpart to apiEntity. Types are not entities, so they get
// their own route rather than a widened one, and /api/schema stays lean enough
// to ship on every page load.
func (s *server) apiType(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Path[len("/api/type/"):]
	if name == "" {
		fail(w, http.StatusNotFound, "not_found", "No type name given")
		return
	}
	resolved, err := introspect.LoadSchema(s.root)
	if err != nil {
		failErr(w, err)
		return
	}
	prov, err := workspace.Provenance(s.root)
	if err != nil {
		failErr(w, err)
		return
	}
	preset, _ := prov.Get("preset")
	presetName, _ := preset.(string)
	// TypeView refuses an undeclared name with errs.UnknownType, which failErr
	// already renders — and it lists the known types, which is what a caller
	// needs to correct the call.
	view, err := introspect.TypeView(resolved, name, presetName)
	if err != nil {
		failErr(w, err)
		return
	}
	writeJSON(w, view)
}

func relationRecords(rels *schema.Ordered[*schema.ResolvedRelation]) []any {
	if rels == nil {
		return []any{}
	}
	out := make([]any, 0, rels.Len())
	for _, predicate := range rels.Keys() {
		rel, _ := rels.Get(predicate)
		record := omap.New()
		record.Set("predicate", rel.Predicate)
		record.Set("kind", rel.Kind)
		record.Set("targets", strList(rel.Targets))
		record.Set("many", rel.Many)
		record.Set("required", rel.Required)
		out = append(out, record)
	}
	return out
}

// writeJSON encodes through canon, never encoding/json: khub emits three JSON
// dialects and a response body is an output path like any other.
func writeJSON(w http.ResponseWriter, v any) {
	doc, err := canon.EncodeCLI(v)
	if err != nil {
		fail(w, http.StatusInternalServerError, "encode_failed", "Could not encode the response")
		return
	}
	writeDoc(w, http.StatusOK, doc)
}

// writeDoc is the one place a JSON response leaves this package, so the headers
// cannot drift between the success path and the failure path. no-store because
// every answer is rebuilt from the tree and a cached one would be the stale
// graph the whole design refuses to serve.
func writeDoc(w http.ResponseWriter, status int, doc string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	if status != http.StatusOK {
		w.WriteHeader(status)
	}
	_, _ = io.WriteString(w, doc)
}

// failErr renders a khub refusal in the CLI's own error envelope, so an
// operator reading the network tab sees the same {error:{code,message}} shape
// the CLI prints. A non-located error is not leaked: it could name a path.
func failErr(w http.ResponseWriter, err error) {
	var located *errs.Located
	if errors.As(err, &located) {
		// Most refusals are "the request was wrong" = 400. Two say something
		// more specific, and a caller acts differently on each.
		status := http.StatusBadRequest
		switch located.Code {
		case "lookup_error":
			status = http.StatusNotFound
		case "ambiguity_error":
			// The message already names the type/slug candidates, which is
			// exactly what the caller needs to retry with a qualified id.
			status = http.StatusConflict
		}
		fail(w, status, located.Code, located.Message)
		return
	}
	if errors.Is(err, fs.ErrNotExist) {
		fail(w, http.StatusNotFound, "not_found", "Not found")
		return
	}
	fail(w, http.StatusInternalServerError, "internal_error", "The workspace could not be read")
}

func fail(w http.ResponseWriter, status int, code, message string) {
	inner := omap.New()
	inner.Set("code", code)
	inner.Set("message", message)
	env := omap.New()
	env.Set("error", inner)
	doc, err := canon.EncodeCLI(env)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeDoc(w, status, doc)
}

func strList(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}
