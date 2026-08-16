// root.go ports cli/main.py: the command registry, the global options parsed
// before the command name, and the exit-code trichotomy (0 success, 1 located
// failure, 2 usage).
package cli

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/version"
	"github.com/endgame-build/khub/internal/workspace"
)

// State is the global CLI state Typer stashes on ctx.obj.
type State struct{ Workspace string }

var state State

// Execute runs the CLI and returns the process exit code.
func Execute(argv []string) int {
	root := newRoot() // registering -C resets state.Workspace to its default
	// Click parses the ROOT GROUP's own options itself, before the command name,
	// and --version is eager: `khub --version status` prints the version and
	// exits without ever resolving `status`. Cobra has no eager-option hook and
	// would hand `--version` to the subcommand, so the group's options are
	// consumed here and never reach cobra.
	rest, code, handled := parseGlobals(root, argv)
	if handled {
		return code
	}
	// Typer's no_args_is_help fires on an EMPTY argv only. `khub -C .` and
	// `khub --` did reach the parser, so Click resolves them to a group with
	// nothing to invoke: "Missing command.", exit 2 — not the help body.
	if len(argv) > 0 && len(rest) == 0 {
		_ = usageBox(root, "Missing command.")
		return 2
	}
	root.SetArgs(rest)
	cmd, err := root.ExecuteC()
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	var usage *errs.Usage
	if errors.As(err, &usage) {
		fmt.Fprintln(os.Stderr, usage.Message)
		return 2
	}
	// Anything cobra rejected before a command body ran is a usage error, and
	// Click exits 2 for those — an unknown command, an unparseable argument.
	// Cobra's own default is 1, which would let every removed surface (`log`,
	// `validate --fix`, `--agent`) exit with the wrong code.
	if isCobraUsageError(err) {
		return usageBoxErr(cmd, err)
	}
	return 1
}

// parseGlobals is Click's group-level option parse: it consumes the options
// declared on the root callback (-C/--workspace, --version, --help) up to the
// first non-option token, then hands the remainder to cobra as the command
// invocation. handled=true means the run is over (version/help printed, or a
// usage error rendered).
//
// Two contracts ride on doing this here rather than on cobra's flag sets:
// --version is EAGER (it short-circuits any command that follows), and the
// group's options are ROOT-ONLY (`khub status -C /x` is "No such option: -C",
// which a cobra persistent flag would silently accept).
func parseGlobals(root *cobra.Command, argv []string) (rest []string, code int, handled bool) {
	i := 0
	for i < len(argv) {
		token := argv[i]
		if token == "--" {
			// Click stops option parsing here; the command name follows.
			i++
			break
		}
		if !strings.HasPrefix(token, "-") || token == "-" {
			break
		}
		name, value, attached := splitOption(token)
		switch name {
		case "--version":
			fmt.Println(version.Version)
			return nil, 0, true
		case "--help":
			renderHelp(root)
			return nil, 0, true
		case "-C", "--workspace":
			if !attached {
				if i+1 >= len(argv) {
					// Click's parser raises this before a Context exists, so
					// UsageError.show() has no usage line to print — the box
					// stands alone.
					fmt.Fprintln(os.Stderr,
						richBox("Error", "Option "+quoteOpt(name)+" requires an argument."))
					return nil, 2, true
				}
				value = argv[i+1]
				i++
			}
			state.Workspace = value
		default:
			_ = usageBox(root, "No such option: "+name+suggest(name, optionNames(root)))
			return nil, 2, true
		}
		i++
	}
	return append([]string{}, argv[i:]...), 0, false
}

// splitOption splits one option token into its name and any attached value:
// --workspace=/x, -C/x, and the bare forms.
func splitOption(token string) (name, value string, attached bool) {
	if strings.HasPrefix(token, "--") {
		if eq := strings.IndexByte(token, '='); eq >= 0 {
			return token[:eq], token[eq+1:], true
		}
		return token, "", false
	}
	if len(token) > 2 {
		return token[:2], token[2:], true
	}
	return token, "", false
}

// quoteOpt renders an option name the way Click's error prose does.
func quoteOpt(name string) string { return "'" + name + "'" }

// isCobraUsageError recognises the errors cobra raises during parsing, before
// any RunE body executes.
func isCobraUsageError(err error) bool {
	msg := err.Error()
	for _, prefix := range []string{
		"unknown command", "unknown flag", "unknown shorthand flag",
		"invalid argument", "flag needs an argument", "bad flag syntax",
	} {
		if strings.HasPrefix(msg, prefix) {
			return true
		}
	}
	return false
}

