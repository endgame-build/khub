// help.go reproduces Typer's Rich-rendered help. Cobra's own template is a
// different document entirely, and `khub` with no arguments prints this help
// (stdout, exit 2) as a byte-pinned fixture, so the layout is a contract, not
// cosmetics.
//
// The shape is Typer's rich_utils.rich_format_help: a padded usage line, the
// command's help prose, then one rounded 80-column Rich Panel per parameter
// group (Arguments, Options, Commands). Inside each panel sits a Rich Table
// with box=None, padding=(0,1) and pad_edge=False, whose column widths follow
// Rich's own algorithm — measure every column, then either distribute the
// surplus or collapse the widest column until the row fits.
package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

// helpWidth is rich.Console's width: COLUMNS wins, then the real terminal, then
// Rich's 80-column fallback for a non-terminal stream.
func helpWidth() int {
	if raw := os.Getenv("COLUMNS"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			return n
		}
	}
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 0 {
		return w
	}
	return 80
}

// renderHelp writes one command's help exactly as Typer's Rich formatter does.
// This is the --help FLAG path, where Click echoes the rendered help and its
// echo() contributes one more newline.
func renderHelp(cmd *cobra.Command) { renderHelpText(cmd, true) }

// renderNoArgsHelp is Typer's no_args_is_help: bare `khub` prints the same help
// and exits 2, but through UsageError rather than Click's echo — so it has NO
// trailing blank line. The fixture pins the difference.
func renderNoArgsHelp(cmd *cobra.Command) { renderHelpText(cmd, false) }

func renderHelpText(cmd *cobra.Command, echoed bool) {
	if !IsTTY() {
		renderPlainHelp(cmd, echoed)
		return
	}
	width := helpWidth()
	var b strings.Builder
	writeUsageBlock(&b, cmd, width)
	writeHelpProse(&b, cmd, width)
	if args := helpArgsFor(cmd); len(args) > 0 {
		writePanel(&b, "Arguments", argumentRows(args), argumentCols(args), width)
	}
	if opts := helpOptsFor(cmd); len(opts) > 0 {
		writePanel(&b, "Options", optionRows(opts), optionCols(opts), width)
	}
	if subs := helpSubcommands(cmd); len(subs) > 0 {
		writePanel(&b, "Commands", commandRows(subs), commandCols(subs), width)
	}
	if echoed {
		b.WriteByte('\n')
	}
	fmt.Fprint(os.Stdout, b.String())
}

// --- usage + prose ------------------------------------------------------------

// writeUsageBlock is `console.print(Padding(usage, 1))`: one blank line, the
// usage indented by one column, one blank line — every line padded to the
// console width.
func writeUsageBlock(b *strings.Builder, cmd *cobra.Command, width int) {
	usage := "Usage: " + cmd.CommandPath() + " " + usageSuffix(cmd)
	writePadded(b, "", width)
	for _, line := range divideLine(usage, width-2) {
		writePadded(b, " "+line, width)
	}
	writePadded(b, "", width)
}

// writeHelpProse is `console.print(Padding(help_text, (0,1,1,1)))`: the first
// paragraph with its single newlines collapsed, then the remaining paragraphs
// verbatim after a blank line, then the bottom pad.
func writeHelpProse(b *strings.Builder, cmd *cobra.Command, width int) {
	first, rest := helpProse(cmd)
	if first == "" && rest == "" {
		return
	}
	for _, line := range divideLine(strings.ReplaceAll(first, "\n", " "), width-2) {
		writePadded(b, " "+line, width)
	}
	if rest != "" {
		writePadded(b, "", width)
		for _, line := range divideLine(rest, width-2) {
			writePadded(b, " "+line, width)
		}
	}
	writePadded(b, "", width)
}

func writePadded(b *strings.Builder, text string, width int) {
	b.WriteString(text)
	if pad := width - runeLen(text); pad > 0 {
		b.WriteString(strings.Repeat(" ", pad))
	}
	b.WriteByte('\n')
}

// --- panels -------------------------------------------------------------------

// writePanel draws one rounded Rich Panel around a laid-out table.
func writePanel(b *strings.Builder, title string, rows [][]htCell, cols []htCol, width int) {
	head := "╭─ " + title + " "
	b.WriteString(head + strings.Repeat("─", max(0, width-runeLen(head)-1)) + "╮\n")
	for _, line := range renderTable(cols, rows, width-4) {
		b.WriteString("│ " + line + " │\n")
	}
	b.WriteString("╰" + strings.Repeat("─", max(0, width-2)) + "╯\n")
}

