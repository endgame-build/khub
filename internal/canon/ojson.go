package canon

// Ordered JSON decoding for the parity harness and (later) khub's JSON entity
// formats: key order is contract, so encoding/json's map is unusable. Values
// decode to *omap.Map / []any / string / bool / nil / int64 / BigInt /
// float64, with {"__date__": "…"} unwrapping to Date (the tagged form the
// corpus generator writes).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/omap"
)

// DecodeOrderedJSON decodes the parity corpora, where dates travel as
// {"__date__": "…"} / {"__datetime__": "…"} tags because JSON has no date
// type. Never use it on entity documents — a real entity may legitimately
// carry a field of that name.
func DecodeOrderedJSON(data []byte) (any, error) {
	v, err := decode(data, false)
	if err != nil {
		return nil, err
	}
	return untagDates(v), nil
}

// untagDates rewrites the corpus date tags into canon scalars.
func untagDates(v any) any {
	switch x := v.(type) {
	case *omap.Map:
		if x.Len() == 1 {
			if d, ok := x.Get("__date__"); ok {
				if iso, isStr := d.(string); isStr {
					return Date{ISO: iso}
				}
			}
			if d, ok := x.Get("__datetime__"); ok {
				if iso, isStr := d.(string); isStr {
					return DateTime{ISO: iso}
				}
			}
		}
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			x.Set(k, untagDates(val))
		}
		return x
	case []any:
		for i, it := range x {
			x[i] = untagDates(it)
		}
		return x
	}
	return v
}

func decode(data []byte, strict bool) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeValue(dec, strict)
	if err != nil {
		return nil, err
	}
	// Python's json.loads rejects trailing content ("Extra data"); Go's stream
	// decoder would silently stop at the first value.
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("Extra data")
	}
	return v, nil
}

// DecodeOrderedJSONStrict rejects duplicate mapping keys ("duplicate key 'k'"),
// the formats.py object_pairs_hook contract.
func DecodeOrderedJSONStrict(data []byte) (any, error) { return decode(data, true) }

func decodeValue(dec *json.Decoder, strict bool) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeFrom(dec, tok, strict)
}

func decodeFrom(dec *json.Decoder, tok json.Token, strict bool) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			m := omap.New()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("non-string key %v", keyTok)
				}
				if strict {
					if _, dup := m.Get(key); dup {
						return nil, fmt.Errorf("duplicate key '%s'", key)
					}
				}
				val, err := decodeValue(dec, strict)
				if err != nil {
					return nil, err
				}
				m.Set(key, val)
			}
			if _, err := dec.Token(); err != nil { // consume '}'
				return nil, err
			}
			return m, nil
		case '[':
			items := []any{}
			for dec.More() {
				v, err := decodeValue(dec, strict)
				if err != nil {
					return nil, err
				}
				items = append(items, v)
			}
			if _, err := dec.Token(); err != nil { // consume ']'
				return nil, err
			}
			return items, nil
		}
		return nil, fmt.Errorf("unexpected delim %v", t)
	case json.Number:
		lit := t.String()
		if !strings.ContainsAny(lit, ".eE") {
			if n, err := strconv.ParseInt(lit, 10, 64); err == nil {
				return n, nil
			}
			return BigInt{Literal: lit}, nil
		}
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		return tok, nil
	}
}
