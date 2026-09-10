package render

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/util"
)

// A terminal treats some characters as commands rather than text. A document
// that could put them on the screen could retitle the window, rewrite the
// clipboard (OSC 52, which kitty allows by default), or move the cursor to
// paint over what was already drawn - so opening an untrusted README would be
// enough. Nothing from a document reaches the terminal without passing
// through Sanitize.
//
// The characters are shown rather than dropped, the way less and cat -v do it:
// a reader should be able to see that a file contains something odd, and
// silently deleting bytes would make the text differ from the file in ways
// that cannot be noticed.

// Sanitize replaces every character a terminal would act on with a visible
// stand-in. C0 controls become their Unicode control pictures (ESC shows as
// ␛), DEL becomes ␡, and C1 controls, NUL and invalid UTF-8 become U+FFFD.
// Tab and newline are kept, since layout handles both.
func Sanitize(s string) string { return sanitize(s, true) }

// sanitizeOutput is Sanitize for text about to be written out. Layout has
// expanded every tab and consumed every newline by then, so one still present
// came from outside the document - a file name, say - and would break the
// line it sits in; newline is shown as ␊ and tab becomes a space.
func sanitizeOutput(s string) string { return sanitize(s, false) }

func sanitize(s string, keepLayout bool) string {
	i := firstUnsafe(s, keepLayout)
	if i < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	b.WriteString(s[:i])
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteRune(utf8.RuneError)
		case (r == '\t' || r == '\n') && keepLayout:
			b.WriteRune(r)
		case r == '\t':
			b.WriteByte(' ')
		case isControl(r):
			b.WriteRune(controlPicture(r))
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// firstUnsafe returns the index of the first byte sanitize would change, or
// -1. Almost all text is clean, so this keeps the common case to one scan and
// no allocation.
func firstUnsafe(s string, keepLayout bool) int {
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if (c < 0x20 || c == 0x7f) && !(keepLayout && (c == '\t' || c == '\n')) {
				return i
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && size == 1) || isControl(r) {
			return i
		}
		i += size
	}
	return -1
}

// isControl reports whether r is a C0 or C1 control character or DEL.
func isControl(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}

func controlPicture(r rune) rune {
	switch {
	case r == 0:
		// CommonMark specifies U+FFFD for NUL, rather than ␀.
		return utf8.RuneError
	case r < 0x20:
		return 0x2400 + r
	case r == 0x7f:
		return '␡'
	default:
		// C1 controls have no control pictures of their own.
		return utf8.RuneError
	}
}

// normalizeSource prepares a document for parsing: line endings become plain
// newlines, and everything else a terminal would act on is made visible.
//
// CommonMark counts CRLF and a lone CR as line endings too, but goldmark keeps
// the CR in the text it hands back. Left there, the CR at the end of every
// line of a Windows-edited file sends the cursor back to column 0, where the
// background padding that follows paints over the line.
func normalizeSource(src []byte) []byte {
	s := string(src)
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return []byte(Sanitize(s))
}

// sanitizeURL makes a link target safe to embed in an OSC 8 sequence, where
// an ESC would end the sequence early and whatever followed would be run as a
// command. Control characters are percent-encoded rather than shown, since a
// URL is not displayed and the encoded form is still the same address.
func sanitizeURL(u string) string {
	unsafe := func(r rune) bool { return isControl(r) || r == utf8.RuneError }
	if strings.IndexFunc(u, unsafe) < 0 {
		return u
	}
	var b strings.Builder
	for i := 0; i < len(u); {
		r, size := utf8.DecodeRuneInString(u[i:])
		if isControl(r) || (r == utf8.RuneError && size == 1) {
			for _, c := range []byte(u[i : i+size]) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		} else {
			b.WriteString(u[i : i+size])
		}
		i += size
	}
	return b.String()
}

// unescape turns markdown text into the text it stands for, the way an HTML
// renderer would before printing it: a backslash before ASCII punctuation
// makes that character literal (\* is a plain asterisk), and entity and
// numeric character references become the character they name (&amp; is &).
//
// It is a single pass, because the two interact: \&amp; is a literal "&amp;",
// which unescaping first and resolving entities second would get wrong.
//
// Code spans and code blocks are raw and must not come through here.
func unescape(src []byte) string {
	s := util.BytesToReadOnlyString(src)
	if !strings.ContainsAny(s, `\&`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && util.IsPunct(s[i+1]):
			b.WriteByte(s[i+1])
			i++
		case c == '&':
			if text, n := readReference(s[i:]); n > 0 {
				b.WriteString(text)
				i += n - 1
				continue
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// readReference decodes an entity or numeric character reference at the start
// of s, returning the text it stands for and its length in s, or a length of
// zero if s does not start with a valid one.
//
// A reference can name any character, including the ones Sanitize exists to
// stop: &#27; is ESC. So what comes out is sanitized again, and whitespace
// controls become a space, which is what HTML would make of them and keeps
// newlines out of the middle of a line.
func readReference(s string) (string, int) {
	end := strings.IndexByte(s, ';')
	if end < 2 || end > 40 {
		return "", 0
	}
	body := s[1:end]

	if body[0] != '#' {
		for i := 0; i < len(body); i++ {
			if !util.IsAlphaNumeric(body[i]) {
				return "", 0
			}
		}
		entity, ok := util.LookUpHTML5EntityByName(body)
		if !ok {
			return "", 0
		}
		return safeReference(string(entity.Characters)), end + 1
	}

	// Numeric: &#1234; is decimal with up to 7 digits, &#x1F600; hex with up
	// to 6, per CommonMark.
	digits, base, maxLen := body[1:], 10, 7
	if len(digits) > 0 && (digits[0] == 'x' || digits[0] == 'X') {
		digits, base, maxLen = digits[1:], 16, 6
	}
	if len(digits) == 0 || len(digits) > maxLen {
		return "", 0
	}
	v, err := strconv.ParseUint(digits, base, 32)
	if err != nil {
		return "", 0
	}
	r := rune(v)
	// Zero, surrogates and out-of-range values stand for U+FFFD.
	if r == 0 || !utf8.ValidRune(r) {
		r = utf8.RuneError
	}
	return safeReference(string(r)), end + 1
}

func safeReference(s string) string {
	return Sanitize(strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\t', '\r', '\f', '\v':
			return ' '
		}
		return r
	}, s))
}
