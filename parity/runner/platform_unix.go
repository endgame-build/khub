//go:build !windows

package main

// platform_unix.go holds the unix side of every platform seam the runner has.
// Each function here reproduces exactly what the runner did before Windows
// existed; platform_windows.go is the only place the two differ.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// shScript picks the script a step runs. Off Windows `sh_windows` is ignored,
// so a step holding only that key runs nothing.
func shScript(st step) string { return st.Sh }

// shCommand runs a setup script under the system sh.
func shCommand(script, _ string, _ int) (*exec.Cmd, error) {
	return exec.Command("sh", "-c", script), nil
}

// platformEnv adds nothing on unix: baseEnv is the whole environment.
func platformEnv(string) []string { return nil }

// canonicalTemp is the identity on unix. The /private realpath on macOS is
// handled by the normalizer, not by rewriting the path khub is given.
func canonicalTemp(p string) string { return p }

// pathForms lists the spellings of p the normalizer replaces: the realpath
// when it differs (macOS /var → /private/var), then p itself.
func pathForms(p string) []string {
	var forms []string
	if real, err := filepath.EvalSymlinks(p); err == nil && real != p {
		forms = append(forms, real)
	}
	return append(forms, p)
}

// replacePath replaces one path spelling and its JSON-escaped forms: `\/`, and
// non-ASCII as \uXXXX.
func replacePath(s, from, to string) string {
	for _, f := range []string{from, jsonASCII(from)} {
		s = strings.ReplaceAll(s, f, to)
		s = strings.ReplaceAll(s, strings.ReplaceAll(f, "/", `\/`), to) // JSON-escaped form
	}
	return s
}

// denialBypass names who ignores denyWrites on unix.
const denialBypass = "root ignores directory mode bits"

// denyWrites is the cases' `chmod 500`; undo restores 0755.
func denyWrites(dir string) (func(), error) {
	if err := os.Chmod(dir, 0o500); err != nil {
		return func() {}, err
	}
	return func() { _ = os.Chmod(dir, 0o755) }, nil
}
