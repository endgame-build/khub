package canon

// ReadText is Python's Path.read_text(): read the file AND apply universal
// newline translation, so "\r\n" and a lone "\r" both become "\n".
//
// Go's os.ReadFile returns bytes verbatim, and that difference is observable:
// a CRLF entity made `get --format raw` emit CRLF where Python emitted LF, and
// made `edit` fail outright with "No frontmatter fence" because the strict
// edit-altitude splitter compares a line against exactly "---" and saw
// "---\r". Python never sees the \r at all — it is gone before any parsing.
//
// Every khub read of a text document goes through here, so the translation
// happens once, at the boundary, exactly as it does in Python.

import (
	"os"
	"strings"
)

// NormalizeNewlines applies Python's universal-newline rules to already-read text.
func NormalizeNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// ReadText reads a file the way Python's read_text() does.
func ReadText(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return NormalizeNewlines(string(raw)), nil
}
