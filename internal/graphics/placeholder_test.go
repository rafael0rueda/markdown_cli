package graphics

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/render"
)

func tmuxRenderer() *Renderer {
	r := testRenderer(Kitty)
	r.Tmux = true
	return r
}

func TestTmuxPassthroughDoublesEscapes(t *testing.T) {
	got := tmuxPassthrough("\x1b_Ga=q\x1b\\")
	want := "\x1bPtmux;\x1b\x1b_Ga=q\x1b\x1b\\\x1b\\"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestPlaceholderCellMatchesIcat holds the encoding to what kitty's own icat
// writes for image 305419896 (0x12345678) in one cell: the low three bytes as
// the color, then row 0, column 0 and the top byte as combining marks.
func TestPlaceholderCellMatchesIcat(t *testing.T) {
	k := &kittyImage{id: 0x12345678}
	got := k.placeholders(1, 0, 1, 0)
	want := "\x1b[38;2;52;86;120m\U0010EEEE̅̅ͤ\x1b[39m"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// placeholderRows splits drawn placeholder text into rows of cells, each cell
// given as its row and column numbers.
func placeholderRows(t *testing.T, s string) [][][2]int {
	t.Helper()
	index := map[rune]int{}
	for i, d := range placeholderDiacritics {
		index[d] = i
	}
	var rows [][][2]int
	for _, line := range strings.Split(s, "\r\x1b[B") {
		var cells [][2]int
		for _, cell := range strings.Split(line, string(placeholderRune))[1:] {
			marks := []rune(cell)
			if len(marks) < 2 {
				t.Fatalf("cell without row and column: %q", cell)
			}
			cells = append(cells, [2]int{index[marks[0]], index[marks[1]]})
		}
		rows = append(rows, cells)
	}
	return rows
}

func TestTmuxEncodeDrawsPlaceholders(t *testing.T) {
	r := tmuxRenderer()
	ref := filepath.Join("testdata", "gradient.png")
	cols, rows, err := r.Measure(ref, 12, 6, render.SizeHint{})
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Encode(ref, cols, rows, 2)
	if err != nil {
		t.Fatal(err)
	}

	// Every graphics command goes through tmux; a bare one would retitle
	// the pane instead.
	if bare := strings.Count(out, "\x1b_G") - strings.Count(out, "\x1b\x1b_G"); bare != 0 {
		t.Errorf("%d graphics commands not wrapped for tmux: %q", bare, truncate(out))
	}
	if !strings.Contains(out, "\x1b\x1b_Ga=p,U=1,") || !strings.Contains(out, "c="+itoa(cols)+",r="+itoa(rows)) {
		t.Errorf("no virtual placement of the reserved size: %q", truncate(out))
	}
	if strings.Contains(out, "C=1") {
		t.Errorf("an ordinary placement was made as well: %q", truncate(out))
	}

	payload := out[strings.LastIndex(out, "\x1b\\")+2:]
	grid := placeholderRows(t, payload)
	if len(grid) != rows {
		t.Fatalf("%d rows drawn, want %d", len(grid), rows)
	}
	for i, cells := range grid {
		if len(cells) != cols {
			t.Fatalf("row %d has %d cells, want %d", i, len(cells), cols)
		}
		for j, c := range cells {
			if c != [2]int{i, j} {
				t.Fatalf("cell %d,%d is numbered %v", i, j, c)
			}
		}
	}
	// Rows after the first line up under the first, beside whatever the
	// indent holds.
	if n := strings.Count(payload, "\r\x1b[B\x1b[2C"); n != rows-1 {
		t.Errorf("%d rows re-indented, want %d", n, rows-1)
	}
}

// TestTmuxCroppedDrawNamesTheVisibleRows is scrolling in the pager: the rows
// scrolled off are left out, and the ones shown keep their own numbers.
func TestTmuxCroppedDrawNamesTheVisibleRows(t *testing.T) {
	r := tmuxRenderer()
	ref := filepath.Join("testdata", "gradient.png")

	first, err := r.DrawCropped(ref, 6, 5, 0, 5, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.DrawCropped(ref, 6, 5, 2, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "\x1bPtmux;") {
		t.Error("the first draw should send the image")
	}
	if strings.Contains(second, "\x1bPtmux;") {
		t.Errorf("the image was sent again: %q", truncate(second))
	}
	grid := placeholderRows(t, second)
	if len(grid) != 2 || grid[0][0] != [2]int{2, 0} || grid[1][5] != [2]int{3, 5} {
		t.Errorf("cropped rows are numbered %v", grid)
	}
	if r.ClearPlacements() != "" {
		t.Error("placeholders are text; there are no placements to clear")
	}
}

// TestPlaceholderImagesFitTheDiacritics: cells past the last numbered row or
// column could not be drawn, so the image is measured to fit.
func TestPlaceholderImagesFitTheDiacritics(t *testing.T) {
	ref := filepath.Join("testdata", "wide.png")
	plain := testRenderer(Kitty)
	plain.CellWidth, plain.CellHeight = 1, 1
	if cols, _, err := plain.Measure(ref, 1000, 0, render.SizeHint{}); err != nil || cols <= maxPlaceholderCells {
		t.Fatalf("test setup: want an image wider than %d cells, got %d (%v)", maxPlaceholderCells, cols, err)
	}

	r := tmuxRenderer()
	r.CellWidth, r.CellHeight = 1, 1
	cols, rows, err := r.Measure(ref, 1000, 0, render.SizeHint{})
	if err != nil {
		t.Fatal(err)
	}
	if cols > maxPlaceholderCells || rows > maxPlaceholderCells {
		t.Errorf("measured %dx%d, past the %d cells placeholders can number", cols, rows, maxPlaceholderCells)
	}
}
