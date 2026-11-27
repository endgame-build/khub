package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
)

// `conform -` reads stdin under the cap a file read takes, and names the
// whole size when it refuses.
func TestReadStdinTextCapsTheEnvelope(t *testing.T) {
	withStdin := func(t *testing.T, text string) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "stdin")
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		saved := os.Stdin
		os.Stdin = f
		t.Cleanup(func() { os.Stdin = saved; _ = f.Close() })
	}

	withStdin(t, "{\"entities\": []}\r\n")
	raw, err := readStdinText()
	if err != nil || string(raw) != "{\"entities\": []}\n" {
		t.Fatalf("read = %q, %v", raw, err)
	}

	size := canon.MaxTextBytes + 10
	withStdin(t, strings.Repeat(" ", size))
	_, err = readStdinText()
	var located *errs.Located
	if !errors.As(err, &located) || located.Code != "malformed_entity" {
		t.Fatalf("want malformed_entity, got %v", err)
	}
	if want := "stdin is 4194314 bytes"; !strings.Contains(located.Message, want) {
		t.Errorf("message = %s, want it to name %q", located.Message, want)
	}
}
