package reindex

// Golden rows for the difflib port. The expected strings are the literal output
// of CPython difflib.unified_diff(a.splitlines(keepends=True),
// b.splitlines(keepends=True), fromfile="index.md", tofile="index.md") — the
// diff text `khub reindex --dry-run` prints verbatim, so it is byte-contract.

import (
	"reflect"
	"testing"
)

func TestUnifiedDiffGoldens(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want string
	}{
		{
			name: "insert into a short file",
			a:    "---\nokf_version: '0.1'\nentity_count: 0\n---\n# Index\n\n_No entities._\n",
			b:    "---\nokf_version: '0.1'\nentity_count: 1\n---\n# Index\n\n## client\n\n- [Acme](clients/acme.md)\n\n",
			want: "--- index.md\n+++ index.md\n@@ -1,7 +1,10 @@\n ---\n okf_version: '0.1'\n" +
				"-entity_count: 0\n+entity_count: 1\n ---\n # Index\n \n-_No entities._\n" +
				"+## client\n+\n+- [Acme](clients/acme.md)\n+\n",
		},
		{
			name: "identical inputs produce no diff at all",
			a:    "a\nb\nc\n",
			b:    "a\nb\nc\n",
			want: "",
		},
		{
			// The empty-range form: "@@ -0,0 +1,2 @@", not "@@ -1,0 ...".
			name: "from an empty file",
			a:    "",
			b:    "line1\nline2\n",
			want: "--- index.md\n+++ index.md\n@@ -0,0 +1,2 @@\n+line1\n+line2\n",
		},
		{
			// Two changes 18 lines apart split into two hunks with 3 lines of
			// context each — the get_grouped_opcodes(n=3) window.
			name: "two hunks with three lines of context",
			a: "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n" +
				"l11\nl12\nl13\nl14\nl15\nl16\nl17\nl18\nl19\nl20\n",
			b: "L1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n" +
				"l11\nl12\nl13\nl14\nl15\nl16\nl17\nl18\nl19\nL20\n",
			want: "--- index.md\n+++ index.md\n@@ -1,4 +1,4 @@\n-l1\n+L1\n l2\n l3\n l4\n" +
				"@@ -17,4 +17,4 @@\n l17\n l18\n l19\n-l20\n+L20\n",
		},
		{
			// keepends=True means the last line carries no "\n", so the two
			// bodies run together exactly as difflib emits them.
			name: "no trailing newline",
			a:    "a\nb",
			b:    "a\nc",
			want: "--- index.md\n+++ index.md\n@@ -1,2 +1,2 @@\n a\n-b+c",
		},
		{
			name: "a pure deletion",
			a:    "a\nb\nc\n",
			b:    "a\nc\n",
			want: "--- index.md\n+++ index.md\n@@ -1,3 +1,2 @@\n a\n-b\n c\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unifiedDiff(
				splitLinesKeepEnds(tc.a), splitLinesKeepEnds(tc.b), "index.md", "index.md")
			if got != tc.want {
				t.Fatalf("diff mismatch\n got: %q\nwant: %q", got, tc.want)
			}
		})
	}
}

// str.splitlines(keepends=True) splits on more than "\n" — the full Python
// line-boundary set, ends kept.
func TestSplitLinesKeepEnds(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"a\n", []string{"a\n"}},
		{"a\nb", []string{"a\n", "b"}},
		{"a\r\nb\n", []string{"a\r\n", "b\n"}},
		{"a\rb", []string{"a\r", "b"}},
		{"a b", []string{"a ", "b"}},
	}
	for _, tc := range cases {
		if got := splitLinesKeepEnds(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitLinesKeepEnds(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// _format_range_unified's three shapes: a single line, an empty range that
// begins one line early, and a normal span.
func TestFormatRangeUnified(t *testing.T) {
	cases := []struct {
		start, stop int
		want        string
	}{
		{0, 1, "1"},
		{0, 0, "0,0"},
		{0, 7, "1,7"},
		{16, 20, "17,4"},
		{5, 5, "5,0"},
	}
	for _, tc := range cases {
		if got := formatRangeUnified(tc.start, tc.stop); got != tc.want {
			t.Errorf("formatRangeUnified(%d, %d) = %q, want %q", tc.start, tc.stop, got, tc.want)
		}
	}
}
