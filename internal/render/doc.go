// Package render turns parsed markdown into a line-addressed document of
// styled text.
//
// The renderer deliberately stops short of producing a finished string. It
// emits a Doc: a flat slice of Lines, each a sequence of styled Runs. Two very
// different consumers need that shape. Writing to a pipe walks the lines once
// and serializes them. The interactive pager needs to draw an arbitrary window
// of lines, repeatedly, without re-parsing - and later, to know which screen
// row a given image lands on. A pre-rendered string would serve neither well.
package render

import (
	"strings"

	"github.com/rivo/uniseg"

	"mdv/internal/theme"
)

// Run is a maximal stretch of text sharing one style.
type Run struct {
	Text  string
	Style theme.Style
	// Link, when non-empty, is the URL this run points at. Serialization turns
	// it into an OSC 8 hyperlink on terminals that support them.
	Link string
}

// Width is the number of terminal cells the run occupies.
func (r Run) Width() int { return uniseg.StringWidth(r.Text) }

// Line is one rendered row of the document.
type Line struct {
	Runs []Run
	// Fill, when set, extends this background color from the end of the last
	// run to the document width. Code blocks use it so their background forms
	// a solid rectangle rather than ending ragged at each line's last glyph.
	Fill theme.Color
}

// Width is the number of cells the line's runs occupy, ignoring any fill.
func (l Line) Width() int {
	n := 0
	for _, r := range l.Runs {
		n += r.Width()
	}
	return n
}

// Text returns the line's content with all styling dropped.
func (l Line) Text() string {
	var b strings.Builder
	for _, r := range l.Runs {
		b.WriteString(r.Text)
	}
	return b.String()
}

// Doc is a fully rendered markdown document.
type Doc struct {
	Lines []Line
	// Width is the column count the document was laid out for. Fills and
	// centered elements are measured against it.
	Width int
}

// Text returns the whole document as unstyled text, one line per row. The
// pager searches over this, and golden tests assert on it.
func (d *Doc) Text() string {
	var b strings.Builder
	for i, l := range d.Lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l.Text())
	}
	return b.String()
}

// appendLine adds a line built from the given runs.
func (d *Doc) appendLine(runs ...Run) {
	d.Lines = append(d.Lines, Line{Runs: runs})
}

// trimTrailingBlanks removes blank lines from the end of the document, so
// output does not end in a run of empty rows.
func (d *Doc) trimTrailingBlanks() {
	for len(d.Lines) > 0 {
		last := d.Lines[len(d.Lines)-1]
		if last.Fill.IsSet() || strings.TrimSpace(last.Text()) != "" {
			return
		}
		d.Lines = d.Lines[:len(d.Lines)-1]
	}
}
