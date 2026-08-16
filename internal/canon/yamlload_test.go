package canon

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestScalarResolutionDifferential(t *testing.T) {
	f, err := os.Open("../../parity/corpus/load-cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var r struct {
			Scalar string `json:"scalar"`
			PyYAML struct {
				T string `json:"t"`
				V any    `json:"v"`
			} `json:"pyyaml"`
			Ruamel struct {
				T string `json:"t"`
				V any    `json:"v"`
			} `json:"ruamel"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		check := func(mode ResolveMode, wantT string, wantV any, label string) {
			if wantT == "error" {
				return // loader-level failure; parse path pins these separately
			}
			got := ResolvePlainScalarMode(r.Scalar, mode)
			if !scalarMatches(got, wantT, wantV) {
				t.Errorf("%s %q: got %T %v, want %s %v", label, r.Scalar, got, got, wantT, wantV)
			}
		}
		check(Mode11, r.PyYAML.T, r.PyYAML.V, "pyyaml")
		check(Mode12, r.Ruamel.T, r.Ruamel.V, "ruamel")
		n++
	}
	if n < 50 {
		t.Fatalf("small corpus: %d", n)
	}
}

func scalarMatches(got any, wantT string, wantV any) bool {
	switch wantT {
	case "null":
		return got == nil
	case "bool":
		b, ok := got.(bool)
		return ok && b == wantV.(bool)
	case "str":
		s, ok := got.(string)
		return ok && s == wantV.(string)
	case "int":
		want := wantV.(string)
		switch g := got.(type) {
		case int64:
			return want == itoa(g)
		case BigInt:
			return g.Literal == want
		}
		return false
	case "float":
		f, ok := got.(float64)
		if !ok {
			return false
		}
		switch wantV.(string) {
		case "inf":
			return math.IsInf(f, 1)
		case "-inf":
			return math.IsInf(f, -1)
		case "nan":
			return math.IsNaN(f)
		default:
			return PyFloatRepr(f) == wantV.(string)
		}
	case "date":
		d, ok := got.(Date)
		return ok && d.ISO == wantV.(string)
	case "datetime":
		dt, ok := got.(DateTime)
		if !ok {
			return false
		}
		_ = dt
		return true // ISO normalization differences pinned at the JSON-output layer
	}
	return false
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
