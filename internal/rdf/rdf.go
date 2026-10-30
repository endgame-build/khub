// rdf.go is the verb: resolve the schema (and, under --instances, rebuild the
// index and the graph from the live files), render the document, and publish
// it. A file write takes the workspace lock and goes through fsio's atomic
// publication like every other derived artifact; stdout and a dry run take
// no lock and create nothing.

package rdf

import (
	"os"
	"path/filepath"

	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/workspace"
)

// DefaultOut is where the projection lands: beside the lock directory, under
// the gitignored generated tree, so external tooling can point at one path in
// every workspace.
const DefaultOut = ".khub/generated/schema.ttl"

// Stdout is the --out spelling that prints the document instead of writing it.
const Stdout = "-"

// Options are the verb's flags.
type Options struct {
	// Out is the output path: "" for DefaultOut, Stdout for the terminal, a
	// relative path against the workspace root, or an absolute path (which
	// may lie outside the workspace, as viz allows).
	Out string
	// Instances adds the ABox.
	Instances bool
	// DryRun renders and reports but writes nothing.
	DryRun bool
	// Base replaces DefaultBase as the root of every minted IRI.
	Base string
}

// Result is what the verb reports.
type Result struct {
	// Path as the caller will see it: the --out spelling, else DefaultOut.
	Path string
	// Content is the rendered Turtle.
	Content string
	Written bool
	Types   int
	// Entities is the individuals emitted; 0 without --instances.
	Entities int
	Triples  int
	Bytes    int
}

// Project renders the document and, unless Out is Stdout or DryRun is set,
// writes it under the workspace lock.
func Project(root string, opt Options) (*Result, error) {
	if opt.Out == Stdout || opt.DryRun {
		return render(root, opt)
	}
	return fsio.Locked(root, func() (*Result, error) {
		res, err := render(root, opt)
		if err != nil {
			return nil, err
		}
		if err := write(root, opt.Out, []byte(res.Content)); err != nil {
			return nil, err
		}
		res.Written = true
		return res, nil
	})
}

// Render is the pure half of Project: the document for a workspace, written
// nowhere.
func Render(root string, opt Options) (*Result, error) { return render(root, opt) }

func render(root string, opt Options) (*Result, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	prov, err := workspace.Provenance(root)
	if err != nil {
		return nil, err
	}
	name, err := workspace.Name(root)
	if err != nil {
		return nil, err
	}
	presetRaw, _ := prov.Get("preset")
	versionRaw, _ := prov.Get("version")
	preset, _ := presetRaw.(string)
	version, _ := versionRaw.(string)
	ns := Mint(opt.Base, preset, name)
	doc := Schema(resolved, ns, Provenance{Preset: preset, Version: version, Workspace: name})
	entities := 0
	if opt.Instances {
		idx, g, err := load(root, resolved)
		if err != nil {
			return nil, err
		}
		entities = Instances(doc, resolved, idx, g, ns)
	}
	content := doc.String()
	path := opt.Out
	if path == "" {
		path = DefaultOut
	}
	return &Result{
		Path:     filepath.ToSlash(path),
		Content:  content,
		Types:    resolved.Types.Len(),
		Entities: entities,
		Triples:  doc.Triples(),
		Bytes:    len(content),
	}, nil
}

// load rebuilds the index and the graph from the live files, refusing a scan
// that dropped files: an ABox missing a whole type is the failure every
// written projection refuses.
func load(root string, resolved *schema.ResolvedSchema) (*index.Index, *graph.Graph, error) {
	scanned, err := index.Build(root, resolved)
	if err != nil {
		return nil, nil, err
	}
	if err := index.RejectMalformed(scanned, "render the entities"); err != nil {
		return nil, nil, err
	}
	idx := index.Filter(scanned, index.StrayNodes(scanned)) // strays are not entities
	return idx, graph.BuildGraph(idx), nil
}

// write publishes the document. Inside the workspace the write is confined to
// the root (symlinks and escapes refused, as for every entity write); an
// absolute --out outside it is an explicit export, confined to its own
// directory the way fsio.AtomicWrite confines one.
func write(root, out string, data []byte) error {
	if out == "" {
		out = DefaultOut
	}
	path := out
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(out))
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absRoot, path)
	if err == nil && filepath.IsLocal(rel) {
		if err := fsio.MkdirAll(root, filepath.Dir(path)); err != nil {
			return err
		}
		return fsio.AtomicWriteIn(root, path, data)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return err
	}
	return fsio.AtomicWrite(path, data)
}
