package main

import "testing"

// The classifier runs on Windows only, where nothing in this repo's CI can
// show it a real ConPTY before the Windows job does. These are the shapes a
// ConPTY is known to produce, so the darwin and linux runs hold it too.
func TestScreenTextAndOutputMode(t *testing.T) {
	cases := []struct {
		name, raw, want string
	}{
		{"json object behind a title and private modes",
			"\x1b[?9001h\x1b[?1004h\x1b]0;C:\\a\\khub.exe\x07\x1b[?25l\x1b[2J\x1b[m\x1b[H{\"id\": \"component/cmp-api\"}\r\n\x1b[?25h", "json"},
		{"json array", "\x1b[H[{\"id\": \"x\"}]\r\n", "json"},
		{"json wrapped at the window width", "{\"path\": \".\", \"preset\": \"build-hub\", \"versi\r\non\": \"0.6.0\"}", "json"},
		{"human table", "\x1b[H query\r\n\x1b[1m┏━━━┓\x1b[m\r\n", "human"},
		{"help padded by cursor moves", "\x1b[H\x1b[80X\r\n\x1b[1C Usage: khub [OPTIONS]", "human"},
		{"prose", "Linked req-a --realized_in--> cmp-api\r\n", "human"},
		{"nothing but a cleared screen", "\x1b[?25l\x1b[2J\x1b[m\x1b[H\x1b]0;khub\x1b\\\x1b[?25h", "empty"},
		{"ST-terminated title", "\x1b]0;title\x1b\\{}", "json"},
	}
	for _, c := range cases {
		if got := outputMode(screenText([]byte(c.raw))); got != c.want {
			t.Errorf("%s: mode %q want %q (text %q)", c.name, got, c.want, screenText([]byte(c.raw)))
		}
	}
}

// The recorded side is classified from the unix bytes, which carry no VT.
func TestOutputModeOfRecordedBytes(t *testing.T) {
	for raw, want := range map[string]string{
		"":                                 "empty",
		"\n":                               "empty",
		"{\"error\": {\"code\": \"x\"}}\n": "json",
		"[\"prd\", \"arc42\"]\n":           "json",
		"                    \n Usage: khub [OPTIONS]\n": "human",
		" query\n┏━━┓\n":                                 "human",
	} {
		if got := outputMode(raw); got != want {
			t.Errorf("outputMode(%q) = %q want %q", raw, got, want)
		}
	}
}

func TestPTYBehaviourFallsBackToStderrMode(t *testing.T) {
	// A console merges the channels; a step recorded with empty stdout is
	// judged on its stderr's mode.
	if !ptyBehaviourMatches("t", []byte("Usage: khub\r\nError"), 2, nil, []byte("Usage: khub\n"), 2) {
		t.Fatal("stderr-only step should match on its stderr's mode")
	}
	if ptyBehaviourMatches("t", []byte("{}"), 0, []byte("{}\n"), nil, 2) {
		t.Fatal("an exit code mismatch must fail")
	}
}
