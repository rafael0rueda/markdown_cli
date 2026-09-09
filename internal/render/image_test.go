package render

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"mdv/internal/theme"
)

// fakeImages stands in for the graphics package so the renderer can be tested
// without decoding anything.
type fakeImages struct {
	cols, rows int
	measureErr error
	encodeErr  error

	measured []string
	encoded  []string
}

func (f *fakeImages) Measure(ref string, maxCols, maxRows int) (int, int, error) {
	f.measured = append(f.measured, ref)
	if f.measureErr != nil {
		return 0, 0, f.measureErr
	}
	cols, rows := f.cols, f.rows
	if cols > maxCols {
		cols = maxCols
	}
	if maxRows > 0 && rows > maxRows {
		rows = maxRows
	}
	return cols, rows, nil
}

func (f *fakeImages) Encode(ref string, cols, rows, indent int) (string, error) {
	f.encoded = append(f.encoded, fmt.Sprintf("%s:%dx%d+%d", ref, cols, rows, indent))
	if f.encodeErr != nil {
		return "", f.encodeErr
	}
	return fmt.Sprintf("<IMG %s %dx%d indent=%d>", ref, cols, rows, indent), nil
}

func renderWithImages(t *testing.T, source string, h ImageHandler) *Doc {
	t.Helper()
	doc, err := Render([]byte(source), Options{
		Width:    60,
		Theme:    theme.Plain(),
		LinkMode: LinkInline,
		Images:   h,
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestBlockImagePlaced(t *testing.T) {
	h := &fakeImages{cols: 20, rows: 5}
	doc := renderWithImages(t, "# Title\n\n![alt text](pic.png)\n\nAfter.\n", h)

	if len(doc.Images) != 1 {
		t.Fatalf("got %d placements, want 1", len(doc.Images))
	}
	img := doc.Images[0]
	if img.Cols != 20 || img.Rows != 5 {
		t.Errorf("footprint = %dx%d, want 20x5", img.Cols, img.Rows)
	}
	if img.Ref != "pic.png" {
		t.Errorf("ref = %q", img.Ref)
	}

	// The rows have to exist in the document, or line numbering breaks.
	if got, ok := doc.ImageAt(img.Line); !ok || got.Ref != img.Ref {
		t.Error("ImageAt did not find the placement at its own line")
	}
	if img.Line+img.Rows > len(doc.Lines) {
		t.Errorf("placement covers lines %d-%d but the document has %d",
			img.Line, img.Line+img.Rows-1, len(doc.Lines))
	}
	// Alt text belongs in the document text whether or not the picture is
	// drawn, so that searching and piping still find it.
	if !strings.Contains(doc.Text(), "alt text") {
		t.Errorf("alt text missing from the document:\n%s", doc.Text())
	}
}

// TestInlineImageNotPlaced covers the rule that only a lone image becomes a
// picture: one inside a sentence cannot be drawn without overwriting the words
// beside it.
func TestInlineImageNotPlaced(t *testing.T) {
	tests := []struct {
		source string
		alts   []string
	}{
		{"Some text ![alt](pic.png) more text.\n", []string{"alt"}},
		{"![one](a.png) ![two](b.png)\n", []string{"one", "two"}},
		{"![alt](pic.png) trailing words\n", []string{"alt"}},
		{"**bold** ![alt](pic.png)\n", []string{"alt"}},
	}
	for _, tt := range tests {
		h := &fakeImages{cols: 20, rows: 5}
		doc := renderWithImages(t, tt.source, h)
		if len(doc.Images) != 0 {
			t.Errorf("%q: got %d placements, want 0", tt.source, len(doc.Images))
		}
		for _, alt := range tt.alts {
			if !strings.Contains(doc.Text(), alt) {
				t.Errorf("%q: alt text %q missing from %q", tt.source, alt, doc.Text())
			}
		}
	}
}

// TestBlockImageIgnoresSurroundingWhitespace checks the common case of a
// formatter having wrapped the line.
func TestBlockImageIgnoresSurroundingWhitespace(t *testing.T) {
	h := &fakeImages{cols: 10, rows: 3}
	doc := renderWithImages(t, "![alt](pic.png)\n", h)
	if len(doc.Images) != 1 {
		t.Errorf("got %d placements, want 1", len(doc.Images))
	}
}

// TestImageMeasureFailureFallsBack is the path taken by a missing or corrupt
// file: a broken picture must not break the document.
func TestImageMeasureFailureFallsBack(t *testing.T) {
	h := &fakeImages{measureErr: errors.New("no such file")}
	doc := renderWithImages(t, "![alt text](missing.png)\n", h)

	if len(doc.Images) != 0 {
		t.Errorf("a failed measure produced %d placements", len(doc.Images))
	}
	if !strings.Contains(doc.Text(), "alt text") {
		t.Errorf("alt text missing:\n%s", doc.Text())
	}
	// Falling back means going through the normal inline path, which shows the
	// destination too.
	if !strings.Contains(doc.Text(), "missing.png") {
		t.Errorf("destination missing:\n%s", doc.Text())
	}
}

func TestImageNilHandlerFallsBack(t *testing.T) {
	doc := renderWithImages(t, "![alt text](pic.png)\n", nil)
	if len(doc.Images) != 0 {
		t.Errorf("got %d placements with no handler", len(doc.Images))
	}
	if !strings.Contains(doc.Text(), "alt text") {
		t.Error("alt text missing")
	}
}

func TestImageIndentInsideStructures(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		wantIndent int
	}{
		{"top level", "![a](pic.png)\n", 0},
		{"in a blockquote", "> ![a](pic.png)\n", 2},
		{"in a list item", "- ![a](pic.png)\n", 2},
		{"in a nested quote", "> > ![a](pic.png)\n", 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeImages{cols: 10, rows: 3}
			doc := renderWithImages(t, tt.source, h)
			if len(doc.Images) != 1 {
				t.Fatalf("got %d placements, want 1", len(doc.Images))
			}
			if got := doc.Images[0].Indent; got != tt.wantIndent {
				t.Errorf("indent = %d, want %d", got, tt.wantIndent)
			}
		})
	}
}

func TestImageRespectsWidth(t *testing.T) {
	h := &fakeImages{cols: 500, rows: 3}
	doc := renderWithImages(t, "![a](pic.png)\n", h)
	if doc.Images[0].Cols > doc.Width {
		t.Errorf("image is %d cols wide in a %d col document", doc.Images[0].Cols, doc.Width)
	}
}

func TestImageRespectsMaxRows(t *testing.T) {
	h := &fakeImages{cols: 10, rows: 500}
	doc, err := Render([]byte("![a](pic.png)\n"), Options{
		Width: 60, Theme: theme.Plain(), Images: h, MaxImageRows: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Images[0].Rows; got != 7 {
		t.Errorf("rows = %d, want 7", got)
	}
}

// --- writing ---

func TestWriteDrawsImage(t *testing.T) {
	h := &fakeImages{cols: 8, rows: 3}
	doc := renderWithImages(t, "Before.\n\n![alt](pic.png)\n\nAfter.\n", h)

	got := renderToString(t, doc, WriteOptions{Color: theme.ColorNone, Images: h})

	if !strings.Contains(got, "<IMG pic.png 8x3 indent=0>") {
		t.Errorf("image sequence missing:\n%q", got)
	}
	// The picture covers the alt text, so it must not also be printed.
	if strings.Contains(got, "alt") {
		t.Errorf("alt text was written under the image:\n%q", got)
	}
	if !strings.Contains(got, "Before.") || !strings.Contains(got, "After.") {
		t.Errorf("surrounding text missing:\n%q", got)
	}
}

// TestWriteReservesTheRows checks that the image's rows are written as real
// lines before the picture is drawn over them. Drawing into lines that do not
// exist yet lets the terminal scroll the image away.
func TestWriteReservesTheRows(t *testing.T) {
	h := &fakeImages{cols: 8, rows: 4}
	doc := renderWithImages(t, "![alt](pic.png)\n", h)
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorNone, Images: h})

	before, _, ok := strings.Cut(got, "<IMG")
	if !ok {
		t.Fatalf("no image sequence in %q", got)
	}
	if n := strings.Count(before, "\n"); n != 4 {
		t.Errorf("%d newlines written before the image, want 4 (one per reserved row): %q", n, before)
	}
}

// TestWriteKeepsPrefixUnderImage is why the rows are written rather than
// skipped: the blockquote bar beside the picture comes from those lines.
func TestWriteKeepsPrefixUnderImage(t *testing.T) {
	h := &fakeImages{cols: 8, rows: 3}
	doc := renderWithImages(t, "> ![alt](pic.png)\n", h)
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorNone, Images: h})

	bar := theme.Plain().Glyphs.QuoteBar
	if n := strings.Count(got, bar); n < 3 {
		t.Errorf("quote bar appears %d times, want one per image row: %q", n, got)
	}
	if !strings.Contains(got, "indent=2") {
		t.Errorf("image was not indented past the bar: %q", got)
	}
}

