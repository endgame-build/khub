// Package doctor is `khub doctor`: a read-only self-diagnosis of what every
// other command assumes before it starts — a workspace, git, a schema that
// resolves, a readable provenance stamp, a lock a write can take, storage
// shaped and writable the way the schema lays it out, and a workspace every
// file of which `check` can read. `validate` and `check` judge the entities
// of a working workspace; doctor judges whether the workspace works.
//
// Doctor writes nothing on macOS and Linux: whether a write would be allowed
// is asked of the kernel (faccessat with the effective ids), and an existing
// lock file is opened for writing and closed, never locked. Windows has no
// such question that sees ACLs, so there the lock check creates and removes
// one probe file, and only inside .khub/generated (gitignored); storage
// writability is not checked there.
package doctor

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
	"github.com/endgame-build/khub/internal/gitlog"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/integrity"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/schema"
	"github.com/endgame-build/khub/internal/workspace"
)

// The statuses a check reports. Only Fail fails the run.
const (
	OK   = "ok"
	Warn = "warn"
	Fail = "fail"
	Skip = "skip"
)

// Check is one diagnosis. When Err is set, the CLI renders it the way every
// other command renders that error, after Message as context when Message is
// set; a Located error's code replaces Code.
type Check struct {
	Name    string
	Status  string
	Code    string
	Message string
	Err     error
}

// Report is every check in its fixed order. Root is "" when no workspace
// was found.
type Report struct {
	Root   string
	Checks []Check
}

// Passed reports whether no check failed. A warning does not fail.
func (r *Report) Passed() bool {
	for _, c := range r.Checks {
		if c.Status == Fail {
			return false
		}
	}
	return true
}

// Names lists the checks in the order Run reports them.
var Names = []string{"workspace", "git", "schema", "provenance", "lock", "layout", "read"}

// Run diagnoses the workspace at root. rootErr is what resolving the
// workspace returned; when it is set, every later check is skipped.
func Run(root string, rootErr error) *Report {
	if rootErr != nil {
		r := &Report{Checks: []Check{{Name: "workspace", Status: Fail, Code: "no_workspace", Err: rootErr}}}
		for _, name := range Names[1:] {
			r.Checks = append(r.Checks, Check{Name: name, Status: Skip, Message: "Skipped: no workspace"})
		}
		return r
	}
	r := &Report{Root: root}
	r.Checks = append(r.Checks, Check{Name: "workspace", Status: OK, Message: filepath.ToSlash(root)})
	r.Checks = append(r.Checks, checkGit(root))
	resolved, layers, err := introspect.LoadSchemaLayers(root)
	if err != nil {
		r.Checks = append(r.Checks, Check{Name: "schema", Status: Fail, Code: "schema_error", Err: err})
	} else {
		r.Checks = append(r.Checks, Check{Name: "schema", Status: OK, Message: fmt.Sprintf(
			"%s resolved from %s", plural(resolved.Types.Len(), "type"), strings.Join(layers, ", "))})
	}
	r.Checks = append(r.Checks, checkProvenance(root))
	r.Checks = append(r.Checks, checkLock(root))
	if resolved == nil {
		r.Checks = append(r.Checks,
			Check{Name: "layout", Status: Skip, Message: "Skipped: the schema does not resolve"},
			Check{Name: "read", Status: Skip, Message: "Skipped: the schema does not resolve"})
		return r
	}
	layout := checkLayout(root, resolved)
	r.Checks = append(r.Checks, layout...)
	if layout[0].Status == Fail {
		r.Checks = append(r.Checks, Check{Name: "read", Status: Skip, Message: "Skipped: fix the layout first"})
	} else {
		r.Checks = append(r.Checks, checkRead(root))
	}
	return r
}

// checkGit asks git what stale and backfill will meet: no git at all stops
// both; no repository or no commit leaves them without dates.
func checkGit(root string) Check {
	state, err := gitlog.Probe(root)
	switch {
	case err != nil && errors.Is(err, exec.ErrNotFound):
		return Check{Name: "git", Status: Fail, Code: "git_unavailable",
			Message: "git is not on the PATH; stale and backfill cannot run without it. Install git"}
	case err != nil:
		return Check{Name: "git", Status: Fail, Code: "git_unavailable",
			Message: "Cannot run git (" + err.Error() + "); stale and backfill cannot run without it"}
	case state == gitlog.RepoGitBroken:
		return Check{Name: "git", Status: Fail, Code: "git_unavailable",
			Message: "git runs but `git --version` fails; stale and backfill cannot run with it. Repair or reinstall git"}
	case state == gitlog.RepoNone:
		return Check{Name: "git", Status: Warn, Code: "not_a_git_repository", Message: "The workspace is not a git " +
			"repository; stale and backfill read no dates from git until it is one with a commit"}
	case state == gitlog.RepoNoCommits:
		return Check{Name: "git", Status: Warn, Code: "no_git_history", Message: "The workspace's git repository " +
			"has no commit; stale and backfill read no dates from git until it has one"}
	}
	return Check{Name: "git", Status: OK, Message: "git found; the workspace has commit history"}
}

