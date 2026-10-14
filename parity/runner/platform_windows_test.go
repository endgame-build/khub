//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitBashForm(t *testing.T) {
	for in, want := range map[string]string{
		`C:\Temp\runner\ws`: "/c/Temp/runner/ws",
		`D:/a/khub`:         "/d/a/khub",
		`\\server\share`:    "",
		`ws`:                "",
	} {
		if got := gitBashForm(in); got != want {
			t.Errorf("gitBashForm(%q) = %q want %q", in, got, want)
		}
	}
}

func TestReplacePathCoversWindowsSpellings(t *testing.T) {
	from := `C:\Users\RunnerAdmin\Temp\parity-1\ws`
	for _, in := range []string{
		`C:\Users\RunnerAdmin\Temp\parity-1\ws`,
		`c:\users\runneradmin\temp\parity-1\ws`,
		`C:\\Users\\RunnerAdmin\\Temp\\parity-1\\ws`,
	} {
		if got := replacePath("<"+in+">", from, "%WS%"); got != "<%WS%>" {
			t.Errorf("replacePath(%q) = %q", in, got)
		}
	}
}

// Every spelling the normalizer derives for a real temp directory replaces
// to the placeholder, the 8.3 form included where the volume keeps one.
func TestPathFormsOfTempDir(t *testing.T) {
	dir := t.TempDir()
	n := &pathNormalizer{}
	for _, f := range pathForms(dir) {
		n.pairs = append(n.pairs, [2]string{f, "%WS%"})
	}
	long := canonicalTemp(dir)
	for _, in := range []string{dir, long, filepath.ToSlash(long), gitBashForm(long), shortPath(long),
		strings.ReplaceAll(long, `\`, `\\`)} {
		if in == "" {
			continue
		}
		if got := string(n.apply([]byte(in + `\x`))); !strings.HasPrefix(got, "%WS%") {
			t.Errorf("%q normalized to %q", in, got)
		}
	}
	if _, err := os.Stat(long); err != nil {
		t.Fatalf("canonicalTemp gave a path that does not exist: %v", err)
	}
}