// --- the table engine ---------------------------------------------------------

// htCol is one column of a Typer help table.
//
//	fixed  — a pinned content width (the Commands panel's name column)
//	ratio  — Rich's flexible-column weight (the Commands panel's description)
//	greedy — a Rich Columns cell, which reports the whole available width as its
//	         maximum and therefore soaks up whatever the other columns leave
type htCol struct {
	fixed  int
	ratio  int
	greedy bool
}

// htCell is one cell: the items of a Rich Columns group. Plain text is a
// one-item cell; the help column adds "[default: …]" / "[required]" as further
// items, which Rich packs onto the same line when they fit.
type htCell struct{ items []string }

func text(s string) htCell {
	if s == "" {
		return htCell{}
	}
	return htCell{items: []string{s}}
}

// renderTable lays the columns out and returns the rendered lines, each padded
// to maxWidth.
func renderTable(cols []htCol, rows [][]htCell, maxWidth int) []string {
	widths := layoutColumns(cols, rows, maxWidth, true, false)
	var out []string
	for _, row := range rows {
		cells, height := wrapRow(widths, row, false)
		for line := 0; line < height; line++ {
			var b strings.Builder
			for i := range widths {
				if line < len(cells[i]) {
					b.WriteString(cells[i][line])
					continue
				}
				b.WriteString(strings.Repeat(" ", widths[i]))
			}
			out = append(out, padTo(strings.TrimRight(b.String(), " "), maxWidth))
		}
	}
	return out
}

// wrapRow renders one row's cells into per-column lines, each already padded to
// its column's padded width. The second result is the row's height in lines
// (Rich's vertical alignment is "top", so a short cell simply runs out).
func wrapRow(widths []int, row []htCell, padEdge bool) (cells [][]string, height int) {
	cells = make([][]string, len(widths))
	for i := range widths {
		padL, padR := cellPadding(i, len(widths), padEdge)
		inner := max(0, widths[i]-padL-padR)
		var lines []string
		if i < len(row) {
			lines = renderCell(row[i], inner)
		}
		for j, ln := range lines {
			lines[j] = strings.Repeat(" ", padL) + padTo(ln, inner) + strings.Repeat(" ", padR)
		}
		cells[i] = lines
		height = max(height, len(lines))
	}
	return cells, height
}

// cellPadding is Rich's padding=(0,1). With pad_edge=False (Typer's help
// panels) the first column loses its left pad and the last its right pad; the
// human data tables keep both.
func cellPadding(index, count int, padEdge bool) (left, right int) {
	left, right = 1, 1
	if padEdge {
		return left, right
	}
	if index == 0 {
		left = 0
	}
	if index == count-1 {
		right = 0
	}
	return left, right
}

// renderCell is rich.columns.Columns: pack the items into as many grid columns
// as fit on one line, falling back to one item per line.
func renderCell(cell htCell, width int) []string {
	switch len(cell.items) {
	case 0:
		return nil
	case 1:
		return divideLine(cell.items[0], width)
	}
	count := len(cell.items)
	for count > 1 {
		colWidths := make([]int, count)
		fits := true
		for i, item := range cell.items {
			slot := i % count
			colWidths[slot] = max(colWidths[slot], runeLen(item))
			used := 0
			for _, w := range colWidths {
				used += w
			}
			if used+count-1 > width {
				count--
				fits = false
				break
			}
		}
		if fits {
			break
		}
	}
	if count <= 1 {
		var lines []string
		for _, item := range cell.items {
			lines = append(lines, divideLine(item, width)...)
		}
		return lines
	}
	// Every item is its own grid column here (khub never emits more items than
	// fit on one row), so the row is the items joined by the grid padding.
	return divideLine(strings.Join(cell.items, " "), width)
}

