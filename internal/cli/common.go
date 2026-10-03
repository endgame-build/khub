package cli

// Adapter plumbing every command file shares: the clock seam, the workspace-
// relative path renderer, Click's usage-error box, and the Click-compatible
// splitter behind the dynamic --<field> commands (add/edit/query).

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/endgame-build/khub/internal/canon"
	"github.com/endgame-build/khub/internal/errs"
	"github.com/endgame-build/khub/internal/omap"
)

// today is date.today() at the CLI's one clock seam. KHUB_PARITY_NOW — the env
// var the parity recorder pins Python's clock with — overrides it.
func today() time.Time {
	if pinned := os.Getenv("KHUB_PARITY_NOW"); pinned != "" {
		if d, err := time.ParseInLocation("2006-01-02", pinned, time.UTC); err == nil {
			return d
		}
	}
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// relPath is Path.relative_to(root) as the records print it: forward slashes,
// no leading "./".
func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

// newCmd builds one leaf command in the shape every khub command shares:
// silent cobra diagnostics plus Click's usage line for this command's
// signature, so an unknown flag renders `Usage: khub validate [OPTIONS] [TARGET]`.
func newCmd(use, short, argSpec string, run func(cmd *cobra.Command, args []string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:           use,
		Short:         short,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          run,
		Annotations:   map[string]string{annotationArgSpec: argSpec},
	}
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return clickUsageErr(c.CommandPath(), argSpec, clickFlagMessage(c, err))
	})
	return cmd
}

// annotationArgSpec stashes a command's Click argument spec ("[ID] [PREDICATE]
// [TARGET]") so the usage line, the arity check, and the help renderer all read
// one source.
const annotationArgSpec = "khub.argspec"

// clickArity is Click's arity gate: a command accepts exactly as many
// positionals as it declares, and anything past them is a usage error. Cobra
// leaves Args nil, which is ArbitraryArgs — `khub status foo` would exit 0.
func clickArity(declared int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) <= declared {
			return nil
		}
		return clickUsageErr(cmd.CommandPath(), cmd.Annotations[annotationArgSpec],
			"Got unexpected extra argument(s) ("+strings.Join(args[declared:], " ")+")")
	}
}

// clickNoSuchCommand is the arity gate for a GROUP: an unmatched token there is
// not a stray argument but an unknown subcommand. Cobra's legacyArgs only
// raises that for a parentless command, so `khub schema bogus` would print the
// whole schema and exit 0.
func clickNoSuchCommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return clickUsageErr(cmd.CommandPath(), "COMMAND [ARGS]...",
		"No such command '"+args[0]+"'."+suggest(args[0], commandNames(cmd)))
}

// clickUsageErr reproduces Click's UsageError rendering: the usage line, the
// try-help line, then the boxed error — all on stderr, exit 2.
func clickUsageErr(cmdPath, argSpec, message string) error {
	usage := "Usage: " + cmdPath + " [OPTIONS]"
	if argSpec != "" {
		usage += " " + argSpec
	}
	fmt.Fprintln(os.Stderr, usage)
	fmt.Fprintf(os.Stderr, "Try '%s --help' for help.\n", cmdPath)
	fmt.Fprintln(os.Stderr, richBox("Error", message))
	return &ExitError{Code: 2}
}

// badParameter is Click's BadParameter rendering — the "Invalid value: " prefix
// on a parameter the command itself rejected (a trailing --field with no value,
// --body with --body-file).
func badParameter(cmdPath, argSpec, message string) error {
	return clickUsageErr(cmdPath, argSpec, "Invalid value: "+message)
}

// missingArgument is Click's rendering for a required argument left out — the
// three commands whose ID/TEXT argument Typer declares with `...`.
func missingArgument(cmdPath, argSpec, name string) error {
	return clickUsageErr(cmdPath, argSpec, "Missing argument '"+name+"'.")
}

