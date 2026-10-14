//go:build windows

package main

// platform_windows.go is the Windows side of the runner's platform seams.
// None of it can change a unix run: platform_unix.go keeps the old behaviour.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

// shScript picks the script a step runs: `sh_windows` replaces `sh` here.
func shScript(st step) string {
	if st.ShWindows != "" {
		return st.ShWindows
	}
	return st.Sh
}

var gitBash struct {
	once sync.Once
	path string
	err  error
}

// gitBashSh finds the sh that setup scripts run under: Git for Windows'
// bin\sh.exe, which GitHub's Windows runners put on PATH. A bash.exe under
// the Windows directory is the WSL launcher, a different system with its own
// filesystem, and is never taken.
func gitBashSh() (string, error) {
	gitBash.once.Do(func() {
		winDir := strings.ToLower(os.Getenv("SYSTEMROOT"))
		for _, name := range []string{"sh", "bash"} {
			p, err := exec.LookPath(name)
			if err != nil {
				continue
			}
			if winDir != "" && strings.HasPrefix(strings.ToLower(p), winDir) {
				continue
			}
			gitBash.path = p
			return
		}
		fallback := filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "sh.exe")
		if _, err := os.Stat(fallback); err == nil {
			gitBash.path = fallback
			return
		}
		gitBash.err = errors.New(`sh steps need Git Bash: no sh or bash on PATH outside the Windows directory, and no %ProgramFiles%\Git\bin\sh.exe; install Git for Windows or put its bin directory on PATH`)
	})
	return gitBash.path, gitBash.err
}

// shCommand runs a setup script under Git Bash. The script goes through a
// file, not `-c`: the MSYS runtime re-parses a Windows command line with its
// own quoting and globbing rules, and the scripts are full of quotes,
// backslashes and printf escapes. A file reaches sh byte for byte. It is
// written beside the workspace, never inside it, so the tree manifest never
// sees it.
func shCommand(script, work string, idx int) (*exec.Cmd, error) {
	sh, err := gitBashSh()
	if err != nil {
		return nil, err
	}
	f := filepath.Join(work, fmt.Sprintf(".sh-step-%02d.sh", idx))
	if err := os.WriteFile(f, []byte(script+"\n"), 0o644); err != nil {
		return nil, err
	}
	return exec.Command(sh, filepath.ToSlash(f)), nil
}

// platformEnv completes baseEnv for Windows.
//
//   - USERPROFILE is where Go's os.UserHomeDir looks on Windows; it points at
//     the same scratch home as HOME, so nothing reaches the runner's profile.
//   - The pass-through variables are the ones a Windows process cannot do
//     without: SYSTEMROOT (Winsock, crypto), TEMP/TMP (os.MkdirTemp falls
//     back to the Windows directory without them), COMSPEC and PATHEXT
//     (command lookup in Git Bash and in exec.LookPath).
//   - Git for Windows ships a system config that sets core.autocrlf=true,
//     plus a second one under ProgramData. GIT_CONFIG_SYSTEM=/dev/null in
//     baseEnv should already drop both; GIT_CONFIG_NOSYSTEM says it twice,
//     and core.autocrlf=false is pinned at command level so no config file
//     anywhere can make git rewrite line endings in a fixture workspace.
func platformEnv(home string) []string {
	env := []string{
		"USERPROFILE=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=core.autocrlf",
		"GIT_CONFIG_VALUE_0=false",
	}
	for _, k := range []string{"SYSTEMROOT", "WINDIR", "SYSTEMDRIVE", "COMSPEC", "PATHEXT",
		"TEMP", "TMP", "PROGRAMDATA", "ProgramFiles", "PROCESSOR_ARCHITECTURE"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// canonicalTemp expands an 8.3 temp root (C:\Users\RUNNER~1\…) to its long
// form, so khub works in the directory a user would have.
func canonicalTemp(p string) string {
	if long := longPath(p); long != "" {
		return long
	}
	return p
}

// pathForms lists every spelling of p khub or a tool it runs might print:
// the realpath, the long and the 8.3 form, each with backslashes, with
// forward slashes, and in Git Bash's /c/… form. Longest first, so no
// spelling is cut by a shorter one that prefixes it. Replacement is
// case-insensitive (replacePath), as Windows paths are.
func pathForms(p string) []string {
	var bases []string
	addBase := func(b string) {
		if b != "" && !containsFold(bases, b) {
			bases = append(bases, b)
		}
	}
	if real, err := filepath.EvalSymlinks(p); err == nil {
		addBase(real)
		addBase(longPath(real))
	}
	addBase(longPath(p))
	addBase(p)
	addBase(shortPath(p))

	var forms []string
	addForm := func(f string) {
		if f != "" && !containsFold(forms, f) {
			forms = append(forms, f)
		}
	}
	for _, b := range bases {
		addForm(b)
		addForm(filepath.ToSlash(b))
		addForm(gitBashForm(b))
	}
	sort.SliceStable(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	return forms
}

// gitBashForm spells a drive path the way MSYS prints it: C:\a\b → /c/a/b.
func gitBashForm(p string) string {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return ""
	}
	return "/" + strings.ToLower(p[:1]) + filepath.ToSlash(p[2:])
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// replacePath replaces one path spelling, case-insensitively, in the three
// forms it can take in output: raw, JSON-escaped backslashes (`\\`), and the
// JSON-escaped slash (`\/`) the unix side also covers.
func replacePath(s, from, to string) string {
	variants := []string{from}
	if esc := strings.ReplaceAll(from, `\`, `\\`); esc != from {
		variants = append(variants, esc)
	}
	if esc := strings.ReplaceAll(from, "/", `\/`); esc != from {
		variants = append(variants, esc)
	}
	for _, v := range variants {
		if esc := jsonASCII(v); esc != v {
			variants = append(variants, esc)
		}
	}
	for _, v := range variants {
		s = regexp.MustCompile("(?i)"+regexp.QuoteMeta(v)).ReplaceAllLiteralString(s, to)
	}
	return s
}

func longPath(p string) string  { return convertPath(p, windows.GetLongPathName) }
func shortPath(p string) string { return convertPath(p, windows.GetShortPathName) }

// convertPath runs GetLongPathName or GetShortPathName; "" when the path does
// not exist or the volume keeps no short names.
func convertPath(p string, conv func(*uint16, *uint16, uint32) (uint32, error)) string {
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return ""
	}
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := conv(in, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			return ""
		}
		if n < uint32(len(buf)) {
			return windows.UTF16ToString(buf[:n])
		}
		buf = make([]uint16, n)
	}
}

// denialBypass names who ignores denyWrites on Windows.
const denialBypass = "an administrator token with the backup and restore privileges bypasses the deny ACE, as GitHub's Windows runner does"

// denyWrites is the cases' icacls deny ACE for Everyone on add file and add
// subdirectory; undo removes it. icacls is native, so no Git Bash is needed.
func denyWrites(dir string) (func(), error) {
	if out, err := exec.Command("icacls", dir, "/deny", "*S-1-1-0:(WD,AD)").CombinedOutput(); err != nil {
		return func() {}, fmt.Errorf("icacls /deny: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return func() { _ = exec.Command("icacls", dir, "/remove:d", "*S-1-1-0").Run() }, nil
}