// layoutColumns is rich.table.Table._calculate_column_widths for the subset
// khub builds: measure, distribute a ratio column's share, collapse the widest
// column while the row overflows, then — for an expanding table — spread any
// surplus. Typer's help panels expand; the human data tables do not.
func layoutColumns(cols []htCol, rows [][]htCell, maxWidth int, expand, padEdge bool) []int {
	widths := make([]int, len(cols))
	for i, col := range cols {
		padL, padR := cellPadding(i, len(cols), padEdge)
		pad := padL + padR
		switch {
		case col.fixed > 0:
			widths[i] = min(col.fixed+pad, maxWidth)
		case col.greedy:
			// A Rich Columns cell has no __rich_measure__, so Measurement.get
			// reports the whole available width as its maximum.
			widths[i] = maxWidth
		default:
			natural := 0
			for _, row := range rows {
				if i < len(row) {
					for _, item := range row[i].items {
						natural = max(natural, runeLen(item))
					}
				}
			}
			widths[i] = min(natural+pad, maxWidth)
		}
		if widths[i] == 0 {
			widths[i] = 1
		}
	}

	if anyRatio(cols) {
		fixed := make([]int, len(cols))
		var ratios, minimums []int
		for i, col := range cols {
			if col.ratio > 0 {
				padL, padR := cellPadding(i, len(cols), padEdge)
				ratios = append(ratios, col.ratio)
				minimums = append(minimums, 1+padL+padR)
				continue
			}
			fixed[i] = widths[i]
		}
		flexible := maxWidth - sumOf(fixed)
		shares := ratioDistribute(flexible, ratios, minimums)
		next := 0
		for i, col := range cols {
			if col.ratio > 0 {
				widths[i] = shares[next]
				next++
			}
		}
	}

	total := sumOf(widths)
	if total > maxWidth {
		wrapable := make([]bool, len(cols))
		for i, col := range cols {
			wrapable[i] = col.fixed == 0
		}
		widths = collapseWidths(widths, wrapable, maxWidth)
		total = sumOf(widths)
	}
	if expand && total < maxWidth {
		for i, pad := range ratioDistribute(maxWidth-total, widths, nil) {
			widths[i] += pad
		}
	}
	return widths
}

func anyRatio(cols []htCol) bool {
	for _, col := range cols {
		if col.ratio > 0 {
			return true
		}
	}
	return false
}

// collapseWidths is rich.table.Table._collapse_widths: repeatedly shave the
// widest wrapable column down toward the runner-up until the row fits.
func collapseWidths(widths []int, wrapable []bool, maxWidth int) []int {
	total := sumOf(widths)
	excess := total - maxWidth
	if !anyTrue(wrapable) {
		return widths
	}
	for total > 0 && excess > 0 {
		maxCol := 0
		for i, w := range widths {
			if wrapable[i] && w > maxCol {
				maxCol = w
			}
		}
		second := 0
		for i, w := range widths {
			if wrapable[i] && w != maxCol && w > second {
				second = w
			}
		}
		difference := maxCol - second
		ratios := make([]int, len(widths))
		any := false
		for i, w := range widths {
			if wrapable[i] && w == maxCol {
				ratios[i] = 1
				any = true
			}
		}
		if !any || difference == 0 {
			break
		}
		maxReduce := make([]int, len(widths))
		for i := range maxReduce {
			maxReduce[i] = min(excess, difference)
		}
		widths = ratioReduce(excess, ratios, maxReduce, widths)
		total = sumOf(widths)
		excess = total - maxWidth
	}
	return widths
}

// ratioDistribute is rich._ratio.ratio_distribute.
func ratioDistribute(total int, ratios []int, minimums []int) []int {
	effective := append([]int{}, ratios...)
	if minimums != nil {
		for i := range effective {
			if minimums[i] == 0 {
				effective[i] = 0
			}
		}
	}
	totalRatio := sumOf(effective)
	remaining := total
	out := make([]int, len(effective))
	for i, ratio := range effective {
		minimum := 0
		if minimums != nil {
			minimum = minimums[i]
		}
		distributed := remaining
		if totalRatio > 0 {
			distributed = max(minimum, ceilDiv(ratio*remaining, totalRatio))
		}
		out[i] = distributed
		totalRatio -= ratio
		remaining -= distributed
	}
	return out
}

// ratioReduce is rich._ratio.ratio_reduce.
func ratioReduce(total int, ratios, maximums, values []int) []int {
	effective := append([]int{}, ratios...)
	for i := range effective {
		if maximums[i] == 0 {
			effective[i] = 0
		}
	}
	totalRatio := sumOf(effective)
	out := append([]int{}, values...)
	if totalRatio == 0 {
		return out
	}
	remaining := total
	for i, ratio := range effective {
		if ratio != 0 && totalRatio > 0 {
			distributed := min(maximums[i], roundDiv(ratio*remaining, totalRatio))
			out[i] = values[i] - distributed
			remaining -= distributed
			totalRatio -= ratio
		}
	}
	return out
}

func ceilDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	q := a / b
	if a%b != 0 && (a > 0) == (b > 0) {
		q++
	}
	return q
}

func roundDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	return (2*a + b) / (2 * b)
}

func sumOf(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}

func anyTrue(bs []bool) bool {
	for _, b := range bs {
		if b {
			return true
		}
	}
	return false
}

func padTo(s string, width int) string {
	if pad := width - runeLen(s); pad > 0 {
		return s + strings.Repeat(" ", pad)
	}
	return s
}

// --- word wrapping ------------------------------------------------------------

// divideLine is rich._wrap.divide_line: greedy word wrap where a word's trailing
// whitespace does not count toward the fit test, so a double space inside the
// prose survives the wrap. Explicit newlines break the line outright.
func divideLine(text string, width int) []string {
	if width < 1 {
		width = 1
	}
	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		out = append(out, wrapParagraph(paragraph, width)...)
	}
	return out
}

func wrapParagraph(text string, width int) []string {
	tokens := splitWords(text)
	if len(tokens) == 0 {
		return []string{""}
	}
	var lines []string
	var cur strings.Builder
	position := 0
	for _, token := range tokens {
		bare := runeLen(strings.TrimRight(token, " \t"))
		if position+bare > width {
			if bare > width {
				if position > 0 {
					lines = append(lines, strings.TrimRight(cur.String(), " "))
					cur.Reset()
					position = 0
				}
				chunks := chopCells(token, width)
				for i, chunk := range chunks {
					if i == len(chunks)-1 {
						cur.WriteString(chunk)
						position = runeLen(chunk)
						break
					}
					lines = append(lines, strings.TrimRight(chunk, " "))
				}
				continue
			}
			if position > 0 {
				lines = append(lines, strings.TrimRight(cur.String(), " "))
				cur.Reset()
				position = 0
			}
		}
		cur.WriteString(token)
		position += runeLen(token)
	}
	lines = append(lines, strings.TrimRight(cur.String(), " "))
	return lines
}

// splitWords is rich._wrap.words: each token is a word with the whitespace that
// leads and trails it.
func splitWords(text string) []string {
	runes := []rune(text)
	var tokens []string
	i := 0
	for i < len(runes) {
		start := i
		for i < len(runes) && isSpace(runes[i]) {
			i++
		}
		if i >= len(runes) {
			if start < len(runes) {
				tokens = append(tokens, string(runes[start:]))
			}
			break
		}
		for i < len(runes) && !isSpace(runes[i]) {
			i++
		}
		for i < len(runes) && isSpace(runes[i]) {
			i++
		}
		tokens = append(tokens, string(runes[start:i]))
	}
	return tokens
}

func isSpace(r rune) bool { return r == ' ' || r == '\t' }

func chopCells(s string, width int) []string {
	runes := []rune(s)
	var out []string
	for len(runes) > width {
		out = append(out, string(runes[:width]))
		runes = runes[width:]
	}
	return append(out, string(runes))
}

// --- the three panel shapes ---------------------------------------------------

// helpArg is one positional parameter as Typer's Arguments panel shows it.
type helpArg struct {
	name     string
	metavar  string
	required bool
	help     string
}

// helpOpt is one option as Typer's Options panel shows it.
type helpOpt struct {
	long     string
	short    string
	metavar  string
	help     string
	def      string
	required bool
}

// argumentCols/optionCols share Typer's column model: an optional required
// marker, the long form, the short form (an argument's metavar name lands
// here), two secondary-form columns khub never populates, the type, and the
// help — the last being a Rich Columns cell.
func argumentCols(args []helpArg) []htCol {
	cols := []htCol{{}, {}, {}, {}, {}, {greedy: true}}
	if anyRequiredArg(args) {
		cols = append([]htCol{{}}, cols...)
	}
	return cols
}

func optionCols(opts []helpOpt) []htCol {
	cols := []htCol{{}, {}, {}, {}, {}, {greedy: true}}
	if anyRequiredOpt(opts) {
		cols = append([]htCol{{}}, cols...)
	}
	return cols
}

func argumentRows(args []helpArg) [][]htCell {
	marker := anyRequiredArg(args)
	var rows [][]htCell
	for _, a := range args {
		row := []htCell{text(""), text(a.name), text(""), text(""), text(a.metavar), helpCell(a.help, "", a.required)}
		if marker {
			star := ""
			if a.required {
				star = "*"
			}
			row = append([]htCell{text(star)}, row...)
		}
		rows = append(rows, row)
	}
	return rows
}

