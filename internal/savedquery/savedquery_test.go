package savedquery

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/query"
)

func workspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".khub"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func str(s string) *string { return &s }

func storeText(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, ".khub", "queries.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func code(err error) string {
	var located *errs.Located
	if errors.As(err, &located) {
		return located.Code
	}
	return ""
}

func TestDefinitionRoundTrip(t *testing.T) {
	root := workspace(t)
	fields := omap.New()
	fields.Set("tier", "2024")
	fields.Set("kind", "yes")
	fields.Set("owner", "null")
	limit := 7
	in := query.Filters{
		Type: str("component"), Fields: fields, Tag: str("infra"), Has: str("depends_on"),
		Missing: str("repo"), Orphan: true, Stale: true, DraftOnly: true, ActiveOnly: true, Limit: &limit,
	}
	if _, err := Save(root, "everything", Definition(in), false); err != nil {
		t.Fatal(err)
	}
	def, err := Get(root, "everything")
	if err != nil {
		t.Fatal(err)
	}
	out, err := Filters("everything", def)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round trip moved the filters:\n in  %+v\n out %+v", in, out)
	}
	want := header + "everything:\n  type: component\n  tier: '2024'\n  kind: yes\n  owner: 'null'\n" +
		"  tag: infra\n  has: depends_on\n  missing: repo\n  orphan: true\n  stale: true\n" +
		"  draft: true\n  active: true\n  limit: 7\n"
	if got := storeText(t, root); got != want {
		t.Fatalf("store bytes:\n%s\nwant:\n%s", got, want)
	}
}

func TestOverwriteRule(t *testing.T) {
	root := workspace(t)
	a := Definition(query.Filters{Type: str("component")})
	b := Definition(query.Filters{Type: str("api")})
	if got, err := Save(root, "q", a, false); err != nil || got != Saved {
		t.Fatalf("first save: %v %v", got, err)
	}
	before := storeText(t, root)
	if got, err := Save(root, "q", Definition(query.Filters{Type: str("component")}), false); err != nil || got != Unchanged {
		t.Fatalf("same definition: %v %v", got, err)
	}
	if _, err := Save(root, "q", b, false); code(err) != "saved_query_exists" {
		t.Fatalf("different definition without replace: %v", err)
	}
	if storeText(t, root) != before {
		t.Fatal("a refused save wrote the store")
	}
	if got, err := Save(root, "q", b, true); err != nil || got != Replaced {
		t.Fatalf("replace: %v %v", got, err)
	}
	def, _ := Get(root, "q")
	if v, _ := def.Get("type"); v != "api" {
		t.Fatalf("replace kept %v", v)
	}
}

func TestSaveKeepsHandComments(t *testing.T) {
	root := workspace(t)
	hand := "# team queries\nmine:  # the one I use\n  type: component\n"
	if err := os.WriteFile(filepath.Join(root, ".khub", "queries.yaml"), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(root, "theirs", Definition(query.Filters{Tag: str("infra")}), false); err != nil {
		t.Fatal(err)
	}
	if got, want := storeText(t, root), hand+"theirs:\n  tag: infra\n"; got != want {
		t.Fatalf("store:\n%s\nwant:\n%s", got, want)
	}
}

func TestRefusals(t *testing.T) {
	root := workspace(t)
	for _, name := range []string{"", "Bad", "1st", "a b", "-x", "a/b", "null", "true", "no", "off", "y"} {
		if _, err := Save(root, name, omap.New(), false); code(err) != "invalid_query_name" {
			t.Errorf("name %q: %v", name, err)
		}
	}
	if _, err := Get(root, "none"); code(err) != "unknown_saved_query" ||
		!strings.Contains(err.Error(), "--save-as none") {
		t.Errorf("empty store: %v", err)
	}
	path := filepath.Join(root, ".khub", "queries.yaml")
	for _, text := range []string{"- a\n", "q: scalar\n", "q: [a]\n", "q: [\n"} {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Save(root, "x", omap.New(), false); code(err) != "invalid_saved_query" {
			t.Errorf("store %q: save %v", text, err)
		}
		if got, _ := os.ReadFile(path); string(got) != text {
			t.Errorf("store %q was rewritten", text)
		}
	}
	bad := map[string]string{
		"q:\n  limit: five\n":  "'limit' must be an integer",
		"q:\n  draft: 'yes'\n": "'draft' must be true or false",
		"q:\n  type: 3\n":      "'type' must be a string",
		"q:\n  kind: [a, b]\n": "'kind' must be a string",
		"q:\n  orphan: null\n": "'orphan' must be true or false",
	}
	for text, want := range bad {
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		def, err := Get(root, "q")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Filters("q", def); code(err) != "invalid_saved_query" || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v", text, err)
		}
	}
}

