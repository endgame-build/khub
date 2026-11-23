// Package savedquery stores named `khub query` filter sets: `query --save-as
// <name>` writes one, `khub run <name>` replays it.
//
// The store is one file, .khub/queries.yaml, a mapping of name → definition.
// It is tracked in git (only .khub/generated/ is ignored): a saved query is
// part of how a team reads its corpus, so it travels with the corpus the way
// the schema does. A definition is the query's filter flags in their own
// words (Keys) and the --<field> filters, never the output flags (--format,
// --receipt), which belong to each run.
//
// Writes take the workspace lock and publish atomically like every other
// mutation. Only a missing file is emitted whole. An existing file is
// spliced: a new name is appended, a replaced definition is re-emitted in
// place as one entry, and every other byte — other entries, their comments
// and style — survives. A change neither splice can make safely refuses
// rather than rewriting the file.
package savedquery

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/query"
)

// File is the store's workspace-relative path.
const File = ".khub/queries.yaml"

const header = "# khub saved queries: `khub query … --save-as <name>` writes one, `khub run <name>` replays it.\n"

// namePattern keeps a name a plain shell word and a YAML key that needs no
// quoting, once the scalars YAML 1.1 reads as a boolean or null are refused
// (ambiguousNames): a name reads the same to khub and to any other tool.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

var ambiguousNames = []string{"null", "true", "false", "yes", "no", "on", "off", "y", "n"}

// Keys are the definition keys a saved query may carry besides its field
// filters, in the order a save writes them: each is the `khub query` flag of
// that name. A field filter's key is its field name, which never collides:
// the declared flag binds first on the command line, and a field name holds
// no dash, so `in-count` and `out-count` are free of the `in_count` field.
var Keys = []string{
	"type", "tag", "has", "missing", "under", "via", "count", "in-count", "out-count",
	"orphan", "stale", "draft", "active", "trust", "limit",
}

// Outcome says what a save did.
type Outcome string

const (
	Saved     Outcome = "saved"
	Replaced  Outcome = "replaced"
	Unchanged Outcome = "unchanged"
)

// Definition renders a filter set as the mapping the store holds: `type`,
// the field filters in command-line order, then the rest of Keys in order. An
// unset option is absent, and a switch is present only when it is on.
func Definition(f query.Filters) *omap.Map {
	def := omap.New()
	if f.Type != nil {
		def.Set("type", *f.Type)
	}
	if f.Fields != nil {
		for _, k := range f.Fields.Keys() {
			v, _ := f.Fields.Get(k)
			def.Set(k, v)
		}
	}
	for _, kv := range []struct {
		key string
		val *string
	}{{"tag", f.Tag}, {"has", f.Has}, {"missing", f.Missing}, {"under", f.Under}, {"via", f.Via}} {
		if kv.val != nil {
			def.Set(kv.key, *kv.val)
		}
	}
	if len(f.Counts) > 0 {
		counts := make([]any, len(f.Counts))
		for i, c := range f.Counts {
			counts[i] = c.Name + comparison(c.Cmp)
		}
		def.Set("count", counts)
	}
	for _, kv := range []struct {
		key string
		cs  []query.Comparison
	}{{"in-count", f.InCount}, {"out-count", f.OutCount}} {
		if len(kv.cs) > 0 {
			bounds := make([]any, len(kv.cs))
			for i, c := range kv.cs {
				bounds[i] = comparison(c)
			}
			def.Set(kv.key, bounds)
		}
	}
	for _, kv := range []struct {
		key string
		on  bool
	}{{"orphan", f.Orphan}, {"stale", f.Stale}, {"draft", f.DraftOnly}, {"active", f.ActiveOnly}} {
		if kv.on {
			def.Set(kv.key, true)
		}
	}
	if f.Trust != nil {
		def.Set("trust", *f.Trust)
	}
	if f.Limit != nil {
		def.Set("limit", int64(*f.Limit))
	}
	return def
}

// comparison writes a bound as `<op><n>`, a bare n as `=n`, the form
// query.ParseComparison reads back.
func comparison(c query.Comparison) string { return c.Op + strconv.Itoa(c.N) }