func TestWriteNoHandlerShowsAltText(t *testing.T) {
	h := &fakeImages{cols: 8, rows: 3}
	doc := renderWithImages(t, "![alt text](pic.png)\n", h)

	got := renderToString(t, doc, WriteOptions{Color: theme.ColorNone})
	if !strings.Contains(got, "alt text") {
		t.Errorf("alt text missing when no handler is set:\n%q", got)
	}
	if strings.Contains(got, "<IMG") {
		t.Errorf("an image was drawn without a handler:\n%q", got)
	}
}

// TestWriteEncodeFailureShowsAltText covers a file disappearing between the
// measure and the write.
func TestWriteEncodeFailureShowsAltText(t *testing.T) {
	measure := &fakeImages{cols: 8, rows: 3}
	doc := renderWithImages(t, "![alt text](pic.png)\n", measure)

	failing := &fakeImages{cols: 8, rows: 3, encodeErr: errors.New("gone")}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorNone, Images: failing})

	if !strings.Contains(got, "alt text") {
		t.Errorf("alt text missing after an encode failure:\n%q", got)
	}
}

// TestWriteLineCountIsStable is the invariant the pager will depend on: an
// image occupies the same number of rows whether or not it can be drawn.
func TestWriteLineCountIsStable(t *testing.T) {
	h := &fakeImages{cols: 8, rows: 5}
	doc := renderWithImages(t, "Before.\n\n![alt](pic.png)\n\nAfter.\n", h)

	drawn := renderToString(t, doc, WriteOptions{Color: theme.ColorNone, Images: h})
	plain := renderToString(t, doc, WriteOptions{Color: theme.ColorNone})

	if a, b := strings.Count(drawn, "\n"), strings.Count(plain, "\n"); a != b {
		t.Errorf("drawn output has %d lines, plain has %d; they must agree", a, b)
	}
}
