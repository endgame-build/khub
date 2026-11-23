package values

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/omap"
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
	go func() { _, _ = w.Write(b); _ = w.Close() }()
	return r
}

func TestLoadedTimestampMustBeOnTheCalendar(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{canon.Date{ISO: "2026-01-15"}, true},
		{canon.Date{ISO: "2024-02-29"}, true},
		{canon.Date{ISO: "2026-13-45"}, false},
		{canon.Date{ISO: "2026-02-29"}, false},
		{canon.DateTime{ISO: "2026-1-5T10:00:00"}, true},
		{canon.DateTime{ISO: "2026-01-15 23:59:59.5 +05:30"}, true},
		{canon.DateTime{ISO: "2026-13-45T00:00:00Z"}, false},
		{canon.DateTime{ISO: "2026-02-30T10:00:00+05:30"}, false},
		{canon.DateTime{ISO: "2026-01-15T24:00:00Z"}, false},
	}
	for _, c := range cases {
		if got := IsDateish(c.v); got != c.want {
			t.Errorf("IsDateish(%v) = %v, want %v", c.v, got, c.want)
		}
	}
}

// `status: draft` decides; a published lifecycle value outranks a legacy
// `draft` flag; any other status (a retired domain value) leaves the flag to
// decide.
func TestIsDraftReadsStatusThenTheLegacyFlag(t *testing.T) {
	meta := func(kv ...any) *omap.Map {
		m := omap.New()
		for i := 0; i+1 < len(kv); i += 2 {
			m.Set(kv[i].(string), kv[i+1])
		}
		return m
	}
	for _, c := range []struct {
		meta *omap.Map
		want bool
	}{
		{meta("status", "draft"), true},
		{meta("status", "stable"), false},
		{meta("status", "Draft"), false},
		{meta("draft", true), true},
		{meta("draft", "yes"), true},
		{meta("draft", false), false},
		{meta("draft", "no"), false},
		// A truthy legacy draft wins until upgrade migrates it.
		{meta("status", "stable", "draft", true), true},
		{meta("status", "deprecated", "draft", true), true},
		{meta("status", "rejected", "draft", true), true},
		{meta("status", "rejected", "draft", false), false},
		{meta("status", "draft", "draft", false), true},
		{omap.New(), false},
	} {
		if got := IsDraftOf(c.meta, false); got != c.want {
			t.Errorf("IsDraftOf(%v) = %v, want %v", c.meta.Keys(), got, c.want)
		}
	}
}

// OKFInstant reads only OKF datetimes (a T and an explicit offset, either
// letter in either case); IsDateish accepts what the OKF pattern accepts.
func TestOKFInstant(t *testing.T) {
	for s, want := range map[string]bool{
		"2026-12-31T00:00:00Z":        true,
		"2026-12-31t00:00:00z":        true,
		"2026-12-31T00:00:00.5+02:00": true,
		"2026-12-31T00:00:00":         false,
		"2026-12-31 00:00:00Z":        false,
		"2026-12-31":                  false,
		"2026-02-30T00:00:00Z":        false,
	} {
		if _, ok := OKFInstant(s); ok != want {
			t.Errorf("OKFInstant(%q) = %v, want %v", s, ok, want)
		}
		if want && !IsDateish(s) {
			t.Errorf("IsDateish(%q) = false for an OKF datetime", s)
		}
	}
}
