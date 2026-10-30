// Package extract is the ingestion half of schema-constrained extraction: the
// contract an extractor fills in (Scaffold) and the check its output passes
// before anything is written (Conform). The extractor itself — an LLM, an
// agent, a script — stays outside khub, which sends nothing outward; khub
// ships the contract and the checker. Alignment of what the extractor named
// against what the workspace already holds (the ladder) and the conflict
// policy are design notes, not shipped here: conform resolves a relation
// target the way every write does and reports what it cannot, never guessing.
//
// Everything is walked from the resolved schema. No type, field or predicate
// is named in this package; the one list of field names it carries is
// entity.WriteSupplied, the base plumbing create writes itself.
package extract

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// Envelope is the document an extractor returns: a list of extracted records.
type Envelope struct {
	Entities []Extracted
}

// Extracted is one record of an envelope. Fields holds the attribute values
// as JSON gave them (never nil); Relations maps predicate to targets as the
// extractor spelled them (never nil); Body is nil when the record carries
// none or only whitespace (create seeds the template for both); Confidence
// is nil when absent, else a JSON number from 0 to 1 as decoded.
type Extracted struct {
	Type       string
	Fields     *omap.Map
	Relations  *omap.Map // predicate -> []string
	Body       *string
	Source     string
	Confidence any
}

// The keys an entity object may carry, in the order the contract lists them.
var entityKeys = []string{"type", "fields", "relations", "body", "source", "confidence"}

// ParseEnvelope decodes an envelope and checks its shape. Every shape fault
// is an `invalid_envelope` refusal that names the entity by its index: the
// extractor's output is the call, and a call that cannot be read is corrected,
// not scored. Duplicate keys are refused as every khub JSON read refuses them.
func ParseEnvelope(data []byte) (*Envelope, error) {
	// A document copied through Windows tooling, or written by a model, may
	// open with a byte-order mark; it is not content.
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	if strings.TrimSpace(string(data)) == "" {
		return nil, invalid("Envelope is empty; expected a JSON object with an 'entities' list")
	}
	doc, err := canon.DecodeOrderedJSONStrict(data)
	if err != nil {
		return nil, invalid(notJSON(err))
	}
	top, ok := doc.(*omap.Map)
	if !ok {
		return nil, invalid("Envelope must be a JSON object with an 'entities' list")
	}
	raw, has := top.Get("entities")
	list, isList := raw.([]any)
	if !has || !isList {
		return nil, invalid("Envelope must be a JSON object with an 'entities' list")
	}
	env := &Envelope{Entities: make([]Extracted, 0, len(list))}
	for i, item := range list {
		ent, err := parseEntity(i, item)
		if err != nil {
			return nil, err
		}
		env.Entities = append(env.Entities, ent)
	}
	return env, nil
}

// notJSON words a decode failure in khub's own text: the byte a syntax error
// stopped at (a document that ends early is one, at its last byte), else
// canon's own message (a duplicate key, trailing content, nesting too deep).
func notJSON(err error) string {
	if offset, ok := canon.JSONSyntaxOffset(err); ok {
		return fmt.Sprintf("Envelope is not valid JSON at byte %d", offset)
	}
	return "Envelope is not valid JSON: " + err.Error()
}

func parseEntity(i int, item any) (Extracted, error) {
	obj, ok := item.(*omap.Map)
	if !ok {
		return Extracted{}, invalid(fmt.Sprintf("Entity #%d is not a JSON object", i))
	}
	for _, key := range obj.Keys() {
		if !contains(entityKeys, key) {
			return Extracted{}, invalid(fmt.Sprintf(
				"Entity #%d has an unknown key '%s'; an entity takes %s",
				i, key, strings.Join(entityKeys, ", ")))
		}
	}
	ent := Extracted{Fields: omap.New(), Relations: omap.New()}
	typeName, _ := obj.Get("type")
	if s, isStr := typeName.(string); isStr && strings.TrimSpace(s) != "" {
		ent.Type = s
	} else {
		return Extracted{}, invalid(fmt.Sprintf("Entity #%d has no 'type'", i))
	}
	if v, has := obj.Get("fields"); has && v != nil {
		m, isMap := v.(*omap.Map)
		if !isMap {
			return Extracted{}, invalid(fmt.Sprintf("Entity #%d 'fields' must be a JSON object", i))
		}
		ent.Fields = m
	}
	if v, has := obj.Get("relations"); has && v != nil {
		m, isMap := v.(*omap.Map)
		if !isMap {
			return Extracted{}, invalid(fmt.Sprintf(
				"Entity #%d 'relations' must be a JSON object of predicate to targets", i))
		}
		for _, predicate := range m.Keys() {
			raw, _ := m.Get(predicate)
			targets, ok := targetList(raw)
			if !ok {
				return Extracted{}, invalid(fmt.Sprintf(
					"Entity #%d relation '%s' must be a string or a list of strings", i, predicate))
			}
			ent.Relations.Set(predicate, targets)
		}
	}
	if v, has := obj.Get("body"); has && v != nil {
		s, isStr := v.(string)
		if !isStr {
			return Extracted{}, invalid(fmt.Sprintf("Entity #%d 'body' must be a string", i))
		}
		// A blank body is no body: create seeds the template for either.
		if text := canon.NormalizeNewlines(s); strings.TrimSpace(text) != "" {
			ent.Body = &text
		}
	}
	if v, has := obj.Get("source"); has && v != nil {
		s, isStr := v.(string)
		if !isStr {
			return Extracted{}, invalid(fmt.Sprintf("Entity #%d 'source' must be a string", i))
		}
		ent.Source = s
	}
	if v, has := obj.Get("confidence"); has && v != nil {
		// A JSON number, not a numeric string, and inside the range the
		// contract states.
		f, isNumber := jsonNumber(v)
		if !isNumber || f < 0 || f > 1 {
			return Extracted{}, invalid(fmt.Sprintf("Entity #%d 'confidence' must be a number from 0 to 1", i))
		}
		ent.Confidence = v
	}
	return ent, nil
}

// targetList reads a relation value: a string is one target, a list holds
// strings. Blank strings are dropped — a relation with none is absent.
func targetList(raw any) ([]string, bool) {
	switch x := raw.(type) {
	case nil:
		return []string{}, true
	case string:
		return nonBlank([]any{x}), true
	case []any:
		for _, item := range x {
			if _, isStr := item.(string); !isStr {
				return nil, false
			}
		}
		return nonBlank(x), true
	}
	return nil, false
}

func nonBlank(items []any) []string {
	out := []string{}
	for _, item := range items {
		if s := strings.TrimSpace(item.(string)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// jsonNumber is the value of a decoded JSON number; a string, a bool or an
// integer past int64 is not one.
func jsonNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

func invalid(message string) *errs.Located {
	return errs.New("invalid_envelope", message)
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