// Filters reads a stored definition back into the filter set `query` runs. A
// hand-edited value of the wrong kind refuses, naming the query and the key.
func Filters(name string, def *omap.Map) (query.Filters, error) {
	var f query.Filters
	fields := omap.New()
	for _, k := range def.Keys() {
		v, _ := def.Get(k)
		switch k {
		case "limit":
			n, ok := v.(int64)
			if !ok {
				return f, invalidDefinition(name, k, "an integer")
			}
			limit := int(n)
			f.Limit = &limit
		case "orphan", "stale", "draft", "active":
			on, ok := v.(bool)
			if !ok {
				return f, invalidDefinition(name, k, "true or false")
			}
			switch k {
			case "orphan":
				f.Orphan = on
			case "stale":
				f.Stale = on
			case "draft":
				f.DraftOnly = on
			case "active":
				f.ActiveOnly = on
			}
		case "count":
			exprs, ok := stringList(v)
			if !ok {
				return f, invalidDefinition(name, k, "a list of <relation><op><n> strings")
			}
			for _, expr := range exprs {
				c, ok := query.ParseCount(expr)
				if !ok {
					return f, invalidDefinition(name, k, "a list of <relation><op><n> strings")
				}
				f.Counts = append(f.Counts, c)
			}
		case "in-count", "out-count":
			exprs, ok := stringList(v)
			if !ok {
				return f, invalidDefinition(name, k, "a list of [<op>]<n> strings")
			}
			for _, expr := range exprs {
				c, ok := query.ParseComparison(expr)
				if !ok {
					return f, invalidDefinition(name, k, "a list of [<op>]<n> strings")
				}
				if k == "in-count" {
					f.InCount = append(f.InCount, c)
				} else {
					f.OutCount = append(f.OutCount, c)
				}
			}
		default:
			s, ok := v.(string)
			if !ok {
				return f, invalidDefinition(name, k, "a string (quote it)")
			}
			switch k {
			case "type":
				f.Type = &s
			case "tag":
				f.Tag = &s
			case "has":
				f.Has = &s
			case "missing":
				f.Missing = &s
			case "under":
				f.Under = &s
			case "via":
				f.Via = &s
			case "trust":
				f.Trust = &s
			default:
				fields.Set(k, s)
			}
		}
	}
	if (f.Under == nil) != (f.Via == nil) {
		missing := "via"
		if f.Under == nil {
			missing = "under"
		}
		return f, invalidDefinition(name, missing, "set with 'under' and 'via' together")
	}
	f.Fields = fields
	return f, nil
}

// stringList reads a YAML sequence of strings.
func stringList(v any) ([]string, bool) {
	items, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, false
		}
		out[i] = s
	}
	return out, true
}

// Load reads the store. A workspace with no file has no saved queries.
func Load(root string) (*omap.Map, error) {
	_, store, err := read(root)
	return store, err
}

// Get returns one saved definition, or refuses naming the saved ones.
func Get(root, name string) (*omap.Map, error) {
	store, err := Load(root)
	if err != nil {
		return nil, err
	}
	v, ok := store.Get(name)
	if !ok {
		return nil, unknownQuery(name, store.Keys())
	}
	return v.(*omap.Map), nil
}

// Save records def under name. A name already holding a different definition
// refuses unless replace is set; one holding the same definition is left
// alone and reports Unchanged, so a repeated save is idempotent.
func Save(root, name string, def *omap.Map, replace bool) (Outcome, error) {
	if !namePattern.MatchString(name) {
		return "", errs.New("invalid_query_name", fmt.Sprintf(
			"Cannot save a query as '%s': a name is lowercase letters, digits, '-' and '_', starting with a letter", name))
	}
	if slices.Contains(ambiguousNames, name) {
		return "", errs.New("invalid_query_name", fmt.Sprintf(
			"Cannot save a query as '%s': YAML reads it as a boolean or null; choose another name", name))
	}
	return fsio.Locked(root, func() (Outcome, error) {
		old, store, err := read(root)
		if err != nil {
			return "", err
		}
		outcome := Saved
		have, existed := store.Get(name)
		if existed {
			same, err := sameDefinition(have, def)
			if err != nil {
				return "", err
			}
			if same {
				return Unchanged, nil
			}
			if !replace {
				return "", errs.New("saved_query_exists", fmt.Sprintf(
					"A saved query '%s' already exists in %s with a different definition. "+
						"Pass --replace to overwrite it, or save under another name", name, File))
			}
			outcome = Replaced
		}
		store.Set(name, def)
		text, err := render(old, store, name)
		if err != nil {
			return "", err
		}
		if err := fsio.AtomicWriteIn(root, path(root), []byte(text)); err != nil {
			return "", err
		}
		return outcome, nil
	})
}

