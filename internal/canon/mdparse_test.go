package canon

// The md SCAN altitude must match python-frontmatter exactly: a fenceless or
// unterminated-fence file yields empty metadata and the text as body, never an
// error. Getting this wrong flips stray/malformed across check, validate,
// status, and the index (found in review by the integrity port).

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func TestMDParseMatchesPythonFrontmatter(t *testing.T) {
	f, err := os.Open("../../parity/corpus/mdparse-cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n := 0
	for sc.Scan() {
		var r struct {
			Text  string         `json:"text"`
			Error *string        `json:"error"`
			Meta  map[string]any `json:"meta"`
			Body  string         `json:"body"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		meta, body, perr := Parse(r.Text, "md")
		if r.Error != nil {
			if perr == nil {
				t.Errorf("%q: want error %s, got none", r.Text, *r.Error)
			}
			n++
			continue
		}
		if perr != nil {
			t.Errorf("%q: unexpected error %v", r.Text, perr)
			n++
			continue
		}
		if body != r.Body {
			t.Errorf("%q: body = %q, want %q", r.Text, body, r.Body)
		}
		if meta.Len() != len(r.Meta) {
			t.Errorf("%q: meta has %d keys, want %d", r.Text, meta.Len(), len(r.Meta))
		}
		for k := range r.Meta {
			if _, ok := meta.Get(k); !ok {
				t.Errorf("%q: meta missing key %q", r.Text, k)
			}
		}
		n++
	}
	if n < 18 {
		t.Fatalf("small corpus: %d", n)
	}
}
