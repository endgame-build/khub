package gitlog

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/errs"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// A signed commit touches only the named file. The preflight refuses without
// a usable signing key (a dry signing probe), and for a file with uncommitted
// changes, so nothing is ever committed unsigned or swept in.
func TestCommitSignedTouchesOnlyItsFile(t *testing.T) {
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("ssh-keygen not available")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "config", "user.name", "Test")
	gitIn(t, dir, "config", "user.email", "test@example.com")

	for _, f := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(f+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var le *errs.Located
	if err := CommitPreflight(dir, "a.md", false); !errors.As(err, &le) || le.Code != "commit_refused" ||
		!strings.Contains(le.Message, "no user.signingkey") {
		t.Fatalf("preflight without a key = %v", err)
	}

	key := filepath.Join(t.TempDir(), "key")
	gitIn(t, dir, "config", "gpg.format", "ssh")
	gitIn(t, dir, "config", "user.signingkey", key+".pub")
	if err := CommitPreflight(dir, "a.md", false); !errors.As(err, &le) || le.Code != "commit_refused" ||
		!strings.Contains(le.Message, "could not sign") {
		t.Fatalf("preflight with a missing key file = %v", err)
	}
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
	if err := CommitPreflight(dir, "a.md", false); err != nil {
		t.Fatal(err)
	}
	if err := CommitPreflight(dir, "a.md", true); err == nil {
		t.Error("a collection file with uncommitted content passed the preflight")
	}
	sha, err := CommitSigned(dir, "a.md", "verify note/a by human:test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gitIn(t, dir, "cat-file", "commit", sha), "gpgsig") {
		t.Error("the commit carries no signature")
	}
	if files := strings.TrimSpace(gitIn(t, dir, "show", "--name-only", "--format=", sha)); files != "a.md" {
		t.Errorf("commit touched %q, want a.md alone", files)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CommitPreflight(dir, "a.md", false); !errors.As(err, &le) || !strings.Contains(le.Message, "uncommitted") {
		t.Errorf("preflight on an edited tracked file = %v", err)
	}
}
