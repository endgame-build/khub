package workspace

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// fakeVolume stands in for filepath.VolumeName on Windows, over slash paths:
// a drive letter, or a //server/share UNC prefix. It lets the volume logic run
// on every platform.
func fakeVolume(p string) string {
	if len(p) >= 2 && p[1] == ':' && (p[0]|0x20 >= 'a' && p[0]|0x20 <= 'z') {
		return p[:2]
	}
	if strings.HasPrefix(p, "//") {
		parts := strings.SplitN(p[2:], "/", 3)
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			return "//" + parts[0] + "/" + parts[1]
		}
	}
	return ""
}

func noVolume(string) string { return "" }

// Off Windows pathVolume is always "", so the helpers must give the
// pre-Windows PurePosixPath answers on every input, unchanged.
func TestPathHelpersPosixUnchanged(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX semantics hold only where filepath.VolumeName is empty")
	}
	cases := map[string][3]string{ // path -> {pyPath, pyParent, pyName}
		"":            {".", ".", ""},
		".":           {".", ".", ""},
		"/":           {"/", "/", ""},
		"a":           {"a", ".", "a"},
		"a/b/":        {"a/b", "a", "b"},
		"./a//b/./c":  {"a/b/c", "a/b", "c"},
		"/a/../b":     {"/a/../b", "/a/..", "b"},
		"/x":          {"/x", "/", "x"},
		"C:/x":        {"C:/x", "C:", "x"}, // a relative path off Windows
		"//srv/share": {"/srv/share", "/srv", "share"},
	}
	for in, want := range cases {
		if v := pathVolume(in); v != "" {
			t.Fatalf("pathVolume(%q) = %q off Windows", in, v)
		}
		got := [3]string{pyPath(in), pyParent(in), pyName(in)}
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
		vless := [3]string{pathPy(in, noVolume), parentPy(in, noVolume), namePy(in, noVolume)}
		if vless != want {
			t.Errorf("%q volume-less: got %q, want %q", in, vless, want)
		}
	}
	if got := pyJoin("/ws", ".khub", "config.yaml"); got != "/ws/.khub/config.yaml" {
		t.Errorf("pyJoin = %q", got)
	}
	if got := pyJoin("/ws", "/abs"); got != "/abs" {
		t.Errorf("pyJoin reset = %q", got)
	}
	if got := pyJoin(".", ".khub"); got != ".khub" {
		t.Errorf("pyJoin dot = %q", got)
	}
}

// The Windows shapes, driven through a stand-in for filepath.VolumeName so
// they run on every platform. pathlib_windows_test.go repeats the core of it
// with the real one.
func TestPathHelpersWindowsVolumes(t *testing.T) {
	cases := map[string][4]string{ // path -> {anchor, pyPath, pyParent, pyName}
		"C:/":                 {"C:/", "C:/", "C:/", ""},
		"C:/x":                {"C:/", "C:/x", "C:/", "x"},
		"c:/Work/me/ws/":      {"c:/", "c:/Work/me/ws", "c:/Work/me", "ws"},
		"C:x":                 {"C:", "C:x", "C:", "x"},
		"//srv/share":         {"//srv/share", "//srv/share", "//srv/share", ""},
		"//srv/share/":        {"//srv/share/", "//srv/share/", "//srv/share/", ""},
		"//srv/share/ws/sub":  {"//srv/share/", "//srv/share/ws/sub", "//srv/share/ws", "sub"},
		"/rooted":             {"/", "/rooted", "/", "rooted"},
		"rel/ws":              {"", "rel/ws", "rel", "ws"},
		"C:/a/./../b":         {"C:/", "C:/a/../b", "C:/a/..", "b"},
		"//srv/share/a//b/./": {"//srv/share/", "//srv/share/a/b", "//srv/share/a", "b"},
	}
	for in, want := range cases {
		anchor, _ := splitAnchor(in, fakeVolume)
		got := [4]string{anchor, pathPy(in, fakeVolume), parentPy(in, fakeVolume), namePy(in, fakeVolume)}
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}

	joins := []struct {
		base string
		rest []string
		want string
	}{
		{"C:/ws", []string{".khub", "config.yaml"}, "C:/ws/.khub/config.yaml"},
		{"C:/ws", []string{"D:/other"}, "D:/other"},
		{"C:/ws", []string{"/rooted"}, "C:/rooted"},
		{"//srv/share/ws", []string{".khub"}, "//srv/share/ws/.khub"},
		{"//srv/share/ws", []string{"/x"}, "//srv/share/x"},
	}
	for _, j := range joins {
		if got := joinPy(fakeVolume, j.base, j.rest...); got != j.want {
			t.Errorf("join(%q, %q) = %q, want %q", j.base, j.rest, got, j.want)
		}
	}

	_, rest := splitAnchor("C:/a/node_modules/b", fakeVolume)
	if got := pySplit(rest); !reflect.DeepEqual(got, []string{"a", "node_modules", "b"}) {
		t.Errorf("parts = %q", got)
	}
}
