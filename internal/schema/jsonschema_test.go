package schema

// The layer JSON Schema against the vocabulary walk it describes: the committed
// copy is what LayerJSONSchema builds, every shipped preset layer passes it,
// every key list the walk forbids extras with is a property set of it, and on
// a table of probes it never rejects what the walk accepts.

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
)

var updateJSONSchema = flag.Bool("update-jsonschema", false,
	"rewrite schemas/layers.schema.json from LayerJSONSchema")

const jsonSchemaFile = "../../schemas/layers.schema.json"

func TestLayerJSONSchemaFileIsCurrent(t *testing.T) {
	want, err := canon.EncodeDisk(LayerJSONSchema())
	if err != nil {
		t.Fatal(err)
	}
	if *updateJSONSchema {
		if err := os.WriteFile(jsonSchemaFile, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(jsonSchemaFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s is stale; regenerate it with\n"+
			"  go test ./internal/schema -run TestLayerJSONSchemaFileIsCurrent -update-jsonschema",
			jsonSchemaFile)
	}
}

// committedSchema decodes the committed file, so the checks below hold for the
// bytes an editor reads.
func committedSchema(t *testing.T) *omap.Map {
	t.Helper()
	raw, err := os.ReadFile(jsonSchemaFile)
	if err != nil {
		t.Fatal(err)
	}
	v, err := canon.DecodeOrderedJSONStrict(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v.(*omap.Map)
}

// TestLayerJSONSchemaCoversVocabulary pins each closed mapping of the schema to
// the key list the walk forbids extras with. LayerJSONSchema already panics on
// a mismatch; this names which list moved.
func TestLayerJSONSchemaCoversVocabulary(t *testing.T) {
	s := committedSchema(t)
	cases := []struct {
		path []string
		keys []string
	}{
		{[]string{}, layerDocKeys},
		{[]string{"properties", "ontology", "anyOf", "0"}, authoredOntologyKeys},
		{[]string{"definitions", "entity"}, ontologyTypeKeys},
		{[]string{"definitions", "attribute"}, attributeKeys},
		{[]string{"definitions", "relation"}, relationKeys},
		{[]string{"definitions", "policy"}, policyTypeKeys},
		{[]string{"definitions", "storage"}, storageTypeKeys},
		{[]string{"definitions", "id_prefix_by"}, idPrefixDeclKeys},
	}
	for _, tc := range cases {
		node := dig(t, s, tc.path)
		props, _ := node.Get("properties")
		got := slices.Clone(props.(*omap.Map).Keys())
		if !slices.Equal(got, tc.keys) {
			t.Errorf("%s: properties %v, vocabulary %v", strings.Join(tc.path, "."), got, tc.keys)
		}
		if ap, _ := node.Get("additionalProperties"); ap != false {
			t.Errorf("%s: additionalProperties = %v, want false (the walk forbids extras)",
				strings.Join(tc.path, "."), ap)
		}
	}
	// The walk's own ontology list differs from the authored one by `base`
	// alone, which mergeLayers rejects from an authored file.
	extra := []string{}
	for _, k := range ontologyKeys {
		if !slices.Contains(authoredOntologyKeys, k) {
			extra = append(extra, k)
		}
	}
	if !slices.Equal(extra, []string{"base"}) {
		t.Errorf("ontologyKeys beyond the authored ones = %v, want [base]", extra)
	}
}

// schemaKeyLists maps each key list the JSON Schema describes to the schema
// node holding its properties. The walk-only lists name why they are not.
var schemaKeyLists = map[string]string{
	"layerDocKeys":         "the root",
	"authoredOntologyKeys": "properties.ontology",
	"ontologyTypeKeys":     "definitions.entity",
	"attributeKeys":        "definitions.attribute",
	"relationKeys":         "definitions.relation",
	"policyTypeKeys":       "definitions.policy",
	"storageTypeKeys":      "definitions.storage",
	"idPrefixDeclKeys":     "definitions.id_prefix_by",
	// Walk only: the base block and the re-nested document are khub's own,
	// never authored. ontologyKeys differs from authoredOntologyKeys by `base`.
	"baseKeys":      "",
	"ontologyKeys":  "",
	"walkLayerKeys": "",
}

// TestExtraKeyChecksUseTheKeyLists reads the package's source so a key cannot
// reach the walk without reaching the schema. In every non-test file:
//
//   - every addExtras call passes, as `name...`, a package-level `…Keys` var
//     whose value is a literal []string of string literals;
//   - no other declaration (a local, a parameter, a range variable) and no
//     assignment reuses such a name, so the name means the package list;
//   - "extra_forbidden" and msgExtra appear only inside addExtras (and as an
//     operand of == or !=, which reads a kind and reports nothing), so no key
//     check bypasses it.
//
// Every list addExtras is called with must be one the JSON Schema describes
// or a walk-only list named in schemaKeyLists.
func TestExtraKeyChecksUseTheKeyLists(t *testing.T) {
	fset := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, n, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	for _, msg := range extraKeyViolations(fset, files) {
		t.Error(msg)
	}
}

// extraKeyViolations is the check of TestExtraKeyChecksUseTheKeyLists over
// parsed files, one message per violation.
func extraKeyViolations(fset *token.FileSet, files []*ast.File) []string {
	var out []string
	report := func(n ast.Node, format string, args ...any) {
		out = append(out, fset.Position(n.Pos()).String()+": "+fmt.Sprintf(format, args...))
	}

	// The package-level key lists.
	lists := map[string]*ast.Ident{}
	for _, f := range files {
		for _, d := range f.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.VAR {
				continue
			}
			for _, spec := range gd.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasSuffix(name.Name, "Keys") {
						continue
					}
					if i >= len(vs.Values) || !isStringListLiteral(vs.Values[i]) {
						report(name, "%s is not a literal []string of string literals", name.Name)
						continue
					}
					lists[name.Name] = name
				}
			}
		}
	}

	used := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fd, isFunc := d.(*ast.FuncDecl)
			if !isFunc {
				// A package-level constant spelling the kind is a way around
				// addExtras too.
				ast.Inspect(d, func(n ast.Node) bool {
					if bl, ok := n.(*ast.BasicLit); ok && isExtraMarker(bl) {
						report(bl, "an extra key kind is spelled outside addExtras")
					}
					return true
				})
				continue
			}
			inAddExtras := fd.Recv == nil && fd.Name.Name == "addExtras"
			ast.Inspect(fd, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BinaryExpr:
					// Comparing a kind reads it and reports nothing.
					if (x.Op == token.EQL || x.Op == token.NEQ) && (isExtraMarker(x.X) || isExtraMarker(x.Y)) {
						return false
					}
				case *ast.Field:
					for _, n := range x.Names {
						if lists[n.Name] != nil {
							report(n, "%s shadows the package key list", n.Name)
						}
					}
				case *ast.BasicLit, *ast.Ident:
					if !inAddExtras && isExtraMarker(x.(ast.Expr)) {
						report(x, "an extra key is reported outside addExtras")
					}
				case *ast.AssignStmt:
					for _, lhs := range x.Lhs {
						if root := rootIdent(lhs); root != nil && lists[root.Name] != nil {
							report(root, "%s is assigned or redeclared; the key lists are fixed", root.Name)
						}
					}
				case *ast.ValueSpec:
					for _, n := range x.Names {
						if lists[n.Name] != nil {
							report(n, "local %s shadows the package key list", n.Name)
						}
					}
				case *ast.RangeStmt:
					for _, e := range []ast.Expr{x.Key, x.Value} {
						if id, ok := e.(*ast.Ident); ok && lists[id.Name] != nil {
							report(id, "range variable %s shadows the package key list", id.Name)
						}
					}
				case *ast.CallExpr:
					fn, ok := x.Fun.(*ast.Ident)
					if !ok || fn.Name != "addExtras" {
						return true
					}
					last := x.Args[len(x.Args)-1]
					id, isIdent := last.(*ast.Ident)
					switch {
					case !x.Ellipsis.IsValid() || !isIdent:
						report(x, "addExtras must take its keys as one package key list, `name...`")
					case lists[id.Name] == nil:
						report(x, "addExtras takes %s, which is not a package-level …Keys literal", id.Name)
					default:
						used[id.Name] = true
					}
				}
				return true
			})
		}
	}
	for name := range used {
		if _, known := schemaKeyLists[name]; !known {
			out = append(out, fmt.Sprintf(
				"addExtras checks %s, which the JSON Schema does not describe; "+
					"add it to LayerJSONSchema and schemaKeyLists", name))
		}
	}
	for name := range schemaKeyLists {
		if !used[name] {
			out = append(out, fmt.Sprintf("schemaKeyLists names %s, which no addExtras call checks", name))
		}
	}
	slices.Sort(out)
	return out
}

