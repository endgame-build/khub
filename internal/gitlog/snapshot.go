// snapshot.go is the point-in-time read behind the global `--at <ref|date>`:
// the workspace's tree as one commit holds it, written to a scratch directory
// every read verb can run against unchanged. The graph is derived, never
// stored, so a past commit's tree is all a past projection needs.
//
// Every git call here is read-only, like the rest of the package: the live
// repository, its index and its working tree are never touched.

package gitlog

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/schema"
)

// Snapshot is a workspace materialized from one commit.
type Snapshot struct {
	Root   string // the scratch workspace root the read verbs run against
	Commit string // the full commit id the tree came from
	live   string // the live workspace root, where git runs
}

// History is where a snapshot's commit dates come from: the live repository,
// read up to the snapshot's commit. Relative paths agree, since Root mirrors
// the live workspace directory.
func (s *Snapshot) History() History { return History{Dir: s.live, Rev: s.Commit} }

// Close removes the scratch directory.
func (s *Snapshot) Close() error { return os.RemoveAll(s.Root) }

var reAtDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// AtDate reports whether an `--at` value is spelled as a date, YYYY-MM-DD,
// and if so whether it is a real one. A date wins over a ref of the same
// spelling; `refs/tags/<name>` names the ref.
func AtDate(spec string) (d time.Time, isDate bool, valid bool) {
	if !reAtDate.MatchString(spec) {
		return time.Time{}, false, false
	}
	d, err := time.ParseInLocation("2006-01-02", spec, time.UTC)
	if err != nil {
		return time.Time{}, true, false
	}
	return d, true, true
}

// gitNoFetch runs git for the snapshot: read-only, and never reaching the
// network. GIT_NO_LAZY_FETCH stops a partial clone from fetching a blob it
// lacks; the snapshot then reports the blob missing instead.
func gitNoFetch(ctx context.Context, dir string, stdin io.Reader, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
	cmd.Stdin = stdin
	return cmd
}

// runNoFetch is run through gitNoFetch: stdout and the exit code, err only
// when git could not be started (or ctx ended).
func runNoFetch(ctx context.Context, dir string, args ...string) (string, int, error) {
	cmd := gitNoFetch(ctx, dir, nil, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return "", 0, ctx.Err()
	}
	if runErr != nil {
		var exit *exec.ExitError
		if !errors.As(runErr, &exit) {
			return "", 0, runErr
		}
		return out.String(), exit.ExitCode(), nil
	}
	return out.String(), 0, nil
}

// ResolveAt turns an `--at` value into a full commit id. A date is the last
// commit on HEAD's first-parent line committed at or before the end of that
// day, UTC; anything else is a git revision (commit, branch, tag, HEAD~3).
func ResolveAt(ctx context.Context, root, spec string) (string, error) {
	if _, code, err := runNoFetch(ctx, root, "rev-parse", "--show-prefix"); err != nil {
		return "", err
	} else if code != 0 {
		return "", errs.NoGitHistory()
	}
	if d, isDate, valid := AtDate(spec); isDate {
		if !valid {
			return "", errs.UnknownRevision(spec)
		}
		ok, err := hasHistory(Head(root))
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errs.NoGitHistory()
		}
		before := d.Format("2006-01-02") + " 23:59:59 +0000"
		out, code, err := runNoFetch(ctx, root, "rev-list", "-1", "--first-parent", "--before="+before, "HEAD")
		if err != nil {
			return "", err
		}
		commit := strings.TrimSpace(out)
		if code != 0 || commit == "" {
			return "", errs.NoCommitBefore(spec)
		}
		return commit, nil
	}
	// A leading '-' would read as an option; no revision is spelled that way.
	if spec == "" || strings.HasPrefix(spec, "-") {
		return "", errs.UnknownRevision(spec)
	}
	out, code, err := runNoFetch(ctx, root, "rev-parse", "--verify", "--quiet", spec+"^{commit}")
	if err != nil {
		return "", err
	}
	commit := strings.TrimSpace(out)
	if code == 0 && commit != "" {
		return commit, nil
	}
	// A repository with no commit at all has nothing --at could name.
	refs, _, err := runNoFetch(ctx, root, "rev-list", "-1", "--all")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(refs) == "" {
		return "", errs.NoGitHistory()
	}
	return "", errs.UnknownRevision(spec)
}

