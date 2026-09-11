package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// tableRows returns the table's content lines, without its top, bottom and
// header rules.
func tableRows(out string) []string {
	var rows []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "│") {
			rows = append(rows, line)
		}
	}
	return rows
}

func widest(lines []string) int {
	w := 0
	for _, l := range lines {
		w = max(w, uniseg.StringWidth(l))
	}
	return w
}

// TestOneLongCellWraps is the case that motivated the sizing: a column of
// single words, and one note that would otherwise set its width for every
// row.
func TestOneLongCellWraps(t *testing.T) {
	src := "| Permission | Octal |\n|:---:|:---:|\n| read | 4 |\n| write | 2 |\n| execute | 1 |\n" +
		"| Example: 750 = owner (7=rwx), group(5=r-x) and others (0=none) | |\n"
	for _, width := range []int{80, 100} {
		rows := tableRows(renderText(t, src, width))
		if len(rows) != 6 {
			t.Errorf("width %d: want the note on two lines, six rows in all; got\n%s",
				width, strings.Join(rows, "\n"))
		}
		if w := widest(rows); w > 50 {
			t.Errorf("width %d: table is %d wide; the long cell is still setting the column", width, w)
		}
	}
}

// TestLongColumnsStayOnOneLine checks the other side of the trade: when many
// cells are long, narrowing would wrap row after row, and a table that fits
// keeps one line per row.
func TestLongColumnsStayOnOneLine(t *testing.T) {
	var b strings.Builder
	b.WriteString("| Flag | Meaning |\n|---|---|\n")
	for i, desc := range []string{
		"Scroll down by one line at a time",
		"Scroll by half a screen in either direction",
		"Jump to the very start or the very end",
		"Search, highlighting matches as you type them in",
		"Quit and restore the screen as it was",
	} {
		fmt.Fprintf(&b, "| -f%d | %s |\n", i, desc)
	}
	rows := tableRows(renderText(t, b.String(), 100))
	if len(rows) != 6 {
		t.Errorf("want one line per row, got\n%s", strings.Join(rows, "\n"))
	}
}

// TestNarrowTableWrapsProseBeforeSplittingWords: when the table has to
// shrink, the column of prose wraps between words, and the identifier beside
// it is left whole. Shrinking the widest column instead would split it.
func TestNarrowTableWrapsProseBeforeSplittingWords(t *testing.T) {
	ident := "render_frontmatter_as_metadata"
	src := "| Setting | Meaning |\n|---|---|\n| " + ident +
		" | Whether the properties at the top of a note are shown as a compact header |\n"
	out := renderText(t, src, 60)
	if !strings.Contains(out, ident) {
		t.Errorf("identifier split across lines:\n%s", out)
	}
	if w := widest(tableRows(out)); w > 60 {
		t.Errorf("table is %d wide, over the page's 60:\n%s", w, out)
	}
}

// TestTableFitsAnyWidth squeezes a table down to where words have to be
// split, and checks it still never overflows or loses text. Twenty columns is
// about the least three columns can be drawn in.
func TestTableFitsAnyWidth(t *testing.T) {
	src := "| Terminal | Images | Hyperlinks |\n|---|---|---|\n" +
		"| iTerm2, Alacritty, Windows Terminal, VS Code | kitty protocol | yes |\n" +
		"| GNOME Terminal (VTE ≥ 0.50) | — | yes |\n"
	for width := 20; width <= 90; width++ {
		out := renderText(t, src, width)
		if w := widest(tableRows(out)); w > width {
			t.Errorf("width %d: table is %d wide:\n%s", width, w, out)
		}
		// Words may be split between lines, but every character has to
		// arrive somewhere.
		if got, want := runeCounts(out), runeCounts(src); !sameCounts(got, want) {
			t.Errorf("width %d: text lost or added:\n%s", width, out)
		}
	}
}

// runeCounts counts the characters of s that are content: not whitespace,
// and not table drawing or markdown table syntax.
func runeCounts(s string) map[rune]int {
	counts := map[rune]int{}
	for _, r := range s {
		if !strings.ContainsRune(" \n│─┌┐└┘├┤┬┴┼|-", r) {
			counts[r]++
		}
	}
	return counts
}

func sameCounts(a, b map[rune]int) bool {
	if len(a) != len(b) {
		return false
	}
	for r, n := range a {
		if b[r] != n {
			return false
		}
	}
	return true
}

// TestCountLinesMatchesWrap keeps the quick count in step with the real
// wrapper, which it has to agree with for the sizing to mean anything.
func TestCountLinesMatchesWrap(t *testing.T) {
	bold := Run{Text: "loud", Style: theme.Style{Bold: true}}
	inputs := [][]Run{
		nil,
		plainRuns("one"),
		plainRuns("the quick brown fox jumps over the lazy dog"),
		plainRuns("  leading and trailing  "),
		plainRuns("double  spaced   words"),
		{{Text: "a "}, bold, {Text: ", then "}, bold, {Text: "."}},
		{{Text: "before"}, hardBreak, {Text: "after the break"}},
		{{Text: "trailing "}, hardBreak},
		plainRuns("日本語 のテキスト が 折り返される"),
		plainRuns("tab\tseparated\twords"),
	}
	for _, runs := range inputs {
		tokens := tokenize(runs)
		for width := 1; width <= 50; width++ {
			n, ok := countLines(tokens, width)
			if !ok {
				continue
			}
			if want := len(wrapRuns(runs, width, nil, nil)); n != want {
				t.Errorf("%q at %d: counted %d lines, wrapRuns made %d", Line{Runs: runs}.Text(), width, n, want)
			}
		}
	}
}
