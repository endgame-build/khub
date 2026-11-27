package schema

// jsonschema.go emits a JSON Schema (draft-07) for the authored layer files —
// .khub/ontology.yaml, policy.yaml and storage.yaml — so an editor can
// complete and check them as they are typed. It describes the vocabulary walk
// in vocab.go: every mapping's keys come from the same lists the walk passes
// to addExtras, so the two cannot list different keys.
//
// One schema covers all three files, as the layers merge on top-level key and
// any file may carry any block. The schema is never stricter than the walk: a
// value the walk accepts (a lax boolean such as `yes`, a null where a field is
// optional) passes here too. It is looser where a rule spans fields or files
// and where JSON Schema cannot say what the loader checks; docs/schema.md
// ("Editor validation") lists both, and TestJSONSchemaKnownGaps pins the
// second. khub reports those when it loads the schema. One
// deliberate exception: a document, a layer block or `entities` that is falsy
// but neither a mapping nor null (`policy: false`, `entities: []`, a document
// that is just `0`) loads as empty, and the schema still asks for a mapping
// or null there.

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/endgame-build/khub/internal/omap"
)

// JSONSchemaDraft is the dialect the emitted schema declares. Draft-07 is the
// newest draft every common YAML editor integration validates.
const JSONSchemaDraft = "http://json-schema.org/draft-07/schema#"

// LayerJSONSchema builds the JSON Schema of an authored layer file. It panics
// when a key list in vocab.go and the property table here disagree: that is a
// build defect the tests reach on every run, never an input condition.
func LayerJSONSchema() *omap.Map {
	defs := omap.New()
	defs.Set("entity", object(
		"One entity type's ontology entry: the domain only, no storage or gates.",
		ontologyTypeKeys, props{
			"when": nullable(str(
				"The moment to capture this type, in one line of domain language. Never a file path.")),
			"attributes": mapOf(ref("attribute"),
				"Attributes keyed by name. Redeclaring a base attribute overrides it facet by facet."),
			"relations": mapOf(ref("relation"),
				"Relations keyed by predicate: the key is the edge's name."),
		}))
	defs.Set("attribute", object(
		"An attribute declaration. Every facet is optional; an override inherits the ones it leaves out.",
		attributeKeys, props{
			"type": nullable(enumOf(scalarTypes,
				"The value type. Default text; may be left out when enum is given or on an override.")),
			"required": nullable(ref("bool", "Whether check demands a value on an active entity.")),
			"default":  desc("The field's default value."),
			"enum":     nullable(listOf(str(""), "The allowed values.")),
			"pattern": nullable(str(
				"A Go RE2 regex the whole value must match, anchored at both ends and case-sensitive.")),
			"fields": mapOf(ref("attribute"),
				"An object's keys, each declared like an attribute. Type object only."),
			"items": ref("attribute", "The element of a list, declared like an attribute. Type list only."),
		}))
	defs.Set("relation", object(
		"A relation declaration. The key it sits under is the predicate.",
		relationKeys, props{
			"to": anyOf("A type name, a list of type names (a union edge), or `any`.",
				str(""), listOf(str(""), "")),
			"many":     ref("bool", "Multi-valued when true. Default false: single-valued."),
			"required": ref("bool", "Whether check demands the edge on an active entity."),
			"inverse": nullable(str(
				"The name of the derived edge on the target, computed at read time and never stored.")),
			"acyclic": ref("bool", "Whether check reports cycles over this predicate."),
		}, "to"))
	defs.Set("policy", object(
		"One type's workspace gates.",
		policyTypeKeys, props{
			"required": ref("bool", "Singleton only: check reports the type's file when it is missing."),
			"orphan": ref("bool",
				"When true, an entity of this type with no edges is never reported as an orphan."),
			"path_refs": ref("bool",
				"When true, this type's relation values may name a workspace file by its path."),
			"retention": ref("retention",
				"The end-of-life rule check reports against. khub never deletes an entity."),
			"waive": nullable(propertyNames(mapOf(ref("waiver"),
				"Accepted check findings, keyed by the slug of an entity of this type."),
				pattern(desc(""), nonBlank))),
			"locked": ref("bool",
				"When true, khub upgrade refuses a schema that drops this type unless --unlock names it."),
			"locked_relations": nullable(listOf(str(""),
				"This type's own predicates that khub upgrade holds the same way.")),
		}))
	defs.Set("retention", object(
		"Report an entity whose updated date, else created, is more than days old.",
		retentionKeys, props{
			"days": minimum(typed("integer", "Age in days past which check reports the entity."), 1),
			"no_outbound": ref("bool",
				"When true, report only an entity that also has no outbound edge."),
		}, "days"))
	defs.Set("waiver", waiver())
	defs.Set("storage", object(
		"Where and how one type's bytes land. An entry must state its layout.",
		storageTypeKeys, props{
			"layout": enumOf(layoutLiterals, layoutDescription()),
			"path": nullable(anyOf(
				"The inventory directory, relative to the workspace root, or the file for a "+
					"collection or singleton. A file layout may give a path template "+
					"placing {slug}, or a list of them.",
				str(""), minItems(listOf(str(""), ""), 1))),
			"format": enumOf(storageFormats(),
				"Per item "+strings.Join(sortedKeys(PerItem), ", ")+" (md the default)"+
					"; for a collection "+strings.Join(sortedKeys(Collection), ", ")+"."),
			"id_prefix": nullable(anyOf(
				"The prefix a minted id starts with: a slug token, or one chosen by the value of an attribute.",
				pattern(str(""), IDPrefixPattern), ref("id_prefix_by"))),
			"id_date":    ref("bool", "When true, a minted id carries the day it was minted: <prefix>-<YYYY-MM-DD>-<slug>."),
			"id_ordinal": ref("bool", "When true, a slug may open with an NNN- ordinal instead of reporting it."),
			"template": nullable(anyOf(
				"The body template's name under .khub/templates/ (default: the type's own name), "+
					"or false for none. Per-item and singleton md types only.",
				pattern(str(""), TemplateNamePattern), constOf(false))),
		}, "layout"))
	defs.Set("id_prefix_by", object(
		"A prefix chosen by the value of another attribute, one per enum member.",
		idPrefixDeclKeys, props{
			"by": str("The attribute whose value picks the prefix."),
			"map": mapOf(pattern(str(""), IDPrefixPattern),
				"The prefix for each value of the attribute."),
		}, "by", "map"))
	defs.Set("bool", laxBool())

	root := object(
		"A khub layer file under .khub/. The layers merge on these top-level keys, never on the file name.",
		layerDocKeys, props{
			"ontology": nullable(object(
				"The domain: which types exist, their attributes and relations. "+
					"The base block is embedded in khub and may not be authored here.",
				authoredOntologyKeys, props{
					"entities": nullable(mapOf(ref("entity"), "Entity types keyed by name.")),
				})),
			"policy": nullable(mapOf(ref("policy"),
				"This workspace's gates, keyed by a type ontology declares.")),
			"storage": nullable(mapOf(ref("storage"),
				"Storage config, keyed by a type ontology declares. "+
					"A type with no entry is stored one file per entity under a directory named for it.")),
			"version": desc("The preset's version, stamped by a preset."),
		})
	// An empty, comment-only or `~` document is null to an editor, and khub
	// reads it as an empty layer file.
	root.Set("type", []any{"object", "null"})
	out := omap.New()
	out.Set("$schema", JSONSchemaDraft)
	out.Set("title", "khub layer file")
	for _, k := range root.Keys() {
		v, _ := root.Get(k)
		out.Set(k, v)
	}
	out.Set("definitions", defs)
	return out
}

