package workspace

import "testing"

// The real filepath.VolumeName behind pathVolume: a drive or UNC path is
// absolute, so init's target keeps its volume instead of reading as relative.
func TestPathHelpersRealVolumes(t *testing.T) {
	cases := map[string][3]string{ // path -> {pyPath, pyParent, pyName}
		"C:/Work/me/ws/":     {"C:/Work/me/ws", "C:/Work/me", "ws"},
		"C:/":                {"C:/", "C:/", ""},
		"//srv/share/ws/sub": {"//srv/share/ws/sub", "//srv/share/ws", "sub"},
		"rel/ws":             {"rel/ws", "rel", "ws"},
	}
	for in, want := range cases {
		got := [3]string{pyPath(in), pyParent(in), pyName(in)}
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if got := pyJoin("C:/ws", ".khub", "config.yaml"); got != "C:/ws/.khub/config.yaml" {
		t.Errorf("pyJoin = %q", got)
	}
	if got := pyParts("C:/ws/node_modules"); len(got) != 2 || got[0] != "ws" {
		t.Errorf("pyParts = %q", got)
	}
}
