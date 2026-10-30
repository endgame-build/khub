package extract

import (
	"errors"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

// Every shape fault is one invalid_envelope refusal naming the entity.
func TestParseEnvelopeRefusesShapeFaults(t *testing.T) {
	cases := []struct{ name, in, wants string }{
		{"empty", "   \n", "Envelope is empty"},
		{"not json", "nope", "Envelope is not valid JSON at byte 2"},
		{"ends early", `{"entities": [`, "Envelope is not valid JSON at byte 14"},
		{"trailing content", `{"entities": []} x`, "Envelope is not valid JSON: Extra data"},
		{"top not object", `[]`, "must be a JSON object with an 'entities' list"},
		{"entities missing", `{"records": []}`, "must be a JSON object with an 'entities' list"},
		{"entities not list", `{"entities": {}}`, "must be a JSON object with an 'entities' list"},
		{"entity not object", `{"entities": [1]}`, "Entity #0 is not a JSON object"},
		{"unknown key", `{"entities": [{"type": "client"}, {"type": "client", "fieldz": {}}]}`,
			"Entity #1 has an unknown key 'fieldz'; an entity takes type, fields, relations, body, source, confidence"},
		{"type missing", `{"entities": [{"fields": {}}]}`, "Entity #0 has no 'type'"},
		{"type blank", `{"entities": [{"type": " "}]}`, "Entity #0 has no 'type'"},
		{"type not string", `{"entities": [{"type": 3}]}`, "Entity #0 has no 'type'"},
		{"fields not object", `{"entities": [{"type": "client", "fields": []}]}`, "Entity #0 'fields' must be a JSON object"},
		{"relations not object", `{"entities": [{"type": "client", "relations": ["x"]}]}`,
			"Entity #0 'relations' must be a JSON object of predicate to targets"},
		{"relation value number", `{"entities": [{"type": "client", "relations": {"partner": 1}}]}`,
			"Entity #0 relation 'partner' must be a string or a list of strings"},
		{"relation list mixed", `{"entities": [{"type": "client", "relations": {"partner": ["a", 1]}}]}`,
			"Entity #0 relation 'partner' must be a string or a list of strings"},
		{"body not string", `{"entities": [{"type": "client", "body": 1}]}`, "Entity #0 'body' must be a string"},
		{"source not string", `{"entities": [{"type": "client", "source": {}}]}`, "Entity #0 'source' must be a string"},
		{"confidence string", `{"entities": [{"type": "client", "confidence": "high"}]}`, "Entity #0 'confidence' must be a number from 0 to 1"},
		{"confidence numeric string", `{"entities": [{"type": "client", "confidence": "0.5"}]}`, "Entity #0 'confidence' must be a number from 0 to 1"},
		{"confidence bool", `{"entities": [{"type": "client", "confidence": true}]}`, "Entity #0 'confidence' must be a number from 0 to 1"},
		{"confidence above one", `{"entities": [{"type": "client", "confidence": 7}]}`, "Entity #0 'confidence' must be a number from 0 to 1"},
		{"confidence negative", `{"entities": [{"type": "client", "confidence": -0.1}]}`, "Entity #0 'confidence' must be a number from 0 to 1"},
		{"duplicate key", `{"entities": [{"type": "client", "type": "person"}]}`, "duplicate key 'type'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseEnvelope([]byte(tc.in))
			var located *errs.Located
			if !errors.As(err, &located) {
				t.Fatalf("want a located error, got %v", err)
			}
			if located.Code != "invalid_envelope" {
				t.Errorf("code = %s, want invalid_envelope", located.Code)
			}
			if !strings.Contains(located.Message, tc.wants) {
				t.Errorf("message %q does not contain %q", located.Message, tc.wants)
			}
		})
	}
}

// The lenient readings the contract pins: a leading BOM is not content, a
// bare string is a one-target list, blank targets are dropped, null optional
// keys read as absent, a blank body is no body, the body's line endings are
// normalized, confidence keeps its number (an integer 1 included).
func TestParseEnvelopeReadsTheContract(t *testing.T) {
	env, err := ParseEnvelope([]byte("\xEF\xBB\xBF" + `{"entities": [
	  {"type": "project", "fields": {"title": "T", "active": true, "n": 2},
	   "relations": {"client": "initech", "team": ["noor", " ", "sam"], "partner": null},
	   "body": "a\r\nb\n", "source": "p. 3", "confidence": 0.75},
	  {"type": "client", "fields": null, "relations": null, "body": null, "source": null, "confidence": null},
	  {"type": "client", "body": "  \n\n", "confidence": 1}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(env.Entities) != 3 {
		t.Fatalf("entities = %d", len(env.Entities))
	}
	if third := env.Entities[2]; third.Body != nil || third.Confidence != int64(1) {
		t.Errorf("blank body should be absent and an integer confidence kept: %+v", third)
	}
	first := env.Entities[0]
	if first.Type != "project" || first.Fields.Len() != 3 || first.Source != "p. 3" {
		t.Errorf("first entity read wrong: %+v", first)
	}
	if got := listOf(first.Relations, "client"); len(got) != 1 || got[0] != "initech" {
		t.Errorf("bare string relation = %v, want [initech]", got)
	}
	if got := listOf(first.Relations, "team"); len(got) != 2 || got[1] != "sam" {
		t.Errorf("list relation = %v, want [noor sam]", got)
	}
	if got := listOf(first.Relations, "partner"); len(got) != 0 {
		t.Errorf("null relation = %v, want empty", got)
	}
	if first.Body == nil || *first.Body != "a\nb\n" {
		t.Errorf("body = %v, want CRLF normalized", first.Body)
	}
	if f, ok := first.Confidence.(float64); !ok || f != 0.75 {
		t.Errorf("confidence = %v", first.Confidence)
	}
	second := env.Entities[1]
	if second.Fields.Len() != 0 || second.Relations.Len() != 0 || second.Body != nil ||
		second.Source != "" || second.Confidence != nil {
		t.Errorf("null optional keys should read as absent: %+v", second)
	}
}
