package entity

import (
	"os"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/clock"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/trust"
	"github.com/endgame-build/khub/internal/values"
)

// Actor is who a write is stamped as, the first of: the explicit --by,
// KHUB_ACTOR, `agent/claude-code` under Claude Code, and `khub`. It never
// reads a human out of git identity: an agent running in someone's shell
// would be stamped as them.
func Actor(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("KHUB_ACTOR"); v != "" {
		return v
	}
	if os.Getenv("CLAUDECODE") != "" {
		return "agent/claude-code"
	}
	return "khub"
}

// snapshot is an entity's whole state as one comparable string: its
// frontmatter in the CLI JSON dialect, then its body.
func snapshot(meta *omap.Map, body string) string {
	text, err := canon.EncodeCLI(meta)
	if err != nil {
		panic(err) // loaded values always encode; a failure is a bug, not input
	}
	return text + "\x00" + body
}

// rowBody is a collection row's prose: its reserved `body` key.
func rowBody(row *omap.Map) string {
	v, _ := row.Get("body")
	s, _ := v.(string)
	return s
}

// stamp sets OKF `generated: {by, at}` on an entity whose content a write verb
// changed:
// the last writer and when, from the clock. A type whose schema has no
// `generated` gets nothing.
func stamp(rtype *schema.ResolvedType, meta *omap.Map, actor string) {
	if !rtype.Attributes.Has("generated") {
		return
	}
	g := omap.New()
	g.Set("by", Actor(actor))
	g.Set("at", canon.DateTime{ISO: clock.Stamp()})
	meta.Set("generated", g)
}

// VerifyResult is the event a verify recorded and where it landed.
type VerifyResult struct {
	Type, Slug, Path, Locator string
	By, At, Rev               string
}

// Verify appends a review event `{by, at, rev}` to id's `verified`. at ""
// is now. rev is trust.Rev over the content as the scan reads it, the same
// reading every later trust check uses. A bare `verified` mapping becomes a
// list. A review is not a write of content: it bumps no `updated` and stamps
// no `generated`.
//
// publish, when not nil, runs on the written file while the workspace lock is
// still held (`--commit` makes its signed commit there). If it fails, the
// file's original bytes are restored, so a refused publish leaves nothing
// written.
func Verify(root, id, by, at string, publish func(path string) error) (*VerifyResult, error) {
	return fsio.Locked(root, func() (*VerifyResult, error) { return verify(root, id, by, at, publish) })
}

func verify(root, id, by, at string, publish func(path string) error) (*VerifyResult, error) {
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		return nil, err
	}
	idx, err := index.BuildWithBodies(root, resolved)
	if err != nil {
		return nil, err
	}
	node, err := resolveID(idx, id)
	if err != nil {
		return nil, err
	}
	rtype, _ := resolved.Types.Get(node.Type)
	attr, ok := rtype.Attributes.Get("verified")
	if !ok || attr.Items == nil || attr.Items.Fields == nil {
		return nil, errs.NoReviewList(node.Type)
	}
	if at == "" {
		at = clock.Stamp()
	} else if f, has := attr.Items.Fields.Get("at"); has && f.Pattern != nil && !f.MatchPattern(at) {
		return nil, errs.PatternViolation(at, "verified.at", *f.Pattern)
	} else if !values.IsDateish(at) {
		return nil, errs.DateViolation(at, "datetime", "verified.at")
	}
	rev := trust.Rev(rtype, idx.Meta[node], idx.Body[node])
	event := omap.New()
	event.Set("by", by)
	event.Set("at", canon.DateTime{ISO: at})
	event.Set("rev", rev)
	record := func(m *omap.Map) {
		existing, _ := m.Get("verified")
		switch v := existing.(type) {
		case nil:
			m.Set("verified", []any{event})
		case []any:
			m.Set("verified", append(v, event))
		default:
			m.Set("verified", []any{v, event})
		}
	}

	path := entityPath(root, idx, node)
	res := &VerifyResult{
		Type: node.Type, Slug: node.Slug, Path: path, Locator: locator(rtype, node.Slug), By: by, At: at, Rev: rev,
	}
	original, err := fsio.ReadFile(root, path)
	if err != nil {
		return nil, err
	}
	if rtype.Storage.Layout == schema.LayoutCollection {
		err = mutateCollection(root, rtype, func(rows *omap.Map) (bool, error) {
			row, has := rows.Get(node.Slug)
			if !has {
				return false, errs.LookupError(id)
			}
			record(row.(*omap.Map))
			return true, nil
		})
	} else {
		var meta *omap.Map
		var body string
		if meta, body, err = readDoc(root, path); err == nil {
			record(meta)
			err = writeDoc(root, path, meta, body)
		}
	}
	if err != nil {
		return nil, err
	}
	if publish != nil {
		if perr := publish(path); perr != nil {
			if rerr := fsio.AtomicWriteIn(root, path, original); rerr != nil {
				return nil, rerr
			}
			return nil, perr
		}
	}
	return res, nil
}
