package extract

// Scaffold and Conform over the shipped presets: firm-ops for the attribute
// kinds, enums, patterns, union and many relations; build-hub for templates
// and id schemes. Shapes are asserted field by field; the CLI bytes are the
// parity fixtures' job (parity/cases/ingest).

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/workspace"
)

func wsFor(t *testing.T, preset string) string {
	t.Helper()
	root := t.TempDir()
	if _, err := workspace.Init(preset, root, workspace.InitOptions{}); err != nil {
		t.Fatalf("init %s: %v", preset, err)
	}
	return root
}

func create(t *testing.T, root, typeName string, pairs ...string) {
	t.Helper()
	f := omap.New()
	for i := 0; i+1 < len(pairs); i += 2 {
		f.Set(pairs[i], pairs[i+1])
	}
	if _, err := entity.Create(root, typeName, entity.CreateOpts{Fields: f, UseTemplate: true}); err != nil {
		t.Fatalf("create %s: %v", typeName, err)
	}
}

func scaffold(t *testing.T, root string, only ...string) *omap.Map {
	t.Helper()
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	prov, err := workspace.Provenance(root)
	if err != nil {
		t.Fatal(err)
	}
	view, err := Scaffold(root, resolved, prov, only)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func typeNamed(t *testing.T, view *omap.Map, name string) *omap.Map {
	t.Helper()
	types, _ := view.Get("types")
	for _, tv := range types.([]any) {
		m := tv.(*omap.Map)
		if n, _ := m.Get("name"); n == name {
			return m
		}
	}
	t.Fatalf("no type %s in the scaffold", name)
	return nil
}

func rowNamed(t *testing.T, tv *omap.Map, list, name string) *omap.Map {
	t.Helper()
	rows, _ := tv.Get(list)
	for _, r := range rows.([]any) {
		m := r.(*omap.Map)
		if n, _ := m.Get("name"); n == name {
			return m
		}
	}
	t.Fatalf("no %s named %s", list, name)
	return nil
}

func value(m *omap.Map, key string) any {
	v, _ := m.Get(key)
	return v
}

func strs(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.(string))
	}
	return out
}

