package workspace

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
)

// Carried is one per-type key of policy.yaml or storage.yaml that upgrade
// kept from the workspace over the shipped file. File is workspace-relative.
type Carried struct {
	File string
	Type string
	Key  string
}

// carryEdits lays the workspace's own per-type keys of one layer file
// (policy or storage) over the shipped document and returns the result with
// what it kept. A key the shipped file lacks is the workspace's addition and
// is always kept, and so is a whole type entry the shipped file lacks. A key
// both carry with different values is kept only at the same preset version,
// where the shipped file has not moved and any difference is an edit. Across
// versions the shipped value wins and the edit stays in the .bak, because
// khub cannot tell an edit from the previous default. Only types the shipped
// ontology declares are carried: ontology.yaml itself is replaced.
// allow, when set, limits what is carried to the keys it accepts.
func carryEdits(target, path, top string, shipped *omap.Map, declared map[string]bool, sameVersion bool, allow func(Carried) bool) (*omap.Map, []Carried, error) {
	raw, err := fsio.ReadFile(osPath(target), osPath(path))
	if errors.Is(err, fs.ErrNotExist) {
		return shipped, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	doc, err := canon.LoadDoc(string(raw))
	if err != nil {
		return shipped, nil, nil // an unreadable edit is replaced and kept in the .bak
	}
	current, _ := mapIn(doc, top)
	if current == nil {
		return shipped, nil, nil
	}
	incoming, _ := mapIn(shipped, top)
	merged := omap.New()
	if incoming != nil {
		for _, t := range incoming.Keys() {
			v, _ := incoming.Get(t)
			merged.Set(t, v)
		}
	}
	name := relTo(target, path)
	var carried []Carried
	for _, t := range current.Keys() {
		if !declared[t] {
			continue
		}
		cv, _ := current.Get(t)
		ct, isMap := cv.(*omap.Map)
		if !isMap {
			continue
		}
		iv, _ := merged.Get(t)
		it, _ := iv.(*omap.Map)
		out := omap.New()
		if it != nil {
			for _, k := range it.Keys() {
				v, _ := it.Get(k)
				out.Set(k, v)
			}
		}
		for _, k := range ct.Keys() {
			v, _ := ct.Get(k)
			was, shippedHas := out.Get(k)
			if shippedHas && (!sameVersion || canon.EqualValues(was, v)) {
				continue
			}
			c := Carried{File: name, Type: t, Key: k}
			if allow != nil && !allow(c) {
				continue
			}
			out.Set(k, v)
			carried = append(carried, c)
		}
		merged.Set(t, out)
	}
	if len(carried) == 0 {
		return shipped, nil, nil
	}
	result := omap.New()
	for _, k := range shipped.Keys() {
		v, _ := shipped.Get(k)
		result.Set(k, v)
	}
	result.Set(top, merged)
	return result, carried, nil
}

// mapIn is doc[key] when both are mappings.
func mapIn(doc any, key string) (*omap.Map, bool) {
	m, ok := doc.(*omap.Map)
	if !ok {
		return nil, false
	}
	v, _ := m.Get(key)
	inner, ok := v.(*omap.Map)
	return inner, ok
}

// Dropped is a carried key upgrade let go because the schema it produced did
// not resolve; Reason is the resolver's message.
type Dropped struct {
	File   string
	Type   string
	Key    string
	Reason string
}

// settleCarried builds the policy and storage documents upgrade writes,
// carrying the workspace's keys (carryEdits). A carried key never blocks the
// upgrade: when the result does not resolve, the keys are kept one at a time,
// in order, while the schema still resolves, and the rest are dropped with
// the resolver's reason. Nothing is written here.
func settleCarried(target, khubDir string, merged *flattened, declared map[string]bool, sameVersion bool) (map[string]*omap.Map, []Carried, []Dropped, error) {
	build := func(allow func(Carried) bool) (map[string]*omap.Map, []Carried, error) {
		docs := map[string]*omap.Map{}
		var carried []Carried
		for _, layer := range []struct {
			file string
			doc  *omap.Map
		}{{"policy.yaml", merged.Policy}, {"storage.yaml", merged.Storage}} {
			top := strings.TrimSuffix(layer.file, ".yaml")
			doc, keys, err := carryEdits(target, pyJoin(khubDir, layer.file), top, layer.doc, declared, sameVersion, allow)
			if err != nil {
				return nil, nil, err
			}
			docs[layer.file] = doc
			carried = append(carried, keys...)
		}
		return docs, carried, nil
	}
	docs, all, err := build(nil)
	if err != nil || len(all) == 0 || resolves(merged.Ontology, docs) == nil {
		return docs, all, nil, err
	}
	var kept []Carried
	var dropped []Dropped
	for _, c := range all {
		trial := append(slices.Clone(kept), c)
		docs, _, err := build(func(x Carried) bool { return slices.Contains(trial, x) })
		if err != nil {
			return nil, nil, nil, err
		}
		if rerr := resolves(merged.Ontology, docs); rerr != nil {
			dropped = append(dropped, Dropped{c.File, c.Type, c.Key, rerr.Error()})
			continue
		}
		kept = trial
	}
	docs, kept, err = build(func(x Carried) bool { return slices.Contains(kept, x) })
	return docs, kept, dropped, err
}

// resolves reports whether the shipped ontology and these policy and storage
// documents load as a schema, by laying them out in a scratch workspace.
func resolves(ontology *omap.Map, docs map[string]*omap.Map) error {
	dir, err := os.MkdirTemp("", "khub-carry-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	khub := filepath.Join(dir, ".khub")
	if err := os.MkdirAll(khub, 0o755); err != nil {
		return err
	}
	for name, doc := range map[string]*omap.Map{"ontology.yaml": ontology, "policy.yaml": docs["policy.yaml"], "storage.yaml": docs["storage.yaml"]} {
		text, err := canon.DumpWide(doc)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(khub, name), []byte(text), 0o644); err != nil {
			return err
		}
	}
	_, err = introspect.LoadSchema(dir)
	return err
}
