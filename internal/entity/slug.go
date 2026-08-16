// Port of the slug half of src/khub/core/entity.py: slugify, _slug_source,
// _slug_base, _minted_base, _next_ordinal, _explicit_slug and _mint_and_write.
package entity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// maxSlug is the longest slug khub mints or accepts; an over-long --id would
// otherwise crash at the write with an OSError (filename too long) instead of a
// located error.
const maxSlug = 100

// ordinalWidth is the zero-padding of a minted ordinal (001, 045, 1200).
const ordinalWidth = 3

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases, collapses non-alphanumeric runs to single hyphens, and
// trims hyphens. Python str.lower(), NOT casefold — target matching folds case,
// slug minting only lowers (go-port-plan R6).
func slugify(text string) string {
	lowered := toLower(text)
	return trimHyphens(slugStrip.ReplaceAllString(lowered, "-"))
}

// slugSource is the string a minted slug derives from: a name, else a title,
// else the type. firm-ops meetings/fragments carry no `name`, so the title
// fallback keeps their slugs meaningful instead of collapsing every one to the
// bare type name.
func slugSource(typeName string, attrs *omap.Map) string {
	for _, key := range []string{"name", "title"} {
		value, ok := attrs.Get(key)
		if ok && truthy(value) {
			return values.Str(value)
		}
	}
	return typeName
}

// slugBase is a minted slug's base: slugified, non-empty, within the length cap.
func slugBase(source string) (string, error) {
	base := slugify(source)
	if base == "" { // an all-symbol source would write a hidden, collision-blind file
		return "", errs.InvalidSlug(source)
	}
	runes := []rune(base)
	if len(runes) > maxSlug {
		return "", errs.New("invalid_slug",
			fmt.Sprintf("Slug '%s…' exceeds %d characters", string(runes[:40]), maxSlug))
	}
	return base, nil
}

// mintedBase is a minted slug: `<prefix>-<number>-<slug>`, or `<number>-<slug>`
// with no prefix. Every minted id carries an ordinal, so a corpus reads in the
// order it was authored and an entity can be named in prose by a short stable
// handle (ad-004). The number is zero-padded to three and keeps counting past
// it — 001, 045, 1200.
func mintedBase(rtype *schema.ResolvedType, attrs *omap.Map, idx *index.Index) (string, error) {
	base, err := slugBase(slugSource(rtype.Name, attrs))
	if err != nil {
		return "", err
	}
	prefix := ""
	if rtype.IdPrefix != nil {
		if p, ok := rtype.IdPrefix.Resolve(attrs); ok {
			prefix = p
		}
	}
	stem := ""
	if prefix != "" {
		stem = prefix + "-"
	}
	ordinal := strconv.Itoa(nextOrdinal(idx, rtype.Name, prefix))
	for len(ordinal) < ordinalWidth {
		ordinal = "0" + ordinal
	}
	return stem + ordinal + "-" + base, nil
}

// nextOrdinal is one past the highest ordinal in use for this type (and prefix,
// if any). Counted per prefix, not per type: a requirement schema minting `fr-`
// and `cst-` keeps two independent sequences, which is what makes the number
// readable as "the fourth constraint" rather than an arbitrary position.
func nextOrdinal(idx *index.Index, typeName, prefix string) int {
	expr := `^(\d+)-`
	if prefix != "" {
		expr = `^` + regexp.QuoteMeta(prefix) + `-(\d+)-`
	}
	pattern := regexp.MustCompile(expr)
	highest := 0
	for node := range idx.Nodes {
		if node.Type != typeName {
			continue
		}
		m := pattern.FindStringSubmatch(node.Slug)
		if m == nil {
			continue
		}
		if n, err := strconv.Atoi(m[1]); err == nil && n > highest {
			highest = n
		}
	}
	return highest + 1
}

// explicitSlug is an explicit --id's slug: slugified, capped, and unique — a
// collision refuses. Unlike a minted slug an explicit id is never auto-suffixed:
// the caller named it, so a within-type collision is an error to surface.
func explicitSlug(id, typeName string, idx *index.Index) (string, error) {
	base, err := slugBase(id)
	if err != nil {
		return "", err
	}
	if idx.Nodes[index.Node{Type: typeName, Slug: base}] {
		return "", slugTaken(base, typeName)
	}
	return base, nil
}

func slugTaken(slug, typeName string) *errs.Located {
	return errs.New("slug_taken",
		fmt.Sprintf("Slug '%s' is already taken in %s; choose another --id", slug, typeName))
}

// mintAndWrite picks the first free base/base-N slug and writes it with O_EXCL.
// The index gives a cheap first guess; the exclusive create is the real gate, so
// a second add racing to the same slug loses the O_EXCL and retries the next
// suffix instead of clobbering the winner.
func mintAndWrite(
	root string, rtype *schema.ResolvedType, base, typeName string,
	idx *index.Index, meta *omap.Map, body string,
) (string, string, error) {
	n, slug := 1, base
	for {
		if idx.Nodes[index.Node{Type: typeName, Slug: slug}] {
			n++
			slug = fmt.Sprintf("%s-%d", base, n)
			continue
		}
		path := entityPath(root, rtype, slug)
		if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
			return "", "", err
		}
		err := writeNew(path, meta, body)
		if err == nil {
			return slug, path, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", "", err
		}
		n++
		slug = fmt.Sprintf("%s-%d", base, n)
	}
}