// props is the property table of one mapping: key to its schema.
type props map[string]*omap.Map

// object is a closed mapping (extra="forbid") whose properties follow keys,
// the vocabulary's declaration order. A key with no schema, or a schema for a
// key the vocabulary does not list, panics.
func object(description string, keys []string, p props, required ...string) *omap.Map {
	for _, k := range slices.Sorted(maps.Keys(p)) {
		if !slices.Contains(keys, k) {
			panic(fmt.Sprintf("jsonschema: property %q is not in the vocabulary %v", k, keys))
		}
	}
	properties := omap.New()
	for _, k := range keys {
		s, ok := p[k]
		if !ok {
			panic(fmt.Sprintf("jsonschema: vocabulary key %q has no schema", k))
		}
		properties.Set(k, s)
	}
	m := omap.New()
	m.Set("description", description)
	m.Set("type", "object")
	m.Set("properties", properties)
	if len(required) > 0 {
		m.Set("required", strs(required))
	}
	m.Set("additionalProperties", false)
	return m
}

func desc(description string) *omap.Map {
	m := omap.New()
	if description != "" {
		m.Set("description", description)
	}
	return m
}

func str(description string) *omap.Map {
	m := desc(description)
	m.Set("type", "string")
	return m
}

// nonBlank matches a string strings.TrimSpace leaves non-empty: one character
// outside unicode.IsSpace's set, spelled out so an ECMA engine reads the same.
const nonBlank = "[^\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]"

