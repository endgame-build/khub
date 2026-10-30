package rdf

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/workspace"
)

func TestValidateBase(t *testing.T) {
	for _, ok := range []string{"urn:khub:", "https://example.org/kb/", "http://x/", "urn:x:y#", "tag:a,2026:", "a+b.c-d:"} {
		if err := ValidateBase(ok); err != nil {
			t.Errorf("ValidateBase(%q) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []string{"", "http://x", "urn:foo", "rel/path", "a b:", "100%", "urn:x y/", "1urn:", "urn:a\tb/", "http://x/<y>/"} {
		err := ValidateBase(bad)
		var located *errs.Located
		if !errors.As(err, &located) || located.Code != "invalid_base" {
			t.Errorf("ValidateBase(%q) = %v, want invalid_base", bad, err)
		}
	}
}

// The base block mints into the khub: namespace beside the annotation
// terms; a base attribute named like one would alias it silently.
func TestVocabularyAndBaseNamesAreDisjoint(t *testing.T) {
	resolved, err := introspect.LoadSchema(facetsWS(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vocabulary {
		local := strings.TrimPrefix(v.term.Prefixed, "khub:")
		// The discriminator maps to rdf:type and mints nothing, so khub:type
		// (the scalar kind) is free to share its spelling.
		if local == TypeKey {
			continue
		}
		if resolved.BaseAttributes.Has(local) || resolved.BaseRelations.Has(local) {
			t.Errorf("base block declares %q, which collides with the vocabulary term khub:%s", local, local)
		}
	}
}

func TestMintIRIs(t *testing.T) {
	ns := Mint("", "firm-ops", "ws")
	if ns.Vocab != "urn:khub:vocab/" || ns.Ontology != "urn:khub:ontology/firm-ops/" ||
		ns.Type != "urn:khub:ontology/firm-ops/type/" || ns.Field != "urn:khub:ontology/firm-ops/field/" ||
		ns.Shape != "urn:khub:ontology/firm-ops/shape/" || ns.Entity != "urn:khub:workspace/ws/" {
		t.Errorf("default namespaces: %+v", ns)
	}
	custom := Mint("https://acme.example/kb/", "firm-ops", "my hub")
	if custom.Vocab != VocabIRI {
		t.Error("--base must not move the khub vocabulary")
	}
	if custom.Type != "https://acme.example/kb/ontology/firm-ops/type/" ||
		custom.Entity != "https://acme.example/kb/workspace/my%20hub/" {
		t.Errorf("custom base: %+v", custom)
	}
	if noPreset := Mint("", "", "local"); noPreset.Ontology != "urn:khub:ontology/local/" {
		t.Errorf("a workspace without a preset keys the ontology by its own name: %s", noPreset.Ontology)
	}
}

func TestPortableAndAnchored(t *testing.T) {
	portable := map[string]string{
		`^[0-9]+$`:                `^[0-9]+$`,
		`^[a-z0-9-]+/[a-z0-9-]+$`: `^[a-z0-9-]+/[a-z0-9-]+$`,
		`a|b`:                     `a|b`,
		`x\.y\-z\[\]`:             `x\.y\-z\[\]`,
		`\p{L}+\P{Nd}`:            `\p{L}+\P{Nd}`,
		`^"[^"]*"$`:               `^"[^"]*"$`,
		`\n\t`:                    `\n\t`,
		`^x\$`:                    `^x\$`,
		// RE2's shorthands are ASCII; XPath's are Unicode, so the ASCII classes go out.
		`\d{3}-\d{4}`: `[0-9]{3}-[0-9]{4}`,
		`[^\s]+`:      "[^\\t\\n\\r\f ]+",
		`\w+\W`:       `[A-Za-z0-9_]+[^A-Za-z0-9_]`,
		`[\d\w]`:      `[0-9A-Za-z0-9_]`,
		`\S\D`:        "[^\\t\\n\\r\f ][^0-9]",
	}
	for in, want := range portable {
		got, ok := PortablePattern(in)
		if !ok || got != want {
			t.Errorf("PortablePattern(%q) = %q, %v; want %q, true", in, got, ok, want)
		}
	}
	unportable := []string{`(?i)abc`, `\Aabc\z`, `\bword\b`, `\x41`, `[[:alpha:]]`, `[a-z-[aeiou]]`, `\pL`,
		`\p{Greek}`, `\Qlit\E`, `\1`, `tail\`, `^[a-z]+\/[a-z]+$`, `\#`, `\a`, `[\D]`, `[\S]`}
	for _, p := range unportable {
		if got, ok := PortablePattern(p); ok {
			t.Errorf("PortablePattern(%q) = %q, true; want refused", p, got)
		}
	}
	anchored := map[string]string{
		`^[0-9]+$`:  `^[0-9]+$`,
		`[0-9]+`:    `^([0-9]+)$`,
		`^a|b$`:     `^(^a|b$)$`,
		`^(a|b)$`:   `^(a|b)$`,
		`^[|]$`:     `^[|]$`,
		`^a\$`:      `^(^a\$)$`,
		`^a\\$`:     `^a\\$`,
		`^\(a\|b$`:  `^\(a\|b$`,
		`^(a\)|b)$`: `^(a\)|b)$`,
	}
	for in, want := range anchored {
		if got := Anchored(in); got != want {
			t.Errorf("Anchored(%q) = %q, want %q", in, got, want)
		}
	}
}

// A preset exercising every facet the mapping has a rule for: a union
// target, an enum, a portable and an unportable pattern, a required relation,
// an acyclic one with an inverse, a list, a number with a default, a
// singleton under policy, a name that is an attribute on one type and a
// relation on another, and a type name that fits no prefixed name.
const facetsOntology = `
version: "1.0.0"
ontology:
  entities:
    note:
      when: capture it
      attributes:
        title: { required: true }
        kind:  { enum: [memo, todo], required: true }
        score: { type: number, default: 1.5 }
        code:  { type: text, pattern: '^[a-z]{2}$' }
        ref:   { type: text, pattern: '(?i)ok' }
        items: { type: list }
        link:  { type: text }
        when:  { type: date }
      relations:
        about: { to: [note, team.lead], required: true }
        follows: { to: note, acyclic: true, inverse: followed_by }
        cites: { to: any, required: true }
    team.lead:
      attributes:
        name: { type: text, required: true }
      relations:
        link: { to: note, many: true }
    charter:
      attributes:
        title: {}
`

const facetsPolicy = `
policy:
  charter: { required: true, orphan: true }
  note: { path_refs: true }
`

const facetsStorage = `
storage:
  note: { layout: file, path: notes, id_prefix: nt, id_date: true }
  team.lead: { layout: collection, format: yaml, path: leads.yaml }
  charter: { layout: singleton, path: charter.md }
`

func facetsWS(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	for name, text := range map[string]string{
		"ontology.yaml": facetsOntology, "policy.yaml": facetsPolicy, "storage.yaml": facetsStorage,
	} {
		p := filepath.Join(src, "facets", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	if _, err := workspace.Init("facets", root, workspace.InitOptions{PresetSource: src, Name: "lab"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	return root
}

func renderTBox(t *testing.T, root string) string {
	t.Helper()
	res, err := Render(root, Options{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return res.Content
}

func TestMappingFacets(t *testing.T) {
	ttl := renderTBox(t, facetsWS(t))
	for _, want := range []string{
		// The union edge is sh:or over the targets; the odd type name goes out as a full IRI.
		"sh:or (\n            [ sh:class type:note ]\n            [ sh:class <urn:khub:ontology/facets/type/team.lead> ]\n        ) ;\n        sh:minCount 1 ;\n        sh:maxCount 1 ;\n        khub:kind \"union\" ;\n        khub:pathRefs true",
		`sh:in ( "memo" "todo" )`,
		"sh:pattern \"^[a-z]{2}$\" ;\n        khub:pattern \"^[a-z]{2}$\"",
		// The unportable pattern keeps only the khub: annotation.
		"sh:maxCount 1 ;\n        khub:pattern \"(?i)ok\" ;\n        khub:type \"text\"",
		"sh:datatype xsd:decimal ;\n        sh:maxCount 1 ;\n        khub:type \"number\" ;\n        khub:default \"1.5\"^^xsd:decimal",
		// Nine base slots (type skipped), title overriding in place, then kind, score, code, ref.
		"sh:name \"items\" ;\n        sh:order 13 ;\n        khub:type \"list\"",
		"khub:acyclic true ;\n        khub:inverse \"followed_by\"",
		// A date admits a datetime, as validate does.
		"sh:name \"when\" ;\n        sh:order 15 ;\n        sh:or (\n            [ sh:datatype xsd:date ]\n            [ sh:datatype xsd:dateTime ]\n        ) ;\n        sh:maxCount 1 ;\n        khub:type \"date\"",
		// A `to: any` relation under path_refs admits a file node.
		"sh:name \"cites\" ;\n        sh:order 22 ;\n        sh:nodeKind sh:IRI ;\n        sh:or (\n            [ sh:class khub:Entity ]\n            [ sh:class khub:File ]\n        ) ;\n        sh:minCount 1 ;\n        sh:maxCount 1 ;\n        khub:kind \"any\" ;\n        khub:pathRefs true",
		"khub:File a rdfs:Class",
		// The singleton's policy and storage.
		"type:charter a rdfs:Class ;\n    rdfs:subClassOf khub:Entity ;\n    rdfs:label \"charter\" ;\n    khub:layout \"singleton\" ;\n    khub:format \"md\" ;\n    khub:path \"charter.md\" ;\n    khub:requiredSingleton true ;\n    khub:orphanExempt true .",
		"khub:idShape \"nt-YYYY-MM-DD-slug\"",
		"khub:when \"capture it\"",
		`<urn:khub:ontology/facets/type/team.lead> a rdfs:Class`,
		"khub:layout \"collection\" ;\n    khub:format \"yaml\" ;\n    khub:path \"leads.yaml\"",
		// `link` is an attribute on note and a relation on team.lead: one shared rdf:Property.
		"field:link a rdf:Property ;\n    rdfs:label \"link\" ;\n    rdfs:comment \"An attribute on some types and a relation on others; each shape says which.\"",
		"field:kind a rdf:Property, owl:DatatypeProperty",
		"field:about a rdf:Property, owl:ObjectProperty",
		"khub:title a rdf:Property, owl:DatatypeProperty",
		"<urn:khub:ontology/facets/> a owl:Ontology ;\n    rdfs:label \"facets\" ;\n    khub:preset \"facets\" ;\n    khub:presetVersion \"1.0.0\" ;\n    khub:workspace \"lab\"",
	} {
		if !strings.Contains(ttl, want) {
			t.Errorf("render lacks:\n%s\n--- render\n%s", want, ttl)
		}
	}
	if strings.Contains(ttl, `sh:path field:type`) || strings.Contains(ttl, `sh:path khub:type `) {
		t.Error("the type discriminator maps to rdf:type, never to a property shape")
	}
	if strings.Contains(ttl, "sh:pattern \"^((?i)ok)$\"") {
		t.Error("an unportable pattern must not reach sh:pattern")
	}
}

func TestRenderIsDeterministic(t *testing.T) {
	root := facetsWS(t)
	if a, b := renderTBox(t, root), renderTBox(t, root); a != b {
		t.Error("two renders of one schema differ")
	}
}

// Every type, attribute and relation of the resolved schema reaches the
// document — the lossless-over-the-ontology claim, checked by name.
func TestEveryDeclarationIsProjected(t *testing.T) {
	root := facetsWS(t)
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	ttl := renderTBox(t, root)
	for _, tn := range resolved.Types.Keys() {
		rt, _ := resolved.Types.Get(tn)
		if !strings.Contains(ttl, `rdfs:label "`+tn+`"`) {
			t.Errorf("type %s has no class", tn)
		}
		for _, name := range rt.FieldNames() {
			if name == TypeKey {
				continue
			}
			if !strings.Contains(ttl, `sh:name "`+name+`"`) {
				t.Errorf("%s.%s has no property shape", tn, name)
			}
		}
	}
}

// The syntactic gate the golden fixture cannot give: with no Turtle parser
// in the module, a strict scanner checks what a parser would. Every IRIREF
// is absolute and holds no forbidden character, every prefixed name uses a
// declared prefix, every literal is closed with only legal escapes inside,
// brackets balance, and every statement ends in a dot.
func TestRenderScansAsTurtle(t *testing.T) {
	root := facetsWS(t)
	res, err := Render(root, Options{Instances: true})
	if err != nil {
		t.Fatal(err)
	}
	scanTurtle(t, res.Content)
}

var (
	schemeRE   = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	prefixedRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*)?:([A-Za-z_][A-Za-z0-9_-]*)?$`)
)

func scanTurtle(t *testing.T, text string) {
	t.Helper()
	prefixes := map[string]bool{}
	depth, lists := 0, 0
	lastSig := byte(0)
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "@prefix ") {
			parts := strings.Fields(line)
			if len(parts) != 4 || parts[3] != "." || !strings.HasSuffix(parts[1], ":") {
				t.Fatalf("malformed prefix line: %s", line)
			}
			prefixes[strings.TrimSuffix(parts[1], ":")] = true
			checkIRIRef(t, parts[2], line)
			continue
		}
		i := 0
		for i < len(line) {
			c := line[i]
			switch c {
			case ' ':
				i++
			case '<':
				end := strings.IndexByte(line[i:], '>')
				if end < 0 {
					t.Fatalf("unterminated IRI: %s", line)
				}
				checkIRIRef(t, line[i:i+end+1], line)
				i += end + 1
				lastSig = '>'
			case '"':
				j := i + 1
				for j < len(line) && line[j] != '"' {
					if line[j] == '\\' {
						if j+1 >= len(line) || strings.IndexByte(`tbnrf"'\u`, line[j+1]) < 0 {
							t.Fatalf("illegal escape in literal: %s", line)
						}
						if line[j+1] == 'u' {
							j += 4
						}
						j++
					}
					if line[j] < 0x20 || line[j] == 0x7f {
						t.Fatalf("raw control character in literal: %q", line)
					}
					j++
				}
				if j >= len(line) {
					t.Fatalf("unterminated literal: %s", line)
				}
				i = j + 1
				if strings.HasPrefix(line[i:], "^^") {
					end := i + 2
					for end < len(line) && line[end] != ' ' {
						end++
					}
					checkName(t, line[i+2:end], prefixes, line)
					i = end
				}
				lastSig = '"'
			case '[':
				depth++
				i++
				lastSig = c
			case ']':
				depth--
				i++
				lastSig = c
			case '(':
				lists++
				i++
				lastSig = c
			case ')':
				lists--
				i++
				lastSig = c
			case ';', ',', '.':
				i++
				lastSig = c
			default:
				end := i
				for end < len(line) && line[end] != ' ' && strings.IndexByte(",;)]", line[end]) < 0 &&
					(line[end] != '.' || end != len(line)-1) {
					end++
				}
				tok := line[i:end]
				switch tok {
				case "a", "true", "false":
				default:
					if _, isNum := numeric(tok); !isNum {
						checkName(t, tok, prefixes, line)
					}
				}
				i = end
				lastSig = 'n'
			}
		}
	}
	if depth != 0 || lists != 0 {
		t.Errorf("unbalanced brackets: [ depth %d, ( depth %d", depth, lists)
	}
	if lastSig != '.' {
		t.Errorf("document does not end in a statement terminator")
	}
}

func numeric(tok string) (string, bool) {
	if tok == "" {
		return "", false
	}
	for _, c := range tok {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	return tok, true
}

func checkIRIRef(t *testing.T, ref, line string) {
	t.Helper()
	if !strings.HasPrefix(ref, "<") || !strings.HasSuffix(ref, ">") {
		t.Fatalf("not an IRIREF: %s in %s", ref, line)
	}
	iri := ref[1 : len(ref)-1]
	if !schemeRE.MatchString(iri) {
		t.Errorf("relative IRI %s in %s", ref, line)
	}
	if strings.ContainsAny(iri, " <>\"{}|^`\\") {
		t.Errorf("forbidden character in IRI %s", ref)
	}
}

func checkName(t *testing.T, tok string, prefixes map[string]bool, line string) {
	t.Helper()
	m := prefixedRE.FindStringSubmatch(tok)
	if m == nil {
		t.Fatalf("not a prefixed name: %q in %s", tok, line)
	}
	if !prefixes[m[1]] {
		t.Errorf("undeclared prefix %q in %s", m[1], line)
	}
}
