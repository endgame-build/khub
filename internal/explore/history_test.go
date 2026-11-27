package explore

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/endgame-build/khub/internal/entity"
	"github.com/endgame-build/khub/internal/graph"
	"github.com/endgame-build/khub/internal/index"
	"github.com/endgame-build/khub/internal/introspect"
	"github.com/endgame-build/khub/internal/omap"
)

func gitIn(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=Ana", "-c", "user.email=ana@example.com",
		"-c", "commit.gpgsign=false", "-c", "core.autocrlf=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// historyWS is one capability, committed, reviewed, edited and edited again.
func historyWS(t *testing.T) (root, path string) {
	t.Helper()
	root = initWS(t, "build-hub")
	mustCreateBody(t, root, "capability", "cap-pay", "One.\nTwo.\nThree.\n", "title", "Payments", "description", "Moving money.")
	path = filepath.Join(root, "knowledge", "capabilities", "cap-pay.md")
	gitIn(t, root, "init", "-q")
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-qm", "capture")
	if _, err := entity.Verify(root, "cap-pay", "human:ana", "", nil); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "commit", "-qam", "review")
	edit(t, path, "Moving money.", "Moving money between accounts.")
	edit(t, path, "Two.", "Two, changed.")
	gitIn(t, root, "commit", "-qam", "edit")
	return root, path
}

func edit(t *testing.T, path, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), from) {
		t.Fatalf("%s holds no %q", path, from)
	}
	writeFile(t, path, strings.Replace(string(raw), from, to, 1))
}

func commitsOf(t *testing.T, out *omap.Map) []string {
	t.Helper()
	var got []string
	for _, c := range get(t, out, "commits").([]any) {
		m := c.(*omap.Map)
		var fields []string
		for _, f := range get(t, m, "changed").([]any) {
			fields = append(fields, f.(string))
		}
		what := get(t, m, "kind").(string)
		if len(fields) > 0 {
			what = strings.Join(fields, " ")
		}
		got = append(got, get(t, m, "subject").(string)+": "+what)
	}
	return got
}

