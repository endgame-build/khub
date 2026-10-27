package schema

// Nested storage for a `file` type: a path template instead of one flat
// directory. `work/{epic}/{slug}.md` keeps a story beside its epic, and
// `spokes/{slug}/spec.md` names a spec after the directory it sits in.
//
// A template is literal directories, `{name}` directory segments, and a leaf
// naming the file: `{name}.<fmt>` or a literal file name. `{slug}` is the
// entity's slug and must appear at least once; every other name is a field of
// the entity, which `add` reads to fill the segment and `check` compares with
// the directory the file sits in. A template that ends in a directory gets
// `{slug}.<fmt>` appended.
//
// Templates may overlap one another, because a templated type claims a file
// only when its frontmatter `type` names that type: an epic and its stories
// share `work/<EP>/`. A template may not overlap a plain layout, which claims
// every file in its directory whatever the file says.

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/endgame-build/khub/internal/fsio"
)

// SlugField is the placeholder that carries the entity's slug.
const SlugField = "slug"

var placeholderRE = regexp.MustCompile(`^\{([a-z_][a-z0-9_-]*)\}(\.[a-z0-9]+)?$`)

// IsPathTemplate reports whether a storage path is a template.
func IsPathTemplate(p string) bool { return strings.ContainsAny(p, "{}") }

// PathTemplate is one parsed storage path template.
type PathTemplate struct {
	Raw  string // as authored
	segs []tseg
}

// tseg is one segment: a literal, or a placeholder with an optional leaf
// extension (`{slug}.md` carries ext ".md").
type tseg struct {
	lit  string
	name string
	ext  string
}

func (s tseg) placeholder() bool { return s.name != "" }

// ParseTemplate parses a template for a type stored as format.
func ParseTemplate(raw, format string) (*PathTemplate, error) {
	text := strings.TrimSuffix(raw, "/")
	if text == "" || strings.HasPrefix(text, "/") {
		return nil, fmt.Errorf("path template '%s' must be workspace-relative", raw)
	}
	parts := strings.Split(text, "/")
	last := parts[len(parts)-1]
	if !strings.Contains(last, ".") || strings.HasPrefix(last, ".") {
		parts = append(parts, "{"+SlugField+"}."+format)
	}
	t := &PathTemplate{Raw: raw}
	slugSeen := false
	for i, part := range parts {
		leaf := i == len(parts)-1
		if part == "" || part == "." || part == ".." || strings.HasPrefix(part, ".") {
			return nil, fmt.Errorf("path template '%s' has an empty, dot or hidden segment '%s'", raw, part)
		}
		if !strings.ContainsAny(part, "{}") {
			if leaf && !strings.HasSuffix(part, "."+format) {
				return nil, fmt.Errorf("path template '%s' names a file that is not .%s", raw, format)
			}
			t.segs = append(t.segs, tseg{lit: part})
			continue
		}
		m := placeholderRE.FindStringSubmatch(part)
		if m == nil {
			return nil, fmt.Errorf("path template '%s': segment '%s' must be a whole {name} "+
				"(or {name}.%s as the file name)", raw, part, format)
		}
		if i == 0 {
			return nil, fmt.Errorf("path template '%s' must start with a literal directory", raw)
		}
		if leaf != (m[2] != "") || (leaf && m[2] != "."+format) {
			return nil, fmt.Errorf("path template '%s': only the file name carries an extension, and it is .%s",
				raw, format)
		}
		slugSeen = slugSeen || m[1] == SlugField
		t.segs = append(t.segs, tseg{name: m[1], ext: m[2]})
	}
	if !slugSeen {
		return nil, fmt.Errorf("path template '%s' must place {%s}", raw, SlugField)
	}
	return t, nil
}

// literalTemplate is a template with no placeholders: a plain layout's
// exact file, used only to test overlap.
func literalTemplate(path string) *PathTemplate {
	t := &PathTemplate{Raw: path}
	for _, part := range strings.Split(path, "/") {
		t.segs = append(t.segs, tseg{lit: part})
	}
	return t
}

// String renders the template with its implied leaf.
func (t *PathTemplate) String() string {
	parts := make([]string, len(t.segs))
	for i, s := range t.segs {
		if s.placeholder() {
			parts[i] = "{" + s.name + "}" + s.ext
		} else {
			parts[i] = s.lit
		}
	}
	return strings.Join(parts, "/")
}

