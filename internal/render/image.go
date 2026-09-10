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
	Measure(ref string, maxCols, maxRows int, hint SizeHint) (cols, rows int, err error)

	// Encode returns the escape sequence drawing the image in a box of exactly
	// cols by rows cells, indented by the given columns, and moving the cursor
	// past it.
	Encode(ref string, cols, rows, indent int) (string, error)
}

// SizeHint is a size the document asked for explicitly, in pixels. Obsidian
// writes it as ![[diagram.png|300]] or ![[diagram.png|300x200]]. Zero in a
// field means unspecified, and the image is sized to fit the available space.
type SizeHint struct {
	Width, Height int
}

// defaultMaxImageRows caps how tall an image may be when the caller does not
// say. A picture that fills several screens buries the text it belongs to, and
// without knowing the terminal height there is no better basis for a limit.
const defaultMaxImageRows = 20

// blockChunk is one piece of a split block: either a run of inline content, or
// a single image standing alone on its own source line.
type blockChunk struct {
	nodes []ast.Node
	image ast.Node // *ast.Image, or a *Wikilink embed
}

// splitInlines divides a paragraph into chunks at any image that sits alone on
// its own source line.
//
// A markdown paragraph runs until a blank line, so this is one paragraph:
//
//	With a 's' in the group execute position.
//	![[diagram.png]]
//
// Rendering it as a single block would leave the picture stranded mid-sentence.
// Splitting it lets the text wrap as prose and the image become a block, which
// is what the document means and what Obsidian shows.
func splitInlines(n ast.Node) []blockChunk {
	var chunks []blockChunk
	var current []ast.Node

	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, blockChunk{nodes: current})
			current = nil
		}
	}

	afterImage := false
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch {
		case standaloneImage(c):
			flush()
			chunks = append(chunks, blockChunk{image: c})
			afterImage = true

		case afterImage && isBreakMarker(c):
			// goldmark represents the newline that ended the image's line as
			// an empty text node carrying the break flag. Keeping it would put
			// a stray blank line under every picture.
			afterImage = false

		default:
			afterImage = false
			current = append(current, c)
		}
	}
	flush()
	return chunks
}

// standaloneImage reports whether n is an image occupying a source line by
// itself, with only line breaks on either side.
//
// The test is made against the siblings rather than the source text because
// goldmark records exactly what is needed: the text node before a line break
// carries the break flag, and a break following an image appears as an empty
// text node carrying it. Anything else next to the image means it shares its
// line with words.
func standaloneImage(n ast.Node) bool {
	if !isImageNode(n) {
		return false
	}
	if prev := n.PreviousSibling(); prev != nil && !endsLine(prev) {
		return false
	}
	if next := n.NextSibling(); next != nil && !isBreakMarker(next) {
		return false
	}
	return true
}

// isImageNode reports whether n draws a picture.
func isImageNode(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.Image:
		return true
	case *Wikilink:
		return n.Embed
	}
	return false
}

// endsLine reports whether n is a text node that ends its source line.
func endsLine(n ast.Node) bool {
	t, ok := n.(*ast.Text)
	return ok && (t.SoftLineBreak() || t.HardLineBreak())
}

// isBreakMarker reports whether n is an empty text node that exists only to
// carry a line break.
func isBreakMarker(n ast.Node) bool {
	t, ok := n.(*ast.Text)
	return ok && t.Segment.Len() == 0 && (t.SoftLineBreak() || t.HardLineBreak())
}

// inlineBlock renders a paragraph or text block, placing any standalone images
// as pictures and the rest as prose.
func (r *renderer) inlineBlock(n ast.Node) {
	chunks := splitInlines(n)

	// The common case is a block with no images at all, which must render
	// exactly as it did before this splitting existed.
	if len(chunks) == 1 && chunks[0].image == nil {
		r.emit(r.inlineNodes(chunks[0].nodes, r.base, ""))
		return
	}

	for i, chunk := range chunks {
		if i > 0 {
			// A picture is a block, so it gets air around it rather than
			// butting straight up against the sentence that introduced it.
			r.blank()
		}
		if chunk.image != nil {
			if r.placeImage(chunk.image) {
				continue
			}
			// Not drawable: fall through to rendering it as alt text.
			r.emit(r.inlineNodes([]ast.Node{chunk.image}, r.base, ""))
			continue
		}
		r.emit(r.inlineNodes(chunk.nodes, r.base, ""))
	}
}

// placeImage measures an image and reserves the rows it needs, reporting
// whether it will be drawn.
func (r *renderer) placeImage(n ast.Node) bool {
	if r.opts.Images == nil {
		return false
	}

	ref, alt, hint := r.imageRef(n)
	if ref == "" {
		return false
	}

	indent := runsWidth(r.rest)
	maxCols := r.contentWidth()
	maxRows := r.opts.MaxImageRows
	if maxRows <= 0 {
		maxRows = defaultMaxImageRows
	}

	cols, rows, err := r.opts.Images.Measure(ref, maxCols, maxRows, hint)
	if err != nil || cols < 1 || rows < 1 {
		return false
	}

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

// imageRef resolves an image node to a path, its alt text, and any size the
// document asked for.
func (r *renderer) imageRef(n ast.Node) (ref string, alt []Run, hint SizeHint) {
	switch n := n.(type) {
	case *ast.Image:
		return r.resolve(unescape(n.Destination)), r.imageAlt(strings.TrimSpace(nodeText(n, r.src))), SizeHint{}

	case *Wikilink:
		path, ok := r.resolveEmbed(n.Target)
		if !ok {
			return "", nil, SizeHint{}
		}
		label := n.Display
		if label == "" {
			label = n.Target
		}
		return path, r.imageAlt(label), SizeHint{Width: n.Width, Height: n.Height}
	}
	return "", nil, SizeHint{}
}

// imageAlt builds the text shown if the image cannot be drawn after all.
func (r *renderer) imageAlt(alt string) []Run {
	if alt == "" {
		alt = "image"
	}
	icon := r.th.Glyphs.ImageIcon
	if icon != "" {
		icon += " "
	}
	return []Run{{Text: icon + alt, Style: r.base.Merge(r.th.ImageAlt)}}
}
