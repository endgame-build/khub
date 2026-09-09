package canon

// Port of core/formats.py — per-format entity serialization, the one strategy
// module. Every read and write of an entity document funnels through here.
// Error messages are byte-contract (code malformed_entity throughout).

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// PerItem lists the formats a one-entity-per-file type may declare.
var PerItem = map[string]bool{"md": true, "json": true, "yaml": true}

// Collection lists the formats a single-file collection type may declare.
var Collection = map[string]bool{"json": true, "jsonl": true, "yaml": true}

const bodyKey = "body"

// FmtOf returns the format a path stores: its suffix sans dot (.yml is NOT aliased).
func FmtOf(path string) string { return strings.TrimPrefix(filepath.Ext(path), ".") }

// PyTypeName maps a canon value to the Python type name khub's messages print
// — here and in the template layer's "'title' must be text, got int".
func PyTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case int64, int:
		return "int"
	case BigInt:
		return "int"
	case float64:
		return "float"
	case Date:
		return "date"
	case DateTime:
		return "datetime"
	case []any:
		return "list"
	case *omap.Map:
		return "dict"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// Parse returns (meta, body) from document text; error on a malformed document.
func Parse(text, fmt_ string) (*omap.Map, string, error) {
	if fmt_ == "md" {
		// python-frontmatter semantics (see ParseFrontmatter): no fence, or an
		// unterminated one, yields empty metadata and the text as body — never
		// an error. That is what makes a loose .md inside a layout a stray
		// instead of a malformed entity.
		yamlText, body, hasFence := ParseFrontmatter(text)
		if !hasFence {
			return omap.New(), body, nil
		}
		m, nonMap, lerr := LoadDocMap(yamlText, Mode11)
		if lerr != nil {
			// Only a fence that split and whose YAML is invalid raises; the
			// PyYAML error propagates through python-frontmatter.
			return nil, "", errs.New("malformed_entity", lerr.Error())
		}
		if nonMap != nil {
			// `metadata.update(fm_data)` runs only when fm_data is a dict, so
			// non-mapping frontmatter is simply empty metadata.
			return omap.New(), body, nil
		}
		return m, body, nil
	}
	if !PerItem[fmt_] {
		return nil, "", errs.New("malformed_entity",
			fmt.Sprintf("'%s' is not a per-item format; collections load via load_collection", fmt_))
	}
	data, err := loadMapping(text, fmt_)
	if err != nil {
		return nil, "", err
	}
	body, err := popBody(data, fmt_, "")
	if err != nil {
		return nil, "", err
	}
	return data, body, nil
}

func loadMapping(text, fmt_ string) (*omap.Map, error) {
	var v any
	var err error
	if fmt_ == "json" {
		if strings.TrimSpace(text) == "" {
			return omap.New(), nil
		}
		v, err = DecodeOrderedJSONStrict([]byte(text))
	} else {
		v, err = LoadDocMode(text, Mode12)
		if v == nil && err == nil {
			return omap.New(), nil
		}
	}
	if err != nil {
		return nil, errs.New("malformed_entity", err.Error())
	}
	m, ok := v.(*omap.Map)
	if !ok {
		return nil, errs.New("malformed_entity",
			fmt.Sprintf("A %s entity must be a single mapping, got %s", fmt_, PyTypeName(v)))
	}
	return m, nil
}

func popBody(data *omap.Map, fmt_, ctx string) (string, error) {
	raw, ok := data.Get(bodyKey)
	if !ok {
		return "", nil
	}
	data.Delete(bodyKey)
	if raw == nil {
		return "", nil
	}
	if s, isStr := raw.(string); isStr {
		return s, nil
	}
	return "", errs.New("malformed_entity",
		fmt.Sprintf("%sthe reserved 'body' key of a %s entity must be a string, got %s",
			ctx, fmt_, PyTypeName(raw)))
}

// Render serializes a document in the format's native shape.
func Render(meta *omap.Map, body, fmt_ string) (string, error) {
	if fmt_ == "md" {
		y, err := DumpRT(meta)
		if err != nil {
			return "", err
		}
		return RenderMD(y, body), nil
	}
	work := meta
	if body != "" {
		work = cloneWith(meta, bodyKey, body)
	}
	if fmt_ == "json" {
		return EncodeDisk(work)
	}
	return DumpRT(work)
}

