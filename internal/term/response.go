package term

import (
	"strconv"
	"strings"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// Control characters that introduce and terminate the escape sequences a
// terminal sends back in reply to a query.
const (
	esc = 0x1b
	bel = 0x07
	st  = `\` // the second byte of a String Terminator, ESC backslash
)

// response holds everything a probe learned from the terminal's replies.
// Parsing is kept separate from the I/O that collects the bytes so it can be
// tested against captured replies from terminals that are not to hand.
type response struct {
	kittyGraphics bool
	kittyKeyboard bool
	sixel         bool
	deviceAttrs   bool // a primary DA reply arrived, so the terminal answered

	background    theme.Color
	hasBackground bool

	cellWidth, cellHeight int
}

// parseResponses interprets a raw buffer of terminal replies.
//
// Replies can arrive interleaved and in any order, and a terminal is free to
// ignore any query it does not recognize, so this scans for every sequence it
// understands and ignores the rest rather than expecting a fixed shape.
func parseResponses(buf []byte) response {
	var r response
	for _, seq := range scanSequences(buf) {
		if len(seq) < 2 {
			continue
		}
		switch seq[1] {
		case '_':
			r.parseAPC(seq)
		case '[':
			r.parseCSI(seq)
		case ']':
			r.parseOSC(seq)
		}
	}
	return r
}

// parseAPC handles Application Program Command replies, which is how the kitty
// graphics protocol answers. A supporting terminal replies to a query with
// ESC _ G i=<id>;OK ST.
func (r *response) parseAPC(seq []byte) {
	body := trimTerminator(seq[2:])
	if len(body) == 0 || body[0] != 'G' {
		return
	}
	if _, payload, ok := strings.Cut(string(body), ";"); ok {
		if strings.HasPrefix(payload, "OK") {
			r.kittyGraphics = true
		}
	}
}

// parseCSI handles Control Sequence Introducer replies: device attributes,
// the kitty keyboard protocol flags, and the cell size report.
func (r *response) parseCSI(seq []byte) {
	body := seq[2:]
	if len(body) == 0 {
		return
	}
	final := body[len(body)-1]
	params := string(body[:len(body)-1])

	switch final {
	case 'c':
		// Primary device attributes: ESC [ ? 62;4;22 c. Each parameter names
		// a capability, and 4 is sixel graphics.
		if !strings.HasPrefix(params, "?") {
			return
		}
		r.deviceAttrs = true
		for _, p := range strings.Split(strings.TrimPrefix(params, "?"), ";") {
			if p == "4" {
				r.sixel = true
			}
		}

	case 'u':
		// Kitty keyboard protocol: ESC [ ? <flags> u. Any reply at all means
		// the terminal implements it; the flags say what is currently enabled.
		if strings.HasPrefix(params, "?") {
			r.kittyKeyboard = true
		}

	case 't':
		// Cell size in pixels: ESC [ 6 ; <height> ; <width> t.
		parts := strings.Split(params, ";")
		if len(parts) != 3 || parts[0] != "6" {
			return
		}
		h, err1 := strconv.Atoi(parts[1])
		w, err2 := strconv.Atoi(parts[2])
		if err1 == nil && err2 == nil && w > 0 && h > 0 {
			r.cellWidth, r.cellHeight = w, h
		}
	}
}

// parseOSC handles Operating System Command replies, used here for the
// background color: ESC ] 11 ; rgb:RRRR/GGGG/BBBB ST.
func (r *response) parseOSC(seq []byte) {
	body := string(trimTerminator(seq[2:]))
	spec, ok := strings.CutPrefix(body, "11;")
	if !ok {
		return
	}
	if c, ok := parseXColor(spec); ok {
		r.background = c
		r.hasBackground = true
	}
}

// parseXColor parses an X11 color specification, the form terminals answer
// OSC color queries with. Components may carry one to four hex digits and are
// scaled to eight bits, so rgb:f/0/0 and rgb:ffff/0000/0000 agree.
func parseXColor(s string) (theme.Color, bool) {
	spec, ok := strings.CutPrefix(strings.TrimSpace(s), "rgb:")
	if !ok {
		return theme.Color{}, false
	}
	parts := strings.Split(spec, "/")
	if len(parts) != 3 {
		return theme.Color{}, false
	}
	var out [3]uint8
	for i, p := range parts {
		if len(p) == 0 || len(p) > 4 {
			return theme.Color{}, false
		}
		v, err := strconv.ParseUint(p, 16, 32)
		if err != nil {
			return theme.Color{}, false
		}
		// Scale from the component's own width down to 8 bits: a two-digit
		// value is already there, a four-digit one is divided by 257.
		max := uint64(1)<<(4*len(p)) - 1
		out[i] = uint8(v * 255 / max)
	}
	return theme.RGB(out[0], out[1], out[2]), true
}

// scanSequences splits a buffer into the escape sequences it contains,
// discarding any bytes that are not part of one.
func scanSequences(buf []byte) [][]byte {
	var out [][]byte
	for i := 0; i < len(buf); {
		if buf[i] != esc || i+1 >= len(buf) {
			i++
			continue
		}
		end := sequenceEnd(buf, i)
		if end < 0 {
			// A truncated sequence at the end of the buffer: the terminal was
			// still writing when the read timed out. Nothing to salvage.
			break
		}
		out = append(out, buf[i:end])
		i = end
	}
	return out
}

// sequenceEnd returns the index just past the escape sequence starting at
// start, or -1 if the buffer ends mid-sequence.
func sequenceEnd(buf []byte, start int) int {
	switch buf[start+1] {
	case '[':
		// CSI: parameter and intermediate bytes, then a final byte in the
		// range 0x40-0x7E.
		for i := start + 2; i < len(buf); i++ {
			if buf[i] >= 0x40 && buf[i] <= 0x7e {
				return i + 1
			}
		}
		return -1

	case ']', '_', 'P', '^':
		// OSC, APC, DCS and PM all run until a String Terminator; OSC also
		// accepts a BEL, which is what several terminals actually send.
		for i := start + 2; i < len(buf); i++ {
			if buf[i] == bel {
				return i + 1
			}
			if buf[i] == esc && i+1 < len(buf) && buf[i+1] == st[0] {
				return i + 2
			}
		}
		return -1

	default:
		// A two-byte escape sequence.
		return start + 2
	}
}

// trimTerminator strips a trailing ST or BEL from a sequence body.
func trimTerminator(b []byte) []byte {
	switch {
	case len(b) >= 2 && b[len(b)-2] == esc && b[len(b)-1] == st[0]:
		return b[:len(b)-2]
	case len(b) >= 1 && b[len(b)-1] == bel:
		return b[:len(b)-1]
	}
	return b
}