// Every attribute kind, enum, pattern, default and required flag, every
// stored relation with its targets and cardinality, in declaration order;
// the supplied base fields left out; no derived inverse.
func TestScaffoldWalksTheResolvedSchema(t *testing.T) {
	root := wsFor(t, "firm-ops")
	view := scaffold(t, root)
	prov := value(view, "provenance").(*omap.Map)
	if p, _ := prov.Get("preset"); p != "firm-ops" {
		t.Errorf("provenance preset = %v", p)
	}
	env := value(view, "envelope").(*omap.Map)
	if got := strs(value(env, "supplied")); strings.Join(got, ",") != strings.Join(entity.WriteSupplied(), ",") {
		t.Errorf("supplied = %v", got)
	}
	if rules := strs(value(env, "rules")); len(rules) != len(Rules) {
		t.Errorf("rules = %d, want %d", len(rules), len(Rules))
	}
	if keys := view.Keys(); strings.Join(keys, ",") != "provenance,envelope,types" {
		t.Errorf("top-level order = %v", keys)
	}

	project := typeNamed(t, view, "project")
	if w := value(project, "when"); w == nil || !strings.Contains(w.(string), "work is agreed") {
		t.Errorf("when = %v", w)
	}
	if s := value(project, "id_shape"); s != "slug" {
		t.Errorf("id_shape = %v", s)
	}
	for _, name := range entity.WriteSupplied() {
		rows, _ := project.Get("fields")
		for _, r := range rows.([]any) {
			if n, _ := r.(*omap.Map).Get("name"); n == name {
				t.Errorf("supplied field %s listed as a field to fill", name)
			}
		}
	}
	title := rowNamed(t, project, "fields", "title")
	if value(title, "kind") != "text" || value(title, "required") != true || value(title, "enum") != nil {
		t.Errorf("title row = %v", title.Keys())
	}
	if keys := title.Keys(); strings.Join(keys, ",") != "name,kind,required,enum,pattern,default" {
		t.Errorf("field key order = %v", keys)
	}
	active := rowNamed(t, project, "fields", "active")
	if value(active, "kind") != "bool" || value(active, "default") != true {
		t.Errorf("bool with default: kind=%v default=%v", value(active, "kind"), value(active, "default"))
	}
	source := rowNamed(t, project, "fields", "source")
	if got := strs(value(source, "enum")); len(got) != 6 || got[0] != "referral" {
		t.Errorf("enum = %v", got)
	}
	repo := rowNamed(t, project, "fields", "external_repo")
	if value(repo, "pattern") != "^[a-z0-9-]+/[a-z0-9-]+$" {
		t.Errorf("pattern = %v", value(repo, "pattern"))
	}
	team := rowNamed(t, project, "relations", "team")
	if value(team, "many") != true || value(team, "required") != false || strs(value(team, "to"))[0] != "person" {
		t.Errorf("team relation = %v", team)
	}
	client := rowNamed(t, project, "relations", "client")
	if value(client, "many") != false || value(client, "required") != true {
		t.Errorf("client relation = %v", client)
	}
	related := rowNamed(t, project, "relations", "related")
	if got := strs(value(related, "to")); len(got) != 1 || got[0] != "any" {
		t.Errorf("any relation to = %v", got)
	}
	if value(project, "body") != nil {
		t.Errorf("firm-ops declares no template; body = %v", value(project, "body"))
	}

	meeting := typeNamed(t, view, "meeting")
	engagement := rowNamed(t, meeting, "relations", "engagement")
	if got := strs(value(engagement, "to")); strings.Join(got, ",") != "opportunity,project,partnership" {
		t.Errorf("union relation to = %v", got)
	}
	if k := value(rowNamed(t, meeting, "fields", "date"), "kind"); k != "datetime" {
		t.Errorf("datetime kind = %v", k)
	}
	if k := value(rowNamed(t, meeting, "fields", "attendees"), "kind"); k != "list" {
		t.Errorf("list kind = %v", k)
	}
	if k := value(rowNamed(t, typeNamed(t, view, "fragment"), "fields", "confidence"), "kind"); k != "number" {
		t.Errorf("number kind = %v", k)
	}
	for _, tv := range value(view, "types").([]any) {
		for _, r := range value(tv.(*omap.Map), "relations").([]any) {
			if _, has := r.(*omap.Map).Get("derived"); has {
				t.Errorf("a derived inverse leaked into the contract")
			}
		}
	}
}

// --type narrows and orders; an unknown name refuses with the declared types.
func TestScaffoldTypeFilter(t *testing.T) {
	root := wsFor(t, "firm-ops")
	view := scaffold(t, root, "person", "client", "person")
	types := value(view, "types").([]any)
	if len(types) != 2 || value(types[0].(*omap.Map), "name") != "person" || value(types[1].(*omap.Map), "name") != "client" {
		t.Errorf("filtered types = %v", types)
	}
	resolved, _ := introspect.LoadSchema(root)
	prov, _ := workspace.Provenance(root)
	_, err := Scaffold(root, resolved, prov, []string{"widget"})
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "unknown_type" {
		t.Fatalf("want unknown_type, got %v", err)
	}
	if !strings.Contains(located.Message, "firm-ops schema. Known types: case-study, client") {
		t.Errorf("message = %s", located.Message)
	}
}

