package pager

import "unicode/utf8"

// decodeRune reads one UTF-8 rune from the front of buf.
//
// A size of zero means buf holds the start of a rune but not all of it, which
// happens when a multi-byte character is split across reads. The caller waits
// for more input rather than emitting a replacement character.
func decodeRune(buf []byte) (rune, int) {
	r, size := utf8.DecodeRune(buf)
	if r == utf8.RuneError && size <= 1 {
		if !utf8.FullRune(buf) {
			return 0, 0 // incomplete
		}
		return utf8.RuneError, 1
	}
	return r, size
}
