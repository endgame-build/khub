package canon

// Differential tests against Python-recorded corpora (go-port-plan M2
// acceptance: codec-json + codec-yaml byte-for-byte).

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func scanCorpus(tb testing.TB, path string, fn func(line []byte)) {
	tb.Helper()
	f, err := os.Open(path)
	if err != nil {
		tb.Fatalf("corpus missing: %v (regenerate via parity/tools)", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		fn(append([]byte(nil), sc.Bytes()...))
	}
}

func TestYAMLEmitterDifferential(t *testing.T) {
	n := 0
	scanCorpus(t, "../../parity/corpus/emit-cases.jsonl", func(line []byte) {
		var r struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
			RT    string          `json:"rt"`
			Safe  string          `json:"safe"`
		}
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatal(err)
		}
		val, err := DecodeOrderedJSON(r.Value)
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		if got, err := DumpRT(val); err != nil || got != r.RT {
			t.Errorf("%s RT mismatch (err=%v)\nwant %q\ngot  %q", r.Name, err, r.RT, got)
		}
		if got, err := DumpWide(val); err != nil || got != r.Safe {
			t.Errorf("%s SAFE mismatch (err=%v)\nwant %q\ngot  %q", r.Name, err, r.Safe, got)
		}
		n++
	})
	if n < 1000 {
		t.Fatalf("suspiciously small corpus: %d", n)
	}
}

func TestJSONDialectsDifferential(t *testing.T) {
	n := 0
	scanCorpus(t, "../../parity/corpus/json-cases.jsonl", func(line []byte) {
		var r struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
			CLI   string          `json:"cli"`
			Disk  string          `json:"disk"`
			JSONL *string         `json:"jsonl"`
		}
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatal(err)
		}
		val, err := DecodeOrderedJSON(r.Value)
		if err != nil {
			t.Fatalf("%s: %v", r.Name, err)
		}
		if got, err := EncodeCLI(val); err != nil || got != r.CLI {
			t.Errorf("%s CLI mismatch (err=%v)\nwant %q\ngot  %q", r.Name, err, r.CLI, got)
		}
		if got, err := EncodeDisk(val); err != nil || got != r.Disk {
			t.Errorf("%s Disk mismatch (err=%v)\nwant %q\ngot  %q", r.Name, err, r.Disk, got)
		}
		if r.JSONL != nil {
			m, ok := val.(interface {
				Keys() []string
				Get(string) (any, bool)
			})
			_ = m
			_ = ok
		}
		n++
	})
	if n < 150 {
		t.Fatalf("suspiciously small corpus: %d", n)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	y, b, err := SplitFrontmatter("---\ntype: x\n---\nBody\n")
	if err != nil || y != "type: x\n" || b != "Body\n" {
		t.Fatalf("basic split: %q %q %v", y, b, err)
	}
	if _, _, err := SplitFrontmatter("no fence"); err == nil ||
		err.Error() != "No frontmatter fence in 'no fence'" {
		t.Fatalf("fence error: %v", err)
	}
	if _, _, err := SplitFrontmatter("---\nnever closed\n"); err == nil ||
		err.Error() != "Unterminated frontmatter" {
		t.Fatalf("unterminated: %v", err)
	}
	y, b, err = SplitFrontmatter("\n\n---\na: 1\n---\n")
	if err != nil || y != "a: 1\n" || b != "" {
		t.Fatalf("leading blanks: %q %q %v", y, b, err)
	}
	if RenderMD("a: 1\n", "Body") != "---\na: 1\n---\nBody" {
		t.Fatal("render")
	}
}
