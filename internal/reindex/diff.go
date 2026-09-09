// A port of the CPython difflib pieces core/reindex.py depends on:
// SequenceMatcher (get_matching_blocks / get_opcodes / get_grouped_opcodes) and
// unified_diff. The diff text is printed VERBATIM by `khub reindex --dry-run`,
// so its bytes are contract — the header lines, the @@ ranges, and the
// three-line context windows must match difflib exactly, which rules out any
// other diff algorithm. difflib is not Myers: it is a recursive
// longest-matching-block search with an autojunk heuristic, and only that
// algorithm produces difflib's hunks.

package reindex

import (
	"sort"
	"strconv"
	"strings"
)

// --- SequenceMatcher ---------------------------------------------------------

type seqMatcher struct {
	a, b []string
	b2j  map[string][]int
}

// newSeqMatcher is SequenceMatcher(None, a, b) — isjunk None (so the junk sets
// stay empty and the junk-extension loops are no-ops) with autojunk on.
func newSeqMatcher(a, b []string) *seqMatcher {
	m := &seqMatcher{a: a, b: b, b2j: map[string][]int{}}
	for i, elt := range b {
		m.b2j[elt] = append(m.b2j[elt], i)
	}
	// Purge popular elements that are not junk (__chain_b).
	if n := len(b); n >= 200 {
		ntest := n/100 + 1
		for elt, idxs := range m.b2j {
			if len(idxs) > ntest {
				delete(m.b2j, elt)
			}
		}
	}
	return m
}

type match struct{ i, j, k int }

// findLongestMatch is SequenceMatcher.find_longest_match with an empty junk
// set: the longest block, earliest in a, then earliest in b.
func (m *seqMatcher) findLongestMatch(alo, ahi, blo, bhi int) match {
	besti, bestj, bestsize := alo, blo, 0
	j2len := map[int]int{}
	for i := alo; i < ahi; i++ {
		newj2len := map[int]int{}
		for _, j := range m.b2j[m.a[i]] {
			if j < blo {
				continue
			}
			if j >= bhi {
				break
			}
			k := j2len[j-1] + 1
			newj2len[j] = k
			if k > bestsize {
				besti, bestj, bestsize = i-k+1, j-k+1, k
			}
		}
		j2len = newj2len
	}
	// Extend by non-junk elements on each end. (The junk-extension passes that
	// follow in Python are unreachable with isjunk=None.)
	for besti > alo && bestj > blo && m.a[besti-1] == m.b[bestj-1] {
		besti, bestj, bestsize = besti-1, bestj-1, bestsize+1
	}
	for besti+bestsize < ahi && bestj+bestsize < bhi &&
		m.a[besti+bestsize] == m.b[bestj+bestsize] {
		bestsize++
	}
	return match{besti, bestj, bestsize}
}

// matchingBlocks is SequenceMatcher.get_matching_blocks, terminated by the
// sentinel (la, lb, 0).
func (m *seqMatcher) matchingBlocks() []match {
	la, lb := len(m.a), len(m.b)
	type span struct{ alo, ahi, blo, bhi int }
	queue := []span{{0, la, 0, lb}}
	var blocks []match
	for len(queue) > 0 {
		q := queue[len(queue)-1] // list.pop() takes the last element
		queue = queue[:len(queue)-1]
		x := m.findLongestMatch(q.alo, q.ahi, q.blo, q.bhi)
		if x.k > 0 {
			blocks = append(blocks, x)
			if q.alo < x.i && q.blo < x.j {
				queue = append(queue, span{q.alo, x.i, q.blo, x.j})
			}
			if x.i+x.k < q.ahi && x.j+x.k < q.bhi {
				queue = append(queue, span{x.i + x.k, q.ahi, x.j + x.k, q.bhi})
			}
		}
	}
	sort.Slice(blocks, func(p, r int) bool {
		if blocks[p].i != blocks[r].i {
			return blocks[p].i < blocks[r].i
		}
		if blocks[p].j != blocks[r].j {
			return blocks[p].j < blocks[r].j
		}
		return blocks[p].k < blocks[r].k
	})
	// Collapse adjacent equal blocks.
	var nonAdjacent []match
	i1, j1, k1 := 0, 0, 0
	for _, blk := range blocks {
		if i1+k1 == blk.i && j1+k1 == blk.j {
			k1 += blk.k
		} else {
			if k1 > 0 {
				nonAdjacent = append(nonAdjacent, match{i1, j1, k1})
			}
			i1, j1, k1 = blk.i, blk.j, blk.k
		}
	}
	if k1 > 0 {
		nonAdjacent = append(nonAdjacent, match{i1, j1, k1})
	}
	return append(nonAdjacent, match{la, lb, 0})
}

type opcode struct {
	tag            string // "replace" | "delete" | "insert" | "equal"
	i1, i2, j1, j2 int
}

