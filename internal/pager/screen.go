// Package pager displays a rendered document interactively: scrolling, search,
// and images that move with the text.
package pager

import (
	"bufio"
	"errors"
	"fmt"
	"os"

	xterm "golang.org/x/term"
)

// Escape sequences used to set up and tear down the screen.
const (
	enterAltScreen = "\x1b[?1049h"
	leaveAltScreen = "\x1b[?1049l"
	hideCursor     = "\x1b[?25l"
	showCursor     = "\x1b[?25h"
	clearScreen    = "\x1b[2J"
	clearLine      = "\x1b[K"

	// The kitty keyboard protocol, pushed and popped so whatever the shell
	// had configured is restored on exit. Flag 1 asks for disambiguated
	// escape codes, which is the part that matters here: without it, Escape
	// is indistinguishable from the start of an arrow key until a timeout
	// expires, and cancelling a search feels sluggish.
	pushKittyKeyboard = "\x1b[>1u"
	popKittyKeyboard  = "\x1b[<u"
)

// ErrNoTerminal reports that there is no terminal to page on.
//
// It is a distinct error because it is not really a failure: a caller that
// asked to page only if possible should fall back to writing the document out
// rather than giving up.
var ErrNoTerminal = errors.New("no controlling terminal to page on")

// screen owns the terminal while the pager is running.
type screen struct {
	tty   *os.File
	out   *bufio.Writer
	state *xterm.State

	// kittyKeyboard records whether the protocol was pushed, so it is only
	// popped if it was.
	kittyKeyboard bool
	// ownTTY is true when this screen opened the terminal and should close it.
	ownTTY bool
}

// openScreen takes over the terminal.
//
// Like the capability probe, this talks to /dev/tty rather than to stdin and
// stdout, so paging works when markdown is piped in or output is redirected.
func openScreen(kittyKeyboard bool) (*screen, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, ErrNoTerminal
	}
	s, err := newScreen(tty, kittyKeyboard)
	if err != nil {
		tty.Close()
		return nil, err
	}
	return s, nil
}

// newScreen sets up an already-open terminal. It is separated from finding
// /dev/tty so tests can drive the pager through a pseudo-terminal.
func newScreen(tty *os.File, kittyKeyboard bool) (*screen, error) {
	state, err := xterm.MakeRaw(int(tty.Fd()))
	if err != nil {
		return nil, fmt.Errorf("putting the terminal in raw mode: %w", err)
	}

	s := &screen{tty: tty, out: bufio.NewWriterSize(tty, 64<<10), state: state, ownTTY: true}
	s.out.WriteString(enterAltScreen)
	s.out.WriteString(hideCursor)
	if kittyKeyboard {
		s.out.WriteString(pushKittyKeyboard)
		s.kittyKeyboard = true
	}
	if err := s.out.Flush(); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// closesTTY controls whether close() also closes the file. A screen the pager
// opened owns it; one handed in by a test does not.
func (s *screen) keepTTYOpen() { s.ownTTY = false }

// close restores the terminal to how it was found.
//
// Every step is attempted even if an earlier one fails: leaving a terminal in
// raw mode with the cursor hidden makes the shell unusable, so a partial
// restore is much better than stopping at the first error.
func (s *screen) close() {
	if s == nil {
		return
	}
	if s.kittyKeyboard {
		s.out.WriteString(popKittyKeyboard)
	}
	s.out.WriteString(showCursor)
	s.out.WriteString(leaveAltScreen)
	s.out.Flush()

	if s.state != nil {
		xterm.Restore(int(s.tty.Fd()), s.state)
	}
	if s.ownTTY {
		s.tty.Close()
	}
}

// size reports the terminal's current dimensions.
func (s *screen) size() (width, height int) {
	w, h, err := xterm.GetSize(int(s.tty.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		return 80, 24
	}
	return w, h
}

// moveTo positions the cursor, converting from zero-based row and column to
// the one-based coordinates the escape sequence uses.
func (s *screen) moveTo(row, col int) {
	fmt.Fprintf(s.out, "\x1b[%d;%dH", row+1, col+1)
}
