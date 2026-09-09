package theme

import "strings"

// Style is the full set of attributes a run of text can carry. The zero value
// is "plain": terminal default colors, no attributes.
type Style struct {
	FG, BG    Color
	Bold      bool
	Faint     bool
	Italic    bool
	Underline bool
	Strike    bool
	Reverse   bool
}

// IsPlain reports whether the style would emit no escape sequence at all.
func (s Style) IsPlain() bool {
	return !s.FG.IsSet() && !s.BG.IsSet() &&
		!s.Bold && !s.Faint && !s.Italic && !s.Underline && !s.Strike && !s.Reverse
}

// Merge layers other on top of s: colors and boolean attributes set in other
// win, everything else is inherited. It is how inline styles compose, e.g. a
// link inside a heading.
func (s Style) Merge(other Style) Style {
	out := s
	if other.FG.IsSet() {
		out.FG = other.FG
	}
	if other.BG.IsSet() {
		out.BG = other.BG
	}
	out.Bold = out.Bold || other.Bold
	out.Faint = out.Faint || other.Faint
	out.Italic = out.Italic || other.Italic
	out.Underline = out.Underline || other.Underline
	out.Strike = out.Strike || other.Strike
	out.Reverse = out.Reverse || other.Reverse
	return out
}

// WithFG returns a copy with the foreground replaced.
func (s Style) WithFG(c Color) Style { s.FG = c; return s }

// WithBG returns a copy with the background replaced.
func (s Style) WithBG(c Color) Style { s.BG = c; return s }

// SGR renders the style as a Select Graphic Rendition escape sequence for the
// given color mode, or "" if the style is a no-op in that mode.
func (s Style) SGR(mode ColorMode) string {
	// ColorNone means the target wants no escape sequences at all, not merely
	// no color, so attributes are dropped along with the palette.
	if mode == ColorNone {
		return ""
	}
	var parts []string
	if s.Bold {
		parts = append(parts, "1")
	}
	if s.Faint {
		parts = append(parts, "2")
	}
	if s.Italic {
		parts = append(parts, "3")
	}
	if s.Underline {
		parts = append(parts, "4")
	}
	if s.Reverse {
		parts = append(parts, "7")
	}
	if s.Strike {
		parts = append(parts, "9")
	}
	if p := s.FG.sgr(mode, true); p != "" {
		parts = append(parts, p)
	}
	if p := s.BG.sgr(mode, false); p != "" {
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}

// Reset is the escape sequence that clears every attribute.
const Reset = "\x1b[0m"
