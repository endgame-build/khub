// Package skill ports src/khub/core/skill.py: installing khub's agent skills
// by copying them out of the package. The Python module resolves its source
// from the wheel or a dev checkout; here the caller passes the skills tree as
// an fs.FS (the embedded skills FS, or os.DirFS over a checkout), so
// skills_dir()/skills_missing have no Go counterpart. Installed skills are
// managed copies, and the sync REPLACES each skill directory rather than
// overlaying it: a file khub no longer ships is removed, so a release that
// renames or drops a file does not leave the old copy beside the new one for
// the agent to read both (kb's _install_skills, which rmtree'd first).
package skill

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/fsio"
)

// TARGETS: where each agent family reads project-local skills. Order is
// contract — it fixes the default install order and the error-message list.
var (
	targetOrder = []string{"claude", "agents", "opencode"}
	targetDirs  = map[string]string{
		"claude":   ".claude/skills",
		"agents":   ".agents/skills",
		"opencode": ".opencode/skills",
	}
)

// TargetNames lists the known --target values in their declared order.
func TargetNames() []string { return append([]string(nil), targetOrder...) }

// GlobalDir is skill.global_dir: the machine-wide directory target reads,
// for --global. Only opencode's differs from its project path (XDG config
// root honoured).
func GlobalDir(target string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if target == "opencode" {
		base := filepath.Join(home, ".config")
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			base = xdg
		}
		return filepath.Join(base, "opencode", "skills"), nil
	}
	rel, ok := targetDirs[target]
	if !ok {
		return "", fmt.Errorf("unknown target %q", target)
	}
	return filepath.Join(home, rel), nil
}

// AvailableSkills is skill.available_skills: every skill the source tree
// ships, by directory name, sorted.
func AvailableSkills(source fs.FS) ([]string, error) {
	matches, err := fs.Glob(source, "*/SKILL.md")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, path.Dir(m))
	}
	sort.Strings(names)
	return names, nil
}

// Write is skill.SkillWrite: one destination file and what happened to it.
// Path is workspace-relative under project scope, absolute under --global;
// Action is "created" | "updated" | "unchanged" | "removed" — the last for a
// file under the skill directory that the shipped tree no longer carries.
type Write struct {
	Path   string
	Action string
}

// Report is skill.SkillReport: the outcome of one install. The CLI JSON
// contract is {scope, skills, dry_run, writes:[{path, action}]} with Scope
// "project" | "global".
type Report struct {
	Scope  string
	Skills []string
	Writes []Write
	DryRun bool
}

// Options carries install_skills' keyword arguments. Nil Skills/Targets mean
// "all", in shipped/declared order respectively.
type Options struct {
	Skills  []string
	Targets []string
	Global  bool
	DryRun  bool
}

// Install is skill.install_skills: copy the named skills from source into the
// target directories under root (or $HOME under Global). Every file is
// byte-compared before it is written, so a re-install reports "unchanged" and
// leaves mtimes alone; DryRun reports the same writes without touching disk.
// root is required for project scope ("" is Python's None) and unused under
// Global.
func Install(source fs.FS, root string, opt Options) (*Report, error) {
	if root == "" && !opt.Global {
		return nil, errs.New("missing_root", "A project-scope skill install needs a workspace root")
	}

	if !opt.Global && !opt.DryRun {
		return fsio.Locked(root, func() (*Report, error) { return install(source, root, opt) })
	}
	return install(source, root, opt)
}

// InstallHeld is Install for a caller that already holds the workspace lock
// (an upgrade running its tails inside its own lock). fsio.Locked is not
// re-entrant, so calling Install there would block forever.
func InstallHeld(source fs.FS, root string, opt Options) (*Report, error) {
	if root == "" && !opt.Global {
		return nil, errs.New("missing_root", "A project-scope skill install needs a workspace root")
	}
	return install(source, root, opt)
}

func install(source fs.FS, root string, opt Options) (*Report, error) {
	avail, err := AvailableSkills(source)
	if err != nil {
		return nil, err
	}
	wanted := avail
	if len(opt.Skills) > 0 {
		wanted = append([]string(nil), opt.Skills...)
	}
	if err := rejectUnknown(wanted, avail, "skill"); err != nil {
		return nil, err
	}
	chosen := targetOrder
	if len(opt.Targets) > 0 {
		chosen = append([]string(nil), opt.Targets...)
	}
	if err := rejectUnknown(chosen, targetOrder, "target"); err != nil {
		return nil, err
	}

	writes := []Write{}
	for _, target := range chosen {
		var destRoot string
		if opt.Global {
			destRoot, err = GlobalDir(target)
			if err != nil {
				return nil, err
			}
		} else {
			destRoot = filepath.Join(root, filepath.FromSlash(targetDirs[target]))
		}
		for _, name := range wanted {
			// Paths report absolute under --global: relative to $HOME they would
			// be byte-identical to a project install.
			base := root
			if opt.Global {
				base = ""
			}
			ws, serr := syncSkill(source, name, filepath.Join(destRoot, name), base, opt.DryRun)
			if serr != nil {
				return nil, serr
			}
			writes = append(writes, ws...)
			if !opt.Global && !opt.DryRun {
				// Ignore only what khub owns, never the whole skills dir.
				line := targetDirs[target] + "/" + name + "/"
				if gerr := appendGitignore(root, filepath.Join(root, ".gitignore"), line); gerr != nil {
					return nil, gerr
				}
			}
		}
	}

	scope := "project"
	if opt.Global {
		scope = "global"
	}
	return &Report{Scope: scope, Skills: wanted, Writes: writes, DryRun: opt.DryRun}, nil
}

