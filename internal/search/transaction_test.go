package search

import (
	"strings"
	"testing"

	"github.com/ncruces/go-sqlite3"

	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
)

// A failure part-way through buildFTS rolls the whole table back: no partial
// projection survives to answer a query. The connection's length limit makes
// the second, longer body fail to insert after the first went in.
func TestBuildFTSRollsBackAFailedInsert(t *testing.T) {
	root := freshWS(t)
	seedRaw(t, root, "clients/a.md", "---\ntype: client\nname: Token\n---\nToken\n")
	seedRaw(t, root, "clients/z.md", "---\ntype: client\nname: Token\n---\nToken "+strings.Repeat("x", 4000)+"\n")
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := index.BuildWithBodies(root, resolved)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	conn.Limit(sqlite3.LIMIT_LENGTH, 1000)
	if _, err := buildFTS(conn, root, idx, nil); err == nil {
		t.Fatal("an insert over the length limit succeeded")
	}
	if !conn.GetAutocommit() {
		t.Fatal("transaction left open")
	}
	hits, err := match(conn, "Token", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("partial projection survived rollback: %v", hitSlugs(hits))
	}
}
