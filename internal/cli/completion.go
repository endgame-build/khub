// completion.go adapts `khub completion <shell>`: print the shell script that
// teaches bash, zsh, fish or PowerShell to complete khub's commands and
// options. The script itself is cobra's; at TAB time it calls back into the
// binary as `khub __complete <words…>`, so the candidates always match the
// binary that answers, never a list frozen into the script.
//
// It is khub's own command rather than cobra's default one. Cobra's default is
// a group with one subcommand per shell, each with cobra-styled help and a
// --no-descriptions flag; khub's surface is Click-shaped, so the shell is a
// positional argument checked like any other, and the help is the Rich panel
// every other command draws.

package cli

import (
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/endgame-build/khub/internal/presets"
)

// completionShells is the closed set, in the order help and errors list it.
var completionShells = []string{"bash", "zsh", "fish", "powershell"}

const completionArgSpec = "SHELL"

func registerCompletion(root *cobra.Command) {
	cmd := newCmd("completion",
		"Print a shell completion script: khub completion bash|zsh|fish|powershell.",
		completionArgSpec, func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return missingArgument(cmd.CommandPath(), completionArgSpec, completionArgSpec)
			}
			shell := args[0]
			if !isCompletionShell(shell) {
				quoted := make([]string, len(completionShells))
				for i, s := range completionShells {
					quoted[i] = "'" + s + "'"
				}
				return clickUsageErr(cmd.CommandPath(), completionArgSpec,
					"Invalid value for '"+completionArgSpec+"': '"+shell+"' is not one of "+
						strings.Join(quoted, ", ")+".")
			}
			return Guard("text", func() error {
				return writeCompletion(cmd.Root(), shell, os.Stdout)
			})
		})
	cmd.Long = "Print a shell completion script: khub completion bash|zsh|fish|powershell.\n\n" +
		"Load it for this session with `source <(khub completion zsh)`, or write it to " +
		"the shell's completion directory to keep it. Candidates come from the binary at TAB time."
	cmd.Args = clickArity(1)
	root.AddCommand(cmd)
}

func isCompletionShell(shell string) bool {
	for _, s := range completionShells {
		if s == shell {
			return true
		}
	}
	return false
}

// writeCompletion emits cobra's script for one shell, with descriptions: the
// candidates carry each command's one-line help. A user who wants them off
// sets KHUB_COMPLETION_DESCRIPTIONS=false, which cobra reads at TAB time.
func writeCompletion(root *cobra.Command, shell string, w io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletionV2(w, true)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	default:
		return root.GenPowerShellCompletionWithDesc(w)
	}
}

// --- what TAB offers ----------------------------------------------------------
//
// Cobra's fallback for an argument or option value it knows nothing about is
// the shell's file completion, which is wrong for nearly every khub value: a
// type, an id, a predicate, a format. So every command gets a completer, and
// a value falls back to files only where it IS a path. Nothing here reads a
// workspace: the closed sets below are the ones the binary fixes, and the
// workspace-defined ones (types, ids, predicates, field values) offer nothing.

// formatValues are the --format values a command accepts beyond text and
// json, keyed by command path. TestFormatValuesMatchHelp holds each value to
// the flag's own help text, so the two cannot drift apart.
var formatValues = map[string][]string{
	"khub get":    {"text", "json", "table", "raw"},
	"khub query":  {"text", "json", "ids"},
	"khub search": {"text", "json", "ids"},
	"khub impact": {"tree", "json"},
}

var defaultFormatValues = []string{"text", "json"}

// optionValues are the closed sets of the other options that have one.
var optionValues = map[string][]string{
	"khub wire --target": {"claude", "agents", "both"},
}

// pathOptions are the options whose value is a path: a file (Default) or a
// directory (FilterDirs). -C/--workspace carries cobra's dirname annotation.
var pathOptions = map[string]cobra.ShellCompDirective{
	"body-file":     cobra.ShellCompDirectiveDefault,
	"out":           cobra.ShellCompDirectiveDefault,
	"preset-source": cobra.ShellCompDirectiveFilterDirs,
}

// positionalValues completes a command's positional arguments by index.
// A command or an index not listed offers nothing and no files.
var positionalValues = map[string][]func() ([]string, cobra.ShellCompDirective){
	"khub init": {
		func() ([]string, cobra.ShellCompDirective) {
			return presets.Known(presets.Embedded()), cobra.ShellCompDirectiveNoFileComp
		},
		func() ([]string, cobra.ShellCompDirective) { return nil, cobra.ShellCompDirectiveFilterDirs },
	},
	"khub completion": {
		func() ([]string, cobra.ShellCompDirective) {
			return completionShells, cobra.ShellCompDirectiveNoFileComp
		},
	},
}