// Prefix is the literal directories before the first placeholder — the tree
// the template's files live under.
func (t *PathTemplate) Prefix() string {
	var parts []string
	for _, s := range t.segs[:len(t.segs)-1] {
		if s.placeholder() {
			break
		}
		parts = append(parts, s.lit)
	}
	return strings.Join(parts, "/")
}

// Fields is the template's field placeholders, every name but {slug}, first
// occurrence first.
func (t *PathTemplate) Fields() []string {
	var out []string
	for _, s := range t.segs {
		if s.placeholder() && s.name != SlugField && !containsName(out, s.name) {
			out = append(out, s.name)
		}
	}
	return out
}

func containsName(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// Depth is the number of path segments a matching file has.
func (t *PathTemplate) Depth() int { return len(t.segs) }

// Literal reports whether segment i is literal, and its text.
func (t *PathTemplate) Literal(i int) (string, bool) {
	s := t.segs[i]
	return s.lit, !s.placeholder()
}

// AcceptsName reports whether a directory entry name can stand at segment i.
func (t *PathTemplate) AcceptsName(i int, name string) bool {
	s := t.segs[i]
	if !s.placeholder() {
		return name == s.lit
	}
	return placeholderValue(s, name) != ""
}

// placeholderValue is what a placeholder segment captures from name, "" when
// the name cannot stand there. Hidden and vendored names never match.
func placeholderValue(s tseg, name string) string {
	if name == "" || strings.HasPrefix(name, ".") || fsio.SkipDirs[name] {
		return ""
	}
	if s.ext == "" {
		return name
	}
	stem, ok := strings.CutSuffix(name, s.ext)
	if !ok {
		return ""
	}
	return stem
}

// Match reports whether rel (slash-separated, workspace-relative) is a path
// this template describes, and what each placeholder captured. A name placed
// twice must capture the same value both times.
func (t *PathTemplate) Match(rel string) (map[string]string, bool) {
	parts := strings.Split(rel, "/")
	if len(parts) != len(t.segs) {
		return nil, false
	}
	caps := map[string]string{}
	for i, s := range t.segs {
		if !s.placeholder() {
			if parts[i] != s.lit {
				return nil, false
			}
			continue
		}
		v := placeholderValue(s, parts[i])
		if v == "" {
			return nil, false
		}
		if prev, seen := caps[s.name]; seen && prev != v {
			return nil, false
		}
		caps[s.name] = v
	}
	return caps, true
}

// PathValueError is a placeholder value that cannot be one path segment.
type PathValueError struct{ Field, Value string }

func (e *PathValueError) Error() string {
	return fmt.Sprintf("{%s} = '%s' is not a path segment", e.Field, e.Value)
}

// Fill renders the path for slug, taking every other placeholder from field.
// missing lists the fields field could not supply; err is set when a value
// cannot stand as one directory name.
func (t *PathTemplate) Fill(slug string, field func(string) (string, bool)) (string, []string, error) {
	parts := make([]string, len(t.segs))
	var missing []string
	for i, s := range t.segs {
		if !s.placeholder() {
			parts[i] = s.lit
			continue
		}
		v := slug
		if s.name != SlugField {
			got, ok := field(s.name)
			if !ok {
				if !containsName(missing, s.name) {
					missing = append(missing, s.name)
				}
				continue
			}
			v = got
		}
		if v == "" || v == "." || v == ".." || strings.HasPrefix(v, ".") ||
			strings.ContainsAny(v, `/\`) || fsio.SkipDirs[v] || ReservedDeviceName(v) {
			return "", nil, &PathValueError{Field: s.name, Value: v}
		}
		parts[i] = v + s.ext
	}
	if len(missing) > 0 {
		return "", missing, nil
	}
	return strings.Join(parts, "/"), nil, nil
}

// intersects reports whether some path matches both templates.
func (t *PathTemplate) intersects(o *PathTemplate) bool {
	if len(t.segs) != len(o.segs) {
		return false
	}
	for i := range t.segs {
		if !segsIntersect(t.segs[i], o.segs[i]) {
			return false
		}
	}
	return true
}

func segsIntersect(a, b tseg) bool {
	switch {
	case !a.placeholder() && !b.placeholder():
		return a.lit == b.lit
	case !a.placeholder():
		return placeholderValue(b, a.lit) != ""
	case !b.placeholder():
		return placeholderValue(a, b.lit) != ""
	default:
		return a.ext == "" || b.ext == "" || a.ext == b.ext
	}
}
