package main

// ptymode.go is the behavioural comparison for `mode: pty` on an OS whose
// pseudo-terminal cannot reproduce bytes (Windows; see pty_windows.go).
//
// A ConPTY is not a byte pipe. conhost owns a screen buffer, the child writes
// into it, and what comes out of the pty is a VT re-rendering of that screen:
// cursor moves instead of runs of spaces, wraps at the window width, a title
// sequence, and stdout and stderr merged because a console is one device. So
// the recorded bytes cannot be compared there. What the pty cases exist to pin
// is the gate's DECISION (isatty → a human rendering; TTY_COMPATIBLE=0 or an
// empty FORCE_COLOR → JSON anyway) and the exit code, and both survive the
// re-rendering. That is what is checked, and the runner prints a
// PTY-BEHAVIOURAL line for every such case so nobody reads a pass as a byte
// match. The bytes stay pinned by the darwin and linux runs.

import (
	"fmt"
	"regexp"
	"strings"
)

// vtRe matches the escape sequences a ConPTY emits: CSI (cursor moves, erase,
// SGR, private modes), OSC (window title, terminated by BEL or ST), and the
// two- and three-byte ESC forms (charset selection, keypad modes).
var vtRe = regexp.MustCompile("\x1b\\[[0-?]*[ -/]*[@-~]|\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\)|\x1b[()*+#][0-9A-Za-z]|\x1b[@-Z\\\\-_=>78]")

// screenText strips VT sequences and carriage returns from what a
// pseudo-terminal produced, leaving the text that was drawn.
func screenText(b []byte) string {
	s := vtRe.ReplaceAllString(string(b), "")
	return strings.ReplaceAll(s, "\r", "")
}

// outputMode classifies one channel's text: "empty" (nothing but white
// space), "json" (a JSON document: the first non-space byte opens an object or
// an array — every khub machine document does), or "human" (anything else:
// tables, trees, help, prose). Human renderings never open with `{` or `[`;
// tables open on a title or a box rule, help on blank padding then "Usage:".
func outputMode(s string) string {
	t := strings.TrimLeft(s, " \t\r\n")
	switch {
	case t == "":
		return "empty"
	case t[0] == '{' || t[0] == '[':
		return "json"
	default:
		return "human"
	}
}

// ptyBehaviourMatches checks one pty step behaviourally: the exit code, and
// the output mode of what the terminal showed against the mode of what was
// recorded. A console merges stderr into the screen, so the recorded mode is
// stdout's, or stderr's when the recorded stdout is empty. It prints a FAIL
// line per mismatch, as the byte comparison does.
func ptyBehaviourMatches(label string, screen []byte, code int, wantOut, wantErr []byte, wantCode int) bool {
	ok := true
	if code != wantCode {
		fmt.Printf("FAIL %s: exit %d want %d\n", label, code, wantCode)
		ok = false
	}
	want := outputMode(string(wantOut))
	if want == "empty" {
		want = outputMode(string(wantErr))
	}
	got := outputMode(screenText(screen))
	if got != want {
		fmt.Printf("FAIL %s: pty output mode %s want %s (behavioural check; bytes not compared)\n  got %q\n",
			label, got, want, clip(screenText(screen), 200))
		ok = false
	}
	return ok
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