// read returns the store's current text ("" when absent) and its mapping.
func read(root string) (string, *omap.Map, error) {
	raw, err := fsio.ReadFile(root, path(root))
	if errors.Is(err, fs.ErrNotExist) {
		return "", omap.New(), nil
	}
	if err != nil {
		return "", nil, err
	}
	text := canon.NormalizeNewlines(string(raw))
	store, nonMap, err := canon.LoadDocMap(text, canon.Mode12)
	if err != nil {
		return "", nil, invalidStore(err.Error())
	}
	if nonMap != nil {
		return "", nil, invalidStore("its top level is not a mapping of query names to definitions")
	}
	for _, k := range store.Keys() {
		v, _ := store.Get(k)
		if _, ok := v.(*omap.Map); !ok {
			return "", nil, invalidStore(fmt.Sprintf("'%s' is not a mapping of query options", k))
		}
	}
	return text, store, nil
}

// render writes the store. A missing file is emitted whole. An existing one
// is spliced so every byte the save need not touch survives: a new name is
// appended and a replaced definition is spliced in place. A change the
// splice cannot make safely (an entry written inline as {…}, an anchor or an
// alias) refuses; nothing is written.
func render(old string, store *omap.Map, name string) (string, error) {
	if old == "" {
		text, err := canon.DumpRT(store)
		if err != nil {
			return "", err
		}
		return header + text, nil
	}
	out, err := canon.SpliceMapping([]byte(old), store, canon.Mode12)
	if errors.Is(err, canon.ErrNoSplice) {
		return "", errs.New("saved_query_unspliceable", fmt.Sprintf(
			"Cannot save query '%s' without rewriting the rest of %s (its layout, an anchor or an alias "+
				"stands in the way); nothing was written. Edit the file by hand", name, File))
	}
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// sameDefinition compares two definitions as the store would write them, so
// a value's representation (int vs int64) never reads as a change.
func sameDefinition(have any, want *omap.Map) (bool, error) {
	a, err := canon.DumpRT(have)
	if err != nil {
		return false, err
	}
	b, err := canon.DumpRT(want)
	if err != nil {
		return false, err
	}
	return a == b, nil
}

func path(root string) string { return filepath.Join(root, filepath.FromSlash(File)) }

func invalidStore(reason string) *errs.Located {
	return errs.New("invalid_saved_query", fmt.Sprintf(
		"%s is not a saved-query file: %s. Fix or remove the file by hand", File, reason))
}

func invalidDefinition(name, key, want string) *errs.Located {
	return &errs.Located{
		Code:    "invalid_saved_query",
		Message: fmt.Sprintf("Saved query '%s' in %s: '%s' must be %s", name, File, key, want),
		Field:   key,
	}
}

// unknownQuery lists the saved names in file order, and the nearest one.
func unknownQuery(name string, saved []string) *errs.Located {
	if len(saved) == 0 {
		return errs.New("unknown_saved_query", fmt.Sprintf(
			"No saved query '%s': %s holds none. Save one with `khub query … --save-as %s`", name, File, name))
	}
	return &errs.Located{
		Code: "unknown_saved_query",
		Message: fmt.Sprintf("No saved query '%s' in %s. Saved queries: %s",
			name, File, strings.Join(saved, ", ")),
		Expected:   append([]string(nil), saved...),
		Suggestion: errs.Nearest(name, saved),
	}
}

// Outdated turns a refusal from running a saved definition into one that
// names the saved query and the fix, when the refusal is the schema no longer
// declaring a name the definition uses (a dropped type, field or predicate).
// The code and the DTO keys are kept, so a caller matching on them sees what
// `khub query` would report. Any other error passes through unchanged.
func Outdated(name string, err error) error {
	var located *errs.Located
	if !errors.As(err, &located) || !slices.Contains(outdatedCodes, located.Code) {
		return err
	}
	out := *located
	out.Message = fmt.Sprintf("Saved query '%s' no longer fits the schema: %s. Save it again with "+
		"`khub query … --save-as %s --replace`, or edit %s", name, located.Message, name, File)
	return &out
}

// outdatedCodes are the refusals a schema change can bring to a definition
// that ran when it was saved.
var outdatedCodes = []string{"unknown_type", "filter_error", "unknown_predicate"}
