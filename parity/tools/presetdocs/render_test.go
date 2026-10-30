package main

import (
	"strings"
	"testing"
)

const fakeSchema = `{"provenance": {"preset": "demo", "version": "1.2.3"}, "types": [
 {"name": "note", "layout": "file", "format": "md", "path": "notes", "id_prefix": null, "id_date": false,
  "id_shape": "slug", "required": false, "orphan": false, "when": "a thought  is worth keeping.",
  "fields": [
   {"name": "type", "type": "text", "required": true, "enum": null, "pattern": null, "default": null},
   {"name": "title", "type": "text", "required": true, "enum": null, "pattern": null, "default": null},
   {"name": "code", "type": "text", "required": false, "enum": null, "pattern": "^(a|b)$", "default": null},
   {"name": "live", "type": "bool", "required": false, "enum": null, "pattern": null, "default": true}],
  "relations": [
   {"predicate": "depends_on", "to": ["any"], "kind": "any", "many": true, "required": false, "inverse": null, "acyclic": true, "derived": false},
   {"predicate": "owner", "to": ["person"], "kind": "typed", "many": false, "required": true, "inverse": null, "acyclic": false, "derived": false}]},
 {"name": "person", "layout": "singleton", "format": "md", "path": "me.md", "id_prefix": null, "id_date": false,
  "id_shape": null, "required": true, "orphan": true, "when": null,
  "fields": [{"name": "type", "type": "text", "required": true, "enum": null, "pattern": null, "default": null}],
  "relations": [
   {"predicate": "depends_on", "to": ["person"], "kind": "typed", "many": true, "required": false, "inverse": null, "acyclic": true, "derived": false},
   {"predicate": "owner", "to": ["person"], "kind": "typed", "many": false, "required": true, "inverse": null, "acyclic": false, "derived": false},
   {"predicate": "owns", "to": ["note", "person"], "kind": "union", "many": true, "required": false, "inverse": "owner", "acyclic": false, "derived": true}]},
 {"name": "deal", "layout": "folder", "format": "md", "path": "deals", "id_ordinal": true, "path_refs": true,
  "id_prefix": {"by": "stage", "map": {"won": "w", "lost": "l"}}, "id_date": false,
  "id_shape": null, "required": false, "orphan": false, "when": "is it worth it?",
  "fields": [], "relations": []}]}`

const fakeBase = `{"fields": [
  {"name": "type", "type": "text", "required": true, "enum": null, "pattern": null, "default": null},
  {"name": "title", "type": "text", "required": false, "enum": null, "pattern": null, "default": null}],
 "relations": [
  {"predicate": "depends_on", "to": ["any"], "kind": "any", "many": true, "required": false, "inverse": null, "acyclic": true, "derived": false}]}`

func fixtures(t *testing.T) (*schemaDoc, *baseDoc) {
	t.Helper()
	var s schemaDoc
	var b baseDoc
	if err := decode([]byte(fakeSchema), &s); err != nil {
		t.Fatal(err)
	}
	if err := decode([]byte(fakeBase), &b); err != nil {
		t.Fatal(err)
	}
	return &s, &b
}

func begin(n string) string { return "<!-- BEGIN GENERATED " + n + " -->" }
func end(n string) string   { return "<!-- END GENERATED " + n + " -->" }

func doc(regions ...string) string {
	parts := []string{"# Demo", "", "Hand prose stays."}
	for _, r := range regions {
		parts = append(parts, "", begin(r), "stale text", end(r))
	}
	return strings.Join(append(parts, "", "Closing prose.", ""), "\n")
}