// usageBoxErr renders a parse failure the way Click does, then exits 2.
func usageBoxErr(cmd *cobra.Command, err error) int {
	msg := err.Error()
	if strings.HasPrefix(msg, "unknown command") {
		// cobra: `unknown command "log" for "khub"` → Click: No such command 'log'.
		if i, j := strings.Index(msg, "\""), strings.LastIndex(msg, "\" for "); i >= 0 && j > i {
			name := msg[i+1 : j]
			msg = "No such command '" + name + "'." + suggest(name, commandNames(cmd))
		}
	} else {
		msg = clickFlagMessage(cmd, err)
	}
	_ = usageBox(cmd, msg)
	return 2
}

func newRoot() *cobra.Command {
	// Registration order is the contract: `khub --help` lists commands in the
	// order main.py registers them, and the bare-invocation fixture pins it.
	// Cobra sorts alphabetically unless told otherwise.
	cobra.EnableCommandSorting = false
	root := &cobra.Command{
		Use:           "khub",
		Short:         "khub — schema-bound context management.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Click parses a group's own options at the group and does NOT forward
		// them to the subcommand, which is what keeps `khub schema --format json
		// types` from making `types` emit JSON. Cobra's Find() hands every
		// leftover token to the child; Traverse() parses per level like Click.
		TraverseChildren: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bare `khub`: Typer's no_args_is_help prints help (stdout) and
			// exits 2 via Click's UsageError.
			renderNoArgsHelp(cmd)
			return &ExitError{Code: 2}
		},
	}
	// Declared for the help renderer only: parseGlobals binds them, so cobra
	// never sees these tokens. They are LOCAL, never persistent — Click does not
	// give a subcommand its group's options.
	root.Flags().StringVarP(&state.Workspace, "workspace", "C", "",
		"Operate on this workspace instead of the working directory.")
	root.Flags().Bool("version", false, "Print the khub version and exit.")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageBox(cmd, clickFlagMessage(cmd, err))
	})
	root.SilenceUsage = true
	// Typer builds the app with add_completion=False, so khub has no
	// completion surface at all: `khub completion` and --install-completion
	// must both be usage errors. Cobra adds `completion` by default.
	root.CompletionOptions.DisableDefaultCmd = true
	// Click has no `help` COMMAND either — `khub help` is "No such command
	// 'help'.", exit 2. A help command whose Use is empty has the empty name,
	// so cobra's InitDefaultHelpCmd finds one already set and never mints the
	// real one; nothing routes to it.
	root.SetHelpCommand(&cobra.Command{Hidden: true})
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) { renderHelp(cmd) })
	// Cobra applies its unknown-subcommand check inside Find(), which
	// TraverseChildren bypasses — say it explicitly, in Click's words.
	root.Args = clickNoSuchCommand

	registerInit(root)
	registerStatus(root)
	registerAdd(root)
	registerGet(root)
	registerEdit(root)
	registerLink(root)
	registerUnlink(root)
	registerRemove(root)
	registerQuery(root)
	registerSearch(root)
	registerNeighbors(root)
	registerImpact(root)
	registerHistory(root)
	registerValidate(root)
	registerCheck(root)
	registerStale(root)
	registerReindex(root)
	registerViz(root)
	registerBackfill(root)
	registerWire(root)
	registerInstallSkills(root)
	registerSchema(root)
	dropHelpShorthand(root) // after every registration, so subcommands are covered
	return root
}

// suggest returns " Did you mean 'x'?" for the nearest candidate, or "" when
// nothing is close enough to be worth saying. A wrong guess is worse than
// silence, so the budget is deliberately tight and scales with the length of
// what was typed: the retired surfaces (`log`, `path`, `build`, `rename`) must
// keep failing with a bare message rather than being pointed at whichever
// command happens to sort nearest.
func suggest(want string, candidates []string) string {
	// Two edits, and never more than the token is long, so a one-character stub
	// cannot reach a two-character name. Two is what separates a typo
	// ("chekc", "stat", "nieghbors") from a different word: every retired
	// surface stays at three or more from its nearest neighbour, so none of
	// them acquires a suggestion.
	limit := 2
	if len(want) < limit {
		limit = len(want)
	}
	best, bestDist := "", 0
	for _, c := range candidates {
		d := editDistance(want, c)
		if d > limit {
			continue
		}
		if best == "" || d < bestDist {
			best, bestDist = c, d
		}
	}
	if best == "" {
		return ""
	}
	return " Did you mean '" + best + "'?"
}