// splitDynamic is the Click parser for a command running with
// ignore_unknown_options + allow_extra_args.
//
// Click assigns every unknown option token AND every bare positional to one
// leftover list in encounter order, then fills the command's declared
// arguments from the front of it; whatever remains is ctx.args, which
// ParseFields turns into the field map. Known options bind wherever they
// appear, before or after the positional. Reproducing that split is the whole
// reason these commands parse their own flags: pflag's unknown-flag allowlist
// DROPS the token instead of handing it back.
// Only the command's OWN options bind here. The root group's options are not
// inherited (Click never forwards them), so `khub add project -C /x` leaves
// "-C" in the leftovers and ParseFields turns it into the field `_C`.
func splitDynamic(cmd *cobra.Command, args []string, declared int) (positional, extra []string, err error) {
	fs := cmd.Flags()
	var known, leftover []string
	i := 0
	for i < len(args) {
		token := args[i]
		switch {
		case token == "--":
			leftover = append(leftover, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(token, "--") && len(token) > 2:
			name := strings.TrimPrefix(token, "--")
			attached := strings.IndexByte(name, '=') >= 0
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
			}
			flag := fs.Lookup(name)
			if flag == nil {
				leftover = append(leftover, token)
				i++
				continue
			}
			known = append(known, token)
			if !attached && flag.NoOptDefVal == "" && i+1 < len(args) {
				known = append(known, args[i+1])
				i++
			}
			i++
		case strings.HasPrefix(token, "-") && len(token) > 1:
			flag := fs.ShorthandLookup(token[1:2])
			if flag == nil {
				leftover = append(leftover, token)
				i++
				continue
			}
			known = append(known, token)
			if len(token) == 2 && flag.NoOptDefVal == "" && i+1 < len(args) {
				known = append(known, args[i+1])
				i++
			}
			i++
		default:
			leftover = append(leftover, token)
			i++
		}
	}
	if perr := fs.Parse(known); perr != nil {
		return nil, nil, perr
	}
	if len(leftover) < declared {
		return leftover, nil, nil
	}
	return leftover[:declared], leftover[declared:], nil
}

// fieldsOrUsage is ParseFields with Click's BadParameter rendering — a trailing
// --field with no value is a usage error (exit 2), never a silent empty string.
func fieldsOrUsage(cmd *cobra.Command, extra []string, argSpec string) (*omap.Map, error) {
	fields, err := ParseFields(extra)
	if err != nil {
		var usage *errs.Usage
		if errors.As(err, &usage) {
			return nil, badParameter(cmd.CommandPath(), argSpec, usage.Message)
		}
		return nil, err
	}
	return fields, nil
}

// helpRequested re-implements the check cobra runs before RunE, which the
// dynamic commands skip by parsing their own flags. Without it `khub add
// --help` would report a missing TYPE instead of printing help.
func helpRequested(cmd *cobra.Command) bool {
	asked, err := cmd.Flags().GetBool("help")
	return err == nil && asked
}

// arg returns the nth declared positional, or "" when the caller left it out —
// Typer's `None` default, which every command turns into its own hand-written
// missing-argument line.
func arg(positional []string, n int) string {
	if n < len(positional) {
		return positional[n]
	}
	return ""
}

// pickBody is entity_cmd._pick_body: the body from --body or --body-file, one
// source only. nil means "no body supplied" (add writes empty, edit keeps the
// current body); a pointer to "" is an explicit empty body.
func pickBody(cmd *cobra.Command, bodyText, bodyFile string) (*string, error) {
	hasText := cmd.Flags().Changed("body")
	hasFile := cmd.Flags().Changed("body-file")
	if hasText && hasFile {
		return nil, errBothBodies
	}
	if hasText {
		return &bodyText, nil
	}
	if !hasFile {
		return nil, nil
	}
	// Both branches normalize line endings, because Python's `sys.stdin.read()`
	// and `Path.read_text()` both apply universal newlines. Without it a CRLF
	// file handed to --body-file put literal \r bytes inside the entity body,
	// which no fixture covered and which nothing downstream ever strips.
	if bodyFile == "-" {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		text := canon.NormalizeNewlines(string(raw))
		return &text, nil
	}
	text, err := canon.ReadText(bodyFile)
	if err != nil {
		return nil, err
	}
	return &text, nil
}

