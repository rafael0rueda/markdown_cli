package pager

import (
	"strings"

	"github.com/rafael0rueda/markdown_cli/internal/render"
	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// view is what the pager is showing: the document, or one of the screens
// drawn in its place.
type view int

const (
	viewDocument view = iota
	viewHelp
	viewContents
)

// scrollAmount reports how far a key scrolls a view of the given height, for
// the keys that scroll.
func scrollAmount(k key, height int) (int, bool) {
	half := max(height/2, 1)
	switch {
	case k.Name == keyDown, k.Rune == 'j':
		return 1, true
	case k.Name == keyUp, k.Rune == 'k':
		return -1, true
	case k.Name == keyPageDown, k.Rune == ' ', k.Ctrl && k.Rune == 'f':
		return height, true
	case k.Name == keyPageUp, k.Rune == 'b', k.Ctrl && k.Rune == 'b':
		return -height, true
	case k.Rune == 'd' && !k.Ctrl:
		return half, true
	case k.Rune == 'u' && !k.Ctrl:
		return -half, true
	}
	return 0, false
}

// jumpHeading moves the view to the next or previous heading.
//
// A heading near the end may already be on screen with nowhere further to
// scroll, so a jump that would not move the view goes on to the heading after
// it, and reports that there are no more once none would.
func (p *pager) jumpHeading(direction int) {
	headings := p.doc.Headings
	if len(headings) == 0 {
		p.message = "no headings"
		return
	}
	start := p.top
	if direction > 0 {
		for _, h := range headings {
			if h.Line > start {
				p.top = h.Line
				p.clampScroll()
				if p.top != start {
					return
				}
			}
		}
		p.message = "no more headings"
		return
	}
	for i := len(headings) - 1; i >= 0; i-- {
		if headings[i].Line < start {
			p.top = headings[i].Line
			p.clampScroll()
			return
		}
	}
	p.message = "no earlier headings"
}

// contents is the state of the table of contents.
type contents struct {
	// selected is the heading the cursor is on, and top the first one shown.
	selected, top int
}

// openContents shows the table of contents, with the cursor on the section
// being read.
func (p *pager) openContents() {
	headings := p.doc.Headings
	if len(headings) == 0 {
		p.message = "no headings"
		return
	}
	p.contents = contents{}
	for i, h := range headings {
		if h.Line > p.top {
			break
		}
		p.contents.selected = i
	}
	p.view = viewContents
	p.keepSelectionVisible()
}

func (p *pager) handleContentsKey(k key) {
	headings := p.doc.Headings
	c := &p.contents
	switch {
	case k.Name == keyEnter:
		p.top = headings[c.selected].Line
		p.clampScroll()
		p.view = viewDocument
		return
	case k.Name == keyEscape, k.Rune == 'q', k.Rune == 't':
		p.view = viewDocument
		return
	case k.Name == keyHome, k.Rune == 'g':
		c.selected = 0
	case k.Name == keyEnd, k.Rune == 'G':
		c.selected = len(headings) - 1
	default:
		if delta, ok := scrollAmount(k, p.viewHeight()); ok {
			c.selected += delta
		}
	}
	c.selected = min(max(c.selected, 0), len(headings)-1)
	p.keepSelectionVisible()
}

// keepSelectionVisible scrolls the table of contents to show the cursor.
func (p *pager) keepSelectionVisible() {
	c := &p.contents
	view := p.viewHeight()
	if c.selected < c.top {
		c.top = c.selected
	}
	if c.selected >= c.top+view {
		c.top = c.selected - view + 1
	}
	c.top = min(max(c.top, 0), max(len(p.doc.Headings)-view, 0))
}

// contentsLines lays out the table of contents: a line per heading, indented
// by level and colored like the heading, with the cursor's line highlighted.
func (p *pager) contentsLines() []render.Line {
	th := p.opts.Theme
	headings := p.doc.Headings
	shallowest := headings[0].Level
	for _, h := range headings {
		shallowest = min(shallowest, h.Level)
	}

	lines := make([]render.Line, len(headings))
	for i, h := range headings {
		indent := strings.Repeat("  ", h.Level-shallowest)
		text := truncateToWidth(" "+indent+h.Text, p.width-1)
		style := headingStyle(th, h.Level)
		if i == p.contents.selected {
			style = th.Status.Merge(theme.Style{Bold: true})
			lines[i] = render.Line{Runs: []render.Run{{Text: text, Style: style}}, Fill: th.Status.BG}
			continue
		}
		lines[i] = render.Line{Runs: []render.Run{{Text: text, Style: style}}}
	}
	return lines
}

func headingStyle(th *theme.Theme, level int) theme.Style {
	return th.Headings[min(max(level, 1), 6)-1]
}
