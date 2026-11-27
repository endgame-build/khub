package gitlog

import (
	"bytes"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Blob is one side of a change: the file's text, that there was none, or that
// git cannot show it here.
type Blob struct {
	State BlobState
	Text  string
}

// BlobState says what git knows of one side of a change.
type BlobState int

// The states a Blob is in.
const (
	BlobUnknown BlobState = iota // the commit is the edge of a shallow clone, or the object was not fetched
	BlobAbsent                   // the file was not there
	BlobHeld
)

// Version is one commit's change to a file: as its first parent had it, and as
// the commit left it.
type Version struct {
	Commit  string // abbreviated
	Date    string // the committer date, YYYY-MM-DD
	Author  string
	Subject string
	Before  Blob
	After   Blob
}

// FileVersions is the last limit commits that changed relpath, newest first,
// and whether git may hold older ones. Outside a git repository, or for a file
// git has never held, there are none. A rename starts a file's history anew, as
// it does for the dates this package derives.
//
// The path is a path, never a pattern, whatever it holds; a config that follows
// renames is overruled; and nothing is fetched, so an object a partial clone
// left behind is unknown rather than a request to its remote.
func FileVersions(root, relpath string, limit int) (versions []Version, more bool, err error) {
	ok, err := HasGitHistory(root)
	if err != nil || !ok {
		return nil, false, err
	}
	out, code, err := runOffline(root, "", "--literal-pathspecs", "-c", "log.follow=false", "-c", "core.quotePath=true",
		"log", "-n", strconv.Itoa(limit+1), "--raw", "--no-abbrev", "--no-renames", "--diff-merges=first-parent",
		"--format=%x00%h%x00%P%x00%cd%x00%an%x00%s", "--date=short", "--", relpath)
	if err != nil || code != 0 {
		return nil, false, err
	}
	shallow, _, err := run(root, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, false, err
	}
	cut := strings.TrimSpace(shallow) == "true"

	type ids struct{ before, after string }
	var objects []ids
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "\x00"):
			f := strings.SplitN(line[1:], "\x00", 5)
			if len(f) != 5 {
				continue
			}
			v := Version{Commit: f[0], Date: f[2], Author: f[3], Subject: f[4]}
			versions = append(versions, v)
			// The edge of a shallow clone reads as a commit with no parent, so
			// what it seems to create may be older.
			at := ids{}
			if cut && f[1] == "" {
				at.before = "?"
				more = true
			}
			objects = append(objects, at)
		case strings.HasPrefix(line, ":") && len(versions) > 0:
			f := strings.Fields(line)
			if len(f) < 5 {
				continue
			}
			at := &objects[len(objects)-1]
			at.after = f[3]
			if at.before == "" {
				at.before = f[2]
			}
		}
	}
	if len(versions) > limit {
		versions, objects, more = versions[:limit], objects[:limit], true
	}
	var want []string
	for _, o := range objects {
		want = append(want, o.before, o.after)
	}
	held, err := blobs(root, want)
	if err != nil {
		return nil, false, err
	}
	for i, o := range objects {
		versions[i].Before, versions[i].After = held[o.before], held[o.after]
	}
	return versions, more, nil
}

// blobs reads the objects ids name, in one git process. An all-zero id is a
// file that was not there.
func blobs(root string, ids []string) (map[string]Blob, error) {
	out := map[string]Blob{}
	var ask []string
	for _, id := range ids {
		if _, done := out[id]; done {
			continue
		}
		out[id] = Blob{}
		switch {
		case id == "" || id == "?":
		case strings.Trim(id, "0") == "":
			out[id] = Blob{State: BlobAbsent}
		default:
			ask = append(ask, id)
		}
	}
	if len(ask) == 0 {
		return out, nil
	}
	raw, _, err := runOffline(root, strings.Join(ask, "\n")+"\n", "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	// Each answer is "<id> blob <size>\n<size bytes>\n", or "<id> missing\n".
	for _, id := range ask {
		head, rest, ok := strings.Cut(raw, "\n")
		if !ok {
			break
		}
		raw = rest
		f := strings.Fields(head)
		if len(f) != 3 || f[0] != id {
			continue
		}
		size, err := strconv.Atoi(f[2])
		if err != nil || size+1 > len(raw) {
			break
		}
		if f[1] == "blob" {
			out[id] = Blob{State: BlobHeld, Text: raw[:size]}
		}
		raw = raw[size+1:]
	}
	return out, nil
}

// runOffline is run with input on stdin and lazy fetching off.
func runOffline(root, input string, args ...string) (stdout string, code int, err error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
	cmd.Stdin = strings.NewReader(input)
	var out bytes.Buffer
	cmd.Stdout = &out
	return finish(cmd, &out)
}