// waiver is one waive entry: the findings accepted on one entity, each with
// its non-blank reason.
func waiver() *omap.Map {
	p := props{}
	for _, f := range WaivableFindings {
		p[f] = pattern(str("Why this finding is accepted."), nonBlank)
	}
	m := object("The check findings accepted on one entity, each with its reason.", WaivableFindings, p)
	m.Set("minProperties", 1)
	return m
}

func typed(typ, description string) *omap.Map {
	m := desc(description)
	m.Set("type", typ)
	return m
}

func minimum(m *omap.Map, n int) *omap.Map {
	m.Set("minimum", n)
	return m
}

func propertyNames(m, names *omap.Map) *omap.Map {
	m.Set("propertyNames", names)
	return m
}

func pattern(m *omap.Map, re string) *omap.Map {
	m.Set("pattern", re)
	return m
}

func enumOf(values []string, description string) *omap.Map {
	m := desc(description)
	m.Set("enum", strs(values))
	return m
}

func constOf(v any) *omap.Map {
	m := omap.New()
	m.Set("const", v)
	return m
}

func listOf(items *omap.Map, description string) *omap.Map {
	m := desc(description)
	m.Set("type", "array")
	m.Set("items", items)
	return m
}

func minItems(m *omap.Map, n int) *omap.Map {
	m.Set("minItems", n)
	return m
}

func mapOf(values *omap.Map, description string) *omap.Map {
	m := desc(description)
	m.Set("type", "object")
	m.Set("additionalProperties", values)
	return m
}

func ref(def string, description ...string) *omap.Map {
	m := omap.New()
	m.Set("$ref", "#/definitions/"+def)
	if len(description) > 0 {
		// Draft-07 ignores siblings of $ref; wrap so the description survives.
		w := desc(description[0])
		w.Set("allOf", []any{m})
		return w
	}
	return m
}

func anyOf(description string, branches ...*omap.Map) *omap.Map {
	m := desc(description)
	list := make([]any, len(branches))
	for i, b := range branches {
		list[i] = b
	}
	m.Set("anyOf", list)
	return m
}

// nullable admits null beside s, keeping s's description at the top where an
// editor shows it on hover.
func nullable(s *omap.Map) *omap.Map {
	m := omap.New()
	inner := omap.New()
	for _, k := range s.Keys() {
		v, _ := s.Get(k)
		if k == "description" {
			m.Set(k, v)
			continue
		}
		inner.Set(k, v)
	}
	null := omap.New()
	null.Set("type", "null")
	m.Set("anyOf", []any{inner, null})
	return m
}

// laxBool is boolLax: a boolean, 0 or 1, or one of its words in any case.
func laxBool() *omap.Map {
	words := append(slices.Clone(boolTrueWords), boolFalseWords...)
	alts := make([]string, len(words))
	for i, w := range words {
		alts[i] = caseless(w)
	}
	b := omap.New()
	b.Set("type", "boolean")
	n := omap.New()
	n.Set("enum", []any{0, 1})
	s := pattern(str(""), "^("+strings.Join(alts, "|")+")$")
	return anyOf("A boolean. khub also reads "+strings.Join(words, ", ")+", in any case.", b, n, s)
}

// caseless spells a word as a case-insensitive pattern ([tT][rR][uU][eE]):
// JSON Schema patterns take no flags.
func caseless(w string) string {
	var b strings.Builder
	for _, r := range w {
		lo, up := unicode.ToLower(r), unicode.ToUpper(r)
		if lo == up {
			b.WriteRune(r)
			continue
		}
		b.WriteString("[" + string(lo) + string(up) + "]")
	}
	return b.String()
}

// storageFormats is every format some layout accepts, in a fixed order: md
// first (the default), then the rest sorted.
func storageFormats() []string {
	set := maps.Clone(PerItem)
	maps.Copy(set, Collection)
	delete(set, "md")
	return append([]string{"md"}, sortedKeys(set)...)
}

func sortedKeys(set map[string]bool) []string { return slices.Sorted(maps.Keys(set)) }

// layoutDescription says what each layout in layoutLiterals stores. A layout
// with no meaning here panics.
func layoutDescription() string {
	meaning := map[string]string{
		"file":       "one entity per file",
		"folder":     "one per folder (_index)",
		"collection": "every entity a row of one file",
		"singleton":  "one fixed file",
	}
	parts := make([]string, len(layoutLiterals))
	for i, l := range layoutLiterals {
		m, ok := meaning[l]
		if !ok {
			panic(fmt.Sprintf("jsonschema: layout %q has no description", l))
		}
		parts[i] = l + ": " + m + "."
	}
	return strings.Join(parts, " ")
}

func strs(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
