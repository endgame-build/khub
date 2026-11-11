package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docs are the Markdown files, relative to the repo root, that show install
// commands to people and agents. CHANGELOG.md is history and stays out: an
// entry records the command a release shipped.
var docs = []string{
	"README.md",
	"RELEASING.md",
	"docs/getting-started.md",
	"docs/plugin.md",
	"docs/cli.md",
	"docs/cli-reference.md",
	"plugin/skills/setup/SKILL.md",
	"plugin/skills/khub/SKILL.md",
	".claude/rules/plugin.md",
}

// trigger finds the start of anything that looks like an install command.
// Words may be split by any run of spaces, and `npx skills` may carry `-y` or a
// version, so every spelling of a command is seen. The npm form needs a scoped
// package or khub before any `&&`, `;` or `|`, so `npm install` alone (a
// project that already pins khub) is not one, and a near-miss of the package
// name reaches matches and fails.
var trigger = regexp.MustCompile(
	`\bnpx\s+(?:(?:-y|--yes)\s+)?skills(?:@[\w.-]+)?\s+add\b` +
		`|(?:/|\bclaude\s+)plugin\s+marketplace\s+(?:add|update)\b` +
		`|(?:/|\bclaude\s+)plugin\s+(?:install|update)\b` +
		"|\\bnpm\\s+(?:install|i)\\b[^`#\n&;|]*?(?:@[\\w.-]+/|\\bkhub\\b)")

// skillsSpelling is the `npx skills add` a doc may spell with `-y` or a
// version; it reads as the plain form.
var skillsSpelling = regexp.MustCompile(`^npx (?:(?:-y|--yes) )?skills(?:@[\w.-]+)? add\b`)

// npxRun finds a package run through npx, past any npx flags: a scoped name,
// or the unscoped khub, which is an unrelated npm package.
var npxRun = regexp.MustCompile(`\bnpx((?:\s+-[\w-]+)*)\s+(@[\w.-]+/[\w.-]+|khub\b)`)

// variants maps each constant to the flags a doc may add after it.
var variants = map[string]*regexp.Regexp{
	NPMInstall:        regexp.MustCompile(`^(?:@X\.Y\.Z|@\d+\.\d+\.\d+)?$`),
	NPMInstallGlobal:  regexp.MustCompile(`^(?:@X\.Y\.Z|@\d+\.\d+\.\d+)?$`),
	SkillsAdd:         regexp.MustCompile(`^(?: (?:-s (?:khub|setup)|-a [a-z-]+|-g|-y))*$`),
	MarketplaceAdd:    regexp.MustCompile(`^$`),
	PluginInstall:     regexp.MustCompile(`^(?: --config binary=\S+)?$`),
	MarketplaceUpdate: regexp.MustCompile(`^$`),
	PluginUpdate:      regexp.MustCompile(`^$`),
}