// A templated type lists its sections with the optional flag and hint; an
// empty contract lists none; a template that does not parse reports null
// sections; a prefixed, dated type renders its id shape.
func TestScaffoldBodyAndIDShape(t *testing.T) {
	root := wsFor(t, "build-hub")
	view := scaffold(t, root, "adr", "requirement")
	adr := typeNamed(t, view, "adr")
	if s := value(adr, "id_shape"); s != "ad-YYYY-MM-DD-slug" {
		t.Errorf("id_shape = %v", s)
	}
	body := value(adr, "body").(*omap.Map)
	if value(body, "template") != "adr" {
		t.Errorf("template = %v", value(body, "template"))
	}
	sections := value(body, "sections").([]any)
	if len(sections) != 5 {
		t.Fatalf("sections = %d", len(sections))
	}
	ctx := sections[0].(*omap.Map)
	if value(ctx, "heading") != "Context" || value(ctx, "optional") != false || value(ctx, "hint") == nil {
		t.Errorf("first section = %v", ctx)
	}
	if alt := sections[1].(*omap.Map); value(alt, "heading") != "Alternatives" || value(alt, "optional") != true {
		t.Errorf("optional section = %v", alt)
	}
	req := typeNamed(t, view, "requirement")
	reqBody := value(req, "body").(*omap.Map)
	if got := value(reqBody, "sections").([]any); len(got) != 0 {
		t.Errorf("an empty contract lists sections = %v", got)
	}

	if err := os.WriteFile(filepath.Join(root, ".khub", "templates", "adr.yaml"), []byte("sections: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := value(typeNamed(t, scaffold(t, root, "adr"), "adr"), "body").(*omap.Map)
	if value(broken, "sections") != nil {
		t.Errorf("a broken template should report null sections, got %v", value(broken, "sections"))
	}
}

func conform(t *testing.T, root, doc string) *Report {
	t.Helper()
	env, err := ParseEnvelope([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Conform(root, env)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func seeded(t *testing.T) string {
	t.Helper()
	root := wsFor(t, "firm-ops")
	create(t, root, "client", "name", "Initech", "aliases", "INTC")
	create(t, root, "client", "name", "Globex")
	create(t, root, "person", "name", "Noor", "role", "partner")
	return root
}

// One entity per verdict the report carries, and the score over them.
func TestConformReportsEachVerdict(t *testing.T) {
	root := seeded(t)
	report := conform(t, root, `{"entities": [
	  {"type": "project", "fields": {"title": "PoV", "active": true, "owner": "noor"}, "relations": {"client": "INTC"}, "confidence": 0.8},
	  {"type": "project", "fields": {"title": "Bad", "source": "pigeon"}, "relations": {"client": "initech", "owner": "noor"}},
	  {"type": "project", "fields": {"title": "Thin"}},
	  {"type": "project", "fields": {"title": "Lost"}, "relations": {"client": "nowhere", "owner": "noor"}},
	  {"type": "project", "fields": {"title": "Two", "external_repo": "not a repo", "active": "banana"}, "relations": {"client": ["initech", "globex"], "owner": "noor", "sponsor": "noor"}},
	  {"type": "project", "fields": {"title": "Typed", "type": "client"}, "relations": {"client": "initech", "owner": "noor"}}
	]}`)
	if len(report.Entities) != 6 || report.Conformant != 2 || report.Score != 0.333 {
		t.Fatalf("summary = %d entities, %d conformant, score %v", len(report.Entities), report.Conformant, report.Score)
	}
	if report.ScoreText() != "0.333" {
		t.Errorf("score text = %s", report.ScoreText())
	}
	e := report.Entities
	if !e[0].OK || len(e[0].Gaps) != 0 || e[0].Index != 0 || e[0].Type != "project" {
		t.Errorf("alias-resolved entity should conform: %+v", e[0])
	}
	if e[1].OK || len(e[1].Errors) != 1 || e[1].Errors[0].Field != "source" ||
		!strings.Contains(e[1].Errors[0].Reason, "'pigeon' is not a valid source (referral") {
		t.Errorf("enum violation: %+v", e[1])
	}
	if !e[2].OK || strings.Join(e[2].Gaps, ",") != "client,owner" {
		t.Errorf("missing required is a gap, never a failure: %+v", e[2])
	}
	if e[3].OK || len(e[3].Unresolved) != 1 || e[3].Unresolved[0].Predicate != "client" ||
		e[3].Unresolved[0].Value != "nowhere" ||
		e[3].Unresolved[0].Reason != "No client 'nowhere' to satisfy relation 'client'" ||
		strings.Join(e[3].Gaps, ",") != "client" {
		t.Errorf("unresolved target: %+v", e[3])
	}
	fields := map[string]string{}
	for _, fe := range e[4].Errors {
		fields[fe.Field] = fe.Reason
	}
	if len(fields) != 4 {
		t.Errorf("want pattern, bool, cardinality and predicate errors, got %v", fields)
	}
	if !strings.Contains(fields["external_repo"], "does not match the pattern") {
		t.Errorf("pattern: %q", fields["external_repo"])
	}
	if fields["active"] != "'banana' is not a valid bool for active" {
		t.Errorf("bool kind: %q", fields["active"])
	}
	if fields["client"] != "single-valued relation has multiple values" {
		t.Errorf("cardinality: %q", fields["client"])
	}
	if fields["sponsor"] != "Predicate 'sponsor' is not legal for type 'project'" {
		t.Errorf("undeclared predicate: %q", fields["sponsor"])
	}
	if e[5].OK || len(e[5].Errors) != 1 || e[5].Errors[0].Field != "type" ||
		e[5].Errors[0].Reason != "'client' disagrees with the entity's type 'project'" {
		t.Errorf("type disagreement: %+v", e[5])
	}
}

// An unknown type refuses the whole call; an empty envelope conforms with
// score 1; the score rounds through three decimals.
func TestConformEdges(t *testing.T) {
	root := seeded(t)
	env, _ := ParseEnvelope([]byte(`{"entities": [{"type": "client", "fields": {"name": "A"}}, {"type": "widget"}]}`))
	_, err := Conform(root, env)
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "unknown_type" {
		t.Fatalf("want unknown_type, got %v", err)
	}
	if !strings.Contains(located.Message, "No type 'widget' in the firm-ops schema. Known types:") {
		t.Errorf("message = %s", located.Message)
	}
	empty := conform(t, root, `{"entities": []}`)
	if !empty.OK() || empty.Score != 1 || empty.ScoreText() != "1.000" {
		t.Errorf("empty envelope: ok=%v score=%v", empty.OK(), empty.Score)
	}
	two := conform(t, root, `{"entities": [
	  {"type": "client", "fields": {"name": "A"}}, {"type": "client", "fields": {"name": "B"}},
	  {"type": "client", "fields": {"name": ""}}]}`)
	if two.Score != 0.667 || two.OK() {
		t.Errorf("score = %v ok=%v", two.Score, two.OK())
	}
	if r := two.Entities[2].Errors[0].Reason; r != "name is empty; omit the field or write null, not ''" {
		t.Errorf("empty string: %q", r)
	}
}

// A body is judged against the type's template exactly as validate judges
// the file; no body passes (add seeds the scaffold); a template that does not
// parse is a template error on every entity of the type that carries a body.
func TestConformBodyShape(t *testing.T) {
	root := wsFor(t, "build-hub")
	report := conform(t, root, `{"entities": [
	  {"type": "adr", "fields": {"title": "Use Postgres", "status": "accepted"}, "body": "## Context\n\nx\n\n## Consequences\n\ny\n"},
	  {"type": "adr", "fields": {"title": "No body", "status": "accepted"}},
	  {"type": "adr", "fields": {"title": "Full", "status": "accepted"}, "body": "## Context\n\nx\n\n## Decision\n\ny\n\n## Consequences\n\nz\n"}
	]}`)
	e := report.Entities
	if e[0].OK || len(e[0].Errors) != 1 || e[0].Errors[0].Field != "body" ||
		e[0].Errors[0].Reason != "missing or out-of-order section '## Decision' (template adr.yaml requires its headings in order)" {
		t.Errorf("body shape: %+v", e[0])
	}
	if !e[1].OK || !e[2].OK {
		t.Errorf("no body and a full body both conform: %+v %+v", e[1], e[2])
	}
	if err := os.WriteFile(filepath.Join(root, ".khub", "templates", "adr.yaml"), []byte("sections: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := conform(t, root, `{"entities": [{"type": "adr", "fields": {"title": "X"}, "body": "hi"}, {"type": "adr", "fields": {"title": "Y"}}]}`)
	if broken.Entities[0].OK || broken.Entities[0].Errors[0].Field != "template" {
		t.Errorf("broken template: %+v", broken.Entities[0])
	}
	if !broken.Entities[1].OK {
		t.Errorf("no body is never judged against a template: %+v", broken.Entities[1])
	}
}

// Conform writes nothing and takes no lock.
func TestConformNeverWrites(t *testing.T) {
	root := seeded(t)
	before := treeOf(t, root)
	conform(t, root, `{"entities": [{"type": "client", "fields": {"name": "Fresh"}, "body": "x"}]}`)
	if after := treeOf(t, root); after != before {
		t.Errorf("tree changed:\n%s\n--\n%s", before, after)
	}
}

func treeOf(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b.WriteString(rel + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}