// syncSkill is skill._sync_skill: copy one skill directory, one action per
// file, then remove what the shipped tree no longer carries. fs.WalkDir's
// sorted depth-first order equals Python's sorted(src.rglob("*")) — both are
// lexicographic over path components — and the removals follow in the same
// order over the destination.
func syncSkill(source fs.FS, name, dest, base string, dryRun bool) ([]Write, error) {
	var files []string
	err := fs.WalkDir(source, name, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	writes := make([]Write, 0, len(files))
	shipped := make(map[string]bool, len(files))
	for _, p := range files {
		rel := strings.TrimPrefix(p, name+"/")
		target := filepath.Join(dest, filepath.FromSlash(rel))
		shipped[target] = true
		payload, rerr := fs.ReadFile(source, p)
		if rerr != nil {
			return nil, rerr
		}
		var existing []byte
		var eerr error
		if base == "" {
			existing, eerr = os.ReadFile(target)
		} else {
			existing, eerr = fsio.ReadFile(base, target)
		}
		action := "updated"
		switch {
		case errors.Is(eerr, fs.ErrNotExist):
			action = "created"
		case eerr != nil:
			return nil, eerr
		case bytes.Equal(existing, payload):
			action = "unchanged"
		}
		if !dryRun && action != "unchanged" {
			var err error
			if base == "" {
				err = os.MkdirAll(filepath.Dir(target), 0o777)
				if err == nil {
					err = fsio.AtomicWrite(target, payload)
				}
			} else {
				err = fsio.MkdirAll(base, filepath.Dir(target))
				if err == nil {
					err = fsio.AtomicWriteIn(base, target, payload)
				}
			}
			if err != nil {
				return nil, err
			}
		}
		writes = append(writes, Write{Path: display(target, base), Action: action})
	}
	removed, err := pruneStale(dest, base, shipped, dryRun)
	if err != nil {
		return nil, err
	}
	return append(writes, removed...), nil
}

// pruneStale removes every regular file under dest that the shipped tree does
// not carry, reporting each as "removed" (a dry run reports and removes
// nothing), then drops the directories that emptied, deepest first. dest
// itself is never removed; anything that is not a regular file is left alone.
func pruneStale(dest, base string, shipped map[string]bool, dryRun bool) ([]Write, error) {
	if _, err := os.Stat(dest); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var stale, dirs []string
	walk := func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		switch {
		case d.IsDir():
			if p != dest {
				dirs = append(dirs, p)
			}
		case d.Type().IsRegular() && !shipped[p]:
			stale = append(stale, p)
		}
		return nil
	}
	var err error
	if base == "" {
		err = filepath.WalkDir(dest, walk)
	} else {
		var r *os.Root
		var rel string
		r, rel, err = fsio.OpenPath(base, dest)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		err = fs.WalkDir(r.FS(), filepath.ToSlash(rel), func(p string, d fs.DirEntry, e error) error {
			return walk(filepath.Join(base, filepath.FromSlash(p)), d, e)
		})
	}
	if err != nil {
		return nil, err
	}
	removed := make([]Write, 0, len(stale))
	for _, p := range stale {
		if !dryRun {
			var rerr error
			if base == "" {
				rerr = os.Remove(p)
			} else {
				rerr = fsio.Remove(base, p, false)
			}
			if rerr != nil {
				return nil, rerr
			}
		}
		removed = append(removed, Write{Path: display(p, base), Action: "removed"})
	}
	if dryRun {
		return removed, nil
	}
	// Deepest first, so a directory whose only content was an emptied
	// subdirectory empties in turn.
	for i := len(dirs) - 1; i >= 0; i-- {
		var entries []os.DirEntry
		var rerr error
		if base == "" {
			entries, rerr = os.ReadDir(dirs[i])
		} else {
			entries, rerr = fsio.ReadDir(base, dirs[i])
		}
		if rerr != nil {
			return nil, rerr
		}
		if len(entries) > 0 {
			continue
		}
		if base == "" {
			rerr = os.Remove(dirs[i])
		} else {
			rerr = fsio.Remove(base, dirs[i], false)
		}
		if rerr != nil {
			return nil, rerr
		}
	}
	return removed, nil
}

// display is skill._display: workspace-relative under project scope; the
// path itself when base is "" (--global) or the path escapes base.
func display(p, base string) string {
	if base == "" {
		return p
	}
	rel, err := filepath.Rel(base, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return rel
}

// rejectUnknown is skill._reject_unknown, message byte-for-byte.
func rejectUnknown(given, known []string, kind string) error {
	var unknown []string
	for _, g := range given {
		found := false
		for _, k := range known {
			if g == k {
				found = true
				break
			}
		}
		if !found {
			unknown = append(unknown, g)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return errs.New(
		"unknown_"+kind,
		fmt.Sprintf("Unknown %s: %s. Known %ss: %s",
			kind, strings.Join(unknown, ", "), kind, strings.Join(known, ", ")),
	)
}

// appendGitignore is a package-private port of workspace._append_gitignore
// (the Python module imports it from core.workspace): append one line unless
// it is already present, preserving the file's trailing-newline shape.
func appendGitignore(root, gitignore, line string) error {
	existing := ""
	if b, err := fsio.ReadFile(root, gitignore); err == nil {
		existing = string(b)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, l := range strings.Split(existing, "\n") {
		if strings.TrimSuffix(l, "\r") == line {
			return nil
		}
	}
	sep := ""
	if existing != "" && !strings.HasSuffix(existing, "\n") {
		sep = "\n"
	}
	return fsio.AtomicWriteIn(root, gitignore, []byte(existing+sep+line+"\n"))
}