// Saves of distinct names from concurrent writers all land: the workspace
// lock covers the read-merge-publish window, so no writer drops another's.
func TestConcurrentSavesAllLand(t *testing.T) {
	root := workspace(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := Save(root, fmt.Sprintf("q%02d", i), Definition(query.Filters{Tag: str("t")}), false); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	store, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if store.Len() != 16 {
		t.Fatalf("%d of 16 saves landed: %v", store.Len(), store.Keys())
	}
}

// Replacing one definition whose keys changed re-emits that entry alone, in
// place: comments on the entries around it, their flow style, and the comment
// lines between entries all survive byte for byte.
func TestReplaceKeepsOtherEntries(t *testing.T) {
	root := workspace(t)
	hand := "# team queries\n" +
		"first:  # keep me\n  type: component\n  # inner note\n  kind: service\n" +
		"\n# about mine\n" +
		"mine:  # goes with the entry\n  type: component\n  # gone too\n  tag: infra\n" +
		"\n# about last\n" +
		"last: {type: api, limit: 2}  # flow kept\n"
	if err := os.WriteFile(filepath.Join(root, ".khub", "queries.yaml"), []byte(hand), 0o644); err != nil {
		t.Fatal(err)
	}
	limit := 4
	def := Definition(query.Filters{Type: str("api"), ActiveOnly: true, Limit: &limit})
	if got, err := Save(root, "mine", def, true); err != nil || got != Replaced {
		t.Fatalf("replace: %v %v", got, err)
	}
	want := "# team queries\n" +
		"first:  # keep me\n  type: component\n  # inner note\n  kind: service\n" +
		"\n# about mine\n" +
		"mine:\n  type: api\n  active: true\n  limit: 4\n" +
		"\n# about last\n" +
		"last: {type: api, limit: 2}  # flow kept\n"
	if got := storeText(t, root); got != want {
		t.Fatalf("store:\n%s\nwant:\n%s", got, want)
	}
	// The last entry, replaced, keeps the comment trailing it.
	if err := os.WriteFile(filepath.Join(root, ".khub", "queries.yaml"), []byte(want+"# end\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(root, "last", Definition(query.Filters{Tag: str("x")}), true); err != nil {
		t.Fatal(err)
	}
	if got, w := storeText(t, root), strings.Replace(want, "last: {type: api, limit: 2}  # flow kept\n", "last:\n  tag: x\n", 1)+"# end\n"; got != w {
		t.Fatalf("store:\n%s\nwant:\n%s", got, w)
	}
}

// A save that cannot leave the rest of the file as it was refuses and writes
// nothing, rather than re-emitting the whole file.
func TestUnspliceableRefuses(t *testing.T) {
	root := workspace(t)
	path := filepath.Join(root, ".khub", "queries.yaml")
	for _, tc := range []struct{ text, name string }{
		{"a: &x\n  type: component\nb: *x\n", "a"},     // b aliases the replaced entry
		{"{a: {type: component}}\n", "b"},              // flow root: nothing to append to
		{"{a: {type: component}, b: {tag: t}}\n", "a"}, // flow root: no entry lines
	} {
		if err := os.WriteFile(path, []byte(tc.text), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := Save(root, tc.name, Definition(query.Filters{Type: str("api"), Stale: true}), true)
		if code(err) != "saved_query_unspliceable" {
			t.Errorf("%q: %v", tc.text, err)
		}
		if got, _ := os.ReadFile(path); string(got) != tc.text {
			t.Errorf("%q was rewritten to %q", tc.text, got)
		}
	}
}

func TestUnreadableStoreNamesTheParseError(t *testing.T) {
	root := workspace(t)
	if err := os.WriteFile(filepath.Join(root, ".khub", "queries.yaml"), []byte("a: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(root)
	if code(err) != "invalid_saved_query" || !strings.Contains(err.Error(), "[1:4]") {
		t.Fatalf("%v", err)
	}
}

func TestOutdatedNamesTheQueryAndTheFix(t *testing.T) {
	err := Outdated("mine", errs.UnknownType("gone", "build-hub", []string{"api"}))
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "unknown_type" ||
		!strings.HasPrefix(located.Message, "Saved query 'mine' no longer fits the schema: No type 'gone'") ||
		!strings.Contains(located.Message, "--save-as mine --replace") {
		t.Fatalf("%v", err)
	}
	other := errs.New("malformed_entity", "x")
	if got := Outdated("mine", other); got.Error() != "x" {
		t.Fatal("an unrelated refusal was rewritten")
	}
}