// errBothBodies is the BadParameter --body/--body-file conflict; the command
// renders it through Click's box because Typer raises it at parameter altitude.
var errBothBodies = errors.New("Pass --body or --body-file, not both")

// optString is Typer's `Option(None)`: the value when the flag was given, nil
// when it was not. `--type ""` and an absent `--type` are NOT the same thing.
func optString(cmd *cobra.Command, name string, value string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}

func optInt(cmd *cobra.Command, name string, value int) *int {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &value
}

// strList renders a []string as the JSON list Python emits — never null.
func strList(ss []string) []any {
	out := make([]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, s)
	}
	return out
}

// nullable renders Python's `str | None`: "" is None on the Go side.
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// pyStr is Python's str() over a loaded frontmatter value — what the human
// entity table prints in its value column.
func pyStr(v any) string {
	switch x := v.(type) {
	case nil:
		return "None"
	case bool:
		return pyBool(x)
	case string:
		return x
	case canon.Date:
		return x.ISO
	case canon.DateTime:
		return x.ISO
	case canon.BigInt:
		return x.Literal
	case float64:
		return canon.PyFloatRepr(x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, pyRepr(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *omap.Map:
		parts := make([]string, 0, x.Len())
		for _, k := range x.Keys() {
			val, _ := x.Get(k)
			parts = append(parts, pyRepr(k)+": "+pyRepr(val))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprint(v)
}

// pyRepr is repr() for the values that appear inside a printed list or dict.
func pyRepr(v any) string {
	if s, ok := v.(string); ok {
		return "'" + strings.ReplaceAll(s, "'", `\'`) + "'"
	}
	return pyStr(v)
}

// pathAction is the {path, action} pair a file-writing tail reports. Wire's
// Outcome is one, and init and upgrade render it the same way.
type pathAction struct{ Path, Action string }

// pathActionRecords renders the pairs as the JSON list those commands carry:
// `{path, action}`, in that order.
func pathActionRecords(items []pathAction) []any {
	out := make([]any, 0, len(items))
	for _, item := range items {
		record := omap.New()
		record.Set("path", item.Path)
		record.Set("action", item.Action)
		out = append(out, record)
	}
	return out
}

// tail is one best-effort step run after a scaffold or an upgrade — wire,
// the legacy skill cleanup, index. Result is set when it ran clean, Err (the located prose)
// when it failed, neither when the caller skipped it. By the time a tail runs
// the workspace is already written, so none of them is ever fatal.
type tail[T any] struct {
	Result *T
	Err    string
}

// tailOf wraps a step's return in a tail: the result when it ran clean, the
// error's prose when it did not.
func tailOf[T any](result *T, err error) tail[T] {
	if err != nil {
		return tail[T]{Err: tailError(err)}
	}
	return tail[T]{Result: result}
}

// set writes the tail into a payload at key: render(Result) when it ran,
// null when it did not and nullWhenAbsent says so (init omits `wire` when
// skipped; upgrade reports every tail), then key_error right after it when
// the step failed.
func (t tail[T]) set(p *omap.Map, key string, render func(*T) any, nullWhenAbsent bool) {
	switch {
	case t.Result != nil:
		p.Set(key, render(t.Result))
	case nullWhenAbsent:
		p.Set(key, nil)
	}
	if t.Err != "" {
		p.Set(key+"_error", t.Err)
	}
}

// tailError is the message a best-effort tail reports: the located prose
// when there is one, the raw error otherwise.
func tailError(err error) string {
	var located *errs.Located
	if errors.As(err, &located) {
		return located.Message
	}
	return err.Error()
}