func isStringListLiteral(e ast.Expr) bool {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return false
	}
	at, ok := cl.Type.(*ast.ArrayType)
	if !ok || at.Len != nil {
		return false
	}
	if elt, ok := at.Elt.(*ast.Ident); !ok || elt.Name != "string" {
		return false
	}
	for _, el := range cl.Elts {
		if bl, ok := el.(*ast.BasicLit); !ok || bl.Kind != token.STRING {
			return false
		}
	}
	return true
}

// isExtraMarker is the kind literal or the message constant of an extra key.
func isExtraMarker(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.BasicLit:
		return x.Kind == token.STRING && x.Value == `"extra_forbidden"`
	case *ast.Ident:
		return x.Name == "msgExtra"
	}
	return false
}

// rootIdent is the variable an assignment target writes through: x in x,
// x[i], x.f or *x.
func rootIdent(e ast.Expr) *ast.Ident {
	for {
		switch x := e.(type) {
		case *ast.Ident:
			return x
		case *ast.IndexExpr:
			e = x.X
		case *ast.SelectorExpr:
			e = x.X
		case *ast.StarExpr:
			e = x.X
		case *ast.ParenExpr:
			e = x.X
		default:
			return nil
		}
	}
}

func TestShippedPresetLayersPassTheJSONSchema(t *testing.T) {
	s := committedSchema(t)
	presets := presetsDir(t)
	entries, err := os.ReadDir(presets)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "core" {
			continue
		}
		for _, p := range presetPaths(t, e.Name()) {
			doc := parseLayerFile(t, p)
			if errs := jsValidate(s, s, doc, "$"); len(errs) > 0 {
				t.Errorf("%s fails the JSON Schema:\n  %s", p, strings.Join(errs, "\n  "))
			}
			checked++
		}
	}
	if checked < 6 {
		t.Fatalf("checked %d preset layer files; want every layer of build-hub and firm-ops", checked)
	}
	// The embedded base is not an authoring surface: an authored ontology.base
	// is refused, and the schema says so too.
	core := parseLayerFile(t, filepath.Join(presets, "core", "ontology.yaml"))
	if errs := jsValidate(s, s, core, "$"); len(errs) == 0 {
		t.Error("core/ontology.yaml passes the JSON Schema; an authored ontology.base must not")
	}
}