func cloneWith(m *omap.Map, key string, v any) *omap.Map {
	out := omap.New()
	for _, k := range m.Keys() {
		val, _ := m.Get(k)
		out.Set(k, val)
	}
	out.Set(key, v)
	return out
}

// FTSBody is the searchable prose — body plus every scalar string field except
// type/title/name.
func FTSBody(meta *omap.Map, body string) string {
	parts := []string{body}
	for _, k := range meta.Keys() {
		if k == "type" || k == "title" || k == "name" {
			continue
		}
		v, _ := meta.Get(k)
		if s, ok := v.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}

// --- collections -------------------------------------------------------------

// LoadCollection returns raw rows keyed by slug; error on ANY bad row (whole-file
// malformed contract). Empty text → zero rows.
func LoadCollection(text, fmt_ string) (*omap.Map, error) {
	rows := omap.New()
	if fmt_ == "jsonl" {
		for n, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			v, err := DecodeOrderedJSONStrict([]byte(line))
			if err != nil {
				return nil, errs.New("malformed_entity", err.Error())
			}
			row, ok := v.(*omap.Map)
			if !ok {
				return nil, errs.New("malformed_entity",
					fmt.Sprintf("jsonl row %d is not an object (%s)", n+1, PyTypeName(v)))
			}
			slugAny, has := row.Get("slug")
			row.Delete("slug")
			slug, isStr := slugAny.(string)
			if !has || !isStr || slug == "" {
				return nil, errs.New("malformed_entity",
					fmt.Sprintf("jsonl row %d is missing its reserved string 'slug' key", n+1))
			}
			if _, dup := rows.Get(slug); dup {
				return nil, errs.New("malformed_entity",
					fmt.Sprintf("duplicate slug '%s' at jsonl row %d", slug, n+1))
			}
			rows.Set(slug, row)
		}
	} else {
		data, err := loadMapping(text, fmt_)
		if err != nil {
			return nil, err
		}
		for _, key := range data.Keys() {
			rowAny, _ := data.Get(key)
			if key == "" {
				return nil, errs.New("malformed_entity",
					fmt.Sprintf("collection key %s is not a slug string", pyRepr(key)))
			}
			row, ok := rowAny.(*omap.Map)
			if !ok {
				return nil, errs.New("malformed_entity",
					fmt.Sprintf("row '%s' is not a mapping (%s)", key, PyTypeName(rowAny)))
			}
			if innerAny, has := row.Get("slug"); has {
				row.Delete("slug")
				if inner, isStr := innerAny.(string); !isStr || inner != key {
					return nil, errs.New("malformed_entity",
						fmt.Sprintf("row '%s' carries a disagreeing slug key '%v'", key, innerAny))
				}
			}
		}
		rows = data
	}
	for _, slug := range rows.Keys() {
		rowAny, _ := rows.Get(slug)
		row := rowAny.(*omap.Map)
		if raw, has := row.Get(bodyKey); has && raw != nil {
			if _, isStr := raw.(string); !isStr {
				return nil, errs.New("malformed_entity",
					fmt.Sprintf("row '%s': the reserved 'body' key of a %s entity must be a string, got %s",
						slug, fmt_, PyTypeName(raw)))
			}
		}
	}
	return rows, nil
}

// DumpCollection serializes rows back to the file's native shape.
func DumpCollection(rows *omap.Map, fmt_ string) (string, error) {
	switch fmt_ {
	case "jsonl":
		var lines []string
		for _, slug := range rows.Keys() {
			rowAny, _ := rows.Get(slug)
			line, err := EncodeJSONLRow(slug, rowAny.(*omap.Map))
			if err != nil {
				return "", err
			}
			lines = append(lines, line)
		}
		if len(lines) == 0 {
			return "", nil
		}
		return strings.Join(lines, "\n") + "\n", nil
	case "json":
		return EncodeDisk(rows)
	default:
		return DumpRT(rows)
	}
}

// RenderRow is one row's stored serialization (get --format raw for a row).
func RenderRow(slug string, row *omap.Map, fmt_ string) (string, error) {
	one := omap.New()
	one.Set(slug, row)
	return DumpCollection(one, fmt_)
}

// SplitRow splits a raw row into (meta, body) — reserved key stripped, row untouched.
func SplitRow(row *omap.Map, fmt_ string) (*omap.Map, string, error) {
	meta := omap.New()
	for _, k := range row.Keys() {
		v, _ := row.Get(k)
		meta.Set(k, v)
	}
	body, err := popBody(meta, fmt_, "")
	return meta, body, err
}
