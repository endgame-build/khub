package rdf

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/workspace"
)

func firmOpsWS(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := workspace.Init("firm-ops", root, workspace.InitOptions{Name: "ws"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	create(t, root, "person", "noor", "name", "Noor", "role", "partner")
	create(t, root, "client", "initech", "name", "Initech", "crm_id", "42")
	create(t, root, "opportunity", "initech-deal", "name", "Initech Deal", "stage", "prospect",
		"client", "initech", "owner", "noor", "tags", "pov,q1")
	create(t, root, "meeting", "kickoff", "title", "Kickoff", "date", "2026-06-01 10:00:00",
		"call_type", "client", "source", "manual", "engagement", "initech-deal")
	return root
}

func create(t *testing.T, root, typeName, id string, pairs ...string) {
	t.Helper()
	fields := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		fields.Set(pairs[i], pairs[i+1])
	}
	if _, err := entity.Create(root, typeName, entity.CreateOpts{Fields: fields, ID: id, UseTemplate: true}); err != nil {
		t.Fatalf("create %s/%s: %v", typeName, id, err)
	}
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	return err == nil
}

func TestProjectWritesTheDefaultPath(t *testing.T) {
	root := firmOpsWS(t)
	res, err := Project(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Written || res.Path != DefaultOut || res.Types != 9 || res.Entities != 0 {
		t.Errorf("result = %+v", res)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(DefaultOut)))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != res.Content || res.Bytes != len(raw) {
		t.Error("the file holds other bytes than the result reports")
	}
	if strings.Contains(res.Content, "urn:khub:workspace/") {
		t.Error("the TBox alone names no individual")
	}
	// A second run replaces the file in place.
	if _, err := Project(root, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestDryRunAndStdoutWriteNothing(t *testing.T) {
	root := firmOpsWS(t)
	for _, opt := range []Options{{DryRun: true}, {Out: Stdout}, {DryRun: true, Out: "x.ttl"}} {
		res, err := Project(root, opt)
		if err != nil {
			t.Fatal(err)
		}
		if res.Written || res.Content == "" {
			t.Errorf("%+v: written=%v, content empty=%v", opt, res.Written, res.Content == "")
		}
	}
	if exists(root, DefaultOut) || exists(root, "x.ttl") {
		t.Error("a dry run or a stdout render created a file")
	}
}

func TestOutInsideAndOutsideTheWorkspace(t *testing.T) {
	root := firmOpsWS(t)
	res, err := Project(root, Options{Out: "exports/schema.ttl"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != "exports/schema.ttl" || !exists(root, "exports/schema.ttl") {
		t.Errorf("relative --out: %+v", res)
	}
	outside := filepath.Join(t.TempDir(), "sub", "export.ttl")
	if _, err := Project(root, Options{Out: outside}); err != nil {
		t.Fatalf("absolute --out outside the workspace: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Error("the outside export was not written")
	}
}

func TestInstancesEmitEntitiesAndForwardEdges(t *testing.T) {
	root := firmOpsWS(t)
	res, err := Project(root, Options{Out: Stdout, Instances: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entities != 4 {
		t.Errorf("entities = %d, want 4", res.Entities)
	}
	for _, want := range []string{
		"<urn:khub:workspace/ws/opportunity/initech-deal> a type:opportunity ;",
		"field:client <urn:khub:workspace/ws/client/initech> ;",
		"field:owner <urn:khub:workspace/ws/person/noor> .",
		`khub:tags "pov", "q1" ;`,
		`khub:created "2026-`,
		`field:crm_id "42"`,
		// khub writes the datetime with a space; XSD's lexical form takes the T.
		`field:date "2026-06-01T10:00:00"^^xsd:dateTime`,
		"field:engagement <urn:khub:workspace/ws/opportunity/initech-deal>",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("ABox lacks %q", want)
		}
	}
	if strings.Contains(block(res.Content, "<urn:khub:workspace/ws/opportunity/initech-deal>"), "khub:description") {
		t.Error("an absent attribute emits no triple")
	}
	// Inverses are derived, never emitted: the client points at nothing.
	if strings.Contains(block(res.Content, "<urn:khub:workspace/ws/client/initech>"), "urn:khub:workspace/ws/opportunity") {
		t.Error("a derived inverse edge was emitted on the target")
	}
}

// block is the subject block opening with the given IRI, up to its blank line
// or the end of the document.
func block(ttl, subject string) string {
	start := strings.Index(ttl, "\n"+subject+" ")
	if start < 0 {
		return ""
	}
	rest := ttl[start+1:]
	if end := strings.Index(rest, "\n\n"); end >= 0 {
		return rest[:end]
	}
	return rest
}

func TestInstancesRefuseAMalformedScan(t *testing.T) {
	root := firmOpsWS(t)
	bad := filepath.Join(root, "clients", "broken.md")
	if err := os.WriteFile(bad, []byte("---\n: [\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Project(root, Options{Out: Stdout, Instances: true})
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "malformed_projection" {
		t.Fatalf("err = %v, want malformed_projection", err)
	}
	if _, err := Project(root, Options{Out: Stdout}); err != nil {
		t.Errorf("the TBox alone reads no entity, so it still renders: %v", err)
	}
}

// The declared kind types the literal, so a validate-clean workspace conforms
// to its own shapes: a date in a datetime field, a datetime in a date field,
// a text field YAML reads back as a boolean, and a text field holding a
// mapping all go out under the shape's datatype.
func TestLiteralsFollowTheDeclaredKind(t *testing.T) {
	root := firmOpsWS(t)
	create(t, root, "meeting", "dateonly", "title", "Date only", "date", "2026-01-15",
		"call_type", "internal", "source", "manual", "engagement", "initech-deal")
	create(t, root, "client", "stamped", "name", "Stamped", "created", "2026-01-15 10:00:00")
	nested := "---\ntype: client\nname: Nested\ncreated: 2026-01-15\nindustry: yes\nwebsite: {nested: {deep: true}}\naliases: [1, true, {k: v}]\n---\n"
	if err := os.WriteFile(filepath.Join(root, "clients", "nested.md"), []byte(nested), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Project(root, Options{Out: Stdout, Instances: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`field:date "2026-01-15T00:00:00"^^xsd:dateTime`,
		`khub:created "2026-01-15T10:00:00"^^xsd:dateTime`,
		`field:industry "true" ;`,
		`field:website "{\"nested\": {\"deep\": true}}"`,
		`khub:aliases "1"^^xsd:decimal, true, "{\"k\": \"v\"}"`,
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("ABox lacks %q", want)
		}
	}
	if strings.Contains(res.Content, "rdf:JSON") {
		t.Error("no literal is typed rdf:JSON")
	}
	for in, want := range map[string]string{
		"text-true": Str("true").String(), "number": `"3"^^xsd:decimal`, "bool-yes": "true",
		"bool-bad": `"maybe"^^xsd:boolean`, "date-dt": `"2026-01-15T10:00:00"^^xsd:dateTime`,
		"dt-date": `"2026-01-15T00:00:00"^^xsd:dateTime`, "list-float": `"1.5"^^xsd:decimal`,
	} {
		var got Term
		switch in {
		case "text-true":
			got = literal(true, "text")
		case "number":
			got = literal("3", "number")
		case "bool-yes":
			got = literal("yes", "bool")
		case "bool-bad":
			got = literal("maybe", "bool")
		case "date-dt":
			got = literal("2026-01-15 10:00:00", "date")
		case "dt-date":
			got = literal("2026-01-15", "datetime")
		case "list-float":
			got = literal(1.5, "list")
		}
		if s := got.(Literal).String(); s != want {
			t.Errorf("literal %s = %s, want %s", in, s, want)
		}
	}
}

// A path reference to a plain file satisfies a `to: any` relation under
// path_refs without an edge in the graph; the ABox emits the file as a
// khub:File node so the shape's minCount holds.
func TestPathReferenceEmitsAFileNode(t *testing.T) {
	root := firmOpsWS(t)
	policy := filepath.Join(root, ".khub", "policy.yaml")
	if err := os.WriteFile(policy, []byte("policy:\n  client: { path_refs: true }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "guide.md"), []byte("# Guide\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	create(t, root, "client", "reader", "name", "Reader", "related", "guide.md,initech")
	res, err := Project(root, Options{Out: Stdout, Instances: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"khub:related <urn:khub:workspace/ws/client/initech>, <urn:khub:workspace/ws/file/guide.md> .",
		"<urn:khub:workspace/ws/file/guide.md> a khub:File ;\n    khub:path \"guide.md\" .",
		"[ sh:class khub:Entity ]\n            [ sh:class khub:File ]",
	} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("render lacks %q\n%s", want, block(res.Content, "<urn:khub:workspace/ws/client/reader>"))
		}
	}
}

func TestOutAndBaseRefusals(t *testing.T) {
	root := firmOpsWS(t)
	for _, c := range []struct {
		opt  Options
		code string
	}{
		{Options{Out: "../escape.ttl"}, "unsafe_path"},
		{Options{Out: "."}, "invalid_out"},
		{Options{Out: "clients"}, "invalid_out"},
		{Options{Out: Stdout, Base: "http://x"}, "invalid_base"},
		{Options{DryRun: true, Base: "rel/path"}, "invalid_base"},
	} {
		_, err := Project(root, c.opt)
		var located *errs.Located
		if !errors.As(err, &located) || located.Code != c.code {
			t.Errorf("%+v: err = %v, want %s", c.opt, err, c.code)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "..", "escape.ttl")); err == nil {
		t.Error("a relative --out climbed out of the workspace")
	}
}

func TestOutRefusesASymlink(t *testing.T) {
	root := firmOpsWS(t)
	gen := filepath.Join(root, ".khub", "generated")
	if err := os.MkdirAll(gen, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "elsewhere.ttl")
	if err := os.Symlink(target, filepath.Join(gen, "schema.ttl")); err != nil {
		t.Skipf("symlink: %v", err)
	}
	_, err := Project(root, Options{})
	if err == nil {
		t.Fatal("a symlinked output path was written through")
	}
	if _, serr := os.Stat(target); serr == nil {
		t.Error("the symlink's target was written")
	}
}