// TestJSONSchemaAgreesWithTheWalk runs probes through both. A probe the walk
// accepts must pass the schema (the schema is never stricter, falsy layer
// blocks aside); a probe the walk rejects on a single-field rule must fail it.
func TestJSONSchemaAgreesWithTheWalk(t *testing.T) {
	s := committedSchema(t)
	const ont = "ontology:\n  entities:\n    a:\n      attributes:\n        kind: { enum: [x, y] }\n    b: {}\n"
	// An empty, comment-only, `~` or bare `---` document is null to an editor
	// and an empty layer file to khub.
	for _, text := range []string{"", "# notes only\n", "~\n", "---\n"} {
		if msg := walkError(mustParseDoc(t, text)); msg != "" {
			t.Errorf("null document %q is refused by the walk: %s", text, msg)
		}
	}
	if errs := jsValidate(s, s, nil, "$"); len(errs) > 0 {
		t.Errorf("a null document fails the JSON Schema: %s", strings.Join(errs, "; "))
	}
	// khub reads a document that is just `0` or `false` as empty too (ParseDoc
	// collapses it before the walk); the schema asks for a mapping or null.
	for _, v := range []any{int64(0), false} {
		if msg := walkError(mustParseDoc(t, fmt.Sprintf("%v\n", v))); msg != "" {
			t.Errorf("document %v is refused by the walk: %s", v, msg)
		}
		if len(jsValidate(s, s, v, "$")) == 0 {
			t.Errorf("document %v passes the JSON Schema", v)
		}
	}
	accepted := []string{
		"",
		"version: '1.0'\n",
		"ontology:\npolicy:\nstorage:\n",
		"ontology:\n  entities:\n",
		ont,
		ont + "policy:\n  a: { required: 'No', orphan: 'On', path_refs: 0 }\n  b: { required: false, orphan: F, path_refs: y }\n",
		ont + "storage:\n  a: { layout: file }\n",
		ont + "storage:\n  a: { layout: file, path: null, format: md, id_prefix: null, template: null }\n",
		ont + "storage:\n  a: { layout: file, path: 'w/{slug}.md' }\n",
		ont + "storage:\n  a: { layout: file, path: ['w/{slug}.md', 'v/{slug}.md'] }\n",
		ont + "storage:\n  a: { layout: collection, format: jsonl }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: ad, id_date: TRUE, id_ordinal: n }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: { by: kind, map: { x: xa, y: ya } } }\n",
		ont + "storage:\n  a: { layout: file, template: false }\n",
		ont + "storage:\n  a: { layout: file, template: note-body }\n",
		"ontology:\n  entities:\n    a:\n      when: null\n      attributes:\n" +
			"        f: { type: null, required: null, default: null, enum: null, pattern: null }\n" +
			"        g: { type: date, default: 2026-01-01, required: 'off' }\n" +
			"        h: {}\n      relations:\n" +
			"        r: { to: any, many: 'yes', required: 0, inverse: null, acyclic: true }\n" +
			"        s: { to: [a, b] }\n",
	}
	rejected := []string{
		"nope: 1\n",
		"ontology:\n  base: {}\n",
		"ontology:\n  entitles: {}\n",
		"ontology:\n  entities:\n    a: { nope: 1 }\n",
		"ontology:\n  entities:\n    a: { when: 3 }\n",
		"ontology:\n  entities:\n    a: { attributes: null }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { type: string }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { enum: x }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { enum: [1] }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { required: maybe }\n",
		"ontology:\n  entities:\n    a:\n      attributes:\n        f: { nope: 1 }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { many: true }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { to: 42 }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { to: [a, 1] }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { to: a, many: 2 }\n",
		"ontology:\n  entities:\n    a:\n      relations:\n        r: { to: a, many: null }\n",
		ont + "policy:\n  a: { nope: true }\n",
		ont + "policy:\n  a: { orphan: null }\n",
		ont + "policy:\n  a: null\n",
		ont + "storage:\n  a: { path: as }\n",
		ont + "storage:\n  a: { layout: nope }\n",
		ont + "storage:\n  a: { layout: file, format: gjson }\n",
		ont + "storage:\n  a: { layout: file, format: null }\n",
		ont + "storage:\n  a: { layout: file, path: [] }\n",
		ont + "storage:\n  a: { layout: file, path: [1] }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: AD }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: { by: kind } }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: { by: kind, map: { x: X } } }\n",
		ont + "storage:\n  a: { layout: file, id_prefix: { by: kind, map: {}, nope: 1 } }\n",
		ont + "storage:\n  a: { layout: file, template: true }\n",
		ont + "storage:\n  a: { layout: file, template: ../x }\n",
		ont + "storage:\n  a: { layout: file, id_date: later }\n",
	}
	// The walk reads a falsy layer block as empty; the schema still asks for
	// a mapping or null there, the one place it is stricter on purpose.
	stricter := []string{
		"ontology:\n  entities: []\n",
		"policy: false\n",
		"storage: ''\n",
	}
	for _, text := range stricter {
		doc := mustParseDoc(t, text)
		if msg := walkError(doc); msg != "" {
			t.Errorf("falsy-block probe is refused by the walk (%s):\n%s", msg, text)
		}
		if errs := jsValidate(s, s, doc, "$"); len(errs) == 0 {
			t.Errorf("falsy-block probe passes the JSON Schema:\n%s", text)
		}
	}
	for _, text := range accepted {
		doc := mustParseDoc(t, text)
		if msg := walkError(doc); msg != "" {
			t.Errorf("probe meant to be accepted is refused by the walk (%s):\n%s", msg, text)
			continue
		}
		if errs := jsValidate(s, s, doc, "$"); len(errs) > 0 {
			t.Errorf("the walk accepts this but the JSON Schema refuses it (%s):\n%s",
				strings.Join(errs, "; "), text)
		}
	}
	for _, text := range rejected {
		doc := mustParseDoc(t, text)
		if walkError(doc) == "" {
			t.Errorf("probe meant to be refused is accepted by the walk:\n%s", text)
			continue
		}
		errs := jsValidate(s, s, doc, "$")
		if len(errs) == 0 {
			t.Errorf("the walk refuses this but the JSON Schema accepts it:\n%s", text)
		}
		for _, e := range errs {
			if strings.Contains(e, "unknown to this test") {
				t.Errorf("probe refused by the validator, not the schema: %s", e)
			}
		}
	}
}

