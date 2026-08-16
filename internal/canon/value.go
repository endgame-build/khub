package canon

import (
	"fmt"
	"strconv"
	"strings"
)

// Date is khub's date scalar: emitted plain ISO in YAML, ISO string in JSON.
type Date struct{ ISO string }

// DateTime mirrors Python datetime values reaching serialization.
type DateTime struct{ ISO string }

// BigInt carries integers beyond int64 (Python ints are unbounded); the
// literal decimal string is the value.
type BigInt struct{ Literal string }

// PyFloatRepr reproduces CPython repr(float): shortest round-trip digits,
// fixed notation for decimal exponents in (-4, 16], else e±NN with a
// two-digit minimum exponent.
func PyFloatRepr(f float64) string {
	shortest := strconv.FormatFloat(f, 'e', -1, 64) // d.ddddde±XX
	mant, expPart, _ := strings.Cut(shortest, "e")
	exp10, _ := strconv.Atoi(expPart)
	neg := strings.HasPrefix(mant, "-")
	mant = strings.TrimPrefix(mant, "-")
	digits := strings.Replace(mant, ".", "", 1)
	decExp := exp10 + 1 // digits[0] sits before this decimal position

	var out string
	switch {
	case -4 < decExp && decExp <= 16:
		switch {
		case decExp <= 0:
			out = "0." + strings.Repeat("0", -decExp) + digits
		case decExp >= len(digits):
			out = digits + strings.Repeat("0", decExp-len(digits)) + ".0"
		default:
			out = digits[:decExp] + "." + digits[decExp:]
		}
	default:
		e := decExp - 1
		sign := "+"
		if e < 0 {
			sign, e = "-", -e
		}
		es := strconv.Itoa(e)
		if len(es) < 2 {
			es = "0" + es
		}
		if len(digits) > 1 {
			out = digits[:1] + "." + digits[1:] + "e" + sign + es
		} else {
			out = digits + "e" + sign + es
		}
	}
	if neg {
		out = "-" + out
	}
	return out
}

func scalarString(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "null", nil
	case bool:
		if x {
			return "true", nil
		}
		return "false", nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case BigInt:
		return x.Literal, nil
	case float64:
		return PyFloatRepr(x), nil
	case Date:
		return x.ISO, nil
	case DateTime:
		return x.ISO, nil
	default:
		return "", fmt.Errorf("not a scalar: %T", v)
	}
}
