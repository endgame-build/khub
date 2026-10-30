package cli

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// scrubSnapshot keeps the --at scratch directory out of printed text, in
// every spelling, and scrubSnapshotJSON keeps the document valid whatever the
// ref holds.
func TestScrubSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "khub-at-1")
	defer func(at, sc string) { state.At, scrubbed = at, sc }(state.At, scrubbed)
	state.At, scrubbed = "HEAD~1", dir

	in := "Cannot parse schema: " + filepath.Join(dir, ".khub", "ontology.yaml") +
		"; also " + filepath.ToSlash(dir) + "/knowledge/a.md and " + dir
	want := "Cannot parse schema: HEAD~1:.khub/ontology.yaml; also HEAD~1:knowledge/a.md and HEAD~1:."
	if filepath.Separator == '\\' {
		want = "Cannot parse schema: HEAD~1:.khub\\ontology.yaml; also HEAD~1:knowledge/a.md and HEAD~1:."
	}
	if got := scrubSnapshot(in); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}

	state.At = `we"ird`
	doc, _ := json.Marshal(map[string]string{"message": filepath.Join(dir, "x.md")})
	out := scrubSnapshotJSON(string(doc))
	var back map[string]string
	if err := json.Unmarshal([]byte(out), &back); err != nil {
		t.Fatalf("scrubbed JSON no longer parses: %s: %v", out, err)
	}
	if back["message"] != `we"ird:x.md` {
		t.Fatalf("message %q", back["message"])
	}

	scrubbed = ""
	if got := scrubSnapshot(in); got != in {
		t.Fatal("no snapshot: text must pass through")
	}
}