// opcodes is SequenceMatcher.get_opcodes.
func (m *seqMatcher) opcodes() []opcode {
	i, j := 0, 0
	var answer []opcode
	for _, blk := range m.matchingBlocks() {
		tag := ""
		switch {
		case i < blk.i && j < blk.j:
			tag = "replace"
		case i < blk.i:
			tag = "delete"
		case j < blk.j:
			tag = "insert"
		}
		if tag != "" {
			answer = append(answer, opcode{tag, i, blk.i, j, blk.j})
		}
		i, j = blk.i+blk.k, blk.j+blk.k
		if blk.k > 0 {
			answer = append(answer, opcode{"equal", blk.i, i, blk.j, j})
		}
	}
	return answer
}

// groupedOpcodes is SequenceMatcher.get_grouped_opcodes(n): opcode groups with
// up to n lines of trailing context, split wherever a long equal run sits.
func (m *seqMatcher) groupedOpcodes(n int) [][]opcode {
	codes := m.opcodes()
	if len(codes) == 0 {
		codes = []opcode{{"equal", 0, 1, 0, 1}}
	}
	// Fixup leading and trailing groups if they show no changes.
	if codes[0].tag == "equal" {
		c := codes[0]
		codes[0] = opcode{c.tag, maxInt(c.i1, c.i2-n), c.i2, maxInt(c.j1, c.j2-n), c.j2}
	}
	if last := len(codes) - 1; codes[last].tag == "equal" {
		c := codes[last]
		codes[last] = opcode{c.tag, c.i1, minInt(c.i2, c.i1+n), c.j1, minInt(c.j2, c.j1+n)}
	}

	nn := n + n
	var groups [][]opcode
	var group []opcode
	for _, c := range codes {
		i1, j1 := c.i1, c.j1
		// End the current group and start a new one whenever there is a large
		// range with no changes.
		if c.tag == "equal" && c.i2-c.i1 > nn {
			group = append(group, opcode{c.tag, i1, minInt(c.i2, i1+n), j1, minInt(c.j2, j1+n)})
			groups = append(groups, group)
			group = nil
			i1, j1 = maxInt(i1, c.i2-n), maxInt(j1, c.j2-n)
		}
		group = append(group, opcode{c.tag, i1, c.i2, j1, c.j2})
	}
	if len(group) > 0 && (len(group) != 1 || group[0].tag != "equal") {
		groups = append(groups, group)
	}
	return groups
}

// --- unified_diff ------------------------------------------------------------

// unifiedDiff is difflib.unified_diff(a, b, fromfile, tofile) joined with "" —
// n=3, lineterm="\n", no file dates. a and b carry their own line endings
// (splitlines(keepends=True)), which is why only the header lines add "\n".
func unifiedDiff(a, b []string, fromfile, tofile string) string {
	var out strings.Builder
	started := false
	sm := newSeqMatcher(a, b)
	for _, group := range sm.groupedOpcodes(3) {
		if !started {
			started = true
			out.WriteString("--- " + fromfile + "\n")
			out.WriteString("+++ " + tofile + "\n")
		}
		first, last := group[0], group[len(group)-1]
		out.WriteString("@@ -" + formatRangeUnified(first.i1, last.i2) +
			" +" + formatRangeUnified(first.j1, last.j2) + " @@\n")
		for _, op := range group {
			if op.tag == "equal" {
				for _, line := range a[op.i1:op.i2] {
					out.WriteString(" " + line)
				}
				continue
			}
			if op.tag == "replace" || op.tag == "delete" {
				for _, line := range a[op.i1:op.i2] {
					out.WriteString("-" + line)
				}
			}
			if op.tag == "replace" || op.tag == "insert" {
				for _, line := range b[op.j1:op.j2] {
					out.WriteString("+" + line)
				}
			}
		}
	}
	return out.String()
}

// formatRangeUnified is difflib._format_range_unified — the "ed" range form.
func formatRangeUnified(start, stop int) string {
	beginning := start + 1 // lines start numbering with one
	length := stop - start
	if length == 1 {
		return strconv.Itoa(beginning)
	}
	if length == 0 {
		beginning-- // empty ranges begin at the line just before the range
	}
	return strconv.Itoa(beginning) + "," + strconv.Itoa(length)
}

// splitLinesKeepEnds is str.splitlines(keepends=True): Python's full
// line-boundary set, not just "\n".
func splitLinesKeepEnds(s string) []string {
	out := []string{}
	runes := []rune(s)
	start := 0
	for i := 0; i < len(runes); i++ {
		if !isLineBoundary(runes[i]) {
			continue
		}
		end := i + 1
		if runes[i] == '\r' && end < len(runes) && runes[end] == '\n' {
			end++
		}
		out = append(out, string(runes[start:end]))
		start = end
		i = end - 1
	}
	if start < len(runes) {
		out = append(out, string(runes[start:]))
	}
	return out
}

func isLineBoundary(ch rune) bool {
	switch ch {
	case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
