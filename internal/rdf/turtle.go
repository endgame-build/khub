// Package rdf is `khub schema rdf`: the RDFS + SHACL projection of the
// resolved schema (and, behind a flag, of the entities) into Turtle, written
// under .khub/generated/ like every other derived artifact. docs/rdf-mapping.md
// is the normative mapping; this package is its implementation, and the
// firm-ops render the parity runner records is its golden fixture.
//
// turtle.go is the emitter: a small term model and a pretty-printer with one
// pinned layout, so the bytes depend on the schema alone. No Turtle library:
// emission is a few escaping rules and a fixed layout, which is less code than
// a dependency plus the shim that would keep its output pinned.
package rdf

import (
	"fmt"
	"strconv"
	"strings"
)

// Term is one RDF term in object position: an IRI, a literal, a blank node
// with its own predicate-object pairs, or an RDF collection.
type Term interface {
	// triples counts the statements the term adds beyond the one that
	// references it: a blank node's pairs, a list's cells.
	triples() int
}

// IRI is an absolute IRI (Full) or a prefixed name (Prefixed). Exactly one is
// set; the writer never resolves or abbreviates.
type IRI struct {
	Full     string
	Prefixed string
}

// Literal is a Turtle literal: Lexical with an optional datatype IRI. Plain
// renders the lexical form bare (booleans, integers, decimals use Turtle's
// shorthand), which the writer sets only for the shorthand-capable types.
type Literal struct {
	Lexical  string
	Datatype IRI
	Plain    bool
}

// Pair is one predicate-object pair under a subject or a blank node.
type Pair struct {
	Pred IRI
	Obj  Term
}

// Blank is an anonymous node with its pairs, rendered inline as `[ … ]`.
type Blank struct{ Pairs []Pair }

// List is an RDF collection, rendered as `( … )`.
type List struct{ Items []Term }

func (IRI) triples() int     { return 0 }
func (Literal) triples() int { return 0 }
func (b Blank) triples() int { return countPairs(b.Pairs) }
func (l List) triples() int {
	// Each cell is rdf:first + rdf:rest; an empty list is the single rdf:nil
	// reference, which the referencing triple already counts.
	n := 2 * len(l.Items)
	for _, it := range l.Items {
		n += it.triples()
	}
	return n
}

func countPairs(pairs []Pair) int {
	n := len(pairs)
	for _, p := range pairs {
		n += p.Obj.triples()
	}
	return n
}

// Subject is one top-level block: a subject IRI and its pairs, in order.
type Subject struct {
	IRI   IRI
	Pairs []Pair
}

// Doc is a Turtle document: a comment header, the prefix block in declaration
// order, then the subject blocks in order. Everything renders exactly as
// appended — the document carries no map, so nothing can reorder it.
type Doc struct {
	Comment  []string
	Prefixes []Prefix
	Subjects []*Subject
}

// Prefix is one @prefix declaration.
type Prefix struct{ Name, IRI string }

// Add appends a pair to the subject.
func (s *Subject) Add(pred IRI, obj Term) { s.Pairs = append(s.Pairs, Pair{Pred: pred, Obj: obj}) }

// Triples counts every statement the document asserts.
func (d *Doc) Triples() int {
	n := 0
	for _, s := range d.Subjects {
		n += countPairs(s.Pairs)
	}
	return n
}