// wireCompletions gives every visible command a positional completer and
// every value-taking option a value completer. It runs once the tree is
// registered, so a new command or option is covered without an entry here.
func wireCompletions(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		if sub.Hidden || sub.Name() == "" {
			continue
		}
		wireCompletions(sub)
	}
	if cmd.HasParent() || cmd.Runnable() {
		cmd.ValidArgsFunction = completeArgs
	}
	if cmd.DisableFlagParsing {
		return // cobra hands option values to completeArgs here
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if f.NoOptDefVal != "" || f.Name == "help" || f.Name == "workspace" {
			return // a switch takes no value; -C completes directories by annotation
		}
		_ = cmd.RegisterFlagCompletionFunc(f.Name,
			func(c *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
				return optionCompletions(c, f.Name)
			})
	})
}

// optionCompletions answers TAB on one option's value.
func optionCompletions(cmd *cobra.Command, name string) ([]string, cobra.ShellCompDirective) {
	if d, ok := pathOptions[name]; ok {
		return nil, d
	}
	if name == "format" {
		if values, ok := formatValues[cmd.CommandPath()]; ok {
			return values, cobra.ShellCompDirectiveNoFileComp
		}
		return defaultFormatValues, cobra.ShellCompDirectiveNoFileComp
	}
	return optionValues[cmd.CommandPath()+" --"+name], cobra.ShellCompDirectiveNoFileComp
}

// completeArgs answers TAB on a positional. A command that parses its own
// options (add, edit, query take schema-driven --<field> options) gets the
// raw words, so an option value is recognised here by the word before it.
func completeArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if cmd.DisableFlagParsing {
		if name, ok := strings.CutPrefix(toComplete, "--"); ok {
			if i := strings.IndexByte(name, '='); i >= 0 {
				values, d := optionCompletions(cmd, name[:i])
				prefixed := make([]string, len(values))
				for j, v := range values {
					prefixed[j] = "--" + name[:i+1] + v
				}
				return prefixed, d
			}
		}
		if len(args) > 0 {
			if name, ok := strings.CutPrefix(args[len(args)-1], "--"); ok && name != "" &&
				!strings.Contains(name, "=") {
				if f := cmd.Flags().Lookup(name); f != nil && f.NoOptDefVal != "" {
					return nil, cobra.ShellCompDirectiveNoFileComp // a switch: what follows is free
				}
				// A declared option or a schema field: both take a value.
				return optionCompletions(cmd, name)
			}
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	fns := positionalValues[cmd.CommandPath()]
	if len(args) < len(fns) {
		return fns[len(args)]()
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// prepareCompletionRequest adapts a `__complete` call before cobra sees it.
// The shell hands over the words as typed, so an attached -C<dir> arrives as
// one word, which cobra reads as a run of shorthand flags; split it the way
// parseGlobals does for a real call. Only the root's leading options are
// split — a later -Cx is a command's own word. A description cut at its
// first newline would end mid-sentence, so multi-line help is joined.
func prepareCompletionRequest(root *cobra.Command, rest []string) []string {
	root.SetOut(skipBlankLines{os.Stdout})
	joinShortLines(root)
	out := []string{rest[0]}
	i := 1
	for ; i < len(rest); i++ {
		w := rest[i]
		if !strings.HasPrefix(w, "-") || w == "-" || w == "--" {
			break
		}
		if strings.HasPrefix(w, "-C") && len(w) > 2 {
			out = append(out, "-C", w[2:])
			continue
		}
		out = append(out, w)
		if (w == "-C" || w == "--workspace") && i+1 < len(rest) {
			i++
			out = append(out, rest[i])
		}
	}
	return append(out, rest[i:]...)
}

func joinShortLines(cmd *cobra.Command) {
	cmd.Short = strings.ReplaceAll(cmd.Short, "\n", " ")
	for _, sub := range cmd.Commands() {
		joinShortLines(sub)
	}
}

// isCompletionRequest reports cobra's hidden callback, `khub __complete …`,
// which the generated scripts run at TAB time.
func isCompletionRequest(rest []string) bool {
	return len(rest) > 0 &&
		(rest[0] == cobra.ShellCompRequestCmd || rest[0] == cobra.ShellCompNoDescRequestCmd)
}

// skipBlankLines drops the empty candidate cobra emits for khub's nameless
// help command. Cobra offers the help command at the root whatever its
// visibility, and khub's has the empty name (Click has no `help` command; see
// newRoot), so without this every root TAB would offer an empty word. Cobra
// writes one candidate per Write call, and no real candidate is empty.
type skipBlankLines struct{ w io.Writer }

func (s skipBlankLines) Write(p []byte) (int, error) {
	if string(p) == "\n" {
		return len(p), nil
	}
	return s.w.Write(p)
}
