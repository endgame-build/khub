package okfimport

// FuzzImportRead feeds one attacker-written concept file through the per-file
// half of import: parseConcept, then resolveLinks against a bundle whose
// node table also holds ids outside the bundle root. `go test` runs the seeds;
// `go test -fuzz=FuzzImportRead ./internal/okfimport` explores.

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/values"
	"github.com/endgame-build/khub/internal/workspace"
)

// fuzzOntology declares one md type with a relation of each cardinality and
// export's `served_by`, so a fuzzed `# Related` section can resolve.
const fuzzOntology = `
version: "0.1.0"
ontology:
  entities:
    note:
      relations:
        cites: { to: any, many: true }
        lead: { to: note }
        served_by: { to: any, many: true }
`

const fuzzStorage = `
storage:
  note: { layout: file, path: notes }
`

// fuzzTypeMap lands every OKF type the seeds use in note.
var fuzzTypeMap = map[string]string{
	"Fragment": "note", "Playbook": "note", "Metric": "note", "Attested Computation": "note",
	"client": "note", "Client": "note", "person": "note", "opportunity": "note", "project": "note",
	"component": "note", "requirement": "note", "use-case": "note", "fragment": "note",
}

// fuzzIDs are the bundle paths a fuzzed concept is read from.
var fuzzIDs = []string{"a", "notes/b", "deep/x/y", "opp/_index"}

// escapes are node ids whose file lies outside the bundle root. An id is its
// bundle path less `.md`, so `..` is the file `...md` inside the root, not an
// escape (the seed in testdata/fuzz records it). The backslash ids escape on
// Windows, and classify reads a backslash as a separator everywhere. A link
// that resolves to one has left the bundle.
var escapes = []string{
	"../..", "../secret", "../../secret", "../../../etc/passwd", "/secret", "/etc/passwd",
	"../a", "../notes/b", "..\\secret", "..\\..\\secret",
}

// importCase finds the concept files a parity import case writes: heredocs
// and printf one-liners into a `.md` path.
var (
	heredoc = regexp.MustCompile(`^(\s*)cat > \S+\.md <<'(\w+)'$`)
	printfs = regexp.MustCompile(`printf -- '((?:[^']|'\\'')*)' > \S+\.md$`)
)

// seedImport adds every concept file the parity import cases write, the
// alias bomb as frontmatter, and hand-written links aimed outside the bundle.
func seedImport(f *testing.F) {
	f.Helper()
	cases, err := filepath.Glob("../../parity/cases/import/*/case.yaml")
	if err != nil {
		f.Fatal(err)
	}
	n := 0
	for _, file := range cases {
		raw, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}
		lines := strings.Split(string(raw), "\n")
		for i := 0; i < len(lines); i++ {
			if m := printfs.FindStringSubmatch(lines[i]); m != nil {
				f.Add(uint8(n), strings.ReplaceAll(m[1], `\n`, "\n"))
				n++
				continue
			}
			m := heredoc.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			var doc strings.Builder
			for i++; i < len(lines) && strings.TrimSpace(lines[i]) != m[2]; i++ {
				doc.WriteString(strings.TrimPrefix(lines[i], m[1]) + "\n")
			}
			f.Add(uint8(n), doc.String())
			n++
		}
	}
	if n < 20 {
		f.Fatalf("suspiciously few import seeds: %d", n)
	}
	bomb, err := os.ReadFile("../canon/testdata/alias-bomb.yaml")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(uint8(0), "---\ntype: note\n"+string(bomb)+"---\n")
	f.Add(uint8(0), "---\ntype: note\nsources: &s [{resource: ../../secret.md}]\nmore: [*s, *s, *s]\n---\n")
	f.Add(uint8(2), "---\ntype: note\nsources:\n- resource: ../../a.md\n---\n"+
		"[b](../../notes/b.md) [c](/notes/c.md) [b2](..%2F..%2Fnotes%2Fb.md) [c2](..\\..\\notes\\c.md#x)\n")
	for i, body := range []string{
		"[up](../secret.md) [root](/../secret.md) [abs](/etc/passwd.md) [deep](../../../etc/passwd.md)",
		"[enc](%2e%2e/secret.md) [enc2](%2E%2E%2F%2E%2E%2Fsecret.md) [slash](%2f..%2fsecret.md)",
		"[bs](..\\secret.md) [bs2](..\\..\\secret.md) [mixed](..%5csecret.md) [drive](C:\\secret.md)",
		"[angle](<../secret.md>) [q](../secret.md?x=1#y) [frag](../a.md#top) [self](a.md)",
		"[ok](../notes/b.md) [sib](b.md) [file](file:///etc/passwd.md) [dots](....//secret.md)",
		"\n# Related\n\n- cites: [x](/../secret.md)\n- lead: [y](</../../secret.md>)\n- served_by: [z](/notes/b.md)\n",
	} {
		f.Add(uint8(i), "---\ntype: note\nsources:\n- resource: ../secret.md\n- resource: /../../secret.md\n---\n"+body+"\n")
	}
}

