package canon

// The three Python json.dumps dialects khub emits. Stock
// encoding/json is banned on output paths: it escapes <>&, drops the space
// separators, and emits raw UTF-8 where the CLI dialect escapes.
//
//	EncodeCLI  — cli/_render.py emit/_fail: separators (", ", ": "),
//	             ensure_ascii=True, default=str (dates → ISO), one line.
//	EncodeDisk — formats.py render/dump_collection for json: indent=2,
//	             ensure_ascii=False, trailing "\n".
//	JSONLRow   — formats.py dump_collection jsonl: compact spaced separators,
//	             ensure_ascii=False, "slug" first.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/endgame-build/khub/internal/omap"
)

// EncodeCLI renders v in the CLI dialect: one line, ASCII-escaped, a space
// after each separator — what every --format json read prints.
func EncodeCLI(v any) (string, error) {
	var b strings.Builder
	if err := encodeJSON(&b, v, true, -1, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

// EncodeDisk renders v in the on-disk dialect: two-space indent, raw UTF-8,
// trailing newline — the bytes of a .json entity.
func EncodeDisk(v any) (string, error) {
	var b strings.Builder
	if err := encodeJSON(&b, v, false, 2, 0); err != nil {
		return "", err
	}
	return b.String() + "\n", nil
}

// EncodeJSONLRow renders one jsonl collection row with slug as its first key.
func EncodeJSONLRow(slug string, row *omap.Map) (string, error) {
	merged := omap.New()
	merged.Set("slug", slug)
	for _, k := range row.Keys() {
		v, _ := row.Get(k)
		merged.Set(k, v)
	}
	var b strings.Builder
	if err := encodeJSON(&b, merged, false, -1, 0); err != nil {
		return "", err
	}
	return b.String(), nil
}

// encodeJSON: indent < 0 → compact-with-spaces (Python default separators);
// indent >= 0 → indented (Python separators become (",", ": ") under indent).
func encodeJSON(b *strings.Builder, v any, ensureASCII bool, indent, depth int) error {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		encodeJSONString(b, x, ensureASCII)
	case int:
		b.WriteString(strconv.Itoa(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case BigInt:
		b.WriteString(x.Literal)
	case float64:
		b.WriteString(PyFloatRepr(x))
	case Date:
		encodeJSONString(b, x.ISO, ensureASCII)
	case DateTime:
		// Every surface agrees on the ISO "T" form (#42). The CLI dialect used
		// to render a datetime with a SPACE separator here, because Python's
		// json.dumps(data, default=str) reached str(datetime) while the disk
		// dialect went through .isoformat() — so `khub get --format json`
		// disagreed with the file it had just read, and the space form is not
		// ISO 8601 for anything parsing it downstream.
		encodeJSONString(b, x.ISO, ensureASCII)
	case *omap.Map:
		if x.Len() == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteString("{")
		for i, k := range x.Keys() {
			val, _ := x.Get(k)
			if i > 0 {
				b.WriteString(",")
				if indent < 0 {
					b.WriteString(" ")
				}
			}
			writeIndentJSON(b, indent, depth+1)
			encodeJSONString(b, k, ensureASCII)
			b.WriteString(": ")
			if err := encodeJSON(b, val, ensureASCII, indent, depth+1); err != nil {
				return err
			}
		}
		writeIndentJSON(b, indent, depth)
		b.WriteString("}")
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteString("[")
		for i, item := range x {
			if i > 0 {
				b.WriteString(",")
				if indent < 0 {
					b.WriteString(" ")
				}
			}
			writeIndentJSON(b, indent, depth+1)
			if err := encodeJSON(b, item, ensureASCII, indent, depth+1); err != nil {
				return err
			}
		}
		writeIndentJSON(b, indent, depth)
		b.WriteString("]")
	default:
		// Python's emit path passes default=str, so an unmodelled value
		// stringifies instead of raising. Mirror that: an unserializable value
		// must never turn a successful command into an error.
		encodeJSONString(b, fmt.Sprint(x), ensureASCII)
	}
	return nil
}

func writeIndentJSON(b *strings.Builder, indent, depth int) {
	if indent >= 0 {
		b.WriteString("\n" + strings.Repeat(" ", indent*depth))
	}
}

func encodeJSONString(b *strings.Builder, s string, ensureASCII bool) {
	b.WriteByte('"')
	for _, ch := range s {
		switch ch {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
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
			switch {
			case ch < 0x20:
				fmt.Fprintf(b, `\u%04x`, ch)
			case ch < 0x7f || !ensureASCII:
				b.WriteRune(ch)
			case ch <= 0xffff:
				fmt.Fprintf(b, `\u%04x`, ch)
			default:
				hi, lo := utf16.EncodeRune(ch)
				fmt.Fprintf(b, `\u%04x\u%04x`, hi, lo)
			}
		}
	}
	b.WriteByte('"')
}
