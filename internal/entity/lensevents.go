package entity

// Appending lens verdicts (`khub review --record`): one batch of `verified`
// events, all or none, under one lock and one scan.

import (
	"errors"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
)

// LensEvent is one event to append to one entity's `verified` list.
type LensEvent struct {
	Node  index.Node
	Event *omap.Map
}

// AppendedEvent is a LensEvent and the file it landed in (absolute).
type AppendedEvent struct {
	LensEvent
	Path string
}

// AppendLensEvents scans the workspace, asks plan for the events to append
// (plan checks the batch against that scan and refuses it whole), and appends
// them all or none. Under a real run the workspace lock covers the scan, the
// plan and every write; a dry run takes no lock, runs every check, and writes
// nothing. Each entity's file is written once, with its events in plan order.
// A write that fails restores every file the batch already wrote and returns
// its error.
//
// An event is not a write of content: like `verify`, this bumps no `updated`
// and stamps no `generated`, and every other byte of the file is kept.
func AppendLensEvents(root string, dryRun bool,
	plan func(*schema.ResolvedSchema, *index.Index) ([]LensEvent, error)) ([]AppendedEvent, error) {
	out := sink{dry: dryRun}
	return locked(root, out, func() ([]AppendedEvent, error) {
		resolved, err := introspect.LoadSchema(root)
		if err != nil {
			return nil, err
		}
		idx, err := index.BuildWithBodies(root, resolved)
		if err != nil {
			return nil, err
		}
		events, err := plan(resolved, idx)
		if err != nil {
			return nil, err
		}
		type file struct {
			path, before string
			original     []byte
			meta         *omap.Map
			body         string
		}
		var files []*file
		byNode := map[index.Node]*file{}
		result := make([]AppendedEvent, 0, len(events))
		for _, ev := range events {
			f := byNode[ev.Node]
			if f == nil {
				rtype, _ := resolved.Types.Get(ev.Node.Type)
				if attr, ok := rtype.Attributes.Get("verified"); !ok || attr.Items == nil || attr.Items.Fields == nil {
					return nil, errs.NoReviewList(ev.Node.Type)
				}
				if rtype.Storage.Layout == schema.LayoutCollection {
					// Lenses read md bodies of per-item and singleton types only.
					return nil, errs.NoReviewList(ev.Node.Type)
				}
				path := entityPath(root, idx, ev.Node)
				before, err := canon.ReadTextIn(root, path)
				if err != nil {
					return nil, err
				}
				original, err := fsio.ReadFile(root, path)
				if err != nil {
					return nil, err
				}
				meta, body, err := readDoc(root, path)
				if err != nil {
					return nil, err
				}
				f = &file{path: path, before: before, original: original, meta: meta, body: body}
				byNode[ev.Node] = f
				files = append(files, f)
			}
			appendVerified(f.meta, ev.Event)
			result = append(result, AppendedEvent{LensEvent: ev, Path: f.path})
		}
		texts := make([]string, len(files))
		for i, f := range files {
			format := canon.FmtOf(f.path)
			text, err := canon.SpliceDoc(f.before, f.meta, f.body, format, canon.Mode12)
			if errors.Is(err, canon.ErrNoSplice) {
				text, err = canon.Render(f.meta, f.body, format)
			}
			if err != nil {
				return nil, err
			}
			texts[i] = text
		}
		if dryRun {
			for _, f := range files {
				if err := fsio.CheckPublish(root, f.path, false); err != nil {
					return nil, err
				}
			}
			return result, nil
		}
		for i, f := range files {
			if err := fsio.AtomicWriteIn(root, f.path, []byte(texts[i])); err != nil {
				for _, done := range files[:i] {
					if rerr := fsio.AtomicWriteIn(root, done.path, done.original); rerr != nil {
						return nil, rerr
					}
				}
				return nil, err
			}
		}
		return result, nil
	})
}
