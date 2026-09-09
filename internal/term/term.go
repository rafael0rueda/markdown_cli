// Package term reports what the attached terminal can do.
//
// Phase one answers only what can be learned from file descriptors and the
// environment: is this a terminal, how wide is it, and how much color does it
// take. Probing the terminal with escape queries - for graphics protocols,
// the keyboard protocol and the true background color - lands here too, but
// needs raw mode and a response timeout, so it is kept separate.
package term

import (
	"os"
	"strings"

	xterm "golang.org/x/term"

	"mdv/internal/theme"
)

// IsTerminal reports whether f is attached to a terminal.
func IsTerminal(f *os.File) bool {
	return f != nil && xterm.IsTerminal(int(f.Fd()))
}

// Size returns the terminal's width and height in cells. It falls back to
// 80x24 when the size cannot be determined, which is the conventional default
// for a terminal of unknown size.
func Size(f *os.File) (width, height int) {
	if f != nil {
		if w, h, err := xterm.GetSize(int(f.Fd())); err == nil && w > 0 && h > 0 {
			return w, h
		}
	}
	return 80, 24
}

// DetectColor decides how much color to emit to f.
//
// The rules follow what terminal programs have converged on: NO_COLOR wins
// over everything, a non-terminal target gets no escapes at all, and the depth
// otherwise comes from COLORTERM and TERM.
func DetectColor(f *os.File) theme.ColorMode {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return theme.ColorNone
	}
	if v := os.Getenv("TERM"); v == "dumb" || v == "" {
		return theme.ColorNone
	}
	if !IsTerminal(f) {
		return theme.ColorNone
	}
	return depthFromEnv()
}

// depthFromEnv reports the color depth the environment advertises, ignoring
// whether the target is actually a terminal. Callers that force color on -
// piping into a pager that understands escapes, say - want this.
func depthFromEnv() theme.ColorMode {
	switch strings.ToLower(os.Getenv("COLORTERM")) {
	case "truecolor", "24bit":
		return theme.ColorTrue
	}
	term := os.Getenv("TERM")
	switch {
	case strings.Contains(term, "truecolor"), strings.Contains(term, "direct"):
		return theme.ColorTrue
	// kitty and a handful of others report truecolor support without setting
	// COLORTERM in every launch path, notably over ssh.
	case strings.HasPrefix(term, "xterm-kitty"), strings.HasPrefix(term, "xterm-ghostty"):
		return theme.ColorTrue
	case strings.Contains(term, "256color"):
		return theme.Color256
	case term == "":
		return theme.ColorNone
	}
	return theme.Color16
}

// ForcedColor returns the color depth to use when the user has asked for color
// unconditionally, defaulting to truecolor if the environment says nothing.
func ForcedColor() theme.ColorMode {
	if m := depthFromEnv(); m != theme.ColorNone {
		return m
	}
	return theme.ColorTrue
}
