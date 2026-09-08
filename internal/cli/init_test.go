package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/endgame-build/khub/internal/reindex"
	"github.com/endgame-build/khub/internal/skill"
	"github.com/endgame-build/khub/internal/wire"
	"github.com/endgame-build/khub/internal/workspace"
)

// tailString flattens a tail[string] into the (action, error) pair the
// assertions below read.
func tailString(step tail[string]) (action, errMsg string) {
	if step.Result != nil {
		action = *step.Result
	}
	return action, step.Err
}

func TestIndexTailWritesThenLeavesTheIndex(t *testing.T) {
	// The tail init and upgrade share: created on a fresh scaffold, unchanged
	// on a re-run, updated when the corpus moved under it.
	ws := filepath.Join(t.TempDir(), "ws")
	if _, err := workspace.Init("build-hub", ws, workspace.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(ws, reindex.IndexName)

	if action, msg := tailString(indexTail(ws)); action != "created" || msg != "" {
		t.Errorf("first tail = (%q, %q)", action, msg)
	}
	if _, err := os.Stat(index); err != nil {
		t.Fatalf("index.md not written: %v", err)
	}
	if action, msg := tailString(indexTail(ws)); action != "unchanged" || msg != "" {
		t.Errorf("second tail = (%q, %q)", action, msg)
	}
	if err := os.WriteFile(index, []byte("stale\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if action, msg := tailString(indexTail(ws)); action != "updated" || msg != "" {
		t.Errorf("third tail = (%q, %q)", action, msg)
	}
}

func TestIndexTailReportsARefusalInsteadOfFailing(t *testing.T) {
	// A malformed entity makes reindex refuse; the scaffold above stands and
	// the tail says why the index was skipped.
	ws := filepath.Join(t.TempDir(), "ws")
	if _, err := workspace.Init("build-hub", ws, workspace.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(ws, "knowledge", "requirements", "broken.md")
	if err := os.MkdirAll(filepath.Dir(bad), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("---\ntype: [unclosed\n---\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	action, msg := tailString(indexTail(ws))
	if action != "" || msg == "" {
		t.Errorf("tail = (%q, %q)", action, msg)
	}
	if _, err := os.Stat(filepath.Join(ws, reindex.IndexName)); err == nil {
		t.Error("index.md was written over a malformed scan")
	}
}

func TestInitPayloadCarriesIndexBeforeTheSkillHint(t *testing.T) {
	result := &workspace.InitResult{Path: ".", Preset: "build-lite", Version: "0.2.0", Name: "ws"}
	created := "created"
	payload := initPayload(result, tailOf(&wire.Result{}, nil), tailOf(&created, nil))
	want := []string{"path", "preset", "version", "name", "source", "entity_files_modified",
		"seeded_over_corpus", "singletons_created", "preserved", "wire", "index", "skill_hint"}
	if !reflect.DeepEqual(payload.Keys(), want) {
		t.Errorf("keys = %v, want %v", payload.Keys(), want)
	}
	payload = initPayload(result,
		tailOf[wire.Result](nil, errors.New("wire broke")),
		tailOf[string](nil, errors.New("index broke")))
	want = []string{"path", "preset", "version", "name", "source", "entity_files_modified",
		"seeded_over_corpus", "singletons_created", "preserved", "wire_error", "index", "index_error", "skill_hint"}
	if !reflect.DeepEqual(payload.Keys(), want) {
		t.Errorf("keys = %v, want %v", payload.Keys(), want)
	}
	if v, _ := payload.Get("index"); v != nil {
		t.Errorf("index = %v on failure, want null", v)
	}
}

func TestUpgradePayloadKeyOrder(t *testing.T) {
	result := &workspace.UpgradeResult{
		Path: "/ws", Preset: "build-lite", VersionFrom: "0.1.0", VersionTo: "0.2.0",
		Config:            []workspace.ConfigChange{{Name: ".khub/storage.yaml", Action: "replaced"}},
		SingletonsCreated: []string{}, SchemaDrift: []string{},
	}
	unchanged := "unchanged"
	payload := upgradePayload(result,
		tailOf(&skill.Report{}, nil), tailOf(&wire.Result{}, nil), tailOf(&unchanged, nil))
	want := []string{"path", "preset", "version_from", "version_to", "config", "singletons_created",
		"schema_drift", "skills", "wire", "index", "dry_run", "removed_types"}
	if !reflect.DeepEqual(payload.Keys(), want) {
		t.Errorf("keys = %v, want %v", payload.Keys(), want)
	}
	config, _ := payload.Get("config")
	record := config.([]any)[0].(interface{ Keys() []string })
	if !reflect.DeepEqual(record.Keys(), []string{"name", "action", "backup"}) {
		t.Errorf("config record keys = %v", record.Keys())
	}

	payload = upgradePayload(result,
		tailOf[skill.Report](nil, errors.New("skills broke")),
		tailOf[wire.Result](nil, errors.New("wire broke")),
		tailOf[string](nil, errors.New("index broke")))
	want = []string{"path", "preset", "version_from", "version_to", "config", "singletons_created",
		"schema_drift", "skills", "skills_error", "wire", "wire_error", "index", "index_error", "dry_run", "removed_types"}
	if !reflect.DeepEqual(payload.Keys(), want) {
		t.Errorf("keys = %v, want %v", payload.Keys(), want)
	}
	for _, key := range []string{"skills", "wire", "index"} {
		if v, _ := payload.Get(key); v != nil {
			t.Errorf("%s = %v on failure, want null", key, v)
		}
	}
}