// FuzzImportRead: one concept file's bytes, read from any bundle path, either
// land as a concept or are refused with a code and a message; never panic;
// past canon's 4 MiB cap refuse as unreadable; quarantine `verified` and
// force a draft;
// and no link, `sources[].resource` or `# Related` entry resolves outside the
// bundle root.
func FuzzImportRead(f *testing.F) {
	seedImport(f)
	root := f.TempDir()
	src := f.TempDir()
	for rel, text := range map[string]string{"fz/ontology.yaml": fuzzOntology, "fz/storage.yaml": fuzzStorage} {
		p := filepath.Join(src, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			f.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			f.Fatal(err)
		}
	}
	if _, err := workspace.Init("fz", root, workspace.InitOptions{PresetSource: src}); err != nil {
		f.Fatalf("init: %v", err)
	}
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		f.Fatal(err)
	}
	nodes := map[string]index.Node{"notes/c": {Type: "note", Slug: "c"}}
	for _, id := range fuzzIDs {
		nodes[id] = index.Node{Type: "note", Slug: path.Base(id)}
	}
	for _, id := range escapes {
		nodes[id] = index.Node{Type: "escaped", Slug: id}
	}
	refusals := map[string]bool{"symlink": true, "unreadable": true, "no_frontmatter": true,
		"malformed_frontmatter": true, "missing_type": true, "unmapped_type": true,
		errs.QuarantineTaken("", "").Code: true}
	file := filepath.Join(f.TempDir(), "concept.md")

	f.Fuzz(func(t *testing.T, sel uint8, text string) {
		id := fuzzIDs[int(sel)%len(fuzzIDs)]
		if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		c, skip := parseConcept(id, file, resolved, fuzzTypeMap)
		if (c == nil) == (skip == nil) {
			t.Fatalf("want a concept or a refusal, got %+v and %+v", c, skip)
		}
		if skip != nil {
			if !refusals[skip.Reason] || skip.Message == "" || skip.Concept != id {
				t.Fatalf("refusal without a known code and message: %+v", skip)
			}
			if len(text) > canon.MaxTextBytes && skip.Reason != "unreadable" {
				t.Fatalf("%d bytes past the cap refused as %s", len(text), skip.Reason)
			}
			return
		}
		if !resolved.Types.Has(c.raw.Type) {
			t.Fatalf("landed in undeclared type %q", c.raw.Type)
		}
		if s, _ := c.raw.Meta.Get("status"); s != values.StatusDraft {
			t.Fatalf("status %#v, want draft", s)
		}
		if _, has := c.raw.Meta.Get("verified"); has {
			t.Fatal("`verified` not quarantined")
		}
		if _, err := canon.EncodeCLI(c.raw.Meta); err != nil {
			t.Fatalf("render: %v", err)
		}

		c.node = nodes[id]
		res := &Result{}
		spell := func(n index.Node) string {
			if n.Type == "escaped" {
				t.Fatalf("a link from %s resolved outside the bundle, to %q:\n%q", id, n.Slug, text)
			}
			return n.Slug
		}
		// Every resolved target reaches spell before it becomes an edge.
		resolveLinks(c, resolved, nodes, spell, res)
	})
}
