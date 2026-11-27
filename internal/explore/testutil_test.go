package explore

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/workspace"
)

// TestMain pins the writer every fixture stamps. Claude Code shells export
// CLAUDECODE, which the actor ladder would otherwise read; nothing here
// asserts a stamp, but a fixture must not depend on the shell it runs in.
func TestMain(m *testing.M) {
	os.Setenv("KHUB_ACTOR", "khub")
	os.Setenv("KHUB_SESSION", "")
	os.Exit(m.Run())
}

var now = time.Now()

func initWS(t *testing.T, preset string) string {
	t.Helper()
	root := t.TempDir()
	if _, err := workspace.Init(preset, root, workspace.InitOptions{}); err != nil {
		t.Fatalf("init %s: %v", preset, err)
	}
	return root
}

func pairs(kv ...string) *omap.Map {
	m := omap.New()
	for i := 0; i+1 < len(kv); i += 2 {
		m.Set(kv[i], kv[i+1])
	}
	return m
}

func mustCreate(t *testing.T, root, typ, id string, draft bool, kv ...string) {
	t.Helper()
	if _, err := entity.Create(root, typ, entity.CreateOpts{
		Fields: pairs(kv...), ID: id, Draft: draft, UseTemplate: true,
	}); err != nil {
		t.Fatalf("create %s %s: %v", typ, id, err)
	}
}

func mustCreateBody(t *testing.T, root, typ, id, body string, kv ...string) {
	t.Helper()
	if _, err := entity.Create(root, typ, entity.CreateOpts{
		Fields: pairs(kv...), ID: id, Body: body, UseTemplate: true,
	}); err != nil {
		t.Fatalf("create %s %s: %v", typ, id, err)
	}
}

// buildHubWS is a small, fully wired build-hub workspace that holds every case
// the explorer has to get right:
//
//   - a component with outgoing (system, consumes) and incoming (served_by →
//     serves, realized_in, provider) edges;
//   - a capability, whose incoming relations declare no inverse;
//   - an adr superseding another (a type relating to itself);
//   - a draft, a stale, an orphan and an incomplete entity;
//   - a base-relation edge (related) between two types.
func buildHubWS(t *testing.T) string {
	t.Helper()
	root := initWS(t, "build-hub")
	mustCreate(t, root, "capability", "cap-pay", false, "title", "Payments", "description", "Moving money between accounts.")
	mustCreate(t, root, "capability", "cap-report", false, "title", "Reporting")
	mustCreate(t, root, "capability", "cap-ideas", true, "title", "Ideas")
	mustCreate(t, root, "actor", "act-clerk", false, "title", "Clerk")
	mustCreate(t, root, "actor", "act-lonely", false, "title", "Lonely")
	mustCreate(t, root, "system", "sys-core", false, "title", "Core")
	mustCreate(t, root, "component", "cmp-ledger", false,
		"title", "Ledger", "kind", "service", "system", "sys-core", "tier", "tier-1", "owner", "team-money")
	mustCreate(t, root, "component", "cmp-gateway", false, "title", "Gateway", "kind", "external")
	mustCreate(t, root, "api", "api-ledger", false, "title", "Ledger API", "kind", "rest", "provider", "cmp-ledger")
	mustCreate(t, root, "component", "cmp-portal", false,
		"title", "Portal", "kind", "service", "system", "sys-core", "consumes", "api-ledger")
	mustCreateBody(t, root, "use-case", "uc-pay",
		"# Pay\n\nThe clerk pays an invoice.\nIt settles the same day.\n",
		"title", "Pay an invoice", "trigger", "human", "actor", "act-clerk",
		"capability", "cap-pay", "served_by", "cmp-ledger,cmp-gateway")
	mustCreate(t, root, "use-case", "uc-refund", false,
		"title", "Refund", "trigger", "event", "capability", "cap-pay", "served_by", "cmp-ledger")
	// No trigger: a required field left out, which check reports as incomplete.
	mustCreate(t, root, "use-case", "uc-half", false, "title", "Half written", "capability", "cap-report")
	mustCreate(t, root, "requirement", "req-settle", false,
		"title", "Settle same day", "kind", "functional",
		"capabilities", "cap-pay", "use_cases", "uc-pay", "realized_in", "cmp-ledger",
		"related", "cap-report")
	mustCreate(t, root, "adr", "ad-2026-01-05-first", false,
		"title", "First", "decision", "accepted", "affects", "cmp-ledger")
	mustCreate(t, root, "adr", "ad-2026-02-05-second", false,
		"title", "Second", "decision", "accepted", "supersedes", "ad-2026-01-05-first")
	// Past its stale_after: stale whatever its age.
	mustCreate(t, root, "system", "sys-old", false,
		"title", "Old", "stale_after", "2000-01-01T00:00:00Z")
	return root
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func keys(cols []Column) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Key)
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
