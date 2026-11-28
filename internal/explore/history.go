package explore

import (
	"slices"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/omap"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/trust"
	"github.com/endgame-build/khub/internal/values"
)

const (
	// historyLimit is how many versions of a file are read.
	historyLimit = 50
	// diffLimit caps the body lines a comparison lists.
	diffLimit = 400
	// diffCells bounds the line comparison: past it the body is reported as
	// changed, without its lines.
	diffCells = 4_000_000
)

// content is an entity as one version of its file holds it.
type content struct {
	meta *omap.Map
	body string
}

// History is how an entity's content came to be (/api/history): the commits
// that touched its file, each with the fields it changed, and, when its newest
// review no longer holds, what changed since that review. It reads git and
// writes nothing.
func History(root, id string, now time.Time) (*omap.Map, error) {
	s, err := load(root, now)
	if err != nil {
		return nil, err
	}
	node, err := entity.ResolveID(s.idx, id)
	if err != nil {
		return nil, err
	}
	rtype, _ := s.resolved.Types.Get(node.Type)
	out := omap.New()
	out.Set("id", node.ID())
	if rtype.Storage.Layout == schema.LayoutCollection {
		out.Set("commits", []any{})
		out.Set("more", false)
		note := "Its file holds every entity of the type, so git has no history of this one alone"
		out.Set("note", note)
		current := &content{s.idx.Meta[node], s.idx.Body[node]}
		out.Set("since_review", s.sinceReview(node, rtype, current, nil, nil, false, note))
		out.Set("malformed", strList(s.malformed))
		return out, nil
	}
	versions, more, err := gitlog.FileVersions(root, s.idx.Paths[node], historyLimit)
	if err != nil {
		return nil, err
	}
	parse := func(b gitlog.Blob) *content {
		if b.State != gitlog.BlobHeld {
			return nil
		}
		meta, body, err := canon.Parse(b.Text, rtype.Storage.Fmt)
		if err != nil {
			return nil
		}
		return &content{meta, body}
	}
	parsed := make([]*content, len(versions))
	for i, v := range versions {
		parsed[i] = parse(v.After)
	}
	current := &content{s.idx.Meta[node], s.idx.Body[node]}

	commits := []any{}
	if len(versions) > 0 && parsed[0] != nil {
		if fields := changed(parsed[0], current); len(fields) > 0 {
			commits = append(commits, commit(gitlog.Version{Subject: "Not committed yet"}, kindChanged, fields))
		}
	}
	for i, v := range versions {
		before := parse(v.Before)
		var fields []string
		kind := kindUnknown
		switch {
		case v.After.State == gitlog.BlobAbsent:
			kind = kindRemoved
		case v.After.State == gitlog.BlobUnknown:
		case parsed[i] == nil:
			kind = kindUnparsable
		case v.Before.State == gitlog.BlobAbsent:
			kind = kindCreated
		case before != nil:
			kind, fields = kindBookkeeping, changed(before, parsed[i])
			if len(fields) > 0 {
				kind = kindChanged
			}
		}
		commits = append(commits, commit(v, kind, fields))
	}
	note := ""
	if len(versions) == 0 {
		note = "Git holds no version of this file yet"
		if ok, err := gitlog.HasGitHistory(root); err != nil {
			return nil, err
		} else if !ok {
			note = "This workspace has no git history"
		}
	}
	out.Set("commits", commits)
	out.Set("more", more)
	out.Set("note", note)
	out.Set("since_review", s.sinceReview(node, rtype, current, versions, parsed, more, note))
	out.Set("malformed", strList(s.malformed))
	return out, nil
}

// What a commit did to an entity.
const (
	kindChanged     = "changed"     // the fields listed, body among them
	kindBookkeeping = "bookkeeping" // dates, stamps or reviews only
	kindCreated     = "created"
	kindRemoved     = "removed"
	kindUnparsable  = "unparsable" // it left a file that does not parse
	kindUnknown     = "unknown"    // git cannot show the version before it
)

func commit(v gitlog.Version, kind string, fields []string) *omap.Map {
	m := omap.New()
	m.Set("commit", v.Commit)
	m.Set("date", v.Date)
	m.Set("author", v.Author)
	m.Set("subject", v.Subject)
	m.Set("kind", kind)
	m.Set("changed", strList(fields))
	return m
}

// changed names what differs between two versions: the fields that are
// content, then "body". A version that only moved bookkeeping (a date, a
// stamp, a review) changed none.
func changed(a, b *content) []string {
	var out []string
	for _, k := range fieldsOf(a, b) {
		x, y := show(a.meta, k), show(b.meta, k)
		if x != y {
			out = append(out, k)
		}
	}
	if clean(a.body) != clean(b.body) {
		out = append(out, "body")
	}
	return out
}

