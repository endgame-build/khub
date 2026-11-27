package rdf

import (
	"strings"
	"testing"
)

// The emitter's escaping is the one place a hostile title could break the
// document: a quote closes the literal, a newline starts a new statement.
func TestQuoteEscapesEveryStructuralCharacter(t *testing.T) {
	cases := map[string]string{
		`plain`:            `"plain"`,
		`say "hi"`:         `"say \"hi\""`,
		`back\slash`:       `"back\\slash"`,
		"line\nbreak":      `"line\nbreak"`,
		"tab\tcr\r":        `"tab\tcr\r"`,
		"bell\x07 del\x7f": `"bell\u0007 del\u007F"`,
		"bs\bff\f":         `"bs\bff\f"`,
		"ünïcödé — ok":     `"ünïcödé — ok"`,
	}
	for in, want := range cases {
		if got := quote(in); got != want {
			t.Errorf("quote(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestIRIEscapingAndSegments(t *testing.T) {
	if got := escapeIRI("urn:x:a b<c>{d}|e^f`g\\h"); got != "urn:x:a%20b%3Cc%3E%7Bd%7D%7Ce%5Ef%60g%5Ch" {
		t.Errorf("escapeIRI = %s", got)
	}
	if got := segment("my hub/é.v1~_"); got != "my%20hub%2F%C3%A9.v1~_" {
		t.Errorf("segment = %s", got)
	}
	for name, ok := range map[string]bool{
		"client": true, "case-study": true, "depends_on": true, "_x": true, "a1": true,
		"": false, "1a": false, "-a": false, "a.b": false, "a/b": false, "a:b": false, "é": false,
	} {
		if localOK(name) != ok {
			t.Errorf("localOK(%q) = %v, want %v", name, !ok, ok)
		}
	}
	if (IRI{Full: "urn:x:a b"}).String() != "<urn:x:a%20b>" {
		t.Error("a full IRI renders escaped inside angle brackets")
	}
}

// One pinned layout: object lists for runs of atoms, inline one-pair blank
// nodes, block blank nodes, inline lists, block lists when a list holds a
// blank node, and `a` for rdf:type.
func TestLayoutIsPinned(t *testing.T) {
	doc := &Doc{
		Comment:  []string{"header"},
		Prefixes: []Prefix{{"ex", "urn:ex/"}, {"sh", "http://www.w3.org/ns/shacl#"}},
	}
	s := &Subject{IRI: pn("ex", "s")}
	s.Add(rdfType, pn("ex", "A"))
	s.Add(rdfType, pn("ex", "B"))
	s.Add(pn("ex", "tag"), Str("x"))
	s.Add(pn("ex", "tag"), Str("y"))
	s.Add(pn("ex", "one"), Blank{Pairs: []Pair{{pn("ex", "k"), Int(1)}}})
	s.Add(pn("ex", "two"), Blank{Pairs: []Pair{{pn("ex", "k"), Int(1)}, {pn("ex", "l"), Bool(true)}}})
	s.Add(pn("ex", "in"), List{Items: []Term{Str("a"), Str("b")}})
	s.Add(pn("ex", "or"), List{Items: []Term{
		Blank{Pairs: []Pair{{shClass, pn("ex", "A")}}},
		Blank{Pairs: []Pair{{shClass, pn("ex", "B")}}},
	}})
	s.Add(pn("ex", "empty"), List{})
	s.Add(pn("ex", "full"), IRI{Full: "urn:ex/full"})
	doc.Subjects = append(doc.Subjects, s, &Subject{IRI: IRI{Full: "urn:ex/bare"}})
	want := `# header
@prefix ex: <urn:ex/> .
@prefix sh: <http://www.w3.org/ns/shacl#> .

ex:s a ex:A, ex:B ;
    ex:tag "x", "y" ;
    ex:one [ ex:k 1 ] ;
    ex:two [
        ex:k 1 ;
        ex:l true
    ] ;
    ex:in ( "a" "b" ) ;
    ex:or (
        [ sh:class ex:A ]
        [ sh:class ex:B ]
    ) ;
    ex:empty () ;
    ex:full <urn:ex/full> .

<urn:ex/bare> .
`
	if got := doc.String(); got != want {
		t.Errorf("layout drifted:\n--- got\n%s--- want\n%s", got, want)
	}
	// 2 types + 2 tags + one(1+1) + two(1+2) + in(1+4) + or(1+4+2) + empty + full = 23.
	if n := doc.Triples(); n != 23 {
		t.Errorf("Triples() = %d, want 23", n)
	}
	if strings.Count(doc.String(), "rdf:type") != 0 {
		t.Error("rdf:type must render as the `a` keyword")
	}
}

func TestLiteralForms(t *testing.T) {
	if got := Typed("3", xsdDecimal).String(); got != `"3"^^xsd:decimal` {
		t.Errorf("typed literal = %s", got)
	}
	if got := Bool(false).String(); got != "false" {
		t.Errorf("bool = %s", got)
	}
	if got := Str(`"`).String(); got != `"\""` {
		t.Errorf("string = %s", got)
	}
}
