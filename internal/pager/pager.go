package pager

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rafael0rueda/markdown_cli/internal/render"
	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// Renderer produces the document laid out for a given width. The pager calls
// it again whenever the window is resized, because wrapping, table widths and
// image footprints all depend on the width.
type Renderer func(width int) (*render.Doc, error)

// ImageDrawer draws part of an image at the cursor.
//
// It is deliberately narrower than the interface the streaming writer uses:
// the pager positions the cursor itself, and needs to draw an image that is
// only partly on screen.
type ImageDrawer interface {
	// ClearPlacements returns the sequence removing the images currently
	// displayed, without discarding the data behind them.
	ClearPlacements() string
	// DrawCropped returns the sequence drawing rows [skip, skip+visible) of an
	// image at the cursor, in a box cols wide whose rows start indent columns
	// from the left margin.
	DrawCropped(ref string, cols, rows, skip, visible, indent int) (string, error)
}

// Options configures a pager session.
type Options struct {
	// Render lays the document out. Required.
	Render Renderer
	// Write controls how lines are serialized.
	Write render.WriteOptions
	// Images draws pictures. Nil leaves their rows blank.
	Images ImageDrawer
	// Theme supplies the styles for the status line and search highlighting.
	Theme *theme.Theme
	// Title is shown in the status line, usually the file name.
	Title string
	// MaxWidth caps the layout width however wide the window is.
	MaxWidth int
	// KittyKeyboard enables the keyboard protocol, which makes Escape
	// unambiguous.
	KittyKeyboard bool
}

// pager holds the state of a session.
type pager struct {
	opts   Options
	screen *screen
	doc    *render.Doc

	width, height int
	// top is the index of the first document line on screen.
	top int

	search  searchState
	message string
	quit    bool

	// view is what is on screen: the document, the help, or the table of
	// contents.
	view     view
	helpTop  int
	contents contents
}

// statusRows is the number of rows reserved at the bottom of the screen.
const statusRows = 1

// Run displays the document until the user quits.
func Run(opts Options) error {
	if opts.Render == nil {
		return errors.New("pager: no renderer given")
	}
	if opts.Theme == nil {
		opts.Theme = theme.Dark()
	}

	scr, err := openScreen(opts.KittyKeyboard)
	if err != nil {
		return err
	}
	return run(scr, opts)
}

// run drives a session on an already-open screen.
func run(scr *screen, opts Options) error {
	defer scr.close()

	p := &pager{opts: opts, screen: scr}
	if err := p.layout(); err != nil {
		return err
	}
	return p.loop()
}

// viewHeight is the number of document lines visible at once.
func (p *pager) viewHeight() int {
	return max(p.height-statusRows, 1)
}

// maxTop is the furthest the view can be scrolled down.
//
// The last line is allowed to sit at the top of an otherwise empty screen,
// which is what every pager does: stopping earlier would make the end of a
// document unreachable when it is shorter than the window.
func (p *pager) maxTop() int {
	return max(len(p.doc.Lines)-p.viewHeight(), 0)
}

// layout re-reads the terminal size and renders the document to fit it.
//
// The scroll position is preserved as a fraction rather than as a line number.
// Re-wrapping at a new width changes how many lines the document has, so line
// 200 of the old layout is not line 200 of the new one, and keeping the number
// would jump the reader somewhere unrelated.
func (p *pager) layout() error {
	fraction := 0.0
	if p.doc != nil && len(p.doc.Lines) > 0 {
		fraction = float64(p.top) / float64(len(p.doc.Lines))
	}

	p.width, p.height = p.screen.size()
	width := p.width
	if p.opts.MaxWidth > 0 && width > p.opts.MaxWidth {
		width = p.opts.MaxWidth
	}

	doc, err := p.opts.Render(width)
	if err != nil {
		return err
	}
	p.doc = doc

	p.top = int(fraction * float64(len(doc.Lines)))
	p.clampScroll()
	if p.view == viewContents {
		p.keepSelectionVisible()
	}
	return nil
}

// clampScroll keeps the view within the document.
func (p *pager) clampScroll() {
	p.top = min(max(p.top, 0), p.maxTop())
}

// scroll moves the view by a number of lines.
func (p *pager) scroll(delta int) {
	p.top += delta
	p.clampScroll()
}

