package schema

import (
	"strings"
	"testing"
)

func TestStorageOwnershipAndPathSafety(t *testing.T) {
	for _, tc := range []struct {
		name, storage string
		valid         bool
	}{
		{"alias", "a: {layout: collection, path: rows.json}\n  b: {layout: collection, path: ./rows.json}", false},
		{"escape", "a: {layout: file, path: ../outside}\n  b: {layout: file, path: b}", false},
		{"absolute", "a: {layout: file, path: /outside}\n  b: {layout: file, path: b}", false},
		{"control", "a: {layout: file, path: .khub/entities}\n  b: {layout: file, path: b}", false},
		{"root index", "a: {layout: singleton, path: index.md}\n  b: {layout: file, path: b}", false},
		{"folder overlap", "a: {layout: folder, path: objects}\n  b: {layout: file, path: objects/nested}", false},
		{"file collision", "a: {layout: file, path: objects}\n  b: {layout: singleton, path: objects/b.md}", false},
		{"file as directory", "a: {layout: collection, path: objects.json}\n  b: {layout: file, path: objects.json/b}", false},
		{"siblings", "a: {layout: file, path: objects/a}\n  b: {layout: file, path: objects/b}", true},
		{"normalize", "a: {layout: file, path: ./objects/a}\n  b: {layout: file, path: objects/b}", true},
		{"reserved singleton", "a: {layout: singleton, path: con.md}\n  b: {layout: file, path: b}", false},
		{"reserved folder", "a: {layout: file, path: notes/NUL/a}\n  b: {layout: file, path: b}", false},
		{"reserved collection", "a: {layout: collection, path: com0.jsonl}\n  b: {layout: file, path: b}", false},
		{"reserved trailing space", "a: {layout: file, path: 'notes/con /a'}\n  b: {layout: file, path: b}", false},
		{"reserved superscript", "a: {layout: file, path: com¹}\n  b: {layout: file, path: b}", false},
		{"reserved-looking", "a: {layout: singleton, path: console.md}\n  b: {layout: file, path: con-b}", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := "ontology:\n  entities:\n    a: {}\n    b: {}\nstorage:\n  " + tc.storage + "\n"
			got, err := ResolveWith(nil, writeSchemaFiles(t, doc))
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if err == nil {
				a, _ := got.Types.Get("a")
				if strings.HasPrefix(a.StorageRelpath(), "./") {
					t.Fatal("path not normalized")
				}
			}
		})
	}
}

func TestInvalidPatternRejectedAtResolve(t *testing.T) {
	_, err := ResolveWith(nil, writeSchemaFiles(t, "ontology:\n  entities:\n    item:\n      attributes:\n        code: {type: text, pattern: '['}\n"))
	if err == nil || !strings.Contains(err.Error(), "Invalid pattern") {
		t.Fatalf("err=%v", err)
	}
}

// A type with no storage path is stored under its own name, so a type named
// after a device is refused too, and the message names the part and the fix.
func TestReservedDeviceNameRefusedInStoragePaths(t *testing.T) {
	_, err := ResolveWith(nil, writeSchemaFiles(t, "ontology:\n  entities:\n    aux: {}\n"))
	if err == nil || !strings.Contains(err.Error(), "storage.aux.path: 'aux' is a reserved device name on Windows") {
		t.Fatalf("err=%v", err)
	}
}

func TestReservedDeviceName(t *testing.T) {
	for _, name := range []string{
		"con", "CON", "Prn", "aux", "nul", "com0", "com1", "COM9", "lpt0", "lpt1", "lpt9",
		"con.md", "nul.tar.gz", "Aux.json",
		"com\u00b9", "COM\u00b2", "lpt\u00b3", "LPT\u00b9.md",
		"conin$", "CONOUT$", "conin$.txt",
		"con ", "con.", "con .txt", "nul  ", "aux. ",
	} {
		if !ReservedDeviceName(name) {
			t.Errorf("ReservedDeviceName(%q) = false", name)
		}
	}
	for _, name := range []string{
		"", "co", "conn", "con-x", "ad-con", "2026-01-15-con", "com", "com10",
		"xnul", "nul-", "my.con", " con", "com\u2074", "com\u00b9\u00b9", "conin", "conout",
	} {
		if ReservedDeviceName(name) {
			t.Errorf("ReservedDeviceName(%q) = true", name)
		}
	}
}
