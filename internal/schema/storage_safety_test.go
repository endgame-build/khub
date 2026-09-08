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
