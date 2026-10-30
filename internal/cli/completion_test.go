package cli

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The scripts are cobra's bytes; parity pins the `__complete` protocol they
// call, not the scripts, so a cobra bump does not turn the parity suite red.
// This holds the properties khub depends on: each script targets the binary
// named khub and calls back through __complete.
func TestCompletionScripts(t *testing.T) {
	root := newRoot()
	firstLines := map[string]string{
		"bash":       "# bash completion V2 for khub",
		"zsh":        "#compdef khub",
		"fish":       "# fish completion for khub",
		"powershell": "# powershell completion for khub",
	}
	for _, shell := range completionShells {
		var b bytes.Buffer
		if err := writeCompletion(root, shell, &b); err != nil {
			t.Fatalf("%s: %v", shell, err)
		}
		script := b.String()
		if !strings.HasPrefix(script, firstLines[shell]) {
			t.Errorf("%s: first line %q, want prefix %q", shell, strings.SplitN(script, "\n", 2)[0], firstLines[shell])
		}
		if !strings.Contains(script, cobra.ShellCompRequestCmd) {
			t.Errorf("%s: script never calls %s", shell, cobra.ShellCompRequestCmd)
		}
	}
}

// Every --format value TAB offers must be one the flag's help names, so the
// table in completion.go cannot drift from what the command accepts.
func TestFormatValuesMatchHelp(t *testing.T) {
	var walk func(cmd *cobra.Command)
	seen := 0
	walk = func(cmd *cobra.Command) {
		if f := cmd.Flags().Lookup("format"); f != nil {
			seen++
			values, _ := optionCompletions(cmd, "format")
			for _, v := range values {
				if !strings.Contains(f.Usage, v) {
					t.Errorf("%s --format offers %q, which its help %q does not name", cmd.CommandPath(), v, f.Usage)
				}
			}
		}
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(newRoot())
	if seen == 0 {
		t.Fatal("no --format flag found")
	}
}

// Every table key must name a real command or option, or it silently does
// nothing.
func TestCompletionTablesNameRealSurface(t *testing.T) {
	root := newRoot()
	find := func(path string) *cobra.Command {
		cmd, _, err := root.Find(strings.Fields(strings.TrimPrefix(path, "khub")))
		if err != nil || cmd.CommandPath() != path {
			t.Fatalf("no command %q", path)
		}
		return cmd
	}
	for path := range formatValues {
		if find(path).Flags().Lookup("format") == nil {
			t.Errorf("%s has no --format", path)
		}
	}
	for key := range optionValues {
		path, name, _ := strings.Cut(key, " --")
		if find(path).Flags().Lookup(name) == nil {
			t.Errorf("%s has no --%s", path, name)
		}
	}
	for path := range positionalValues {
		find(path)
	}
	var names []string
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, f.Name) })
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(root)
	for name := range pathOptions {
		if !slices.Contains(names, name) {
			t.Errorf("no command declares --%s", name)
		}
	}
}

func TestPrepareCompletionRequest(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"__complete", "-C/x", "st"}, []string{"__complete", "-C", "/x", "st"}},
		{[]string{"__complete", "-C/x"}, []string{"__complete", "-C", "/x"}},
		{[]string{"__complete", "-C", "-Cweird", "st"}, []string{"__complete", "-C", "-Cweird", "st"}},
		{[]string{"__complete", "--workspace", "/x", "-C/y", "st"}, []string{"__complete", "--workspace", "/x", "-C", "/y", "st"}},
		// Past the root's options a -Cx is the command's own word.
		{[]string{"__complete", "add", "project", "-Cx"}, []string{"__complete", "add", "project", "-Cx"}},
	}
	for _, c := range cases {
		got := prepareCompletionRequest(newRoot(), c.in)
		if !slices.Equal(got, c.want) {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}