// editDistance is plain Levenshtein over runes, two rows.
func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(min(cur[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

// commandNames lists the commands a user could have meant at cmd's level.
func commandNames(cmd *cobra.Command) []string {
	var names []string
	for _, sub := range cmd.Commands() {
		if !sub.Hidden && sub.Name() != "" {
			names = append(names, sub.Name())
		}
	}
	return names
}

// optionNames lists the long options declared on cmd, plus the group-level ones
// when cmd is the root (parseGlobals binds those, so cobra never sees them).
func optionNames(cmd *cobra.Command) []string {
	var names []string
	if cmd == nil {
		return names
	}
	cmd.Flags().VisitAll(func(f *pflag.Flag) { names = append(names, "--"+f.Name) })
	return names
}

// clickFlagMessage renders Go's flag error the way Click phrases it.
func clickFlagMessage(cmd *cobra.Command, err error) string {
	msg := err.Error()
	if strings.HasPrefix(msg, "unknown flag: ") {
		opt := strings.TrimPrefix(msg, "unknown flag: ")
		return "No such option: " + opt + suggest(opt, optionNames(cmd))
	}
	if strings.HasPrefix(msg, "unknown shorthand flag: ") {
		// pflag: `unknown shorthand flag: 'x' in -x` -> Click: `No such option: -x`
		if i := strings.LastIndex(msg, " in "); i >= 0 {
			return "No such option: " + msg[i+4:]
		}
		return "No such option: " + msg
	}
	// A subcommand parses -h before we can reject it, so pflag reports its own
	// ErrHelp. Click has no -h at all: report it as the unknown option it is.
	if msg == "pflag: help requested" {
		return "No such option: -h"
	}
	return msg
}

// usageBox reproduces Click's Rich-rendered usage error: the usage line, the
// try-help line, then the boxed error, all on stderr, exit 2.
func usageBox(cmd *cobra.Command, message string) error {
	// A command built through newCmd carries its own Click argument spec, which
	// is what makes `khub schema show -h` say "… [OPTIONS] {type}".
	if spec, ok := cmd.Annotations[annotationArgSpec]; ok {
		return clickUsageErr(cmd.CommandPath(), spec, message)
	}
	path := cmd.CommandPath()
	usage := fmt.Sprintf("Usage: %s [OPTIONS] COMMAND [ARGS]...", path)
	if !cmd.HasSubCommands() {
		usage = fmt.Sprintf("Usage: %s [OPTIONS]", path)
	}
	fmt.Fprintln(os.Stderr, usage)
	fmt.Fprintf(os.Stderr, "Try '%s --help' for help.\n", path)
	fmt.Fprintln(os.Stderr, richBox("Error", message))
	return &ExitError{Code: 2}
}

// richBox draws Rich's default 80-column rounded panel.
func richBox(title, body string) string {
	const width = 80
	head := "╭─ " + title + " "
	pad := width - runeLen(head) - 1
	if pad < 0 {
		pad = 0
	}
	top := head + strings.Repeat("─", pad) + "╮"
	inner := width - 4
	rows := make([]string, 0, 2)
	for _, line := range wrapRich(body, inner) {
		rows = append(rows, "│ "+line+strings.Repeat(" ", max(0, inner-runeLen(line)))+" │")
	}
	bottom := "╰" + strings.Repeat("─", width-2) + "╯"
	return top + "\n" + strings.Join(rows, "\n") + "\n" + bottom
}

// reRichWord is Rich's own word splitter (rich/_wrap.py `re_word`): each chunk
// is one word plus the whitespace trailing it.
var reRichWord = regexp.MustCompile(`\s*\S+\s*`)

// wrapRich is Rich's Text.wrap/divide_line, which is what wraps a Panel's body
// — a message longer than the panel's inner width breaks onto a second row
// rather than overflowing and ragging the right border.
//
// The fit test deliberately measures the word WITHOUT its trailing space while
// the running position counts WITH it, so a word that ends exactly at the
// margin still fits. Every recorded usage message is short enough never to
// reach here, so the fixtures alone do not pin this.
func wrapRich(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var lines []string
	var cur []rune
	flush := func() {
		lines = append(lines, strings.TrimRight(string(cur), " \t"))
		cur = nil
	}
	for _, word := range reRichWord.FindAllString(text, -1) {
		runes := []rune(word)
		bare := runeLen(strings.TrimRight(word, " \t"))
		if len(cur)+bare > width {
			if len(cur) > 0 {
				flush()
			}
			// A single word wider than the line folds at exactly `width`
			// (Rich's divide_line with fold=True).
			for len(runes) > width {
				lines = append(lines, string(runes[:width]))
				runes = runes[width:]
			}
		}
		cur = append(cur, runes...)
	}
	flush()
	return lines
}

// dropHelpShorthand pre-registers a shorthand-less `--help` on every command,
// so cobra's InitDefaultHelpFlag (which would add `-h, --help`) finds one
// already there and leaves it alone. Click has no -h: `khub schema -h` is
// "No such option: -h", exit 2.
func dropHelpShorthand(cmd *cobra.Command) {
	if cmd.Flags().Lookup("help") == nil {
		cmd.Flags().Bool("help", false, "Show this message and exit.")
	}
	for _, sub := range cmd.Commands() {
		dropHelpShorthand(sub)
	}
}

func runeLen(s string) int { return len([]rune(s)) }

// resolveRoot is cli/_render.resolve_root: the workspace root honoring -C.
func resolveRoot() (string, error) {
	start := state.Workspace
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		start = cwd
	}
	return workspace.FindWorkspace(start)
}
