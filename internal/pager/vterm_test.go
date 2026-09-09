package pager

import (
	"strconv"
	"strings"
)

// vterm is a minimal terminal emulator used by the tests.
//
// The pager draws by positioning the cursor and clearing lines rather than by
// writing a stream of text, so assertions cannot be made on its raw output:
// stripping the escape sequences runs every row together. Interpreting the
// sequences into a grid makes the tests ask what is actually on screen, which
// is the thing that matters.
type vterm struct {
	cols, rows int
	grid       [][]rune
	row, col   int
	savedRow   int
	savedCol   int
}

func newVTerm(cols, rows int) *vterm {
	v := &vterm{cols: cols, rows: rows}
	v.resize(cols, rows)
	return v
}

func (v *vterm) resize(cols, rows int) {
	v.cols, v.rows = cols, rows
	v.grid = make([][]rune, rows)
	for i := range v.grid {
		v.grid[i] = blankRow(cols)
	}
	v.row, v.col = 0, 0
}

func blankRow(cols int) []rune {
	row := make([]rune, cols)
	for i := range row {
		row[i] = ' '
	}
	return row
}

// write feeds terminal output into the emulator.
func (v *vterm) write(b []byte) {
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			i += v.escape(b[i:])
		case c == '\r':
			v.col = 0
			i++
		case c == '\n':
			v.row++
			v.col = 0
			i++
		case c == '\t':
			v.col = min(v.col+4, v.cols)
			i++
		case c < 0x20:
			i++ // other control characters are not modelled
		default:
			r, size := decodeRune(b[i:])
			if size == 0 {
				return // incomplete rune at the end of the buffer
			}
			v.put(r)
			i += size
		}
	}
}

// put writes one character at the cursor.
func (v *vterm) put(r rune) {
	if v.row >= 0 && v.row < v.rows && v.col >= 0 && v.col < v.cols {
		v.grid[v.row][v.col] = r
	}
	v.col++
}

// escape interprets one escape sequence and returns how many bytes it used.
func (v *vterm) escape(b []byte) int {
	if len(b) < 2 {
		return len(b)
	}
	switch b[1] {
	case '[':
		return v.csi(b)
	case ']', '_', 'P', '^':
		// OSC, APC (kitty graphics), DCS (sixel) and PM all run to a string
		// terminator. Their content is not drawn as text, so it is skipped.
		for i := 2; i < len(b); i++ {
			if b[i] == 0x07 {
				return i + 1
			}
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
		}
		return len(b)
	case '7':
		v.savedRow, v.savedCol = v.row, v.col
		return 2
	case '8':
		v.row, v.col = v.savedRow, v.savedCol
		return 2
	}
	return 2
}

// csi interprets a Control Sequence Introducer sequence.
func (v *vterm) csi(b []byte) int {
	end := -1
	for i := 2; i < len(b); i++ {
		if b[i] >= 0x40 && b[i] <= 0x7e {
			end = i
			break
		}
	}
	if end < 0 {
		return len(b)
	}
	params := string(b[2:end])
	final := b[end]
	n := end + 1

	// Private sequences - the alternate screen, cursor visibility, the
	// keyboard protocol - change no visible text.
	if strings.HasPrefix(params, "?") || strings.HasPrefix(params, ">") || strings.HasPrefix(params, "<") {
		return n
	}

	nums := parseParams(params)
	switch final {
	case 'H', 'f':
		v.row = at(nums, 0, 1) - 1
		v.col = at(nums, 1, 1) - 1
	case 'A':
		v.row -= at(nums, 0, 1)
	case 'B':
		v.row += at(nums, 0, 1)
	case 'C':
		v.col += at(nums, 0, 1)
	case 'D':
		v.col -= at(nums, 0, 1)
	case 'K':
		v.clearLine(at(nums, 0, 0))
	case 'J':
		if at(nums, 0, 0) == 2 {
			v.resize(v.cols, v.rows)
		}
	}
	return n
}

// clearLine erases part of the current row.
func (v *vterm) clearLine(mode int) {
	if v.row < 0 || v.row >= v.rows {
		return
	}
	switch mode {
	case 0: // cursor to end of line
		for c := max(v.col, 0); c < v.cols; c++ {
			v.grid[v.row][c] = ' '
		}
	case 1: // start of line to cursor
		for c := 0; c <= min(v.col, v.cols-1); c++ {
			v.grid[v.row][c] = ' '
		}
	case 2:
		v.grid[v.row] = blankRow(v.cols)
	}
}

func parseParams(s string) []int {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

func at(nums []int, i, fallback int) int {
	if i < len(nums) && nums[i] > 0 {
		return nums[i]
	}
	return fallback
}

// line returns the text of one row, with trailing spaces removed.
func (v *vterm) line(n int) string {
	if n < 0 || n >= v.rows {
		return ""
	}
	return strings.TrimRight(string(v.grid[n]), " ")
}

// String renders the whole screen.
func (v *vterm) String() string {
	rows := make([]string, v.rows)
	for i := range v.grid {
		rows[i] = v.line(i)
	}
	return strings.Join(rows, "\n")
}

// statusLine is the bottom row, where the pager draws its bar.
func (v *vterm) statusLine() string {
	return v.line(v.rows - 1)
}
