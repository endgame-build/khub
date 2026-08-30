package schema

// Ports the schema-matrix rejection halves of the Python suite: the direct
// TypeDecl storage-matrix tests of tests/test_singletons_templates.py, the
// resolve-level test_schema_matrix of tests/test_collections.py, and the
// format-whitelist rejections of tests/test_format_entities.py. CLI-driven
// halves stay with the fixtures; only the library-level assertions live here.

import (
	"strings"
	"testing"
)

func strp(s string) *string { return &s }

// --- direct TypeDecl construction (pydantic-instantiation tests) -------------

func TestSingletonNeedsPath(t *testing.T) {
	td := &TypeDecl{Layout: "singleton", Format: "md"}
	err := td.storageMatrix()
	if err == nil || !strings.Contains(err.Error(), "exact file") {
		t.Errorf("err = %v, want the exact-file message", err)
	}
	if err != nil && err.Error() != "a singleton type needs path: the exact file it lives at" {
		t.Errorf("message = %q", err.Error())
	}
}

func TestSingletonSuffixMustAgree(t *testing.T) {
	td := &TypeDecl{Layout: "singleton", Path: strp("prd.md"), Format: "yaml", FormatSet: true}
	err := td.storageMatrix()
	if err == nil || !strings.Contains(err.Error(), "disagrees") {
		t.Errorf("err = %v, want the disagrees message", err)
	}
	if err != nil && err.Error() != "path suffix '.md' disagrees with format 'yaml'" {
		t.Errorf("message = %q", err.Error())
	}
}

func TestRequiredIsSingletonOnly(t *testing.T) {
	// Checked in finishLayered, not storageMatrix: `required` is authored in
	// POLICY, so its violation is located there — a storage.<type> loc would
	// send the user to a layer file that may not even mention the type.
	le := resolveLocated(t, matrixBase+"  entities:\n    note: {}\n"+
		"policy:\n  note: { required: true }\n"+
		"storage:\n  note: { layout: file, path: notes }\n")
	want := "Invalid schema at policy.note.required: Value error, " +
		"'required' is singleton-only (a required file/folder/collection " +
		"type has no single artifact to require)"
	if le.Message != want {
		t.Errorf("message = %q, want %q", le.Message, want)
	}
}

func TestTemplateIsPerItemMdOnly(t *testing.T) {
	// A `template:` key (the opt-out included) on a collection or non-md type
	// configures something nothing ever reads — add seeds md only, validate
	// skips non-md and collections — so it is a matrix error, not a permanent
	// `check` finding for a file no verb would consult.
	for _, tc := range []struct{ name, store string }{
		{"collection", "note: { layout: collection, format: yaml, template: decision }"},
		{"non-md", "note: { layout: file, path: notes, format: json, template: decision }"},
		{"opt-out on non-md", "note: { layout: file, path: notes, format: json, template: false }"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			le := resolveLocated(t, matrixBase+"  entities:\n    note: {}\nstorage:\n  "+tc.store+"\n")
			if !strings.Contains(le.Message, "'template' applies only to per-item and singleton md types") {
				t.Errorf("message = %q", le.Message)
			}
			if !strings.Contains(le.Message, "storage.note") {
				t.Errorf("message %q not located at storage.note", le.Message)
			}
		})
	}
	// The legal cells stay legal: a declared name on a file-md and a
	// singleton-md type.
	resolveDocs(t, matrixBase+"  entities:\n    a: {}\n    b: {}\n"+
		"storage:\n  a: { layout: file, path: as, template: decision }\n"+
		"  b: { layout: singleton, path: b.md, template: decision }\n")
}

// --- resolve-level matrix (test_collections.test_schema_matrix) --------------

// matrixBase opens the ontology mapping for `matrixBase + "  entities:..."`
// composition. No base: an authored ontology.base is forbidden, and none of
// the matrix/error fixtures needs one.
const matrixBase = `
ontology:
`

func TestSchemaMatrix(t *testing.T) {
	// collection×md rejected; formatless collection rejected; explicit
	// format/suffix disagreement rejected; reserved row keys rejected; the
	// suffix drives the format when none is authored.
	cases := []struct {
		name  string
		attrs string // repo's ontology attributes fragment ("" = none)
		store string // repo's storage decl
		want  string // "" = resolves; else the exact invalid_schema message
	}{
		{
			name:  "explicit md is never a collection format",
			store: `repo: { layout: collection, format: md, path: repo.md }`,
			want: "Invalid schema at storage.repo: Value error, format 'md' is not a collection " +
				"format; use json, jsonl, or yaml (md is per-item only)",
		},
		{
			name:  "explicit md never rebinds to the path suffix",
			store: `repo: { layout: collection, format: md, path: repo.yaml }`,
			want: "Invalid schema at storage.repo: Value error, format 'md' is not a collection " +
				"format; use json, jsonl, or yaml (md is per-item only)",
		},
		{
			name:  "no format and no path suffix",
			store: `repo: { layout: collection }`,
			want: "Invalid schema at storage.repo: Value error, a collection type needs format: " +
				"json|jsonl|yaml (or a path carrying that extension)",
		},
		{
			name:  "explicit format disagrees with the path suffix",
			store: `repo: { layout: collection, format: yaml, path: repo.jsonl }`,
			want: "Invalid schema at storage.repo: Value error, path suffix '.jsonl' disagrees " +
				"with format 'yaml'",
		},
		{
			name:  "slug is a reserved row key",
			attrs: `attributes: { slug: { type: text } }`,
			store: `repo: { layout: collection, format: jsonl }`,
			want: "Invalid schema at storage.repo: Value error, 'slug' is a reserved row key on a " +
				"collection type (row identity / the schema binding); rename the field",
		},
		{
			name:  "type is a reserved row key",
			attrs: `attributes: { type: { type: text } }`,
			store: `repo: { layout: collection, format: jsonl }`,
			want: "Invalid schema at storage.repo: Value error, 'type' is a reserved row key on a " +
				"collection type (row identity / the schema binding); rename the field",
		},
		{
			name:  "format derived from the path suffix",
			store: `repo: { layout: collection, path: stuff/repo.yaml }`,
			want:  "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decl := "{}"
			if tc.attrs != "" {
				decl = "{ " + tc.attrs + " }"
			}
			doc := matrixBase + "  entities:\n    repo: " + decl + "\nstorage:\n  " + tc.store + "\n"
			if tc.want == "" {
				schema := resolveDocs(t, doc)
				if got := typeOf(t, schema, "repo").Storage.Fmt; got != "yaml" {
					t.Errorf("repo.Storage.Fmt = %q, want yaml (derived from suffix)", got)
				}
				return
			}
			e := resolveLocated(t, doc)
			if e.Code != "invalid_schema" {
				t.Errorf("code = %q, want invalid_schema", e.Code)
			}
			if e.Message != tc.want {
				t.Errorf("message = %q,\nwant      %q", e.Message, tc.want)
			}
		})
	}
}