// fieldsOf is every content field either version holds, the newer one's order
// first.
func fieldsOf(a, b *content) []string {
	var out []string
	for _, m := range []*omap.Map{b.meta, a.meta} {
		for _, k := range m.Keys() {
			if !trust.Bookkeeping(k) && !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	return out
}

// show renders a field's value for a reader; "" when it has none.
func show(meta *omap.Map, key string) string {
	v, _ := meta.Get(key)
	return render(v)
}

func render(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []any:
		parts := make([]string, 0, len(x))
		for _, e := range x {
			parts = append(parts, render(e))
		}
		return strings.Join(parts, ", ")
	case *omap.Map:
		parts := make([]string, 0, x.Len())
		for _, k := range x.Keys() {
			e, _ := x.Get(k)
			parts = append(parts, k+": "+render(e))
		}
		return strings.Join(parts, " · ")
	}
	return values.Str(v)
}

func clean(body string) string { return strings.TrimRight(canon.NormalizeNewlines(body), "\n") }

// sinceReview compares the entity now with the version its newest review saw.
// nil when that review still holds, or there is none. note is why git shows no
// version at all, "" when it shows some.
func (s *snapshot) sinceReview(node index.Node, rtype *schema.ResolvedType, current *content,
	versions []gitlog.Version, parsed []*content, more bool, note string) any {
	newest := trust.NewestReview(current.meta)
	if newest < 0 {
		return nil
	}
	event := trust.Events(current.meta)[newest]
	rev, _ := event.Get("rev")
	unmarked := !values.Present(rev)
	if !s.superseded(node) && !unmarked {
		return nil
	}
	by, _ := event.Get("by")
	at, _ := event.Get("at")
	m := omap.New()
	m.Set("by", values.Str(by))
	m.Set("at", values.Str(at))
	for i, p := range parsed {
		if unmarked || p == nil || trust.Rev(rtype, p.meta, p.body) != values.Str(rev) {
			continue
		}
		m.Set("found", true)
		m.Set("commit", versions[i].Commit)
		m.Set("date", versions[i].Date)
		fields := []any{}
		for _, k := range fieldsOf(p, current) {
			before, after := show(p.meta, k), show(current.meta, k)
			if before == after {
				continue
			}
			f := omap.New()
			f.Set("name", k)
			f.Set("before", before)
			f.Set("after", after)
			fields = append(fields, f)
		}
		lines, cut := diffLines(clean(p.body), clean(current.body))
		m.Set("fields", fields)
		m.Set("body", lines)
		m.Set("cut", cut)
		return m
	}
	m.Set("found", false)
	m.Set("holds", !s.superseded(node))
	why := "No commit of this file holds the version reviewed. It was reviewed before it was committed, or under another file name"
	switch {
	case unmarked:
		why = "The review names no version of the content, so nothing says what changed after it"
	case note != "":
		why = note
	case more:
		why = "The version reviewed is older than the commits read here"
	}
	m.Set("reason", why)
	return m
}

// diffLines is the line difference between two bodies, as kept (" "), removed
// ("-") and added ("+") lines in order, unchanged runs shortened to the two
// lines around a change. cut reports that lines were left out at the end.
func diffLines(a, b string) (lines []any, cut bool) {
	lines = []any{}
	if a == b {
		return lines, false
	}
	x, y := strings.Split(a, "\n"), strings.Split(b, "\n")
	if len(x)*len(y) > diffCells {
		return lines, true
	}
	// The longest common subsequence, by suffix lengths.
	lcs := make([][]int32, len(x)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(y)+1)
	}
	for i := len(x) - 1; i >= 0; i-- {
		for j := len(y) - 1; j >= 0; j-- {
			if x[i] == y[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	type line struct{ op, text string }
	var all []line
	i, j := 0, 0
	for i < len(x) || j < len(y) {
		switch {
		case i < len(x) && j < len(y) && x[i] == y[j]:
			all = append(all, line{" ", x[i]})
			i, j = i+1, j+1
		case j == len(y) || (i < len(x) && lcs[i+1][j] >= lcs[i][j+1]):
			all = append(all, line{"-", x[i]})
			i++
		default:
			all = append(all, line{"+", y[j]})
			j++
		}
	}
	near := make([]bool, len(all))
	for k, l := range all {
		if l.op != " " {
			for n := max(0, k-2); n <= min(len(all)-1, k+2); n++ {
				near[n] = true
			}
		}
	}
	skipped := false
	for k, l := range all {
		if !near[k] {
			skipped = true
			continue
		}
		if len(lines) == diffLimit {
			return lines, true
		}
		if skipped && len(lines) > 0 {
			lines = append(lines, diffLine("…", ""))
		}
		skipped = false
		lines = append(lines, diffLine(l.op, l.text))
	}
	return lines, false
}

func diffLine(op, text string) *omap.Map {
	m := omap.New()
	m.Set("op", op)
	m.Set("text", text)
	return m
}
