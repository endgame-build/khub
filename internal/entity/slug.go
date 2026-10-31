// Port of the slug half of src/khub/core/entity.py: slugify, _slug_source,
// _slug_base and _explicit_slug. The ordinal machinery that sat beside them
// (_minted_base, _next_ordinal, _mint_and_write) went with the `-NNN-` scheme;
// see mintSlug for why.

package entity

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/values"
)

// maxSlug is the longest slug khub mints or accepts; an over-long --id would
// otherwise crash at the write with an OSError (filename too long) instead of a
// located error.
const maxSlug = 100

var slugStrip = regexp.MustCompile(`[^a-z0-9]+`)

// isoDay is the YYYY-MM-DD opening of a date or datetime scalar — the part of
// `created` a dated id carries.
var isoDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// latinLetters spells the Latin letters that carry no combining mark, so no
// Unicode decomposition reaches an ASCII base for them.
var latinLetters = strings.NewReplacer(
	"ß", "ss", "ẞ", "ss", "æ", "ae", "Æ", "ae", "œ", "oe", "Œ", "oe", "ø", "o", "Ø", "o",
	"ł", "l", "Ł", "l", "đ", "d", "Đ", "d", "ð", "d", "Ð", "d", "þ", "th", "Þ", "th", "ı", "i",
)

// slugify lowercases, collapses non-alphanumeric runs to single hyphens, and
// trims hyphens. Python str.lower(), NOT casefold — target matching folds case,
// slug minting only lowers.
//
// Latin accents are spelled out first, so `Café` mints `cafe` whether it
// arrives composed (NFC) or decomposed (NFD, as macOS file names do):
// x/text decomposes and drops the combining marks, and latinLetters covers
// the letters that do not decompose. Other scripts are left to slugStrip.
func slugify(text string) string {
	plain, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), text)
	if err != nil {
		plain = text // a transformer over a whole in-memory string does not fail
	}
	lowered := toLower(latinLetters.Replace(plain))
	return trimHyphens(slugStrip.ReplaceAllString(lowered, "-"))
}

// SlugSource is the string a minted slug derives from: `name`, else `title`.
// ok is false when neither carries a value. There is no type-name fallback:
// it existed to keep capture unblocked while the ordinal told two untitled
// entities apart, and without the ordinal it would mint one id per type.
//
// Exported for the id gate: validate tells a retired `NNN-` ordinal apart from
// a title that genuinely starts with digits by slugifying the same source add
// would have minted from.
func SlugSource(attrs *omap.Map) (string, bool) {
	for _, key := range []string{"name", "title"} {
		value, ok := attrs.Get(key)
		if ok && truthy(value) {
			return values.Str(value), true
		}
	}
	return "", false
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

// chooseSlug is the slug a create writes under: an explicit --id, slugified
// and capped exactly like a minted one, else the minted id.
func chooseSlug(rtype *schema.ResolvedType, meta *omap.Map, id string) (string, error) {
	if id != "" {
		return slugBase(id)
	}
	return mintSlug(rtype, meta)
}

// mintSlug is `<prefix>-<YYYY-MM-DD>-<slug>`, with the prefix and the date each
// optional per type (storage `id_prefix` and `id_date`).
//
// A pure function of the schema, the type and the frontmatter — it reads no
// siblings. The ordinal it replaced was max(existing)+1 over an index scan, a
// read-modify-write that two branches, worktrees or agents each won: the
// filenames differed, so git merged both and nothing ever reported the
// collision. Minting the same id twice is now a refusal (Create's index
// pre-check and the O_EXCL write) or an add/add merge conflict on one
// filename, which is the point.
//
// The date is the one this id was minted on, and it stays that even if
// `created` is later edited. Do not add a check that the two agree: it would
// fire on every legitimate edit and on every --id.
func mintSlug(rtype *schema.ResolvedType, meta *omap.Map) (string, error) {
	source, ok := SlugSource(meta)
	if !ok {
		return "", errs.NoSlugSource(rtype.Name)
	}
	base, err := slugBase(source)
	if err != nil {
		return "", err
	}
	return mintFrom(rtype, meta, base)
}

// mintFrom is the type's id scheme around base: its prefix (literal, or
// decided by an attribute's value) and its date.
func mintFrom(rtype *schema.ResolvedType, meta *omap.Map, base string) (string, error) {
	stem := ""
	if rtype.IDPrefix != nil {
		prefix, resolved := rtype.IDPrefix.Resolve(meta)
		if !resolved {
			// Only the by-value form can fail to resolve — a literal always
			// does — so By is set here. The ordinal used to stand in for the
			// missing prefix (`NNN-slug`); without it there is nothing to mint.
			return "", errs.IDPrefixUndecided(rtype.Name, *rtype.IDPrefix.By, memberValues(rtype.IDPrefix))
		}
		stem = prefix + "-"
	}
	dated := ""
	if rtype.IDDate {
		dated = mintDate(meta) + "-"
	}
	return stem + dated + base, nil
}

// mintDate is the day a dated id carries: the entity's `created` as assembled
// by Create (so a --created override dates the id), else today — `--created ""`
// clears the field to null, and the id still needs the day it was minted on.
func mintDate(meta *omap.Map) string {
	if v, has := meta.Get("created"); has && v != nil {
		if s := values.Str(v); isoDay.MatchString(s) {
			return s[:10]
		}
	}
	return today().ISO
}

// memberValues lists the enum values a by-value prefix decides on, in declared
// order — the `<functional|constraint|…>` a refusal names.
func memberValues(p *schema.IDPrefix) []string {
	out := make([]string, len(p.Members))
	for i, m := range p.Members {
		out[i] = m.Value
	}
	return out
}