func optionRows(opts []helpOpt) [][]htCell {
	marker := anyRequiredOpt(opts)
	var rows [][]htCell
	for _, o := range opts {
		row := []htCell{text(o.long), text(o.short), text(""), text(""), text(o.metavar),
			helpCell(o.help, o.def, o.required)}
		if marker {
			star := ""
			if o.required {
				star = "*"
			}
			row = append([]htCell{text(star)}, row...)
		}
		rows = append(rows, row)
	}
	return rows
}

// helpCell is typer.rich_utils._get_parameter_help: the prose, then the default,
// then the required marker, grouped as Rich Columns items.
func helpCell(prose, def string, required bool) htCell {
	var items []string
	if prose != "" {
		items = append(items, strings.ReplaceAll(prose, "\n", " "))
	}
	if def != "" {
		items = append(items, "[default: "+def+"]")
	}
	if required {
		items = append(items, "[required]")
	}
	return htCell{items: items}
}

func anyRequiredArg(args []helpArg) bool {
	for _, a := range args {
		if a.required {
			return true
		}
	}
	return false
}

func anyRequiredOpt(opts []helpOpt) bool {
	for _, o := range opts {
		if o.required {
			return true
		}
	}
	return false
}

func commandCols(subs []*cobra.Command) []htCol {
	longest := 0
	for _, sub := range subs {
		longest = max(longest, runeLen(sub.Name()))
	}
	return []htCol{{fixed: longest}, {ratio: 10}}
}

func commandRows(subs []*cobra.Command) [][]htCell {
	var rows [][]htCell
	for _, sub := range subs {
		rows = append(rows, []htCell{text(sub.Name()), text(sub.Short)})
	}
	return rows
}

// helpSubcommands lists a group's visible children in registration order.
func helpSubcommands(cmd *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, sub := range cmd.Commands() {
		if sub.Hidden || sub.Name() == "" {
			continue
		}
		out = append(out, sub)
	}
	return out
}

// helpOptsFor reads a command's options off its flag set in declaration order,
// which is the order Click reports them (with --help always last).
func helpOptsFor(cmd *cobra.Command) []helpOpt {
	flags := cmd.Flags()
	flags.SortFlags = false
	var out []helpOpt
	flags.VisitAll(func(f *pflag.Flag) {
		if f.Hidden {
			return
		}
		short := ""
		if f.Shorthand != "" {
			short = "-" + f.Shorthand
		}
		out = append(out, helpOpt{
			long:    "--" + f.Name,
			short:   short,
			metavar: flagMetavar(cmd, f),
			help:    f.Usage,
			def:     flagDefault(f),
		})
	})
	return out
}

// pathMetavars are the options Typer declares as `Path`, which Click renders as
// <path> rather than <str>.
var pathMetavars = map[string]bool{
	"khub --workspace":          true,
	"khub init --preset-source": true,
}

func flagMetavar(cmd *cobra.Command, f *pflag.Flag) string {
	if pathMetavars[cmd.CommandPath()+" --"+f.Name] {
		return "<path>"
	}
	switch f.Value.Type() {
	case "bool":
		return ""
	case "int":
		return "<int>"
	default:
		return "<str>"
	}
}

// flagDefault renders Click's "[default: …]" suffix. Typer's `Option(None)`
// shows nothing, and its Go twin is the zero value: an empty string, a zero
// count, an empty repeatable, or any bool.
func flagDefault(f *pflag.Flag) string {
	switch f.Value.Type() {
	case "bool":
		return ""
	case "int":
		if f.DefValue == "0" {
			return ""
		}
	case "stringSlice", "stringArray":
		return ""
	default:
		if f.DefValue == "" {
			return ""
		}
	}
	return f.DefValue
}

// --- per-command metadata ------------------------------------------------------

// usageSuffix is the argument half of Click's usage line.
func usageSuffix(cmd *cobra.Command) string {
	if cmd.HasSubCommands() && len(helpSubcommands(cmd)) > 0 {
		return "[OPTIONS] COMMAND [ARGS]..."
	}
	if spec, ok := cmd.Annotations[annotationArgSpec]; ok && spec != "" {
		return "[OPTIONS] " + spec
	}
	return "[OPTIONS]"
}