func TestSpliceRewritesRegionsAndKeepsProse(t *testing.T) {
	s, b := fixtures(t)
	got, problems := splice(doc("summary", "relations", "type note", "type person", "type deal"), s, b)
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	for _, want := range []string{
		"Hand prose stays.", "Closing prose.",
		"Preset version **1.2.3**. 3 entity types, and 2 relation predicates: 1 declared by the preset and 1 from the base block. Computed inverses, never stored: `owns`.",
		"- **Base attributes**: `type`, `title`.",
		"| `owner` | note, person | person | no | yes | no |  |",
		"| `depends_on` | person | person | yes | no | yes |  |",
		"| `depends_on` | every other type | any | yes | no | yes |  |",
		"| `title` (base, redeclared) | text | yes |  |  |",
		"| `code` | text | no | pattern `^(a\\|b)$` |  |",
		"| `live` | bool | no |  | `true` |",
		"Record one when a thought is worth keeping.",
		"Layout `file`, format `md`, stored at `notes/{slug}.md`, ids `slug`.",
		"Layout `folder`, format `md`, stored at `deals/{slug}/_index.md`. The id prefix follows `stage`: `won` → `w`, `lost` → `l`. Accepts a legacy `NNN-` slug ordinal. A relation value that names no entity may name a workspace path.",
		"Record one when is it worth it?\n",
		"Layout `singleton`, format `md`, stored at `me.md`. Required: `check` fails until one exists. Exempt from the orphan sweep.",
		"Attributes: only the base block.",
		"Computed: `owns` → note | person (many, inverse of `owner`).",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "stale text") {
		t.Errorf("stale region content survived:\n%s", got)
	}
	again, problems := splice(got, s, b)
	if len(problems) > 0 || again != got {
		t.Errorf("second run is not a no-op (problems %v)", problems)
	}
}

func TestSpliceTypesRegionAddsHeadings(t *testing.T) {
	s, b := fixtures(t)
	got, problems := splice(doc("summary", "relations", "types"), s, b)
	if len(problems) > 0 {
		t.Fatalf("problems: %v", problems)
	}
	if !strings.Contains(got, "### note\n\nLayout `file`") || !strings.Contains(got, "### person\n\n") {
		t.Errorf("types region lacks headings:\n%s", got)
	}
}

func TestSpliceRefusesWhatWouldPassByOmission(t *testing.T) {
	s, b := fixtures(t)
	cases := map[string]struct {
		doc  string
		want string
	}{
		"no summary":       {doc("relations", "types"), "no BEGIN GENERATED summary region"},
		"no relations":     {doc("summary", "types"), "no BEGIN GENERATED relations region"},
		"uncovered type":   {doc("summary", "relations", "type note", "type deal"), "type person has no generated section"},
		"twice":            {doc("summary", "relations", "types", "type note"), "type note is generated 2 times"},
		"unknown type":     {doc("summary", "relations", "types", "type ghost"), "type ghost is not in the demo schema"},
		"unknown region":   {doc("summary", "relations", "types", "tables"), `unknown region "tables"`},
		"duplicate region": {doc("summary", "summary", "relations", "types"), "region summary appears 2 times"},
		"unclosed": {
			"# Demo\n" + begin("summary") + "\n" + doc("relations", "types"),
			"has no matching END GENERATED summary",
		},
		"stray end": {doc("summary", "relations", "types") + end("summary") + "\n", "END GENERATED summary without a BEGIN"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, problems := splice(c.doc, s, b)
			joined := strings.Join(problems, "\n")
			if !strings.Contains(joined, c.want) {
				t.Errorf("want a problem containing %q, got:\n%s", c.want, joined)
			}
		})
	}
}

func TestUnrenderableSchemaFailsLoudly(t *testing.T) {
	var s schemaDoc
	if err := decode([]byte(`{"provenance": {"preset": "x", "version": "1"}, "types": [], "brand_new": 1}`), &s); err == nil {
		t.Error("an unknown top-level key decoded silently")
	}
	if err := decode([]byte(`{"provenance": {"preset": "x", "version": "1"}, "types": [{"name": "a", "brand_new": true}]}`), &s); err == nil {
		t.Error("an unknown type key decoded silently")
	}
	bad := schemaDoc{Types: []typeDef{
		{Name: "a", Layout: "shard", Format: "md"},
		{Name: "b", Layout: "file", Format: "md", Relations: []relation{{Predicate: "p", To: []string{"a"}, Kind: "weighted"}}},
	}}
	joined := strings.Join(validate(&bad), "\n")
	for _, want := range []string{`type a: layout "shard"`, `relation p has kind "weighted"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("want %q in:\n%s", want, joined)
		}
	}
}

func TestSentenceKeepsQuestionMarks(t *testing.T) {
	for in, want := range map[string]string{"a": "a.", "a.": "a.", "a?": "a?", "a!": "a!"} {
		if got := sentence(in); got != want {
			t.Errorf("sentence(%q) = %q, want %q", in, got, want)
		}
	}
}
