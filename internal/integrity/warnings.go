// Session-scoped write warnings: the gating `check` findings a write verb
// reports on stderr, once per session.
//
// An agent has to choose to run `check`, and mostly does not. So a write in a
// session also runs it and names each finding the session has not been shown:
// the first write surfaces the workspace's standing debt, a later one only what
// changed. Advisory, never blocking — the write has already landed, and the
// `validate` and `check` gates themselves are untouched.

package integrity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/fsio"
)

// Finding is one gating `check` finding flattened to a line. Rule is the name
// of the `check --format json` bucket that holds it, so an agent can map a
// warning back to the gate's own report.
type Finding struct {
	ID      string // the entity id, or the path, type, or alias the finding is about
	Rule    string
	Message string
}

// Line is `<id> › <rule>: <message>`, with every control character in the id
// and the message escaped. Both carry workspace text (a file name, an alias, a
// template's reason), and the line goes to stderr raw in every format, so a
// newline would forge a second warning and an escape sequence would reach the
// terminal.
func (f Finding) Line() string {
	return escapeControls(f.ID) + " › " + f.Rule + ": " + escapeControls(f.Message)
}

// escapeControls spells a C0 or C1 control, DEL, or a Unicode line or
// paragraph separator as `\xNN` or `\uNNNN`; everything else passes through.
func escapeControls(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		case (r >= 0x80 && r <= 0x9f) || r == 0x2028 || r == 0x2029:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Findings flattens every finding that fails the gate, in the bucket order of
// `check --format json`. Informational findings (orphans outside strict mode,
// optional drafted singletons, thin bodies) are not findings here: they never
// fail `check`, so they are not debt a write should nag about.
func (r *CheckReport) Findings() []Finding {
	out := []Finding{}
	add := func(id, rule, message string) { out = append(out, Finding{id, rule, message}) }
	for _, i := range r.Incomplete {
		gaps := append(append([]string{}, i.MissingFields...), i.MissingRelations...)
		add(i.ID(), "incomplete", "missing "+strings.Join(gaps, ", "))
	}
	if r.Strict {
		for _, o := range r.Orphans {
			add(o, "orphans", "no inbound or outbound relation")
		}
	}
	for _, d := range r.Dangling {
		add(d.ID(), "dangling", d.Predicate+" -> '"+d.Target+"' does not resolve")
	}
	for _, s := range r.Strays {
		add(s, "strays", "not a valid entity of the type whose layout holds it")
	}
	for _, s := range r.StrayTemplates {
		add(s, "stray_templates", "no type declares it and none is named for it")
	}
	for _, s := range r.MissingTemplates {
		typeName, path, _ := strings.Cut(s, ": ")
		add(typeName, "missing_templates", "declared template "+path+" does not exist")
	}
	for _, t := range r.TemplateInvalid {
		add(t.ID(), "template_invalid", t.Reason)
	}
	for _, b := range r.BodyShape {
		add(b.ID(), "body_shape", b.Reason)
	}
	for _, m := range r.Misplaced {
		add(m.Path, "misplaced", "declares type '"+m.Type+"' but sits outside "+m.Expected+" — no command can see it")
	}
	for _, m := range r.Malformed {
		add(m, "malformed", "does not parse as an entity")
	}
	for _, cycle := range r.Cycles {
		if len(cycle) == 0 {
			continue
		}
		add(cycle[0], "cycles", "cycle "+strings.Join(cycle, " -> "))
	}
	for _, name := range r.MissingSingletons {
		add(name, "missing_singletons", "required singleton is missing")
	}
	for _, name := range r.DraftRequiredSingletons {
		add(name, "draft_required_singletons", "required singleton is unpublished (draft: true)")
	}
	for _, c := range r.AliasConflicts {
		add(c.Alias, "alias_conflicts", "names more than one entity: "+strings.Join(c.Claimants, ", "))
	}
	return out
}

// SessionDir holds one file per session: the digests of the findings that
// session has been shown. It sits under .khub/generated/, which is gitignored
// and skipped by every scan.
func SessionDir(root string) string {
	return filepath.Join(root, ".khub", "generated", "sessions")
}

// sessionTTL is how long an untouched session's record is kept. A record is
// rewritten on every write in its session, so one this old belongs to a
// session that ended.
const sessionTTL = 30 * 24 * time.Hour

// SessionWarnings runs `check` (default mode) and returns the findings the
// named session has not been shown yet, then records the current set as shown.
// Recording the current set, not the union, means a finding that is fixed and
// later comes back is reported again.
//
// The check runs outside the workspace lock, so a concurrent writer never
// waits on it; the lock covers only reading, comparing and recording the
// session's record, so two writes in one session never both report the same
// finding. Records untouched for sessionTTL are pruned under the same lock.
func SessionWarnings(root, session string) ([]Finding, error) {
	report, err := Check(root, false)
	if err != nil {
		return nil, err
	}
	findings := report.Findings()
	return fsio.Locked(root, func() ([]Finding, error) {
		dir := SessionDir(root)
		sum := sha256.Sum256([]byte(session))
		path := filepath.Join(dir, hex.EncodeToString(sum[:16]))
		seen := map[string]bool{}
		prior, err := fsio.ReadFile(root, path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return nil, err
		default:
			for line := range strings.SplitSeq(string(prior), "\n") {
				seen[line] = true
			}
		}
		fresh := []Finding{}
		var record strings.Builder
		for _, f := range findings {
			digest := findingDigest(f)
			if !seen[digest] {
				fresh = append(fresh, f)
			}
			record.WriteString(digest + "\n")
		}
		if err := fsio.MkdirAll(root, dir); err != nil {
			return nil, err
		}
		if err := fsio.AtomicWriteIn(root, path, []byte(record.String())); err != nil {
			return nil, err
		}
		pruneSessions(root, dir, path, time.Now().Add(-sessionTTL))
		return fresh, nil
	})
}

// pruneSessions removes session records last written before cutoff, keeping
// keep. Best effort: a record that cannot be read or removed stays, and costs
// nothing but its few bytes.
func pruneSessions(root, dir, keep string, cutoff time.Time) {
	entries, err := fsio.ReadDir(root, dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		if e.IsDir() || path == keep {
			continue
		}
		if info, ierr := e.Info(); ierr == nil && info.ModTime().Before(cutoff) {
			_ = fsio.Remove(root, path, false)
		}
	}
}

// findingDigest keys a finding by its whole line, hashed so a path or a reason
// holding a newline cannot split the record.
func findingDigest(f Finding) string {
	sum := sha256.Sum256([]byte(f.Line()))
	return hex.EncodeToString(sum[:])
}