func TestSchemaRejectsBodyAttributeOnNonMD(t *testing.T) {
	// Declaring an attribute named `body` on a json type fails schema
	// resolution (the reserved prose channel).
	doc := matrixBase + `  entities:
    fragment:
      attributes:
        body: { type: text }
storage:
  fragment: { layout: file, format: json, path: fragments }
`
	e := resolveLocated(t, doc)
	if !strings.Contains(e.Message, "body") || !strings.Contains(e.Message, "reserved") {
		t.Errorf("message = %q, want it to name body + reserved", e.Message)
	}
	want := "Invalid schema at storage.fragment: Value error, 'body' is reserved on a json type " +
		"(it is the prose channel); rename the field or use format: md"
	if e.Message != want {
		t.Errorf("message = %q,\nwant      %q", e.Message, want)
	}
}

func TestSchemaRejectsUnimplementedFormats(t *testing.T) {
	// jsonl (collection-only) and gjson (deferred) fail schema resolution
	// loudly, on every layout.
	cases := []struct {
		name string
		decl string
		want string
	}{
		{
			name: "jsonl on a file layout",
			decl: `fragment: { layout: file, format: jsonl }`,
			want: "Invalid schema at storage.fragment: Value error, format 'jsonl' is not " +
				"supported for a file/folder layout; use md, json, or yaml (jsonl is " +
				"collection-only, gjson is deferred)",
		},
		{
			name: "gjson on a folder layout",
			decl: `fragment: { layout: folder, format: gjson }`,
			want: "Invalid schema at storage.fragment: Value error, format 'gjson' is not " +
				"supported for a file/folder layout; use md, json, or yaml (jsonl is " +
				"collection-only, gjson is deferred)",
		},
		{
			name: "gjson on a singleton",
			decl: `fragment: { layout: singleton, path: data.gjson }`,
			want: "Invalid schema at storage.fragment: Value error, format 'gjson' is not " +
				"supported for a singleton; use md, json, or yaml",
		},
		{
			name: "gjson on a collection",
			decl: `fragment: { layout: collection, format: gjson, path: r.gjson }`,
			want: "Invalid schema at storage.fragment: Value error, format 'gjson' is not a " +
				"collection format; use json, jsonl, or yaml (md is per-item only)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := resolveLocated(t, matrixBase+"  entities:\n    fragment: {}\nstorage:\n  "+tc.decl+"\n")
			if e.Message != tc.want {
				t.Errorf("message = %q,\nwant      %q", e.Message, tc.want)
			}
		})
	}
}

func TestRequiredSingletonResolves(t *testing.T) {
	// The singleton-bearing preset shape of test_singletons_templates.PRESET:
	// `required: true` is legal on a singleton and carries onto the resolved
	// type; the format derives md from the path.
	doc := matrixBase + `  entities:
    prd:
      attributes:
        title: { required: true }
policy:
  prd: { required: true }
storage:
  prd: { layout: singleton, path: knowledge/prd.md }
`
	prd := typeOf(t, resolveDocs(t, doc), "prd")
	if !prd.Required {
		t.Error("prd.Required = false, want true")
	}
	if prd.Storage.Fmt != "md" {
		t.Errorf("prd.Storage.Fmt = %q, want md", prd.Storage.Fmt)
	}
}

func TestCollectionRelpathDefaultRule(t *testing.T) {
	// model.ResolvedType.collection_relpath: path else "{name}.{fmt}".
	doc := matrixBase + `  entities:
    repo: {}
    other: {}
storage:
  repo:  { layout: collection, format: jsonl }
  other: { layout: collection, path: data/repos.yaml }
`
	schema := resolveDocs(t, doc)
	if got := typeOf(t, schema, "repo").CollectionRelpath(); got != "repo.jsonl" {
		t.Errorf("repo.CollectionRelpath() = %q, want repo.jsonl", got)
	}
	if got := typeOf(t, schema, "other").CollectionRelpath(); got != "data/repos.yaml" {
		t.Errorf("other.CollectionRelpath() = %q, want data/repos.yaml", got)
	}
}
