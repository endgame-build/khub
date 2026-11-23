package project

import (
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/wire"
)

var (
	headingLine = regexp.MustCompile(`^#{1,6}(\s|$)`)
	listLine    = regexp.MustCompile(`^\s*[*+-]\s`)
	logDate     = regexp.MustCompile(`^##\s+\d{4}-\d{2}-\d{2}\s*$`)
)

// OKFNonconformant lists the Markdown files that keep the workspace from being
// an OKF 0.2 bundle (SPEC §11), as sorted workspace-relative slash paths.
//
// The tree is index.WalkTree's, the one `check`'s misplaced walk reads, minus
// the root agent files `khub wire` writes (wire.Files), which are khub's own
// wiring like .khub/. Nothing else is exempt, so a README.md without
// frontmatter is listed. A concept needs parseable frontmatter with a
// non-empty string `type`; an index.md follows §8 and a log.md §9. A file too
// large to read is listed too. Broken links never count (§11).
func OKFNonconformant(root string, ignore *schema.IgnoreList) ([]string, error) {
	var out []string
	err := index.WalkTree(root, ignore, func(path string) error {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".md") || wiring(rel) {
			return nil
		}
		text, err := canon.ReadTextIn(root, path)
		var le *errs.Located
		if errors.As(err, &le) && le.Code == "malformed_entity" {
			out = append(out, rel)
			return nil
		}
		if err != nil {
			return err
		}
		if !okfConformant(rel, text) {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// wiring reports whether rel is a root agent file `khub wire` writes.
func wiring(rel string) bool {
	for _, f := range wire.Files {
		if rel == f.Name {
			return true
		}
	}
	return false
}

// okfConformant judges one file by its reserved name, or as a concept.
func okfConformant(rel, text string) bool {
	meta, body, err := canon.Parse(text, "md")
	if err != nil {
		return false
	}
	_, _, fenced := canon.ParseFrontmatter(text)
	switch filepath.Base(rel) {
	case "index.md":
		// Only the bundle root's index may carry frontmatter, and only
		// okf_version (§8, §12).
		if fenced && rel != "index.md" {
			return false
		}
		for _, key := range meta.Keys() {
			if key != "okf_version" {
				return false
			}
		}
		for _, line := range strings.Split(body, "\n") {
			if strings.TrimSpace(line) != "" && !headingLine.MatchString(line) && !listLine.MatchString(line) {
				return false
			}
		}
		return true
	case "log.md":
		if fenced {
			return false
		}
		for _, line := range strings.Split(body, "\n") {
			if strings.HasPrefix(line, "## ") && !logDate.MatchString(line) {
				return false
			}
		}
		return true
	}
	t, _ := meta.Get("type")
	s, ok := t.(string)
	return ok && strings.TrimSpace(s) != ""
}
