package gitlog

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
)

// emptyTree is git's well-known empty tree, present in every repository.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// CommitPreflight refuses a signed commit of relpath that could not be made
// cleanly, before anything is written: root must be in a git work tree that
// holds relpath, the file must carry no uncommitted change (a collection's
// file holds every row, so any change at all), and git must be able to sign,
// proven by signing a throwaway commit object that no ref points at.
func CommitPreflight(root, relpath string, collection bool) error {
	top, code, err := runGit(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	if code != 0 {
		return errs.CommitRefused("the workspace is not inside a git work tree")
	}
	if !inside(strings.TrimSpace(top), filepath.Join(root, relpath)) {
		return errs.CommitRefused(relpath + " is outside the git work tree")
	}
	status, _, err := runGit(root, "status", "--porcelain", "--", relpath)
	if err != nil {
		return err
	}
	if s := strings.TrimSpace(status); s != "" && (collection || !strings.HasPrefix(s, "??")) {
		return errs.CommitRefused(relpath + " has uncommitted changes, which the commit would sweep in; " +
			"commit or discard them first")
	}
	if key, _, err := runGit(root, "config", "user.signingkey"); err != nil {
		return err
	} else if strings.TrimSpace(key) == "" {
		return errs.CommitRefused("git has no user.signingkey, so the commit could not be signed; " +
			"configure signing (SSH or GPG) or drop --commit")
	}
	if out, code, err := runGit(root, "commit-tree", "-S", "-m", "khub signing probe", emptyTree); err != nil {
		return err
	} else if code != 0 {
		return errs.CommitRefused("git could not sign a commit (" + strings.TrimSpace(out) + "); " +
			"configure signing (SSH or GPG) or drop --commit")
	}
	return nil
}

// CommitSigned commits relpath alone, signed (`-S`, never unsigned), and
// returns the new commit's hash. A failure unstages the file again, so the
// caller can restore it and leave the work tree as it found it.
func CommitSigned(root, relpath, message string) (string, error) {
	if out, code, err := runGit(root, "add", "--", relpath); err != nil {
		return "", err
	} else if code != 0 {
		return "", errs.CommitRefused("git add failed: " + strings.TrimSpace(out))
	}
	out, code, err := runGit(root, "commit", "-S", "-q", "-m", message, "--", relpath)
	if err == nil && code == 0 {
		sha, _, err := runGit(root, "rev-parse", "HEAD")
		return strings.TrimSpace(sha), err
	}
	if _, _, rerr := runGit(root, "reset", "-q", "--", relpath); rerr != nil {
		return "", rerr
	}
	if err != nil {
		return "", err
	}
	return "", errs.CommitRefused("git commit failed: " + strings.TrimSpace(out))
}

// runGit runs git with stderr folded into the output and nothing to prompt
// from: stdin is empty and GIT_TERMINAL_PROMPT=0, so khub never waits on a
// question. err is non-nil only when git could not start.
func runGit(root string, args ...string) (string, int, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return "", 0, err
		}
		return string(out), exit.ExitCode(), nil
	}
	return string(out), 0, nil
}

// inside reports whether path lies under top, both resolved through symlinks.
func inside(top, path string) bool {
	t, err := filepath.EvalSymlinks(top)
	if err != nil {
		return false
	}
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(t, p)
	return err == nil && filepath.IsLocal(rel)
}
