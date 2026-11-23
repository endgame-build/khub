package fsio

import (
	"os"
	"path/filepath"
	"strings"
)

// Overlap reports whether a and b are one directory or either holds the
// other. Each is compared where it lands: made absolute, with every symlink in
// its existing part resolved, so a path named through a link is caught. The
// comparison asks the filesystem, not the spelling: each existing directory
// above a path is matched to the other with os.SameFile, so `/X/HUB/out`
// overlaps `/x/hub` on a case-insensitive volume.
func Overlap(a, b string) (bool, error) {
	ra, err := realPath(a)
	if err != nil {
		return false, err
	}
	rb, err := realPath(b)
	if err != nil {
		return false, err
	}
	return within(ra, rb) || within(rb, ra), nil
}

// within reports whether p is dir or lies under it: by spelling, or by an
// existing ancestor of p, p included, that is the same directory as dir.
func within(p, dir string) bool {
	if p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator)) {
		return true
	}
	target, err := os.Stat(dir)
	if err != nil {
		return false // a directory that does not exist holds nothing
	}
	for cur := p; ; cur = filepath.Dir(cur) {
		if info, err := os.Stat(cur); err == nil && os.SameFile(info, target) {
			return true
		}
		if filepath.Dir(cur) == cur {
			return false
		}
	}
}

// realPath is p made absolute with the symlinks in its existing part resolved;
// the part that does not exist yet is joined back on unchanged.
func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rest := ""
	for cur := abs; ; cur = filepath.Dir(cur) {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest), nil
		}
		if filepath.Dir(cur) == cur {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
	}
}
