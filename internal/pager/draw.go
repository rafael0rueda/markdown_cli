package pager

import (
	"github.com/rafael0rueda/markdown_cli/internal/render"
)

// draw paints one frame.
//
// The order matters. Image placements are cleared first, so pictures from the
// previous frame do not linger over the new text. The text is written next,
// filling every row whether or not it has content, which is what erases the
// frame before it. Images go last, drawn over the blank rows that were
// reserved for them during layout.
func (p *pager) draw() error {
	out := p.screen.out

	if p.opts.Images != nil {
		out.WriteString(p.opts.Images.ClearPlacements())
	}

	p.drawText()
	p.drawStatus()

	if p.opts.Images != nil {
		p.drawImages()
	}

	// Park the cursor at the end of the status line. It is hidden, but a
	// terminal that ignores that should at least leave it somewhere sensible.
	p.screen.moveTo(p.height-1, min(render.NewRun(p.statusText()).Width(), p.width-1))
	return out.Flush()
}

// drawText writes the visible document lines.
func (p *pager) drawText() {
	view := p.viewHeight()
	query := ""
	if p.search.active() {
		query = p.search.highlightQuery()
	}

	for row := 0; row < view; row++ {
		p.screen.moveTo(row, 0)
		p.screen.out.WriteString(clearLine)

		index := p.top + row
		if index >= len(p.doc.Lines) {
			continue
		}
		line := p.doc.Lines[index]
		if query != "" {
			if ranges := render.LineMatches(line, query); len(ranges) > 0 {
				line = render.Highlight(line, ranges, p.opts.Theme.SearchMatch)
			}
		}
		p.screen.out.WriteString(render.RenderLine(line, p.doc.Width, p.opts.Write))
	}
}

// drawStatus writes the bar along the bottom of the screen.
func (p *pager) drawStatus() {
	p.screen.moveTo(p.height-1, 0)
	p.screen.out.WriteString(clearLine)

	text := truncateToWidth(p.statusText(), p.width)
	line := render.Line{
		Runs: []render.Run{{Text: text, Style: p.opts.Theme.Status}},
		Fill: p.opts.Theme.Status.BG,
	}
	p.screen.out.WriteString(render.RenderLine(line, p.width, p.opts.Write))
}

// drawImages draws the pictures that fall within the view.
//
// An image only partly on screen is cropped rather than skipped, so scrolling
// through one is continuous instead of having it appear and disappear whole.
func (p *pager) drawImages() {
	view := p.viewHeight()
	bottom := p.top + view

	for _, img := range p.doc.Images {
		start, end := img.Line, img.Line+img.Rows
		if end <= p.top || start >= bottom {
			continue // entirely off screen
		}

		// skip counts the rows scrolled off the top of the image; row is where
		// what remains begins on screen.
		skip := max(p.top-start, 0)
		row := max(start-p.top, 0)
		visible := min(img.Rows-skip, view-row)
		if visible < 1 {
			continue
		}

		seq, err := p.opts.Images.DrawCropped(img.Ref, img.Cols, img.Rows, skip, visible)
		if err != nil {
			// The alt text is already on the line beneath, so a picture that
			// cannot be drawn simply leaves it showing.
			continue
		}
		p.screen.moveTo(row, img.Indent)
		p.screen.out.WriteString(seq)
	}
}