// String renders the document.
//
// Layout, pinned: each subject opens a block, every pair sits on its own
// four-space-indented line ending in `;` (the last in `.`), a blank node with
// one pair is inline and a larger one opens its own indented block, and a list
// is inline unless it holds a blank node, in which case each item takes a
// line. Objects never wrap: a long literal stays on one line, so a diff of the
// file is a diff of the schema.
func (d *Doc) String() string {
	var b strings.Builder
	for _, line := range d.Comment {
		b.WriteString("# ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, p := range d.Prefixes {
		fmt.Fprintf(&b, "@prefix %s: <%s> .\n", p.Name, escapeIRI(p.IRI))
	}
	for _, s := range d.Subjects {
		b.WriteByte('\n')
		b.WriteString(s.IRI.String())
		if len(s.Pairs) == 0 {
			b.WriteString(" .\n")
			continue
		}
		b.WriteString(" ")
		writePairs(&b, s.Pairs, 1)
		b.WriteString(" .\n")
	}
	return b.String()
}

// writePairs renders pairs at the given depth: the first pair continues the
// current line, each later pair starts a new indented line, and the caller
// closes the last one.
func writePairs(b *strings.Builder, pairs []Pair, depth int) {
	for i := 0; i < len(pairs); i++ {
		p := pairs[i]
		if i > 0 {
			b.WriteString(" ;\n")
			b.WriteString(indent(depth))
		}
		if p.Pred.Prefixed == "rdf:type" {
			b.WriteString("a") // the Turtle keyword for rdf:type
		} else {
			b.WriteString(p.Pred.String())
		}
		b.WriteByte(' ')
		writeTerm(b, p.Obj, depth)
		// A run of IRIs or literals under one predicate folds into an object
		// list; a blank node or a collection keeps its own predicate line.
		for i+1 < len(pairs) && pairs[i+1].Pred == p.Pred && atomic(p.Obj) && atomic(pairs[i+1].Obj) {
			i++
			b.WriteString(", ")
			writeTerm(b, pairs[i].Obj, depth)
		}
	}
}

func atomic(t Term) bool {
	switch t.(type) {
	case IRI, Literal:
		return true
	}
	return false
}

func writeTerm(b *strings.Builder, t Term, depth int) {
	switch x := t.(type) {
	case IRI:
		b.WriteString(x.String())
	case Literal:
		b.WriteString(x.String())
	case Blank:
		switch len(x.Pairs) {
		case 0:
			b.WriteString("[]")
		case 1:
			b.WriteString("[ ")
			writePairs(b, x.Pairs, depth+1)
			b.WriteString(" ]")
		default:
			b.WriteString("[\n")
			b.WriteString(indent(depth + 1))
			writePairs(b, x.Pairs, depth+1)
			b.WriteByte('\n')
			b.WriteString(indent(depth))
			b.WriteString("]")
		}
	case List:
		if len(x.Items) == 0 {
			b.WriteString("()")
			return
		}
		if !holdsBlank(x.Items) {
			b.WriteString("(")
			for _, it := range x.Items {
				b.WriteByte(' ')
				writeTerm(b, it, depth)
			}
			b.WriteString(" )")
			return
		}
		b.WriteString("(\n")
		for _, it := range x.Items {
			b.WriteString(indent(depth + 1))
			writeTerm(b, it, depth+1)
			b.WriteByte('\n')
		}
		b.WriteString(indent(depth))
		b.WriteString(")")
	default:
		panic(fmt.Sprintf("rdf: unknown term %T", t))
	}
}

func holdsBlank(items []Term) bool {
	for _, it := range items {
		if _, ok := it.(Blank); ok {
			return true
		}
	}
	return false
}

func indent(depth int) string { return strings.Repeat("    ", depth) }

// String renders the IRI as a prefixed name or an IRIREF.
func (i IRI) String() string {
	if i.Prefixed != "" {
		return i.Prefixed
	}
	return "<" + escapeIRI(i.Full) + ">"
}

// String renders the literal: the bare shorthand when Plain, else a quoted
// string with its datatype when one is set.
func (l Literal) String() string {
	if l.Plain {
		return l.Lexical
	}
	s := quote(l.Lexical)
	if l.Datatype.Full != "" || l.Datatype.Prefixed != "" {
		s += "^^" + l.Datatype.String()
	}
	return s
}

// quote renders a STRING_LITERAL_QUOTE: the five named escapes, \u for every
// other control character, and everything else — non-ASCII included — as the
// UTF-8 it is.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// escapeIRI percent-encodes the characters an IRIREF may not hold: controls,
// space, and <>"{}|^`\. A minted IRI never carries one (segment encodes
// stricter); this is the safety net for a --base someone typed.
func escapeIRI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c <= 0x20, c == 0x7f, strings.IndexByte("<>\"{}|^`\\", c) >= 0:
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// segment percent-encodes one path segment of a minted IRI: everything but
// RFC 3986 unreserved characters, so a workspace name or a slug with a space,
// a slash or a non-ASCII letter stays one segment.
func segment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// localOK reports whether name can be the local part of a prefixed name
// without escapes. Turtle allows more (colons, escaped punctuation); khub
// names are ASCII words with hyphens and underscores, and anything else goes
// out as a full IRI instead of an escape sequence.
func localOK(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		alpha := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
		digit := c >= '0' && c <= '9'
		if i == 0 && !alpha {
			return false
		}
		if !alpha && !digit && c != '-' {
			return false
		}
	}
	return true
}

// Str is a plain string literal (xsd:string in RDF 1.1).
func Str(s string) Literal { return Literal{Lexical: s} }

// Bool is the boolean shorthand.
func Bool(v bool) Literal { return Literal{Lexical: strconv.FormatBool(v), Plain: true} }

// Int is the integer shorthand.
func Int(n int) Literal { return Literal{Lexical: strconv.Itoa(n), Plain: true} }

// Typed is a literal with an explicit datatype.
func Typed(lexical string, datatype IRI) Literal {
	return Literal{Lexical: lexical, Datatype: datatype}
}
