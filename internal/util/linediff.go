// Copyright (c) 2026 Aristarh Ucolov.
//
// A line diff sized for config files.
//
// The panel keeps a .bak next to every file it overwrites, but restoring one
// was blind: you picked a timestamp and hoped. Showing what actually differs
// turns "I broke types.xml an hour ago" into a ten-second decision.
//
// types.xml is ~30 000 lines, so a plain O(n·m) LCS is not an option. Real
// edits touch a handful of lines, so we strip the common prefix and suffix
// first — which is nearly the whole file — and run the quadratic part only on
// what is left, with a ceiling beyond which we report a summary instead of
// pretending to produce a readable diff.
package util

import "strings"

// DiffOp is one line in a rendered diff.
type DiffOp struct {
	// Kind is " " (context), "-" (only in the old file) or "+" (only in the new).
	Kind string `json:"kind"`
	// OldLine / NewLine are 1-based line numbers, 0 when the line does not
	// exist on that side.
	OldLine int    `json:"oldLine,omitempty"`
	NewLine int    `json:"newLine,omitempty"`
	Text    string `json:"text"`
}

// DiffResult is what the UI renders.
type DiffResult struct {
	Ops []DiffOp `json:"ops"`
	// Added / Removed count changed lines even when Ops is empty.
	Added   int `json:"added"`
	Removed int `json:"removed"`
	// Truncated is set when the changed region was too large to diff line by
	// line; Ops is then empty and only the counts are meaningful.
	Truncated bool `json:"truncated"`
	Identical bool `json:"identical"`
}

// maxDiffRegion caps the quadratic step. 4000×4000 is ~16M cells, which is
// fast enough, and a change bigger than that is not something anyone reads
// line by line anyway.
const maxDiffRegion = 4000

// DiffLines compares two texts and returns a unified-style op list with
// `context` unchanged lines kept around each change.
func DiffLines(oldText, newText string, context int) DiffResult {
	if oldText == newText {
		return DiffResult{Identical: true}
	}
	a := splitLines(oldText)
	b := splitLines(newText)

	// Common prefix / suffix — on a config file this is almost everything.
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	midA := a[pre : len(a)-suf]
	midB := b[pre : len(b)-suf]

	if len(midA) > maxDiffRegion || len(midB) > maxDiffRegion {
		return DiffResult{Added: len(midB), Removed: len(midA), Truncated: true}
	}

	ops := lcsDiff(midA, midB, pre)
	added, removed := 0, 0
	for _, o := range ops {
		switch o.Kind {
		case "+":
			added++
		case "-":
			removed++
		}
	}

	// Re-attach a little unchanged context so a change is readable in place.
	full := make([]DiffOp, 0, len(ops)+2*context)
	for i := 0; i < context && pre-context+i >= 0; i++ {
		idx := pre - context + i
		full = append(full, DiffOp{Kind: " ", OldLine: idx + 1, NewLine: idx + 1, Text: a[idx]})
	}
	full = append(full, ops...)
	for i := 0; i < context && len(a)-suf+i < len(a); i++ {
		idx := len(a) - suf + i
		full = append(full, DiffOp{Kind: " ", OldLine: idx + 1, NewLine: len(b) - suf + i + 1, Text: a[idx]})
	}

	return DiffResult{Ops: full, Added: added, Removed: removed}
}

// lcsDiff produces ops for the differing middle. offset is how many identical
// lines were stripped from the front, so line numbers stay absolute.
func lcsDiff(a, b []string, offset int) []DiffOp {
	n, m := len(a), len(b)
	// table[i][j] = LCS length of a[i:] and b[j:]
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}

	var ops []DiffOp
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, DiffOp{Kind: " ", OldLine: offset + i + 1, NewLine: offset + j + 1, Text: a[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, DiffOp{Kind: "-", OldLine: offset + i + 1, Text: a[i]})
			i++
		default:
			ops = append(ops, DiffOp{Kind: "+", NewLine: offset + j + 1, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, DiffOp{Kind: "-", OldLine: offset + i + 1, Text: a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, DiffOp{Kind: "+", NewLine: offset + j + 1, Text: b[j]})
	}
	return ops
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
