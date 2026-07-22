// Copyright (c) 2026 Aristarh Ucolov.
package util

import (
	"fmt"
	"strings"
	"testing"
)

func render(r DiffResult) string {
	var b strings.Builder
	for _, o := range r.Ops {
		b.WriteString(o.Kind + o.Text + "\n")
	}
	return b.String()
}

func TestDiffLinesFindsASingleEdit(t *testing.T) {
	old := "a\nb\nc\nd\ne\n"
	nw := "a\nb\nCHANGED\nd\ne\n"
	r := DiffLines(old, nw, 1)
	if r.Identical {
		t.Fatal("reported identical")
	}
	if r.Added != 1 || r.Removed != 1 {
		t.Errorf("added=%d removed=%d, want 1/1\n%s", r.Added, r.Removed, render(r))
	}
	out := render(r)
	if !strings.Contains(out, "-c\n") || !strings.Contains(out, "+CHANGED\n") {
		t.Errorf("edit not represented:\n%s", out)
	}
}

func TestDiffLinesIdentical(t *testing.T) {
	r := DiffLines("x\ny\n", "x\ny\n", 2)
	if !r.Identical || len(r.Ops) != 0 {
		t.Errorf("identical files produced a diff: %+v", r)
	}
}

func TestDiffLinesAdditionAndRemoval(t *testing.T) {
	r := DiffLines("a\nb\n", "a\nb\nc\n", 0)
	if r.Added != 1 || r.Removed != 0 {
		t.Errorf("append: added=%d removed=%d", r.Added, r.Removed)
	}
	r2 := DiffLines("a\nb\nc\n", "a\nc\n", 0)
	if r2.Added != 0 || r2.Removed != 1 {
		t.Errorf("delete: added=%d removed=%d", r2.Added, r2.Removed)
	}
}

// The case this exists for: a 30 000-line types.xml with three changed lines
// must diff instantly, not quadratically.
func TestDiffLinesHandlesALargeFileWithASmallEdit(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 30000; i++ {
		line := fmt.Sprintf("        <nominal>%d</nominal>\n", i)
		a.WriteString(line)
		if i == 15000 {
			b.WriteString("        <nominal>999</nominal>\n")
		} else {
			b.WriteString(line)
		}
	}
	r := DiffLines(a.String(), b.String(), 2)
	if r.Truncated {
		t.Fatal("a three-line edit was reported as too large to diff")
	}
	if r.Added != 1 || r.Removed != 1 {
		t.Errorf("added=%d removed=%d, want 1/1", r.Added, r.Removed)
	}
	if len(r.Ops) > 20 {
		t.Errorf("ops = %d; the common prefix/suffix was not stripped", len(r.Ops))
	}
}

// A rewrite of a huge file must degrade to counts rather than hang.
func TestDiffLinesTruncatesAnEnormousChange(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 6000; i++ {
		fmt.Fprintf(&a, "old %d\n", i)
		fmt.Fprintf(&b, "new %d\n", i)
	}
	r := DiffLines(a.String(), b.String(), 2)
	if !r.Truncated {
		t.Fatal("expected the diff to be truncated")
	}
	if len(r.Ops) != 0 {
		t.Error("truncated diff should carry no ops")
	}
	if r.Added == 0 || r.Removed == 0 {
		t.Errorf("counts should still be reported: +%d -%d", r.Added, r.Removed)
	}
}

// CRLF and LF versions of the same content are not a real change.
func TestDiffLinesIgnoresLineEndingStyle(t *testing.T) {
	r := DiffLines("a\r\nb\r\n", "a\nb\n", 0)
	if r.Added != 0 || r.Removed != 0 {
		t.Errorf("line-ending change reported as content change: +%d -%d", r.Added, r.Removed)
	}
}
