package cli

// parse_fields port (cli/_render.py): the dynamic --<field> <value> parser
// behind add/edit/query. Accepts --key value, --key=value, and bare key value;
// '-' → '_' in KEYS only; a trailing key without a value is a usage error
// (exit 2), never a silent empty string.

import (
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// ParseFields consumes the extra args cobra left unparsed.
//
// Exactly TWO leading dashes are the flag prefix; anything else is part of the
// key. `khub query -C /x` is not a workspace option here (that one is
// root-only) — Click hands the raw "-C" back as a leftover, so the field is
// `_C`, dash-to-underscore like any other.
func ParseFields(args []string) (*omap.Map, error) {
	out := omap.New()
	i := 0
	for i < len(args) {
		token := args[i]
		key := token
		if strings.HasPrefix(token, "--") {
			key = token[2:]
			if eq := strings.IndexByte(key, '='); eq >= 0 {
				out.Set(fieldKey(key[:eq]), key[eq+1:])
				i++
				continue
			}
		}
		if i+1 >= len(args) {
			return nil, &errs.Usage{Message: "Field '" + key + "' has no value"}
		}
		out.Set(fieldKey(key), args[i+1])
		i += 2
	}
	return out, nil
}

// fieldKey is Python's key.replace("-", "_"): CLI dashes become schema
// underscores, in the key only.
func fieldKey(key string) string { return strings.ReplaceAll(key, "-", "_") }
