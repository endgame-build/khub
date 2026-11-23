package entity

import (
	"path/filepath"
	"strings"
	"testing"
)

// add --draft with a --status that is not draft asks for two publish states:
// refused, nothing written. --status draft --draft agree.
func TestAddRefusesStatusDraftConflict(t *testing.T) {
	ws := newWS(t, "firm-ops")
	_, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Acme", "status", "stable"), Draft: true})
	_ = requireCode(t, err, "status_draft_conflict")
	res, err := Create(ws, "client", CreateOpts{Fields: fields("name", "Acme", "status", "draft"), Draft: true})
	requireNoError(t, err)
	if !res.Draft {
		t.Fatal("--status draft --draft did not write a draft")
	}
}

// An edit that writes `status` drops a retired `draft` flag in the same write,
// so the entity is migrated and upgrade sees no conflict.
func TestEditStatusDropsLegacyDraft(t *testing.T) {
	ws := newWS(t, "firm-ops")
	seed(t, ws, "clients/old.md", kv("type", "client", "name", "Old", "draft", true))
	if _, err := Update(ws, "old", UpdateOpts{Fields: fields("status", "stable")}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got := readFile(t, filepath.Join(ws, "clients", "old.md"))
	if strings.Contains(got, "draft:") || !strings.Contains(got, "status: stable") {
		t.Fatalf("legacy draft kept:\n%s", got)
	}
}
