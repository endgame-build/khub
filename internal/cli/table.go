package cli

// Human-mode tables. Per the port decision, TTY output is content-pinned (the
// fixtures normalize box geometry away), so this is not a byte-exact Rich
// emulation — but it does share Rich's column solver with help.go, because a
// cell that Rich WRAPS and this one did not is a content difference, not a
// geometry one: it changes how many lines the row occupies.
//
// The model is rich.table.Table's defaults, which are what cli/_render.py gets
// from a bare `Table(title=…)`: HEAVY_HEAD box, padding (0,1) on every column
// including the edges, headers shown, and no expansion — a narrow table stays
// narrow, and only an over-wide one has its widest column collapsed.

import (
	"fmt"
	"os"
	"strings"
)

func printTable(title string, headers []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}
	widths := tableWidths(headers, rows)
	var b strings.Builder
	if title != "" {
		b.WriteString(" " + title + "\n")
	}
	b.WriteString(tableRule("┏", "┳", "┓", "━", widths))
	b.WriteString(tableRow("┃", widths, headers))
	b.WriteString(tableRule("┡", "╇", "┩", "━", widths))
	for _, r := range rows {
		b.WriteString(tableRow("│", widths, r))
	}
	b.WriteString(tableRule("└", "┴", "┘", "─", widths))
	fmt.Fprint(os.Stdout, b.String())
}

// tableWidths solves the column widths, padding included. The box costs one
// column per divider plus the two edges, which is the width Rich withholds from
// the columns before measuring them.
func tableWidths(headers []string, rows [][]string) []int {
	cols := make([]htCol, len(headers))
	cells := make([][]htCell, 0, len(rows)+1)
	cells = append(cells, textRow(headers))
	for _, r := range rows {
		cells = append(cells, textRow(r))
	}
	budget := max(1, helpWidth()-(len(headers)+1))
	return layoutColumns(cols, cells, budget, false, true)
}

func textRow(values []string) []htCell {
	row := make([]htCell, len(values))
	for i, v := range values {
		row[i] = text(v)
	}
	return row
}

func tableRule(left, mid, right, fill string, widths []int) string {
	var b strings.Builder
	b.WriteString(left)
	for i, w := range widths {
		b.WriteString(strings.Repeat(fill, w))
		if i < len(widths)-1 {
			b.WriteString(mid)
		}
	}
	b.WriteString(right + "\n")
	return b.String()
}

// tableRow emits one logical row, which is as many physical lines as its
// tallest wrapped cell needs.
func tableRow(edge string, widths []int, values []string) string {
	cells, height := wrapRow(widths, textRow(values), true)
	var b strings.Builder
	for line := 0; line < height; line++ {
		b.WriteString(edge)
		for i := range widths {
			if line < len(cells[i]) {
				b.WriteString(cells[i][line])
			} else {
				b.WriteString(strings.Repeat(" ", widths[i]))
			}
			if i < len(widths)-1 {
				b.WriteString("│")
			}
		}
		b.WriteString(edge + "\n")
	}
	return b.String()
}
