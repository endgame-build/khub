package values

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func TestPredicatesDifferential(t *testing.T) {
	f, err := os.Open("../../parity/corpus/values-cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		var r struct {
			Kind     string          `json:"kind"`
			V        json.RawMessage `json:"v"`
			Present  bool            `json:"present"`
			IsBool   bool            `json:"is_bool"`
			AsBool   bool            `json:"as_bool"`
			IsNumber bool            `json:"is_number"`
			IsDate   bool            `json:"is_dateish"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		var v any
		dec := json.NewDecoder(bytesReader(r.V))
		dec.UseNumber()
		if err := dec.Decode(&v); err != nil {
			t.Fatal(err)
		}
		v = normalize(v)
		if got := Present(v); got != r.Present {
			t.Errorf("present(%v %s) = %v want %v", v, r.V, got, r.Present)
		}
		if got := IsBool(v); got != r.IsBool {
			t.Errorf("is_bool(%v %s) = %v want %v", v, r.V, got, r.IsBool)
		}
		if got := AsBool(v); got != r.AsBool {
			t.Errorf("as_bool(%v %s) = %v want %v", v, r.V, got, r.AsBool)
		}
		if got := IsNumber(v); got != r.IsNumber {
			t.Errorf("is_number(%v %s) = %v want %v", v, r.V, got, r.IsNumber)
		}
		if got := IsDateish(v); got != r.IsDate {
			t.Errorf("is_dateish(%v %s) = %v want %v", v, r.V, got, r.IsDate)
		}
		n++
	}
	if n < 60 {
		t.Fatalf("small corpus: %d", n)
	}
}

func normalize(v any) any {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		f, _ := x.Float64()
		return f
	case []any:
		for i, item := range x {
			x[i] = normalize(item)
		}
		return x
	case map[string]any:
		return x // only reached for {} / {"k":1} python cases; predicates treat as "other"
	}
	return v
}

func bytesReader(b []byte) *os.File { // tiny shim: use a pipe-free reader
	r, w, _ := os.Pipe()
	go func() { w.Write(b); w.Close() }()
	return r
}