// walkError runs one document through what Resolve runs over the authored
// layers: mergeLayers, the vocabulary walk, then the storage matrix. "" means
// accepted.
func walkError(doc *omap.Map) string {
	layers, err := mergeLayers([]schemaDoc{{name: "probe.yaml", m: doc}})
	if err != nil {
		return err.Error()
	}
	raw := omap.New()
	ont := omap.New()
	ont.Set("entities", layers.entities)
	raw.Set("ontology", ont)
	raw.Set("policy", layers.policy)
	raw.Set("storage", layers.storage)
	sf, verrs := validateSchemaFile(raw)
	if len(verrs) > 0 {
		return smuggledError(verrs).Error()
	}
	if err := finishLayered(sf); err != nil {
		return err.Error()
	}
	return ""
}

func mustParseDoc(t *testing.T, text string) *omap.Map {
	t.Helper()
	m, err := ParseDoc("probe.yaml", text)
	if err != nil {
		t.Fatalf("parse probe: %v\n%s", err, text)
	}
	return m
}

func parseLayerFile(t *testing.T, path string) *omap.Map {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return mustParseDoc(t, string(raw))
}

func dig(t *testing.T, m *omap.Map, path []string) *omap.Map {
	t.Helper()
	var cur any = m
	for _, p := range path {
		switch x := cur.(type) {
		case *omap.Map:
			cur, _ = x.Get(p)
		case []any:
			i, err := strconv.Atoi(p)
			if err != nil {
				t.Fatalf("bad index %q in %v", p, path)
			}
			cur = x[i]
		}
	}
	out, ok := cur.(*omap.Map)
	if !ok {
		t.Fatalf("no mapping at %v", path)
	}
	return out
}

