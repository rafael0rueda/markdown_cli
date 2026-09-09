package render

import (
	"strings"

	"github.com/yuin/goldmark/ast"
)

// ImageHandler measures and draws images on behalf of the renderer.
//
// It is an interface so that this package knows nothing about decoding images
// or about any particular graphics protocol; the terminal-specific work lives
// behind it. A nil handler means images are shown as alt text.
type ImageHandler interface {
	// Measure reports the cell footprint an image needs within the given box.
	// An error means the image cannot be drawn - missing, unreadable, or in an
	// unsupported format - and is an ordinary outcome, not a failure: the
	// caller falls back to alt text.
	Measure(ref string, maxCols, maxRows int) (cols, rows int, err error)

	// Encode returns the escape sequence drawing the image in a box of exactly
	// cols by rows cells, indented by the given columns, and moving the cursor
	// past it.
	Encode(ref string, cols, rows, indent int) (string, error)
}

// defaultMaxImageRows caps how tall an image may be when the caller does not
// say. A picture that fills several screens buries the text it belongs to, and
// without knowing the terminal height there is no better basis for a limit.
const defaultMaxImageRows = 20

// blockImage renders a paragraph that contains nothing but an image.
//
// Only a lone image becomes a picture. An image sitting in the middle of a
// sentence stays alt text, because the graphics protocols draw into a
// rectangle of whole cells: placing one mid-line would either overwrite the
// words beside it or force the line to be as tall as the picture. Markdown
// that means to show a picture puts it on its own line, so this matches how
// documents are actually written.
//
// It reports whether the image was placed.
func (r *renderer) blockImage(n ast.Node) bool {
	if r.opts.Images == nil {
		return false
	}
	img := soleImage(n, r.src)
	if img == nil {
		return false
	}

	ref := r.resolve(string(img.Destination))
	if ref == "" {
		return false
	}

	indent := runsWidth(r.rest)
	maxCols := r.contentWidth()
	maxRows := r.opts.MaxImageRows
	if maxRows <= 0 {
		maxRows = defaultMaxImageRows
	}

	cols, rows, err := r.opts.Images.Measure(ref, maxCols, maxRows)
	if err != nil || cols < 1 || rows < 1 {
		return false
	}

	// Reserve the rows as blank lines. They are real lines in the document so
	// that line numbers stay continuous, which is what lets the pager scroll
	// through an image without knowing anything about it.
	alt := r.imageAlt(img)
	first, _ := r.prefixes()
	prefix := append([]Run(nil), first...)

	start := len(r.doc.Lines)
	for i := 0; i < rows; i++ {
		if i == 0 {
			// The alt text goes on the first reserved line. Anything that can
			// draw the image writes over these rows; anything that cannot -
			// a pipe, or a terminal without graphics - shows the alt text and
			// the reserved space simply stays empty below it.
			runs := append(append([]Run(nil), prefix...), alt...)
			r.doc.Lines = append(r.doc.Lines, Line{Runs: runs})
			continue
		}
		r.blank()
	}

	r.doc.Images = append(r.doc.Images, Image{
		Ref:    ref,
		Alt:    alt,
		Prefix: prefix,
		Line:   start,
		Cols:   cols,
		Rows:   rows,
		Indent: indent,
	})
	return true
}

// imageAlt builds the text shown if the image cannot be drawn after all.
func (r *renderer) imageAlt(n *ast.Image) []Run {
	alt := strings.TrimSpace(nodeText(n, r.src))
	if alt == "" {
		alt = "image"
	}
	icon := r.th.Glyphs.ImageIcon
	if icon != "" {
		icon += " "
	}
	return []Run{{Text: icon + alt, Style: r.base.Merge(r.th.ImageAlt)}}
}

// soleImage returns the image if the block holds one and nothing else.
//
// The block may be a paragraph or, inside a tight list item, a text block;
// goldmark uses the latter where a paragraph would produce unwanted spacing,
// and an image in a list is common enough to be worth handling.
//
// Whitespace around it is ignored: a line break before or after the image is
// invisible in the rendered source, and markdown formatters routinely insert
// them. Anything else alongside the image means the block is a sentence that
// happens to contain a picture, which is drawn as alt text instead.
func soleImage(n ast.Node, src []byte) *ast.Image {
	var found *ast.Image
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Image:
			if found != nil {
				return nil // more than one image: treat as prose
			}
			found = c
		case *ast.Text:
			if strings.TrimSpace(string(c.Text(src))) != "" {
				return nil
			}
		default:
			return nil
		}
	}
	return found
}