func TestHistoryNamesWhatEachCommitChanged(t *testing.T) {
	root, path := historyWS(t)
	out, err := History(root, "capability/cap-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	// A review is bookkeeping: it changed nothing the entity says.
	if got, want := commitsOf(t, out), []string{"edit: description body", "review: bookkeeping", "capture: created"}; !eq(got, want) {
		t.Errorf("commits %q, want %q", got, want)
	}
	// What is on disk and not committed leads.
	edit(t, path, "title: Payments", "title: Payouts")
	out, err = History(root, "cap-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := commitsOf(t, out); got[0] != "Not committed yet: title" {
		t.Errorf("commits %q", got)
	}
}

func TestChangeSinceReview(t *testing.T) {
	root, _ := historyWS(t)
	out, err := History(root, "cap-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	since := get(t, out, "since_review").(*omap.Map)
	if get(t, since, "by") != "human:ana" || get(t, since, "found") != true {
		t.Fatalf("since_review %v", since)
	}
	field := get(t, since, "fields").([]any)[0].(*omap.Map)
	if get(t, field, "name") != "description" || get(t, field, "before") != "Moving money." ||
		get(t, field, "after") != "Moving money between accounts." {
		t.Errorf("field %v", field)
	}
	var lines []string
	for _, l := range get(t, since, "body").([]any) {
		m := l.(*omap.Map)
		lines = append(lines, get(t, m, "op").(string)+get(t, m, "text").(string))
	}
	if got := strings.Join(lines, "|"); !strings.Contains(got, "-Two.|+Two, changed.") {
		t.Errorf("body %q", got)
	}
	// Reviewed as it now is: nothing to compare.
	if _, err := entity.Verify(root, "cap-pay", "human:ana", "", nil); err != nil {
		t.Fatal(err)
	}
	out, err = History(root, "cap-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	if v := get(t, out, "since_review"); v != nil {
		t.Errorf("since_review %v after a review that holds", v)
	}
}

// A commit is compared with its own parent, so a branch's commit is not
// credited with what the branch it merged into had changed.
func TestHistoryComparesWithTheParent(t *testing.T) {
	root, path := historyWS(t)
	gitIn(t, root, "checkout", "-q", "-b", "side")
	edit(t, path, "Three.", "Three, on the side.")
	gitIn(t, root, "commit", "-qam", "side")
	gitIn(t, root, "checkout", "-q", "-")
	edit(t, path, "between accounts.", "between two accounts.")
	gitIn(t, root, "commit", "-qam", "trunk")
	gitIn(t, root, "merge", "-q", "--no-ff", "-m", "merge", "side")
	out, err := History(root, "cap-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	got := commitsOf(t, out)
	for _, want := range []string{"side: body", "trunk: description"} {
		if !slices.Contains(got, want) {
			t.Errorf("commits %q hold no %q", got, want)
		}
	}
}

// A commit that removed the file or broke it says so, and none is bookkeeping.
func TestHistoryNamesRemovalAndBreakage(t *testing.T) {
	root, path := historyWS(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "---\ntitle: [\n---\n")
	gitIn(t, root, "commit", "-qam", "break")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	gitIn(t, root, "commit", "-qam", "drop")
	writeFile(t, path, string(raw))
	gitIn(t, root, "add", "-A")
	gitIn(t, root, "commit", "-qm", "restore")
	out, err := History(root, "cap-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := commitsOf(t, out)[:3], []string{"restore: created", "drop: removed", "break: unparsable"}; !eq(got, want) {
		t.Errorf("commits %q, want %q", got, want)
	}
}

// The edge of a shallow clone is not where the file began.
func TestHistoryOfAShallowClone(t *testing.T) {
	root, _ := historyWS(t)
	clone := filepath.Join(t.TempDir(), "clone")
	from := filepath.ToSlash(root)
	if !strings.HasPrefix(from, "/") {
		from = "/" + from // a Windows drive
	}
	gitIn(t, root, "clone", "-q", "--depth", "1", "file://"+from, clone)
	out, err := History(clone, "cap-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	if got := commitsOf(t, out); !eq(got, []string{"edit: unknown"}) || get(t, out, "more") != true {
		t.Errorf("commits %q, more %v", got, get(t, out, "more"))
	}
}

// A review that names no version says there is nothing to compare with.
func TestReviewWithoutAVersion(t *testing.T) {
	root, path := historyWS(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	for _, line := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "rev:") {
			kept = append(kept, line)
		}
	}
	writeFile(t, path, strings.Join(kept, "\n"))
	out, err := History(root, "cap-pay", now)
	if err != nil {
		t.Fatal(err)
	}
	since, ok := get(t, out, "since_review").(*omap.Map)
	if !ok || get(t, since, "found") != false || get(t, since, "holds") != true || get(t, since, "reason") == "" {
		t.Errorf("since_review %v", get(t, out, "since_review"))
	}
}

// Without a version to compare with, the page says why.
func TestHistoryWithoutGitSaysSo(t *testing.T) {
	root := reviewedWS(t)
	out, err := History(root, "capability/cap-report", now)
	if err != nil {
		t.Fatal(err)
	}
	if get(t, out, "note") == "" || len(get(t, out, "commits").([]any)) != 0 {
		t.Errorf("history outside git %v", out)
	}
	since := get(t, out, "since_review").(*omap.Map)
	if get(t, since, "found") != false || get(t, since, "reason") == "" {
		t.Errorf("since_review %v", since)
	}
}

// Reach lists each entity neighbors lists past its own connections, once.
func TestReachAgreesWithNeighbors(t *testing.T) {
	root := buildHubWS(t)
	resolved, err := introspect.LoadSchema(root)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := index.Build(root, resolved)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"component/cmp-ledger", "requirement/req-settle", "capability/cap-pay"} {
		out, err := Reach(root, id, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, direction := range []string{graph.DirectionOut, graph.DirectionIn} {
			near, err := graph.Neighbors(idx, id, nil, direction, reachDepth)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]int{}
			for _, n := range near {
				key := n.Type + "/" + n.Slug
				if d, ok := want[key]; !ok || n.Depth < d {
					want[key] = n.Depth
				}
			}
			got := map[string]int{}
			for _, l := range get(t, out, direction).([]any) {
				level := l.(*omap.Map)
				for _, ty := range get(t, level, "types").([]any) {
					for _, i := range get(t, ty.(*omap.Map), "items").([]any) {
						got[get(t, i.(*omap.Map), "id").(string)] = get(t, level, "depth").(int)
					}
				}
			}
			for key, d := range want {
				if d > 1 && got[key] != d {
					t.Errorf("%s %s: %s at %d, neighbors has %d", id, direction, key, got[key], d)
				}
			}
			for key, d := range got {
				if want[key] != d {
					t.Errorf("%s %s lists %s at %d, neighbors has %d", id, direction, key, d, want[key])
				}
			}
		}
	}
}