// helpProse splits a command's docstring the way Typer does: the first
// paragraph (single newlines collapsed) and everything after the first blank
// line, verbatim.
func helpProse(cmd *cobra.Command) (first, rest string) {
	if cmd.Long != "" {
		paragraphs := strings.SplitN(cmd.Long, "\n\n", 2)
		if len(paragraphs) == 2 {
			return paragraphs[0], paragraphs[1]
		}
		return cmd.Long, ""
	}
	return cmd.Short, ""
}

// helpArgsFor is the Arguments panel's contents. Cobra models positionals only
// as prose, so their names, types and help live here — the one place the port
// carries per-command help metadata.
func helpArgsFor(cmd *cobra.Command) []helpArg {
	return helpArguments[cmd.CommandPath()]
}

var helpArguments = map[string][]helpArg{
	"khub init": {
		{name: "preset", metavar: "<str>", help: "Named preset to seed from (e.g. firm-ops)."},
		{name: "path", metavar: "<path>", help: "Target directory (default: .)."},
	},
	"khub add": {
		{name: "TYPE", metavar: "<str>", help: "The entity type to create."},
	},
	"khub get": {
		{name: "ID...", metavar: "<str>", help: "One or more bare slugs or qualified type/slug IDs, in output order."},
	},
	"khub edit": {
		{name: "ID", metavar: "<str>", help: "A bare slug, or type/slug on ambiguity."},
	},
	"khub link": {
		{name: "ID", metavar: "<str>"},
		{name: "PREDICATE", metavar: "<str>"},
		{name: "TARGET", metavar: "<str>"},
	},
	"khub unlink": {
		{name: "ID", metavar: "<str>"},
		{name: "PREDICATE", metavar: "<str>"},
		{name: "TARGET", metavar: "<str>"},
	},
	"khub remove": {
		{name: "ID", metavar: "<str>", help: "A bare slug, or type/slug on ambiguity."},
	},
	"khub search": {
		{name: "text", metavar: "<str>", required: true,
			help: `FTS5 MATCH text: terms, "phrases", OR, NEAR, prefix*.`},
	},
	"khub neighbors": {
		{name: "ID", metavar: "<str>", required: true, help: "A bare slug, or type/slug on ambiguity."},
	},
	"khub impact": {
		{name: "ID", metavar: "<str>", required: true, help: "A bare slug, or type/slug on ambiguity."},
	},
	"khub history": {
		{name: "ID", metavar: "<str>", required: true, help: "A bare slug, or type/slug on ambiguity."},
	},
	"khub validate": {
		{name: "TARGET", metavar: "<str>", help: "A type or type/slug; default: all."},
	},
	"khub schema show": {
		{name: "type", metavar: "<str>", required: true, help: "Type name."},
	},
}

// Plain help reuses the same metadata and ordering without terminal chrome.
func renderPlainHelp(cmd *cobra.Command, echoed bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s %s\n", cmd.CommandPath(), usageSuffix(cmd))
	first, rest := helpProse(cmd)
	for _, prose := range []string{first, rest} {
		if prose != "" {
			fmt.Fprintf(&b, "\n%s\n", prose)
		}
	}
	if args := helpArgsFor(cmd); len(args) > 0 {
		b.WriteString("\nArguments:\n")
		for _, a := range args {
			fmt.Fprintf(&b, "  %s %s", a.name, a.metavar)
			if a.help != "" {
				fmt.Fprintf(&b, "  %s", a.help)
			}
			if a.required {
				b.WriteString(" [required]")
			}
			b.WriteByte('\n')
		}
	}
	if opts := helpOptsFor(cmd); len(opts) > 0 {
		b.WriteString("\nOptions:\n")
		for _, o := range opts {
			name := o.long
			if o.short != "" {
				name += ", " + o.short
			}
			if o.metavar != "" {
				name += " " + o.metavar
			}
			fmt.Fprintf(&b, "  %s  %s", name, o.help)
			if o.def != "" {
				fmt.Fprintf(&b, " [default: %s]", o.def)
			}
			if o.required {
				b.WriteString(" [required]")
			}
			b.WriteByte('\n')
		}
	}
	if subs := helpSubcommands(cmd); len(subs) > 0 {
		b.WriteString("\nCommands:\n")
		for _, sub := range subs {
			fmt.Fprintf(&b, "  %s  %s\n", sub.Name(), sub.Short)
		}
	}
	if echoed {
		b.WriteByte('\n')
	}
	fmt.Fprint(os.Stdout, b.String())
}
