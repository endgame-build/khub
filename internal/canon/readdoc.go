package canon

// ReadDoc is formats.read_doc: the EDIT-altitude round-trip load, as opposed to
// Parse's scan altitude.
//
// The distinction is byte-visible. Parse mirrors python-frontmatter, which
// strips the body and resolves scalars under YAML 1.1; read_doc fence-splits
// and loads under ruamel's 1.2, preserving the body exactly — including its
// trailing newline. Any caller that READS A DOCUMENT IN ORDER TO REWRITE IT
// must use this one, or it will silently reformat the parts it did not intend
// to touch.

import (
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

func ReadDoc(text, fmt_ string) (*omap.Map, string, error) {
	if fmt_ != "md" {
		return Parse(text, fmt_)
	}
	yamlText, body, splitErr := SplitFrontmatter(text)
	if splitErr != nil {
		return nil, "", errs.New("malformed_entity", splitErr.Error())
	}
	meta, nonMap, loadErr := LoadDocMap(yamlText, Mode12)
	if loadErr != nil {
		return nil, "", errs.New("malformed_entity", loadErr.Error())
	}
	if nonMap != nil {
		// Python would TypeError on the first `cmap[key] = value`.
		return nil, "", errs.New("malformed_entity", "A md entity must carry mapping frontmatter")
	}
	return meta, body, nil
}
