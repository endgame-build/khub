package cli

import "testing"

func TestCygwinPtyNameMatchesOnlyAPtyPipe(t *testing.T) {
	for name, want := range map[string]bool{
		`\msys-dd50a72ab4668b33-pty0-to-master`:                      true,
		`\msys-dd50a72ab4668b33-pty0-from-master`:                    true,
		`\cygwin-e022582115c10879-pty4-from-master`:                  true,
		`\cygwin-E022582115C10879-pty12-to-master`:                   true,
		`\Device\NamedPipe\msys-dd50a72ab4668b33-pty0-to-master`:     true,
		`\Device\NamedPipe\cygwin-e022582115c10879-pty1-from-master`: true,
		`\msys-dd50a72ab4668b33-pty0-to-master-nat`:                  true,
		``:                                            false,
		`\msys-dd50a72ab4668b33-pty-to-master`:        false, // no pty number
		`\msys-zz50a72ab4668b33-pty0-to-master`:       false, // key not hex
		`\msys--pty0-to-master`:                       false, // no key
		`\msys-dd50a72ab4668b33-pty0-to-slave`:        false,
		`\msys-dd50a72ab4668b33-pty0-sideways-master`: false,
		`\mingw-dd50a72ab4668b33-pty0-to-master`:      false,
		`msys-dd50a72ab4668b33-pty0-to-master`:        false, // no leading separator
		`\Device\NamedPipe\anonymous-pipe-1234`:       false,
		`\x\msys-dd50a72ab4668b33-pty0-to-master`:     false,
	} {
		if got := isCygwinPtyName(name); got != want {
			t.Errorf("isCygwinPtyName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestWindowsCodesMapOntoThePinnedErrnos(t *testing.T) {
	// The mappings the os_error and internal_path_error envelopes depend on.
	for code, want := range map[uint32]posixErrno{
		2:   posixENOENT,       // ERROR_FILE_NOT_FOUND
		3:   posixENOENT,       // ERROR_PATH_NOT_FOUND
		5:   posixEACCES,       // ERROR_ACCESS_DENIED
		80:  posixEEXIST,       // ERROR_FILE_EXISTS
		183: posixEEXIST,       // ERROR_ALREADY_EXISTS
		267: posixENOTDIR,      // ERROR_DIRECTORY
		206: posixENAMETOOLONG, // ERROR_FILENAME_EXCED_RANGE
		112: posixENOSPC,       // ERROR_DISK_FULL
		109: posixEPIPE,        // ERROR_BROKEN_PIPE
		232: posixEPIPE,        // ERROR_NO_DATA
	} {
		if got, ok := windowsToPOSIX[code]; !ok || got != want {
			t.Errorf("windowsToPOSIX[%d] = %v (present %v), want %v", code, got, ok, want)
		}
	}
	if _, ok := windowsToPOSIX[1]; ok {
		t.Error("ERROR_INVALID_FUNCTION is mapped; it means nothing a POSIX errno says")
	}
}

func TestSlashedFileURI(t *testing.T) {
	for _, c := range []struct{ in, goos, want string }{
		{"/Work/x/viz.html", "darwin", "file:///Work/x/viz.html"},
		{"/home/x/viz.html", "linux", "file:///home/x/viz.html"},
		{"C:/Work/x/viz.html", "windows", "file:///C:/Work/x/viz.html"},
		{"//host/share/viz.html", "windows", "file:////host/share/viz.html"},
		{"rel/viz.html", "darwin", "file://rel/viz.html"}, // Abs failed: unchanged off Windows
	} {
		if got := slashedFileURI(c.in, c.goos); got != c.want {
			t.Errorf("slashedFileURI(%q, %q) = %q, want %q", c.in, c.goos, got, c.want)
		}
	}
}

func TestErrnoClassName(t *testing.T) {
	for n, want := range map[int]string{
		2: "FileNotFoundError", 13: "PermissionError", 17: "FileExistsError",
		66: "FileExistsError", 20: "OSError", 28: "OSError", 0: "OSError",
	} {
		if got := errnoClassName(n); got != want {
			t.Errorf("errnoClassName(%d) = %q, want %q", n, got, want)
		}
	}
}

// A Windows build tells a path-construction bug by these three errnos, so
// they must be the numbers the darwin-pinned internal_path_error prints.
func TestPathBugErrnosAreTheDarwinOnes(t *testing.T) {
	for _, c := range []struct {
		got  posixErrno
		want int
	}{{posixENOTDIR, 20}, {posixEISDIR, 21}, {posixENAMETOOLONG, 63}} {
		if c.got.num != c.want {
			t.Errorf("%q = %d, want %d", c.got.text, c.got.num, c.want)
		}
	}
}
