package search

import (
	"testing"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/ncruces/go-sqlite3"
)

func TestBuildFTSRollsBackParseFailure(t *testing.T) {
	root := freshWS(t)
	for _, slug := range []string{"a", "z"} {
		seedRaw(t, root, "clients/"+slug+".md", "---\ntype: client\nname: Token\n---\nToken body\n")
	}
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		t.Fatal(err)
	}
	seedRaw(t, root, "clients/z.md", "---\nbroken: [\n---\n")
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if err := buildFTS(conn, root, idx, nil); err == nil {
		t.Fatal("parse failure swallowed")
	}
	if !conn.GetAutocommit() {
		t.Fatal("transaction left open")
	}
	hits, err := match(conn, "Token", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("partial projection survived rollback: %v", hits)
	}
}
