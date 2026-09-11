package pager

import (
	"github.com/rafael0rueda/markdown_cli/internal/render"
)

// helpText is the key reference, written in markdown and rendered like any
// document so it takes on the theme.
const helpText = `| Key | Action |
|---|---|
| <kbd>j</kbd> <kbd>k</kbd>, <kbd>↓</kbd> <kbd>↑</kbd>, mouse wheel | Scroll a line |
| <kbd>d</kbd> <kbd>u</kbd> | Scroll half a screen |
| <kbd>space</kbd> <kbd>b</kbd>, <kbd>PgDn</kbd> <kbd>PgUp</kbd> | Scroll a screen |
| <kbd>g</kbd> <kbd>G</kbd>, <kbd>Home</kbd> <kbd>End</kbd> | Go to the start or the end |
| <kbd>]</kbd> <kbd>[</kbd> | Next or previous heading |
| <kbd>t</kbd> | Table of contents |
| <kbd>/</kbd> | Search; matches highlight as you type |
| <kbd>n</kbd> <kbd>N</kbd> | Next or previous match |
| <kbd>Esc</kbd> | Cancel the search |
| <kbd>?</kbd> | This help |
| <kbd>q</kbd> | Quit |
`

// helpLines lays the key reference out for the current window.
func (p *pager) helpLines() []render.Line {
	width := p.width
	if p.opts.MaxWidth > 0 {
		width = min(width, p.opts.MaxWidth)
	}
	doc, err := render.Render([]byte(helpText), render.Options{Width: width, Theme: p.opts.Theme})
	if err != nil {
		return []render.Line{{Runs: []render.Run{render.NewRun(err.Error())}}}
	}
	return doc.Lines
}

// handleHelpKey scrolls the help if it is longer than the window, and closes
// it on any other key.
func (p *pager) handleHelpKey(k key) {
	maxTop := max(len(p.helpLines())-p.viewHeight(), 0)
	if maxTop > 0 {
		switch {
		case k.Name == keyHome, k.Rune == 'g':
			p.helpTop = 0
			return
		case k.Name == keyEnd, k.Rune == 'G':
			p.helpTop = maxTop
			return
		}
		if delta, ok := scrollAmount(k, p.viewHeight()); ok {
			p.helpTop = min(max(p.helpTop+delta, 0), maxTop)
			return
		}
	}
	p.view = viewDocument
}