// checkProvenance reads .khub/config.yaml the way every command that loads it
// does — the preset stamp, then the stale threshold — and then holds the
// stamp against the preset upgrade would read.
func checkProvenance(root string) Check {
	unreadable := func(err error) Check {
		c := Check{Name: "provenance", Status: Fail, Code: "provenance_unreadable", Err: err}
		var located *errs.Located
		var pe *fs.PathError
		if !errors.As(err, &located) && !errors.As(err, &pe) {
			c.Message = ".khub/config.yaml" // the error does not name the file
		}
		return c
	}
	prov, err := workspace.Provenance(root)
	if err != nil {
		return unreadable(err)
	}
	if _, err := workspace.StaleDays(root); err != nil {
		return unreadable(err)
	}
	preset, _ := prov.Get("preset")
	version, _ := prov.Get("version")
	var missing []string
	if preset == "" {
		missing = append(missing, "preset")
	}
	if version == "" {
		missing = append(missing, "version")
	}
	if len(missing) > 0 {
		return Check{Name: "provenance", Status: Warn, Code: "provenance_incomplete", Message: fmt.Sprintf(
			".khub/config.yaml records no %s; upgrade cannot tell what to refresh from", strings.Join(missing, " or "))}
	}
	shipped, err := workspace.ShippedVersion(root, preset.(string))
	if err != nil {
		return Check{Name: "provenance", Status: Warn, Code: "provenance_unknown_preset",
			Message: "upgrade cannot refresh this workspace", Err: err}
	}
	if newer(version.(string), shipped) {
		return Check{Name: "provenance", Status: Warn, Code: "provenance_newer", Message: fmt.Sprintf(
			"The workspace records preset %s %s, newer than the %s this khub ships; upgrade khub before upgrading the workspace",
			preset, version, shipped)}
	}
	return Check{Name: "provenance", Status: OK, Message: fmt.Sprintf("preset %s, version %s", preset, version)}
}

