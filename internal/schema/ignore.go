package schema

// The workspace's path-exclusion list, `.khub/ignore`: files no scan reads,
// so an example corpus or test fixtures that claim a type stay out of the
// graph — never an entity, a stray or a misplaced finding.
//
// One pattern per line, a subset of gitignore:
//
//	docs/examples/        a directory, anchored at the workspace root
//	/notes.md             a file at the root
//	fixtures/             a pattern with no inner slash matches a name at any depth
//	platform/**/phase8/   `**` spans any number of directories
//	*.draft.md            `*`, `?` and `[…]` match within one name
//
// A trailing `/` matches directories only. A matched directory excludes
// everything below it. Blank lines and `#` comments are skipped. Negation
// (`!`) is refused rather than half-supported.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
)

// IgnoreFile is the exclusion list's workspace-relative path.
const IgnoreFile = ".khub/ignore"

// IgnoreList is a parsed `.khub/ignore`. A nil list ignores nothing.
type IgnoreList struct {
	pats []ignorePat
}

type ignorePat struct {
	segs     []string
	anywhere bool // no inner slash: matches a name at any depth
	dirOnly  bool
}

// LoadIgnore reads root's `.khub/ignore`; a missing file is an empty list.
func LoadIgnore(root string) (*IgnoreList, error) {
	raw, err := fsio.ReadFile(root, filepath.Join(root, filepath.FromSlash(IgnoreFile)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseIgnore(string(raw))
}

// ParseIgnore parses the text of an exclusion list.
func ParseIgnore(text string) (*IgnoreList, error) {
	l := &IgnoreList{}
	for n, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		bad := func(why string) error {
			return errs.New("schema_error", fmt.Sprintf("Cannot read %s line %d ('%s'): %s",
				IgnoreFile, n+1, line, why))
		}
		if strings.HasPrefix(line, "!") {
			return nil, bad("negation is not supported")
		}
		p := ignorePat{}
		body := strings.TrimSuffix(line, "/")
		p.dirOnly = body != line
		anchored := strings.HasPrefix(body, "/")
		body = strings.TrimPrefix(body, "/")
		if body == "" {
			return nil, bad("the pattern names nothing")
		}
		p.anywhere = !anchored && !strings.Contains(body, "/")
		p.segs = strings.Split(body, "/")
		for _, s := range p.segs {
			if s == "" || s == "." || s == ".." {
				return nil, bad("empty, '.' or '..' segment")
			}
			if _, err := path.Match(s, ""); err != nil {
				return nil, bad("malformed glob")
			}
		}
		l.pats = append(l.pats, p)
	}
	return l, nil
}

// Ignored reports whether rel (workspace-relative; isDir when rel itself is a
// directory) is excluded — by a pattern matching it or any directory above it.
func (l *IgnoreList) Ignored(rel string, isDir bool) bool {
	if l == nil || len(l.pats) == 0 {
		return false
	}
	segs := strings.Split(filepath.ToSlash(filepath.Clean(rel)), "/")
	for i := 1; i <= len(segs); i++ {
		dir := i < len(segs) || isDir
		for _, p := range l.pats {
			if p.dirOnly && !dir {
				continue
			}
			if p.anywhere {
				if ok, _ := path.Match(p.segs[0], segs[i-1]); ok {
					return true
				}
				continue
			}
			if matchSegs(p.segs, segs[:i]) {
				return true
			}
		}
	}
	return false
}

// IgnoredPath is Ignored for an absolute or root-joined path, deciding
// directory-ness from the filesystem.
func (l *IgnoreList) IgnoredPath(root, p string) bool {
	if l == nil {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	info, err := os.Lstat(p)
	return l.Ignored(rel, err == nil && info.IsDir())
}

// matchSegs matches pattern segments against path segments, `**` spanning
// zero or more of them.
func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for k := 0; k <= len(segs); k++ {
			if matchSegs(pat[1:], segs[k:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], segs[0]); !ok {
		return false
	}
	return matchSegs(pat[1:], segs[1:])
}
