package graphics

import (
	"fmt"
	"strings"
)

// Inside tmux an image cannot simply be placed at the cursor. tmux does not
// know the picture is there, so the moment it scrolls the pane, switches
// windows or redraws, the text moves and the image stays behind - or is wiped.
//
// The kitty protocol's answer is Unicode placeholders. The image is sent
// through tmux to the terminal outside, with a virtual placement that says how
// many cells it fills, and is then drawn by writing ordinary characters: each
// cell is a placeholder character whose color names the image and whose
// combining marks name the row and column. tmux stores and moves those like
// any other text, and the terminal paints the matching piece of the picture
// wherever they land. This is what kitty's own icat does inside tmux.

// placeholderRune is the character every placeholder cell is made of.
const placeholderRune = '\U0010EEEE'

// maxPlaceholderCells is the widest and tallest image the diacritics can
// number.
var maxPlaceholderCells = len(placeholderDiacritics)

// tmuxPassthrough wraps an escape sequence so tmux hands it unchanged to the
// terminal outside instead of acting on it. tmux only does so when
// allow-passthrough is on, and discards the sequence otherwise.
func tmuxPassthrough(seq string) string {
	return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
}

// virtualPlacement returns the sequence telling the terminal that the image is
// drawn by placeholder cells, and fitted to cols by rows of them.
func (k *kittyImage) virtualPlacement(cols, rows int) string {
	return fmt.Sprintf("\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", k.id, cols, rows)
}

// placeholders returns the text drawing rows [skip, skip+visible) of an image
// cols cells wide, starting at the cursor. Each further row begins indent
// columns in from the left margin, so the picture stays square beside a list
// marker or a blockquote bar.
//
// Every cell carries its row and column rather than leaving the terminal to
// infer them from the cell before, so that a line tmux redraws from the middle
// still comes out right.
func (k *kittyImage) placeholders(cols, skip, visible, indent int) string {
	var b strings.Builder
	// The low three bytes of the id are the color; the top byte, when there
	// is one, is a third combining mark.
	fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm", byte(k.id>>16), byte(k.id>>8), byte(k.id))
	high := ""
	if top := int(k.id >> 24); top > 0 && top < maxPlaceholderCells {
		high = string(placeholderDiacritics[top])
	}
	for row := skip; row < skip+visible; row++ {
		if row > skip {
			// A carriage return rather than moving left by the width: after
			// a cell in the last column the cursor has not advanced, and
			// counting back from there would land one column short.
			b.WriteString("\r\x1b[B")
			if indent > 0 {
				fmt.Fprintf(&b, "\x1b[%dC", indent)
			}
		}
		for col := 0; col < cols; col++ {
			b.WriteRune(placeholderRune)
			b.WriteRune(placeholderDiacritics[row])
			b.WriteRune(placeholderDiacritics[col])
			b.WriteString(high)
		}
	}
	b.WriteString("\x1b[39m")
	return b.String()
}