// Materialize writes the workspace directory's tree at the commit `spec`
// names into dir, an empty scratch directory the caller made and removes
// (Snapshot.Close). root is the live workspace root; the snapshot takes the
// same directory within the repository, so a workspace nested in a larger
// repository is read from the same place.
//
// The tree is read blob by blob (`ls-tree` + `cat-file --batch`), not through
// `git archive`, so export-ignore and export-subst attributes cannot drop or
// rewrite a file. Blobs are streamed to disk as stored: no smudge filter, no
// line-ending conversion. Only the files a khub scan can open get their
// content: everything under .khub/, and the md, json, jsonl and yaml files
// .khub/ignore does not exclude. Every other file is read by nothing and
// left out, unless a relation may name a file by path (policy `path_refs`):
// then each is created empty, so its existence still counts. A
// submodule becomes an empty directory, as a checkout without
// `--recurse-submodules` leaves it. Every write goes through an os.Root and
// refuses to replace a file, so neither a crafted tree nor two paths a
// case-insensitive filesystem folds together can place or merge a file
// silently.
func Materialize(ctx context.Context, root, spec, dir string) (*Snapshot, error) {
	commit, err := ResolveAt(ctx, root, spec)
	if err != nil {
		return nil, err
	}
	prefixOut, _, err := runNoFetch(ctx, root, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(prefixOut)
	if _, code, err := runNoFetch(ctx, root, "cat-file", "-e", commit+":"+prefix+".khub"); err != nil {
		return nil, err
	} else if code != 0 {
		return nil, errs.NoWorkspaceAt(spec)
	}
	listing, code, err := runNoFetch(ctx, root, "ls-tree", "-r", "-z", "--full-tree", commit+":"+prefix)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, errs.SnapshotError(spec, "git ls-tree could not list the commit's tree")
	}
	entries, err := parseTree(spec, listing)
	if err != nil {
		return nil, err
	}
	ignore := commitIgnore(ctx, root, commit, prefix)

	scratch, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = scratch.Close() }()
	w := &treeWriter{spec: spec, root: scratch, dirs: map[string]bool{".": true}}
	var content, rest []treeEntry
	for _, e := range entries {
		switch {
		case e.kind == "commit": // a submodule: an empty directory, as a plain checkout leaves it
			if err := w.mkdir(e.path); err != nil {
				return nil, err
			}
		case e.kind != "blob":
			continue
		case e.mode == "120000" || scanReads(e.path, ignore):
			content = append(content, e)
		default:
			rest = append(rest, e)
		}
	}
	if err := w.stream(ctx, root, content); err != nil {
		return nil, err
	}
	// The rest only matter by existence, and only to a relation that may name
	// a file by path. Without one they are left out; a schema that does not
	// load gets them all, so the scan sees what a checkout holds.
	if len(rest) > 0 && namesFiles(dir) {
		for _, e := range rest {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err := w.write(e, nil); err != nil {
				return nil, err
			}
		}
	}
	return &Snapshot{Root: dir, Commit: commit, live: root}, nil
}

// namesFiles reports whether the snapshot's schema lets a relation name a
// workspace file by path (policy `path_refs`), or cannot say.
func namesFiles(dir string) bool {
	resolved, err := introspect.LoadSchema(dir)
	if err != nil {
		return true
	}
	for _, name := range resolved.Types.Keys() {
		t, _ := resolved.Types.Get(name)
		for _, pred := range t.Relations.Keys() {
			if r, _ := t.Relations.Get(pred); r.ByPath {
				return true
			}
		}
	}
	return false
}

// scanExts are the extensions a khub scan opens: every storage format, and
// the files `check` reads for a misplaced entity.
var scanExts = map[string]bool{".md": true, ".json": true, ".jsonl": true, ".yaml": true, ".yml": true}

// scanReads reports whether a khub read can open rel's content.
func scanReads(rel string, ignore *schema.IgnoreList) bool {
	if strings.HasPrefix(rel, ".khub/") {
		return true
	}
	if !scanExts[strings.ToLower(path.Ext(rel))] {
		return false
	}
	return !ignore.Ignored(rel, false)
}

// commitIgnore is the commit's .khub/ignore. One that cannot be read or
// parsed ignores nothing here: the copy is then complete, and the scan reports
// the bad file itself.
func commitIgnore(ctx context.Context, root, commit, prefix string) *schema.IgnoreList {
	out, code, err := runNoFetch(ctx, root, "cat-file", "blob", commit+":"+prefix+schema.IgnoreFile)
	if err != nil || code != 0 {
		return nil
	}
	list, err := schema.ParseIgnore(out)
	if err != nil {
		return nil
	}
	return list
}

// treeEntry is one `ls-tree -r` line.
type treeEntry struct {
	mode, kind, oid, path string
}

// parseTree reads `ls-tree -r -z` output: `<mode> <type> <oid>\t<path>\0`.
func parseTree(spec, listing string) ([]treeEntry, error) {
	var out []treeEntry
	for _, line := range strings.Split(listing, "\x00") {
		if line == "" {
			continue
		}
		head, p, found := strings.Cut(line, "\t")
		fields := strings.Fields(head)
		if !found || len(fields) != 3 {
			return nil, errs.SnapshotError(spec, fmt.Sprintf("unexpected git ls-tree line %q", line))
		}
		if !safeTreePath(p, runtime.GOOS == "windows") {
			return nil, errs.SnapshotError(spec,
				fmt.Sprintf("the commit holds the path '%s', which cannot be written here", p))
		}
		out = append(out, treeEntry{mode: fields[0], kind: fields[1], oid: fields[2], path: p})
	}
	return out, nil
}