// units splits a Markdown text into the stretches a command is read from. A
// paragraph is joined into one line, so a code span that wraps is read whole;
// a block fenced with ``` or ~~~ is read line by line.
func units(text string) []string {
	var out []string
	var para []string
	fence := ""
	flush := func() {
		if len(para) > 0 {
			out = append(out, strings.Join(para, " "))
			para = nil
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		marker := ""
		for _, m := range []string{"```", "~~~"} {
			if strings.HasPrefix(trimmed, m) {
				marker = m
			}
		}
		switch {
		case marker != "" && (fence == "" || fence == marker):
			flush()
			if fence == "" {
				fence = marker
			} else {
				fence = ""
			}
		case fence != "":
			out = append(out, line)
		case trimmed == "":
			flush()
		default:
			para = append(para, trimmed)
		}
	}
	flush()
	return out
}

// commands returns every install command in a Markdown text, its words joined
// by single spaces.
func commands(text string) []string {
	var found []string
	for _, unit := range units(text) {
		for _, loc := range trigger.FindAllStringIndex(unit, -1) {
			rest := unit[loc[1]:]
			if cut := strings.IndexAny(rest, "`#,;|)"); cut >= 0 {
				rest = rest[:cut]
			}
			if cut := strings.Index(rest, "&&"); cut >= 0 {
				rest = rest[:cut]
			}
			cmd := unit[loc[0]:loc[1]] + strings.TrimRight(rest, " .")
			cmd = strings.Join(strings.Fields(cmd), " ")
			found = append(found, skillsSpelling.ReplaceAllString(cmd, "npx skills add"))
		}
	}
	return found
}

// runs returns every package a Markdown text runs through npx.
func runs(text string) []string {
	var found []string
	for _, unit := range units(text) {
		for _, m := range npxRun.FindAllStringSubmatch(unit, -1) {
			found = append(found, m[2])
		}
	}
	return found
}

// matches reports whether cmd is one of the constants, with the flags its
// variant allows. A shell `claude plugin …` reads as the `/plugin …` typed at
// the Claude Code prompt.
func matches(cmd string) bool {
	if after, ok := strings.CutPrefix(cmd, "claude plugin "); ok && !strings.HasPrefix(after, "marketplace update ") && !strings.HasPrefix(after, "update ") {
		cmd = "/plugin " + after
	}
	for c, flags := range variants {
		if rest, ok := strings.CutPrefix(cmd, c); ok && flags.MatchString(rest) {
			return true
		}
	}
	return false
}

func TestDocsInstallCommandsMatchTheConstants(t *testing.T) {
	root := filepath.Join("..", "..")
	used := map[string]bool{}
	ran := false
	for _, doc := range docs {
		data, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		for _, cmd := range commands(string(data)) {
			if !matches(cmd) {
				t.Errorf("%s: %q is not an install command internal/install holds (see variants in install_test.go for the flags each allows)", doc, cmd)
				continue
			}
			for c := range variants {
				if strings.HasPrefix(cmd, c) {
					used[c] = true
				}
			}
		}
		for _, pkg := range runs(string(data)) {
			if pkg != Package {
				t.Errorf("%s: npx runs %q, not the package internal/install holds (%q)", doc, pkg, Package)
				continue
			}
			ran = true
		}
	}
	// A trigger that stopped matching would pass every doc vacuously.
	for c := range variants {
		if !used[c] {
			t.Errorf("no doc shows %q; the scan or the trigger has drifted", c)
		}
	}
	if !ran {
		t.Errorf("no doc runs %q through npx; the scan has drifted", Package)
	}
}

// The names the constants carry are set by the marketplace manifest, the
// plugin manifest and the npm package template. A rename there must fail here.
func TestConstantsMatchTheManifests(t *testing.T) {
	root := filepath.Join("..", "..")
	read := func(path string, into any) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, into); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	var marketplace struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name string `json:"name"`
		} `json:"plugins"`
	}
	read(".claude-plugin/marketplace.json", &marketplace)
	var plugin struct {
		Name string `json:"name"`
	}
	read("plugin/.claude-plugin/plugin.json", &plugin)
	var pkg struct {
		Name       string `json:"name"`
		Repository struct {
			URL string `json:"url"`
		} `json:"repository"`
	}
	read("npm/khub/package.json.tmpl", &pkg)

	if marketplace.Name != Marketplace {
		t.Errorf("marketplace.json names the marketplace %q, Marketplace is %q", marketplace.Name, Marketplace)
	}
	if got := plugin.Name + "@" + marketplace.Name; got != Plugin {
		t.Errorf("plugin.json and marketplace.json make the plugin id %q, Plugin is %q", got, Plugin)
	}
	listed := false
	for _, p := range marketplace.Plugins {
		listed = listed || p.Name == plugin.Name
	}
	if !listed {
		t.Errorf("marketplace.json does not list the plugin %q", plugin.Name)
	}
	if pkg.Name != Package {
		t.Errorf("package.json.tmpl names the package %q, Package is %q", pkg.Name, Package)
	}
	if want := "git+https://github.com/" + Repo + ".git"; pkg.Repository.URL != want {
		t.Errorf("package.json.tmpl's repository is %q, Repo makes it %q", pkg.Repository.URL, want)
	}
}

func TestCommandsReadsWrappedSpansAndHints(t *testing.T) {
	text := "From a shell the same two steps are `claude plugin marketplace add\n" +
		"endgame-build/khub` and `claude plugin install khub@khub`.\n\n" +
		"```text\n  cd my-hub && npx skills add endgame-build/khub\n" +
		"In Claude Code: /plugin marketplace add endgame-build/khub, then /plugin install khub@khub\n```\n" +
		"If pinned, `npm ci` (or `npm install`) and use `npx @endgame-build/khub`.\n"
	got := commands(text)
	want := []string{
		"claude plugin marketplace add endgame-build/khub",
		"claude plugin install khub@khub",
		"npx skills add endgame-build/khub",
		"/plugin marketplace add endgame-build/khub",
		"/plugin install khub@khub",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands() = %q, want %q", got, want)
	}
	for _, bad := range []string{
		"npx skills add endgame-build/khb",
		"/plugin install khub@khub-marketplace",
		"npm install -D khub",
		"npx skills add endgame-build/khub --all",
		"npx skills add endgame-build/khub -a endgame-buld/khub",
		"npm install -D @endgame-build/kub",
	} {
		if matches(bad) {
			t.Errorf("matches(%q) = true, want false", bad)
		}
	}
	if got := runs(text); strings.Join(got, "\n") != Package {
		t.Errorf("runs() = %q, want [%q]", got, Package)
	}
}

func TestCommandsSeesEverySpelling(t *testing.T) {
	text := "~~~sh\n" +
		"npm install -D @endgame-build/khub && npx @endgame-build/khub init firm-ops .\n" +
		"npm i -g @endgame-build/khb\n" +
		"npx -y skills@latest add endgame-build/khub -a claude-code\n" +
		"npx  skills  add endgame-build/khub\n" +
		"/plugin  install khub@khub\n" +
		"npx --no-install @endgame-bild/khub --version; npx khub init\n" +
		"~~~\n"
	got := commands(text)
	want := []string{
		"npm install -D @endgame-build/khub",
		"npm i -g @endgame-build/khb",
		"npx skills add endgame-build/khub -a claude-code",
		"npx skills add endgame-build/khub",
		"/plugin install khub@khub",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands() = %q, want %q", got, want)
	}
	if gotRuns, wantRuns := runs(text), []string{Package, "@endgame-bild/khub", "khub"}; strings.Join(gotRuns, "\n") != strings.Join(wantRuns, "\n") {
		t.Fatalf("runs() = %q, want %q", gotRuns, wantRuns)
	}
}
