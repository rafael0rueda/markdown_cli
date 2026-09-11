package render

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// A callout is a blockquote whose first line names a kind in brackets:
//
//	> [!warning] Disk almost full
//	> Clear the cache before the next backup.
//
// Obsidian draws these as colored boxes with an icon and a title, and GitHub
// uses the same syntax for its alerts (> [!NOTE]). Rendered as a plain quote,
// the marker shows up as literal text and the title runs into the body.

// calloutMarker matches the start of a callout's first line: the kind, an
// optional fold marker (+ or -), and the space before the title.
var calloutMarker = regexp.MustCompile(`^\[!([A-Za-z0-9_-]+)\]([+-]?)[ \t]*`)

// calloutAliases maps every kind name Obsidian accepts onto the canonical kind
// that decides its color and icon.
var calloutAliases = map[string]string{
	"note":      "note",
	"abstract":  "abstract",
	"summary":   "abstract",
	"tldr":      "abstract",
	"info":      "info",
	"todo":      "todo",
	"tip":       "tip",
	"hint":      "tip",
	"important": "tip",
	"success":   "success",
	"check":     "success",
	"done":      "success",
	"question":  "question",
	"help":      "question",
	"faq":       "question",
	"warning":   "warning",
	"caution":   "warning",
	"attention": "warning",
	"failure":   "failure",
	"fail":      "failure",
	"missing":   "failure",
	"danger":    "danger",
	"error":     "danger",
	"bug":       "bug",
	"example":   "example",
	"quote":     "quote",
	"cite":      "quote",
}

// callout describes a blockquote recognized as a callout.
type callout struct {
	// kind is the canonical kind; unknown kinds are drawn as notes, which is
	// what Obsidian does with them.
	kind string
	// name is the kind as written, which becomes the title when none is given.
	name string
	// title holds the inline nodes of the first line after the marker.
	title []ast.Node
}

// detectCallout reports whether a blockquote is a callout and, if it is,
// detaches the marker and the title line from its first paragraph so that
// what remains is the body.
func (r *renderer) detectCallout(bq *ast.Blockquote) (callout, bool) {
	p, ok := bq.FirstChild().(*ast.Paragraph)
	if !ok || p.Lines().Len() == 0 {
		return callout{}, false
	}
	first := p.Lines().At(0)
	m := calloutMarker.FindSubmatchIndex(first.Value(r.src))
	if m == nil {
		return callout{}, false
	}
	name := string(first.Value(r.src)[m[2]:m[3]])
	kind, known := calloutAliases[strings.ToLower(name)]
	if !known {
		kind = "note"
	}

	c := callout{kind: kind, name: name}
	c.title = splitTitleLine(p, first.Start+m[1])
	if p.FirstChild() == nil {
		bq.RemoveChild(bq, p)
	}
	return c, true
}

// splitTitleLine removes the first line of a paragraph and returns its inline
// nodes, less the callout marker that ends at markerEnd.
//
// The marker cannot be removed node by node: goldmark splits "[!note] Title"
// into "[" and "!note] Title", because a bracket might have opened a link. So
// it is cut by source position, trimming whichever text node it ends inside.
// The first line ends at the first text node carrying a line break, which is
// how goldmark marks the end of every line but the last.
func splitTitleLine(p *ast.Paragraph, markerEnd int) []ast.Node {
	var title []ast.Node
	inMarker := true
	for c := p.FirstChild(); c != nil; {
		next := c.NextSibling()
		t, isText := c.(*ast.Text)
		endsLine := isText && (t.SoftLineBreak() || t.HardLineBreak())

		if inMarker && isText && t.Segment.Start < markerEnd {
			if t.Segment.Stop <= markerEnd {
				p.RemoveChild(p, c)
				if endsLine {
					return title // a marker with no title after it
				}
				c = next
				continue
			}
			t.Segment = t.Segment.WithStart(markerEnd)
		}
		inMarker = false

		p.RemoveChild(p, c)
		title = append(title, c)
		if endsLine {
			t.SetSoftLineBreak(false)
			t.SetHardLineBreak(false)
			return title
		}
		c = next
	}
	return title
}

// calloutBlock draws a callout: a bar in the kind's color down the left, a
// title line with the kind's icon, and the body beneath.
//
// The body is not tinted the way a quotation is. A callout is the author's
// own text set apart for attention, not someone else's words.
func (r *renderer) calloutBlock(bq *ast.Blockquote, c callout) {
	style := r.th.Callouts[c.kind]
	titleStyle := style.Merge(theme.Style{Bold: true})

	bar := []Run{{Text: r.th.Glyphs.QuoteBar, Style: style}, {Text: " "}}
	r.withPrefix(nil, bar, theme.Style{}, func() {
		title := trimLeadingSpace(r.inlineNodes(c.title, r.base.Merge(titleStyle), ""))
		if runsWidth(title) == 0 {
			title = []Run{{Text: defaultCalloutTitle(c.name), Style: r.base.Merge(titleStyle)}}
		}
		if icon := r.th.Glyphs.CalloutIcons[c.kind]; icon != "" {
			title = append([]Run{{Text: icon + " ", Style: r.base.Merge(titleStyle)}}, title...)
		}
		r.emit(title)
		r.renderChildren(bq, false)
	})
}

// defaultCalloutTitle is the title shown when none is written: the kind name,
// capitalized, as Obsidian and GitHub both show it.
func defaultCalloutTitle(name string) string {
	if name == "" {
		return ""
	}
	lower := strings.ToLower(name)
	return strings.ToUpper(lower[:1]) + lower[1:]
}

// trimLeadingSpace drops leading whitespace from a run slice.
func trimLeadingSpace(runs []Run) []Run {
	for len(runs) > 0 {
		trimmed := strings.TrimLeft(runs[0].Text, " \t")
		if trimmed != "" {
			runs[0].Text = trimmed
			return runs
		}
		runs = runs[1:]
	}
	return runs
}
