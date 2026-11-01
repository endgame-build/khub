package fsio

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOverlap(t *testing.T) {
	base := t.TempDir()
	hub := filepath.Join(base, "hub")
	if err := os.MkdirAll(filepath.Join(hub, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		b    string
		want bool
	}{
		{"same", hub, true},
		{"inside", filepath.Join(hub, "inner"), true},
		{"inside and not there yet", filepath.Join(hub, "new", "out"), true},
		{"holds it", base, true},
		{"sibling", filepath.Join(base, "other"), false},
		{"sibling not there yet", filepath.Join(base, "out"), false},
		{"sibling sharing a name prefix", filepath.Join(base, "hub-out"), false},
	}
	for _, c := range cases {
		got, err := Overlap(hub, c.b)
		if err != nil || got != c.want {
			t.Errorf("%s: Overlap(hub, %s) = %v, %v; want %v", c.name, c.b, got, err, c.want)
		}
	}
}

// On a case-insensitive volume (macOS's default APFS, Windows' NTFS) a path
// spelled in another case is the same directory.
func TestOverlapFoldsCaseWhereTheVolumeDoes(t *testing.T) {
	base := t.TempDir()
	hub := filepath.Join(base, "hub")
	if err := os.Mkdir(hub, 0o755); err != nil {
		t.Fatal(err)
	}
	upper := filepath.Join(base, "HUB")
	if _, err := os.Stat(upper); err != nil {
		t.Skip("case-sensitive volume")
	}
	for _, b := range []string{upper, filepath.Join(upper, "out"), filepath.Join(strings.ToUpper(base), "HUB", "out")} {
		if got, err := Overlap(hub, b); err != nil || !got {
			t.Errorf("Overlap(hub, %s) = %v, %v; want true", b, got, err)
		}
		if got, err := Overlap(b, hub); err != nil || !got {
			t.Errorf("Overlap(%s, hub) = %v, %v; want true", b, got, err)
		}
	}
}

func TestOverlapThroughALink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base := t.TempDir()
	hub := filepath.Join(base, "hub")
	if err := os.Mkdir(hub, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(hub, link); err != nil {
		t.Fatal(err)
	}
	if got, err := Overlap(hub, filepath.Join(link, "out")); err != nil || !got {
		t.Errorf("Overlap through a link = %v, %v; want true", got, err)
	}
}
