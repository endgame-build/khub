package corpus

// The read and write cases bench and footprint share, and the manifest
// handles that fill them. One list, so the two tools time and measure the
// same calls and neither spells an id, a title or an id scheme.

import "strings"

// Case is one khub call. `{…}` placeholders in Argv are filled from the
// corpus's own manifest through Handles. Exit is enforced before anything is
// measured: a command that is fast, or small, because it errored out is the
// one mistake neither tool can afford.
type Case struct {
	Label string
	Argv  []string
	Exit  int
}

// Reads is the read surface, in the order the tables print it. The last two
// are the controls: `schema show <type>` reads the ontology and not the
// corpus, so its cost should not move with n; `--version` is the process
// floor, what a process costs before khub does anything.
var Reads = []Case{
	{"status", []string{"status"}, 0},
	{"schema", []string{"schema"}, 0},
	{"check", []string{"check"}, 0},
	{"check --strict", []string{"check", "--strict"}, 1},
	{"validate (whole corpus)", []string{"validate"}, 0},
	{"validate <one entity>", []string{"validate", "requirement/{needle_id}"}, 0},
	{"query", []string{"query"}, 0},
	{"query --type requirement", []string{"query", "--type", "requirement"}, 0},
	{"query --orphan", []string{"query", "--orphan"}, 0},
	{"query --tag", []string{"query", "--tag", "{tag}"}, 0},
	{"search (one hit)", []string{"search", "{needle}"}, 0},
	{"search (many hits)", []string{"search", "{tag}"}, 0},
	{"search (no hit)", []string{"search", "quxzzytl"}, 0},
	{"get", []string{"get", "{needle_id}"}, 0},
	{"get --edges", []string{"get", "{held}", "--edges"}, 0},
	{"get <chain middle> --edges", []string{"get", "{history_middle}", "--edges"}, 0},
	{"neighbors", []string{"neighbors", "{neighbors_id}"}, 0},
	{"neighbors --depth 12", []string{"neighbors", "{neighbors_id}", "--depth", "12"}, 0},
	{"impact", []string{"impact", "{deepest_id}"}, 0},
	{"impact --reverse", []string{"impact", "{hub_id}", "--reverse"}, 0},
	{"history", []string{"history", "{history_id}"}, 0},
	{"stale --days 90", []string{"stale", "--days", "90"}, 0},
	{"reindex --dry-run", []string{"reindex", "--dry-run"}, 0},
	// upgrade copies the workspace into a candidate, migrates and validates
	// it. A real run syncs every file it publishes, so it is disk bound. The
	// first run brings the corpus up to date; every run after it changes
	// nothing.
	{"upgrade --dry-run", []string{"upgrade", "--dry-run", "--no-wire"}, 0},
	{"upgrade", []string{"upgrade", "--no-wire"}, 0},
	{Control, []string{"schema", "show", "requirement"}, 0},
	{Floor, []string{"--version"}, 0},
}

// Control and Floor name the two reference rows of Reads.
const (
	Control = "schema show <type>"
	Floor   = "--version (process floor)"
)

// PathBound names the Reads rows whose answer spells the workspace path
// relative to the caller, so footprint cannot pin their bytes. Bench still
// times them.
var PathBound = map[string]bool{"upgrade --dry-run": true, "upgrade": true}

// Writes is one self-cleaning cycle on a probe entity: every verb a write
// path has, and a no-op link that must report `changed: false`. `{probe}` is
// the slug `add` minted, which only the caller knows. The corpus is as it
// was afterwards, which is what lets the cycle run on a copy and the copy be
// compared with the original.
var Writes = []Case{
	{"add", []string{"add", "requirement", "--title", "{probe_title}", "--kind", "functional", "--format", "json"}, 0},
	{"link", []string{"link", "{probe}", "realized_in", "{link_target}", "--format", "json"}, 0},
	{"link (no-op)", []string{"link", "{probe}", "realized_in", "{link_target}", "--format", "json"}, 0},
	{"edit", []string{"edit", "{probe}", "kind", "non-functional", "--format", "json"}, 0},
	{"unlink", []string{"unlink", "{probe}", "realized_in", "{link_target}", "--format", "json"}, 0},
	{"remove", []string{"remove", "{probe}", "--format", "json"}, 0},
}

// Handles are the ids and terms the cases need, read out of the manifest.
func Handles(m *Manifest) map[string]string {
	topTag, topN := "", -1
	for _, tag := range Tags {
		if n := m.Tags[tag]; n > topN {
			topTag, topN = tag, n
		}
	}
	subs := map[string]string{
		"needle":         m.Search.Needle,
		"needle_id":      m.Search.ID,
		"tag":            topTag,
		"neighbors_id":   m.Degrees[0].ID,
		"history_id":     m.History.Newest,
		"history_middle": m.History.Middle,
		"held":           m.Probes.HeldComponent,
		"link_target":    m.Probes.LinkTarget,
	}
	for _, c := range m.Closures {
		switch c.Role {
		case "hub":
			subs["hub_id"] = c.ID
		case "deepest":
			subs["deepest_id"] = c.ID
		}
	}
	return subs
}

// Fill substitutes every `{key}` in argv from subs, returning a new slice.
func Fill(argv []string, subs map[string]string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		for key, value := range subs {
			a = strings.ReplaceAll(a, "{"+key+"}", value)
		}
		out[i] = a
	}
	return out
}