// jsValidate is a draft-07 validator for the keywords LayerJSONSchema emits.
// It returns one line per failure; any keyword it does not know fails, so the
// schema cannot grow a keyword this test silently ignores.
func jsValidate(root, s *omap.Map, v any, at string) []string {
	var out []string
	fail := func(format string, args ...any) {
		out = append(out, at+": "+fmt.Sprintf(format, args...))
	}
	for _, kw := range s.Keys() {
		arg, _ := s.Get(kw)
		switch kw {
		case "$schema", "title", "description", "definitions":
		case "$ref":
			name := strings.TrimPrefix(arg.(string), "#/definitions/")
			defs, _ := root.Get("definitions")
			def, ok := defs.(*omap.Map).Get(name)
			if !ok {
				fail("unresolved $ref %v", arg)
				continue
			}
			out = append(out, jsValidate(root, def.(*omap.Map), v, at)...)
		case "allOf":
			for _, sub := range arg.([]any) {
				out = append(out, jsValidate(root, sub.(*omap.Map), v, at)...)
			}
		case "anyOf":
			var why []string
			for _, sub := range arg.([]any) {
				errs := jsValidate(root, sub.(*omap.Map), v, at)
				if len(errs) == 0 {
					why = nil
					break
				}
				why = append(why, errs...)
			}
			if why != nil {
				fail("matches no branch of anyOf")
				out = append(out, why...)
			}
		case "type":
			types := []any{arg}
			if list, ok := arg.([]any); ok {
				types = list
			}
			if !slices.ContainsFunc(types, func(typ any) bool { return jsIsType(v, typ.(string)) }) {
				fail("%T is not %v", v, arg)
			}
		case "enum":
			if !slices.ContainsFunc(arg.([]any), func(e any) bool { return jsEqual(e, v) }) {
				fail("%v is not one of %v", v, arg)
			}
		case "const":
			if !jsEqual(arg, v) {
				fail("%v is not %v", v, arg)
			}
		case "pattern":
			if str, ok := v.(string); ok && !regexp.MustCompile(arg.(string)).MatchString(str) {
				fail("%q does not match %v", str, arg)
			}
		case "minItems":
			if list, ok := v.([]any); ok && int64(len(list)) < arg.(int64) {
				fail("fewer than %v items", arg)
			}
		case "items":
			if list, ok := v.([]any); ok {
				for i, it := range list {
					out = append(out, jsValidate(root, arg.(*omap.Map), it, fmt.Sprintf("%s[%d]", at, i))...)
				}
			}
		case "required":
			if m, ok := v.(*omap.Map); ok {
				for _, k := range arg.([]any) {
					if _, has := m.Get(k.(string)); !has {
						fail("missing %v", k)
					}
				}
			}
		case "properties":
		case "additionalProperties":
			m, ok := v.(*omap.Map)
			if !ok {
				continue
			}
			props, _ := s.Get("properties")
			for _, k := range m.Keys() {
				val, _ := m.Get(k)
				if props != nil {
					if ps, has := props.(*omap.Map).Get(k); has {
						out = append(out, jsValidate(root, ps.(*omap.Map), val, at+"."+k)...)
						continue
					}
				}
				switch ap := arg.(type) {
				case bool:
					if !ap {
						fail("unknown key %q", k)
					}
				case *omap.Map:
					out = append(out, jsValidate(root, ap, val, at+"."+k)...)
				}
			}
		default:
			fail("keyword %q unknown to this test's validator", kw)
		}
	}
	return out
}

func jsIsType(v any, typ string) bool {
	switch typ {
	case "null":
		return v == nil
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "string":
		switch v.(type) {
		case string, canon.Date, canon.DateTime:
			return true
		}
	case "object":
		_, ok := v.(*omap.Map)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	}
	return false
}

func jsEqual(a, b any) bool {
	fa, aNum := jsNumber(a)
	fb, bNum := jsNumber(b)
	if aNum || bNum {
		return aNum && bNum && fa == fb
	}
	switch a.(type) {
	case string, bool, nil:
		return a == b
	}
	return false
}

func jsNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, !math.IsNaN(x)
	}
	return 0, false
}
