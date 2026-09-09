package render

import (
	"strings"

	"github.com/rivo/uniseg"
)

// hardBreak is the sentinel run that forces a line break during wrapping. It
// carries no text, so it never reaches output; markdown hard breaks and the
// line structure of code blocks are expressed with it.
var hardBreak = Run{Text: "\x00break"}

func isBreak(r Run) bool { return r.Text == hardBreak.Text }

// token is one wrappable unit: a word, a stretch of whitespace, or a break.
type token struct {
	run     Run
	width   int
	isSpace bool
	isBreak bool
}

// tokenize splits runs into words and whitespace while preserving each
// fragment's style and link.
func tokenize(runs []Run) []token {
	var out []token
	for _, r := range runs {
		if isBreak(r) {
			out = append(out, token{isBreak: true})
			continue
		}
		if r.Text == "" {
			continue
		}
		var cur strings.Builder
		curSpace := false
		flush := func() {
			if cur.Len() == 0 {
				return
			}
			text := cur.String()
			out = append(out, token{
				run:     Run{Text: text, Style: r.Style, Link: r.Link},
				width:   uniseg.StringWidth(text),
				isSpace: curSpace,
			})
			cur.Reset()
		}
		for _, ch := range r.Text {
			space := ch == ' ' || ch == '\t'
			if cur.Len() > 0 && space != curSpace {
				flush()
			}
			curSpace = space
			if ch == '\t' {
				cur.WriteString("    ")
				continue
			}
			cur.WriteRune(ch)
		}
		flush()
	}
	return out
}

// wrapRuns lays runs out into lines no wider than width.
//
// first is prepended to the first line and rest to every continuation line;
// this is how list markers ("1. " then "   ") and blockquote bars are applied
// without the block renderers having to know about wrapping.
func wrapRuns(runs []Run, width int, first, rest []Run) []Line {
	if width < 1 {
		width = 1
	}
	firstW, restW := runsWidth(first), runsWidth(rest)

	var lines []Line
	cur := append([]Run(nil), first...)
	curW := firstW
	started := false // has any non-prefix content landed on this line?

	avail := func() int {
		if len(lines) == 0 {
			return width - firstW
		}
		return width - restW
	}

	flush := func() {
		lines = append(lines, Line{Runs: cur})
		cur = append([]Run(nil), rest...)
		curW = restW
		started = false
	}

	var pending *token // a space held back until we know it is not trailing

	for _, t := range tokenize(runs) {
		switch {
		case t.isBreak:
			pending = nil
			flush()
		case t.isSpace:
			if started {
				sp := t
				pending = &sp
			}
		default:
			space := 0
			if pending != nil {
				space = pending.width
			}
			if started && curW+space+t.width > width {
				pending = nil
				flush()
				space = 0
			}
			if pending != nil {
				cur = appendRun(cur, pending.run)
				curW += pending.width
				pending = nil
			}
			// A word too long for a whole line has to be broken mid-word.
			word := t.run
			w := t.width
			for curW+w > width && avail() > 0 {
				head, tail, headW := splitToWidth(word.Text, width-curW)
				if head == "" {
					if started {
						// Not even one cluster fits in what is left of this
						// line; try again on a fresh one.
						flush()
						continue
					}
					// Nothing fits even on an empty line, which happens when
					// a double-width glyph meets a one-cell column. Emit a
					// single cluster and overflow by a cell: the alternative
					// is putting the whole unbroken word on one line, which
					// overflows by far more.
					head, tail, headW = firstCluster(word.Text)
					if head == "" {
						break
					}
				}
				cur = appendRun(cur, Run{Text: head, Style: word.Style, Link: word.Link})
				curW += headW
				started = true
				flush()
				word.Text = tail
				w = uniseg.StringWidth(tail)
				if tail == "" {
					break
				}
			}
			if word.Text != "" {
				cur = appendRun(cur, word)
				curW += w
				started = true
			}
		}
	}

	// Emit the final line unless nothing at all was written to it. A document
	// that ends mid-block still needs its last row.
	if started || len(lines) == 0 {
		lines = append(lines, Line{Runs: cur})
	}
	return lines
}

// splitToWidth cuts s at the last grapheme cluster boundary that keeps the
// result within max cells, returning the head, the remainder, and the head's
// width. Splitting on clusters rather than runes keeps combining marks and
// emoji sequences intact.
func splitToWidth(s string, max int) (head, tail string, headWidth int) {
	if max <= 0 {
		return "", s, 0
	}
	g := uniseg.NewGraphemes(s)
	end, w := 0, 0
	for g.Next() {
		cw := uniseg.StringWidth(g.Str())
		if w+cw > max {
			break
		}
		w += cw
		_, end = g.Positions()
	}
	return s[:end], s[end:], w
}

// appendRun adds r to runs, merging it into the previous run when they share
// styling. Fewer, longer runs mean fewer escape sequences on output.
func appendRun(runs []Run, r Run) []Run {
	if r.Text == "" {
		return runs
	}
	if n := len(runs); n > 0 && runs[n-1].Style == r.Style && runs[n-1].Link == r.Link {
		runs[n-1].Text += r.Text
		return runs
	}
	return append(runs, r)
}

// runsWidth totals the display width of a run slice.
func runsWidth(runs []Run) int {
	n := 0
	for _, r := range runs {
		n += r.Width()
	}
	return n
}

// padTo appends spaces to bring runs out to the given width.
func padTo(runs []Run, width int) []Run {
	if pad := width - runsWidth(runs); pad > 0 {
		runs = append(runs, Run{Text: strings.Repeat(" ", pad)})
	}
	return runs
}