// safeTreePath accepts a relative slash path with no empty, `.`, `..` or
// `.git` component: what a checkout would accept. A backslash is an ordinary
// filename character except on Windows, where it is a separator.
func safeTreePath(p string, windows bool) bool {
	if p == "" || strings.HasPrefix(p, "/") || (windows && strings.Contains(p, "\\")) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		switch strings.ToLower(part) {
		case "", ".", "..", ".git":
			return false
		}
	}
	return true
}

// treeWriter writes entries under the scratch root, remembering the
// directories it has made.
type treeWriter struct {
	spec string
	root *os.Root
	dirs map[string]bool
}

func (w *treeWriter) mkdir(dir string) error {
	if w.dirs[dir] {
		return nil
	}
	if err := w.root.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for d := dir; d != "." && !w.dirs[d]; d = path.Dir(d) {
		w.dirs[d] = true
	}
	return nil
}

// write creates one blob's file from r (nil: an empty file). A symlink is
// recreated as one, so khub refuses it exactly as it would in a checkout;
// where the platform cannot create it, the link text is written as a file,
// which is what git itself does with core.symlinks=false. A name that already
// exists is two tree paths the filesystem folds together, and refuses.
func (w *treeWriter) write(e treeEntry, r io.Reader) error {
	if err := w.mkdir(path.Dir(e.path)); err != nil {
		return err
	}
	if e.mode == "120000" && r != nil {
		target, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		err = w.root.Symlink(string(target), e.path)
		if err == nil {
			return nil
		}
		if errors.Is(err, fs.ErrExist) {
			return w.folded(e.path)
		}
		r = bytes.NewReader(target)
	}
	perm := fs.FileMode(0o644)
	if e.mode == "100755" {
		perm = 0o755
	}
	f, err := w.root.OpenFile(e.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if errors.Is(err, fs.ErrExist) {
		return w.folded(e.path)
	}
	if err != nil {
		return err
	}
	if r != nil {
		if _, err := io.Copy(f, r); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

func (w *treeWriter) folded(p string) error {
	return errs.SnapshotError(w.spec, fmt.Sprintf(
		"the commit holds two paths this filesystem cannot tell apart ('%s' and one differing only in case)", p))
}

// stream pipes every content blob through one `git cat-file --batch` and
// streams each record straight to its file.
func (w *treeWriter) stream(ctx context.Context, gitDir string, blobs []treeEntry) error {
	if len(blobs) == 0 {
		return nil
	}
	var stdin bytes.Buffer
	for _, b := range blobs {
		stdin.WriteString(b.oid + "\n")
	}
	cmd := gitNoFetch(ctx, gitDir, &stdin, "cat-file", "--batch")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	readErr := w.readBatch(bufio.NewReaderSize(stdout, 64<<10), blobs)
	if readErr != nil {
		_, _ = io.Copy(io.Discard, stdout)
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readErr != nil {
		return readErr
	}
	if waitErr != nil {
		return errs.SnapshotError(w.spec, "git cat-file failed: "+
			strings.TrimSpace(waitErr.Error()+" "+stderr.String()))
	}
	return nil
}

// readBatch reads one `<oid> <type> <size>\n<content>\n` record per blob, in
// the order they were asked for. A blob the repository lacks (a partial
// clone, with fetching off) answers `<oid> missing`.
func (w *treeWriter) readBatch(r *bufio.Reader, blobs []treeEntry) error {
	for _, b := range blobs {
		header, err := r.ReadString('\n')
		if err != nil {
			return errs.SnapshotError(w.spec, "git cat-file stopped before '"+b.path+"'")
		}
		fields := strings.Fields(header)
		if len(fields) == 2 && fields[1] == "missing" {
			return errs.SnapshotError(w.spec, fmt.Sprintf(
				"the repository lacks the content of '%s' (a partial or shallow clone?); fetch it, or pick another commit", b.path))
		}
		if len(fields) != 3 || fields[1] != "blob" {
			return errs.SnapshotError(w.spec, fmt.Sprintf("unexpected git cat-file header %q", strings.TrimSpace(header)))
		}
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return errs.SnapshotError(w.spec, fmt.Sprintf("unexpected git cat-file header %q", strings.TrimSpace(header)))
		}
		lr := &io.LimitedReader{R: r, N: size}
		if err := w.write(b, lr); err != nil {
			return err
		}
		if _, err := io.Copy(io.Discard, lr); err != nil || lr.N != 0 {
			return errs.SnapshotError(w.spec, "git cat-file stopped inside '"+b.path+"'")
		}
		if _, err := r.Discard(1); err != nil { // the record's trailing newline
			return errs.SnapshotError(w.spec, "git cat-file stopped inside '"+b.path+"'")
		}
	}
	return nil
}