// loop draws and handles input until the user quits.
func (p *pager) loop() error {
	resized, stopResize := watchResize()
	defer stopResize()

	keys, stopKeys := readKeys(p.screen.tty)
	defer stopKeys()

	if err := p.draw(); err != nil {
		return err
	}

	for !p.quit {
		select {
		case <-resized:
			if err := p.layout(); err != nil {
				return err
			}
		case k, open := <-keys:
			if !open {
				return nil
			}
			p.handleKey(k)
		}
		if p.quit {
			break
		}
		if err := p.draw(); err != nil {
			return err
		}
	}
	return nil
}

// handleKey applies a keypress.
func (p *pager) handleKey(k key) {
	// While a search is being typed, every key belongs to the prompt.
	if p.search.typing {
		p.handleSearchKey(k)
		return
	}
	if k.Ctrl && (k.Rune == 'c' || k.Rune == 'd') {
		p.quit = true
		return
	}

	p.message = ""
	switch p.view {
	case viewHelp:
		p.handleHelpKey(k)
		return
	case viewContents:
		p.handleContentsKey(k)
		return
	}

	if delta, ok := scrollAmount(k, p.viewHeight()); ok {
		p.scroll(delta)
		return
	}
	switch {
	case k.Name == keyRune && k.Rune == 'q':
		p.quit = true

	case k.Name == keyHome, k.Rune == 'g':
		p.top = 0
	case k.Name == keyEnd, k.Rune == 'G':
		p.top = p.maxTop()

	case k.Rune == ']':
		p.jumpHeading(1)
	case k.Rune == '[':
		p.jumpHeading(-1)
	case k.Rune == 't':
		p.openContents()
	case k.Rune == '?':
		p.view, p.helpTop = viewHelp, 0

	case k.Rune == '/':
		p.search.begin()
	case k.Rune == 'n':
		p.nextMatch(1)
	case k.Rune == 'N':
		p.nextMatch(-1)
	}
}

// handleSearchKey edits the search prompt.
func (p *pager) handleSearchKey(k key) {
	switch {
	case k.Name == keyEscape, k.Ctrl && k.Rune == 'c':
		p.search.cancel()
	case k.Name == keyEnter:
		p.search.commit()
		p.runSearch()
	case k.Name == keyBackspace:
		p.search.backspace()
	case k.Name == keyRune && k.Rune >= ' ':
		p.search.append(k.Rune)
	}
}

// runSearch finds the matches for the committed query and jumps to the first
// one at or after the current position.
func (p *pager) runSearch() {
	p.search.matches = p.doc.Search(p.search.query)
	if len(p.search.matches) == 0 {
		p.message = fmt.Sprintf("no match for %q", p.search.query)
		return
	}
	p.search.index = -1
	p.nextMatch(1)
}

// nextMatch moves to the next or previous match.
func (p *pager) nextMatch(direction int) {
	if p.search.query == "" {
		return
	}
	// Re-run against the current layout: a resize since the last search would
	// have moved every line.
	if len(p.search.matches) == 0 {
		p.search.matches = p.doc.Search(p.search.query)
	}
	if len(p.search.matches) == 0 {
		p.message = fmt.Sprintf("no match for %q", p.search.query)
		return
	}

	target, ok := p.search.step(direction, p.top)
	if !ok {
		return
	}
	// Put the match a little below the top of the screen so the reader can see
	// what leads up to it.
	p.top = target - min(p.viewHeight()/4, 3)
	p.clampScroll()

	p.message = fmt.Sprintf("match %d of %d", p.search.index+1, len(p.search.matches))
}

// statusText builds the line shown at the bottom of the screen.
func (p *pager) statusText() string {
	if p.search.typing {
		// The draft, not the committed query: what is shown has to be what is
		// being typed, or the prompt never appears to accept input.
		return "/" + p.search.draft
	}
	if p.message != "" {
		return p.message
	}
	switch p.view {
	case viewHelp:
		if len(p.helpLines()) > p.viewHeight() {
			return "Keys  ·  j/k scroll  any other key closes"
		}
		return "Keys  ·  any key closes"
	case viewContents:
		return "Contents  ·  j/k move  Enter go  Esc close"
	}

	position := "all"
	if p.maxTop() > 0 {
		switch {
		case p.top == 0:
			position = "top"
		case p.top >= p.maxTop():
			position = "end"
		default:
			position = fmt.Sprintf("%d%%", 100*p.top/p.maxTop())
		}
	}

	title := p.opts.Title
	if title == "" {
		title = "mdv"
	}
	return fmt.Sprintf("%s  %s  ·  j/k scroll  / search  ? help  q quit", title, position)
}

// truncateToWidth cuts a string to fit the given number of columns.
func truncateToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if render.NewRun(s).Width() <= width {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := render.NewRun(string(r)).Width()
		if used+w > width {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}