// newer reports whether dotted version a is above b. A version with a
// non-numeric part is never judged.
func newer(a, b string) bool {
	pa, oka := numeric(a)
	pb, okb := numeric(b)
	if !oka || !okb {
		return false
	}
	for i := 0; i < max(len(pa), len(pb)); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func numeric(v string) ([]int, bool) {
	var out []int
	for _, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// nearestDir walks rel's components from the workspace root and returns the
// deepest one that exists, which is a directory, or the first one that exists
// and is not a directory (notDir). A symlink component is refused as every
// storage operation refuses it.
func nearestDir(r *os.Root, root, rel string) (existing, notDir string, err error) {
	existing = "."
	part := ""
	for _, name := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		part = filepath.Join(part, name)
		info, err := r.Lstat(part)
		if errors.Is(err, fs.ErrNotExist) {
			return existing, "", nil
		}
		if err != nil {
			return "", "", err
		}
		if err := fsio.RefuseLink(info, filepath.Join(root, part)); err != nil {
			return "", "", err
		}
		if !info.IsDir() {
			return existing, part, nil
		}
		existing = part
	}
	return existing, "", nil
}

// checkLock proves a write could take the workspace lock without taking it.
func checkLock(root string) Check {
	lockPath := fsio.WorkspaceLockPath(root)
	shown := relSlash(root, lockPath)
	fail := func(err error) Check {
		return Check{Name: "lock", Status: Fail, Code: "lock_unwritable", Message: shown, Err: err}
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return fail(err)
	}
	defer r.Close()
	rel, _ := filepath.Rel(root, lockPath)
	dir, notDir, err := nearestDir(r, root, filepath.Dir(rel))
	if err != nil {
		return fail(err)
	}
	if notDir != "" {
		return Check{Name: "lock", Status: Fail, Code: "lock_unwritable", Message: fmt.Sprintf(
			"%s is not a directory; the workspace lock lives under it", filepath.ToSlash(notDir))}
	}
	if dir == filepath.Dir(rel) {
		if info, err := r.Lstat(rel); err == nil {
			if err := fsio.RefuseLink(info, lockPath); err != nil {
				return fail(err)
			}
			if !info.Mode().IsRegular() {
				return Check{Name: "lock", Status: Fail, Code: "lock_unwritable", Message: fmt.Sprintf(
					"%s is not a regular file; every write opens it as the workspace lock", shown)}
			}
			// Opening for writing changes nothing on disk, and is exactly
			// what a write does before it locks.
			f, err := r.OpenFile(rel, os.O_RDWR, 0)
			if err != nil {
				return fail(fsio.FileInPath(err, root, lockPath))
			}
			_ = f.Close()
			return Check{Name: "lock", Status: OK, Message: shown + " opens for writing"}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return fail(err)
		}
	}
	// The lock does not exist yet: the first write creates it and the
	// directories above it, so the nearest existing directory must take a
	// new entry.
	checked, err := writableDir(filepath.Join(root, dir))
	if err != nil {
		return fail(err)
	}
	if !checked {
		generated := filepath.Join(".khub", "generated")
		if dir != generated && !strings.HasPrefix(dir, generated+string(filepath.Separator)) {
			return Check{Name: "lock", Status: Skip, Message: fmt.Sprintf(
				"Skipped: %s does not exist, and on this platform doctor probes for writing only inside .khub/generated",
				filepath.ToSlash(generated))}
		}
		if err := probeDir(r, root, dir); err != nil {
			return fail(err)
		}
	}
	return Check{Name: "lock", Status: OK, Message: shown + " can be created"}
}

// probeDir creates one exclusive probe file in dir and removes it, for a
// platform where writableDir cannot answer. An error names dir, never the
// probe, so the message does not carry a process id.
func probeDir(r *os.Root, root, dir string) error {
	for n := 0; ; n++ {
		name := filepath.Join(dir, fmt.Sprintf(".doctor-probe-%d-%d", os.Getpid(), n))
		f, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if errors.Is(err, fs.ErrExist) && n < 8 {
			continue
		}
		if err != nil {
			var pe *fs.PathError
			if errors.As(err, &pe) {
				return &fs.PathError{Op: pe.Op, Path: filepath.Join(root, dir), Err: pe.Err}
			}
			return err
		}
		_ = f.Close()
		return r.Remove(name)
	}
}

// checkLayout holds every declared type's storage against its layout: the
// directories its storage path runs through are directories, each storage
// root that exists is the shape the layout reads (a directory for file and
// folder layouts and for a template's prefix, a file for a singleton or a
// collection), a write could create an entry where the type's entities go,
// and the type then scans as every read scans it. A missing storage path is
// an empty type, not a finding.
func checkLayout(root string, resolved *schema.ResolvedSchema) []Check {
	r, err := os.OpenRoot(root)
	if err != nil {
		return []Check{{Name: "layout", Status: Fail, Code: "layout_mismatch", Err: err}}
	}
	defer r.Close()
	var out []Check
	for _, name := range resolved.Types.Keys() {
		rtype, _ := resolved.Types.Get(name)
		if c, bad := layoutOf(r, root, resolved, rtype); bad {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return []Check{{Name: "layout", Status: OK, Message: fmt.Sprintf(
			"Storage matches the layout of every type (%s)", plural(resolved.Types.Len(), "type"))}}
	}
	return out
}

func layoutOf(r *os.Root, root string, resolved *schema.ResolvedSchema, rtype *schema.ResolvedType) (Check, bool) {
	failed := func(code string, err error) (Check, bool) {
		return Check{Name: "layout", Status: Fail, Code: code, Message: rtype.Name, Err: err}, true
	}
	wantFile := !rtype.Templated() &&
		(rtype.Storage.Layout == schema.LayoutCollection || rtype.Storage.Layout == schema.LayoutSingleton)
	layout := "the " + rtype.Storage.Layout + " layout"
	if rtype.Templated() {
		layout = "a path template"
	}
	for _, rel := range rtype.StorageRoots() {
		if rel == "" || resolved.Ignore.Ignored(rel, !wantFile) {
			continue
		}
		osRel := filepath.FromSlash(rel)
		dir, notDir, err := nearestDir(r, root, osRel)
		if err != nil {
			return failed("layout_mismatch", err)
		}
		switch {
		case notDir == osRel && !wantFile:
			return mismatch(rtype.Name, rel, "a directory", layout), true
		case notDir != "" && notDir != osRel:
			return mismatch(rtype.Name, filepath.ToSlash(notDir), "a directory", "the storage path "+rel), true
		case notDir == "" && dir == osRel && wantFile:
			return mismatch(rtype.Name, rel, "a file", layout), true
		}
		if notDir == osRel {
			info, err := r.Lstat(osRel)
			if err != nil {
				return failed("layout_mismatch", err)
			}
			if !info.Mode().IsRegular() {
				return mismatch(rtype.Name, rel, "a file", layout), true
			}
		}
		// Where a write would create an entry: the storage directory, the
		// directory holding a singleton or collection file, or, for what
		// does not exist yet, the nearest directory above it.
		if _, err := writableDir(filepath.Join(root, dir)); err != nil {
			return failed("storage_unwritable", err)
		}
	}
	if err := index.ScanOne(root, resolved, rtype); err != nil {
		return failed("layout_mismatch", err)
	}
	return Check{}, false
}

func mismatch(typ, rel, want, needs string) Check {
	return Check{Name: "layout", Status: Fail, Code: "layout_mismatch", Message: fmt.Sprintf(
		"%s: %s is not %s, which %s needs; move it aside or change the type's storage path",
		typ, rel, want, needs)}
}

// checkRead runs the whole of `check`'s read — every entity, the stray and
// template sweeps, and the misplaced-file walk over the workspace — and
// reports only whether it could read everything. Its findings are `check`'s.
func checkRead(root string) Check {
	if _, err := integrity.Check(root, false); err != nil {
		return Check{Name: "read", Status: Fail, Code: "read_error", Err: err}
	}
	return Check{Name: "read", Status: OK, Message: "check reads every file it walks"}
}

func relSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}
