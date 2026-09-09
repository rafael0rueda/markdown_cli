package render

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"mdv/internal/theme"
)

func plainRuns(texts ...string) []Run {
	out := make([]Run, 0, len(texts))
	for _, t := range texts {
		out = append(out, Run{Text: t})
	}
	return out
}

func lineTexts(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Text()
	}
	return out
}

func TestWrapBasic(t *testing.T) {
	lines := wrapRuns(plainRuns("the quick brown fox jumps over the lazy dog"), 12, nil, nil)
	want := []string{"the quick", "brown fox", "jumps over", "the lazy dog"}
	got := lineTexts(lines)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestWrapHangingIndent(t *testing.T) {
	first := plainRuns("- ")
	rest := plainRuns("  ")
	lines := wrapRuns(plainRuns("alpha beta gamma delta"), 12, first, rest)
	got := lineTexts(lines)
	if got[0] != "- alpha beta" {
		t.Errorf("first line = %q", got[0])
	}
	for _, l := range got[1:] {
		if !strings.HasPrefix(l, "  ") {
			t.Errorf("continuation %q lacks the hanging indent", l)
		}
	}
}

func TestWrapNeverExceedsWidth(t *testing.T) {
	inputs := []string{
		"short",
		strings.Repeat("supercalifragilistic ", 8),
		"a " + strings.Repeat("x", 200),
		"日本語のテキストが折り返されることを確認する",
		"emoji 👨‍👩‍👧‍👦 family sequence stays whole",
		strings.Repeat("ab ", 100),
	}
	for _, in := range inputs {
		for _, width := range []int{1, 2, 3, 5, 10, 40} {
			// A double-width glyph cannot be split below two cells, so at
			// width 1 a single cell of overflow is the best available answer.
			limit := width
			if limit < 2 {
				limit = 2
			}
			for _, line := range wrapRuns(plainRuns(in), width, nil, nil) {
				if w := line.Width(); w > limit {
					t.Errorf("width=%d: %q is %d cells", width, line.Text(), w)
				}
			}
		}
	}
}

// TestWrapPreservesAllText checks that no characters are dropped. Words longer
// than the line are legitimately broken mid-word, so the comparison is made
// with all whitespace removed rather than word by word.
func TestWrapPreservesAllText(t *testing.T) {
	in := "alpha beta gamma delta epsilon zeta eta theta"
	want := strings.ReplaceAll(in, " ", "")
	for _, width := range []int{1, 2, 5, 9, 20, 100} {
		lines := wrapRuns(plainRuns(in), width, nil, nil)
		got := strings.Join(strings.Fields(strings.Join(lineTexts(lines), "")), "")
		if got != want {
			t.Errorf("width=%d: round trip lost text\n got %q\nwant %q", width, got, want)
		}
	}
}

func TestWrapHardBreak(t *testing.T) {
	runs := []Run{{Text: "before"}, hardBreak, {Text: "after"}}
	got := lineTexts(wrapRuns(runs, 40, nil, nil))
	if len(got) != 2 || got[0] != "before" || got[1] != "after" {
		t.Errorf("got %q, want [before after]", got)
	}
}

func TestWrapDropsTrailingSpaceAtBreak(t *testing.T) {
	lines := wrapRuns(plainRuns("alpha beta gamma"), 10, nil, nil)
	for _, l := range lines {
		if strings.HasSuffix(l.Text(), " ") {
			t.Errorf("line %q ends in a space", l.Text())
		}
	}
}

func TestWrapEmptyProducesOneLine(t *testing.T) {
	if got := wrapRuns(nil, 40, nil, nil); len(got) != 1 {
		t.Errorf("got %d lines, want 1", len(got))
	}
}

func TestWrapPreservesStyle(t *testing.T) {
	bold := theme.Style{Bold: true}
	runs := []Run{{Text: "plain "}, {Text: "loud words here", Style: bold}}
	lines := wrapRuns(runs, 10, nil, nil)
	found := false
	for _, l := range lines {
		for _, r := range l.Runs {
			if r.Style == bold && strings.Contains(r.Text, "loud") {
				found = true
			}
		}
	}
	if !found {
		t.Error("bold style did not survive wrapping")
	}
}

func TestSplitToWidth(t *testing.T) {
	tests := []struct {
		in    string
		max   int
		head  string
		tail  string
		width int
	}{
		{"hello", 3, "hel", "lo", 3},
		{"hello", 0, "", "hello", 0},
		{"hello", 10, "hello", "", 5},
		{"日本語", 2, "日", "本語", 2},
		{"日本語", 3, "日", "本語", 2}, // a 2-cell glyph cannot fill 3 cells
		{"", 5, "", "", 0},
	}
	for _, tt := range tests {
		head, tail, w := splitToWidth(tt.in, tt.max)
		if head != tt.head || tail != tt.tail || w != tt.width {
			t.Errorf("splitToWidth(%q, %d) = (%q, %q, %d), want (%q, %q, %d)",
				tt.in, tt.max, head, tail, w, tt.head, tt.tail, tt.width)
		}
	}
}

// TestSplitRunsAlwaysProgresses guards the loops in codeBlock and htmlLine,
// which run until the tail is empty and would hang if a split ever returned
// the whole input as its remainder.
func TestSplitRunsAlwaysProgresses(t *testing.T) {
	for _, text := range []string{"日本語", "abc", "👨‍👩‍👧‍👦x"} {
		for _, width := range []int{1, 2, 3} {
			runs := plainRuns(text)
			for i := 0; ; i++ {
				head, tail := splitRuns(runs, width)
				if len(head) == 0 {
					t.Fatalf("splitRuns(%q, %d) made no progress", text, width)
				}
				if len(tail) == 0 {
					break
				}
				runs = tail
				if i > uniseg.StringWidth(text)+10 {
					t.Fatalf("splitRuns(%q, %d) did not terminate", text, width)
				}
			}
		}
	}
}

func TestAppendRunMerges(t *testing.T) {
	bold := theme.Style{Bold: true}
	runs := appendRun(nil, Run{Text: "a"})
	runs = appendRun(runs, Run{Text: "b"})
	runs = appendRun(runs, Run{Text: "c", Style: bold})
	runs = appendRun(runs, Run{Text: ""})
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2: %+v", len(runs), runs)
	}
	if runs[0].Text != "ab" || runs[1].Text != "c" {
		t.Errorf("unexpected merge: %+v", runs)
	}
}

func TestAppendRunKeepsLinksApart(t *testing.T) {
	runs := appendRun(nil, Run{Text: "a", Link: "x"})
	runs = appendRun(runs, Run{Text: "b", Link: "y"})
	if len(runs) != 2 {
		t.Errorf("runs with different links were merged: %+v", runs)
	}
}

func TestPadTo(t *testing.T) {
	if got := runsWidth(padTo(plainRuns("ab"), 5)); got != 5 {
		t.Errorf("padTo width = %d, want 5", got)
	}
	if got := runsWidth(padTo(plainRuns("abcdef"), 3)); got != 6 {
		t.Errorf("padTo shrank a run: got %d, want 6", got)
	}
}
