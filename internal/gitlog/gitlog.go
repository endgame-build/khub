// Package gitlog ports src/khub/core/gitlog.py — the git-derived reads behind
// `khub stale`, plus the two commit-date helpers `backfill` shares (FS-005).
// Every git call is read-only: a date is derived from history, never written
// back (INT-008).
//
// gitlog.go carries the git half (gitlog.py:38-87); stale.go carries the
// `stale` report itself.
package gitlog

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// run is gitlog._git: `git -C <root> <args>` with text capture, never failing
// on a non-zero exit (Python's check=False). The returned code is the
// process's returncode.
//
// Python builds no env, so subprocess.run inherits os.environ untouched — no
// scrub, no locale pin, no GIT_* suppression. exec.Cmd with a nil Env inherits
// the same way, so the port is the absence of code, deliberately.
//
// err is non-nil only when the git binary could not be started at all, which
// is where Python raises FileNotFoundError (an OSError, rendered by the CLI
// boundary as code os_error).
func run(root string, args ...string) (stdout string, code int, err error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	runErr := cmd.Run()
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return "", 0, runErr // could not spawn git at all
		}
		return out.String(), exit.ExitCode(), nil
	}
	return out.String(), 0, nil
}

// HasGitHistory is gitlog.has_git_history: whether root is a git repo with at
// least one commit. An unborn HEAD reads the same as a non-repo — neither has
// history to derive a date from.
func HasGitHistory(root string) (bool, error) {
	_, code, err := run(root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return false, err
	}
	return code == 0, nil
}

// LastCommitDate is gitlog.last_commit_date: the committer date of the last
// commit touching relpath; ok=false when the path is untracked (Python's
// None). Committer date, not author date, so one commit reads the same here
// and anywhere else khub renders history.
func LastCommitDate(root, relpath string) (d time.Time, ok bool, err error) {
	stdout, code, err := run(root, "log", "-1", "--format=%cd", "--date=short", "--", relpath)
	if err != nil {
		return time.Time{}, false, err
	}
	out := strings.TrimSpace(stdout)
	if code != 0 || out == "" {
		return time.Time{}, false, nil
	}
	d, err = parseShortDate(out)
	if err != nil {
		return time.Time{}, false, err
	}
	return d, true, nil
}

// FirstCommitDate is gitlog.first_commit_date: the committer date of the FIRST
// commit touching relpath, which `backfill` derives `created` from. git log
// lists newest-first, so the oldest commit is the last line.
//
// ponytail (carried from the Python): lines[-1] is the oldest commit on a
// linear history. A merge with non-monotonic committer dates could reorder the
// tail; lift to `git log --diff-filter=A` if a merge-heavy history mis-dates
// `created`.
func FirstCommitDate(root, relpath string) (d time.Time, ok bool, err error) {
	stdout, code, err := run(root, "log", "--format=%cd", "--date=short", "--", relpath)
	if err != nil {
		return time.Time{}, false, err
	}
	lines := splitLines(strings.TrimSpace(stdout))
	if code != 0 || len(lines) == 0 {
		return time.Time{}, false, nil
	}
	d, err = parseShortDate(lines[len(lines)-1])
	if err != nil {
		return time.Time{}, false, err
	}
	return d, true, nil
}

// splitLines is str.splitlines() over already-stripped git output: the argv
// pins --date=short, so the only line boundary that can occur is \n (\r\n is
// tolerated for a core.autocrlf checkout of a hook-rewritten stream).
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	for i, p := range parts {
		parts[i] = strings.TrimSuffix(p, "\r")
	}
	return parts
}

// parseShortDate is date.fromisoformat over `--date=short` output. The format
// is pinned by the argv, so only YYYY-MM-DD can arrive; anything else is the
// ValueError Python would raise, surfaced as an error instead of a panic.
func parseShortDate(s string) (time.Time, error) {
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid isoformat string: %q", s)
	}
	return d, nil
}
