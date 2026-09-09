package render

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// Range is a half-open span of bytes within a line's text.
type Range struct {
	Start, End int
}

// FindAll returns every occurrence of query in s, as byte ranges.
//
// Matching is case-insensitive but works over the original string rather than
// a lowercased copy, because case folding can change a string's length - the
// offsets from a folded copy would not line up with the text being drawn.
func FindAll(s, query string) []Range {
	if query == "" || s == "" {
		return nil
	}
	var out []Range
	for start := 0; start < len(s); {
		n := matchAt(s[start:], query)
		if n < 0 {
			// No match beginning here; advance one whole rune.
			_, size := utf8.DecodeRuneInString(s[start:])
			start += max(size, 1)
			continue
		}
		out = append(out, Range{Start: start, End: start + n})
		// Overlapping matches are not reported: after "aa" in "aaa" the next
		// search resumes past it, which is what a reader stepping through
		// matches expects.
		start += max(n, 1)
	}
	return out
}

// matchAt reports the byte length of a case-insensitive match of query at the
// start of s, or -1 if there is none.
func matchAt(s, query string) int {
	consumed := 0
	for _, want := range query {
		got, size := utf8.DecodeRuneInString(s[consumed:])
		if size == 0 {
			return -1
		}
		if unicode.ToLower(got) != unicode.ToLower(want) {
			return -1
		}
		consumed += size
	}
	return consumed
}

// Highlight returns a copy of the line with the given byte ranges restyled.
//
// The ranges are offsets into the line's plain text, which is what a search
// works over, so this has to walk the runs and split any that a range falls
// partway through.
func Highlight(line Line, ranges []Range, style theme.Style) Line {
	if len(ranges) == 0 {
		return line
	}

	var out []Run
	offset := 0
	for _, run := range line.Runs {
		end := offset + len(run.Text)
		// Cut this run at every range boundary that falls inside it.
		for cut := offset; cut < end; {
			next, inRange := nextBoundary(cut, end, ranges)
			piece := run.Text[cut-offset : next-offset]
			s := run.Style
			if inRange {
				s = s.Merge(style)
			}
			out = appendRun(out, Run{Text: piece, Style: s, Link: run.Link})
			cut = next
		}
		offset = end
	}
	line.Runs = out
	return line
}

// nextBoundary returns the offset at which the highlighting state changes
// after pos, bounded by limit, and whether pos itself is inside a range.
func nextBoundary(pos, limit int, ranges []Range) (next int, inRange bool) {
	next = limit
	for _, r := range ranges {
		switch {
		case pos >= r.Start && pos < r.End:
			// Inside this range; it ends at r.End or the run does.
			return min(r.End, limit), true
		case r.Start > pos:
			// Outside, and this range starts later.
			next = min(next, r.Start)
		}
	}
	return next, false
}

// LineMatches reports the ranges of query within a line's text.
func LineMatches(line Line, query string) []Range {
	return FindAll(line.Text(), query)
}

// Search finds the lines of a document containing query, in order.
func (d *Doc) Search(query string) []int {
	if strings.TrimSpace(query) == "" {
		return nil
	}
	var out []int
	for i, line := range d.Lines {
		if len(FindAll(line.Text(), query)) > 0 {
			out = append(out, i)
		}
	}
	return out
}
