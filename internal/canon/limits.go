package canon

import "github.com/endgame-build/khub/internal/errs"

// MaxTextBytes caps one text document khub reads or parses: an entity file, a
// collection, a bundle file. Real documents sit far below it; a larger one is
// refused rather than loaded and re-rendered whole.
const MaxTextBytes = 4 << 20

// MaxAliasNodes caps how many nodes a document's YAML aliases may expand to.
// The loader shares an anchored value instead of copying it, so loading stays
// cheap, but every render (get, query, a JSON or YAML write) expands each
// alias in full. A billion-laughs document would load in microseconds and then
// render without end.
const MaxAliasNodes = 100_000

// checkSize refuses text past MaxTextBytes as a malformed document.
func checkSize(text string) error {
	if len(text) > MaxTextBytes {
		return errs.TextTooLarge("", len(text), MaxTextBytes)
	}
	return nil
}
