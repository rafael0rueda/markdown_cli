package pager

import (
	"strconv"
	"strings"
)

// key is a decoded keypress.
type key struct {
	// Rune is the character typed, for ordinary keys.
	Rune rune
	// Name identifies a key that has no character, such as an arrow.
	Name keyName
	// Ctrl reports whether control was held.
	Ctrl bool
}

type keyName int

const (
	keyRune keyName = iota
	keyUp
	keyDown
	keyLeft
	keyRight
	keyHome
	keyEnd
	keyPageUp
	keyPageDown
	keyEnter
	keyEscape
	keyBackspace
	keyUnknown
)

// decodeKey reads one keypress from the front of buf.
//
// It returns the key and how many bytes it consumed, or ok=false if buf holds
// only part of a sequence and more input is needed. Both the traditional
// escape sequences and the kitty keyboard protocol are understood, because the
// protocol is only enabled when the terminal supports it and the rest of the
// world still speaks the old encoding.
func decodeKey(buf []byte) (k key, n int, ok bool) {
	if len(buf) == 0 {
		return key{}, 0, false
	}

	if buf[0] != 0x1b {
		return decodeSimple(buf)
	}
	if len(buf) == 1 {
		// A lone escape byte. With the kitty protocol enabled this cannot
		// happen - Escape arrives as a full sequence - so it is unambiguous.
		return key{Name: keyEscape}, 1, true
	}

	switch buf[1] {
	case '[':
		return decodeCSI(buf)
	case 'O':
		// The older application-cursor encoding, still sent by some terminals.
		if len(buf) < 3 {
			return key{}, 0, false
		}
		if name, found := cursorKeys[buf[2]]; found {
			return key{Name: name}, 3, true
		}
		return key{Name: keyUnknown}, 3, true
	}
	// Escape followed by anything else is treated as Escape alone; the pager
	// has no use for Alt-modified keys.
	return key{Name: keyEscape}, 1, true
}

// decodeSimple handles a single byte that is not the start of a sequence.
func decodeSimple(buf []byte) (key, int, bool) {
	switch c := buf[0]; {
	case c == '\r', c == '\n':
		return key{Name: keyEnter}, 1, true
	case c == 0x7f, c == 0x08:
		return key{Name: keyBackspace}, 1, true
	case c < 0x20:
		// Control characters: 0x01 is ctrl-a, and so on.
		return key{Rune: rune(c + 'a' - 1), Ctrl: true}, 1, true
	}

	// A multi-byte rune has to arrive whole before it can be decoded.
	r, size := decodeRune(buf)
	if size == 0 {
		return key{}, 0, false
	}
	return key{Rune: r}, size, true
}

// cursorKeys maps the final byte of a cursor sequence to a key.
var cursorKeys = map[byte]keyName{
	'A': keyUp, 'B': keyDown, 'C': keyRight, 'D': keyLeft,
	'H': keyHome, 'F': keyEnd,
}

// decodeCSI handles a Control Sequence Introducer sequence.
func decodeCSI(buf []byte) (key, int, bool) {
	// Find the final byte, which ends the sequence.
	end := -1
	for i := 2; i < len(buf); i++ {
		if buf[i] >= 0x40 && buf[i] <= 0x7e {
			end = i
			break
		}
	}
	if end < 0 {
		return key{}, 0, false // incomplete
	}
	params := string(buf[2:end])
	final := buf[end]
	n := end + 1

	switch final {
	case 'A', 'B', 'C', 'D', 'H', 'F':
		return key{Name: cursorKeys[final]}, n, true

	case '~':
		// Numbered keys: 1 home, 4 end, 5 page up, 6 page down.
		switch numericParam(params) {
		case 1, 7:
			return key{Name: keyHome}, n, true
		case 4, 8:
			return key{Name: keyEnd}, n, true
		case 5:
			return key{Name: keyPageUp}, n, true
		case 6:
			return key{Name: keyPageDown}, n, true
		}
		return key{Name: keyUnknown}, n, true

	case 'u':
		// The kitty keyboard protocol: CSI unicode-key ; modifiers u. This is
		// what makes Escape unambiguous, which is the reason for enabling it.
		return decodeKittyKey(params, n)
	}
	return key{Name: keyUnknown}, n, true
}

// decodeKittyKey interprets a kitty keyboard protocol event.
func decodeKittyKey(params string, n int) (key, int, bool) {
	fields := strings.Split(params, ";")
	code := numericParam(fields[0])
	if code == 0 {
		return key{Name: keyUnknown}, n, true
	}

	k := key{}
	if len(fields) > 1 {
		// Modifiers are a bitmask offset by one: 1 means none, and bit 0 of
		// the remainder is shift, bit 2 is control.
		if mods := numericParam(fields[1]); mods > 1 {
			k.Ctrl = (mods-1)&4 != 0
		}
	}

	switch code {
	case 27:
		k.Name = keyEscape
	case 13:
		k.Name = keyEnter
	case 127, 8:
		k.Name = keyBackspace
	default:
		k.Rune = rune(code)
	}
	return k, n, true
}

// numericParam reads the leading number of a parameter string, ignoring any
// private-mode prefix and sub-parameters.
func numericParam(s string) int {
	s = strings.TrimLeft(s, "?<=>")
	if i := strings.IndexAny(s, ";:"); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
