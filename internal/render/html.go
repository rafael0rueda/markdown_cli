package render

import (
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// Markdown allows raw HTML, and READMEs lean on a handful of tags for things
// markdown has no syntax for: <kbd> for keys, <br> for a line break inside a
// table cell, <sub> and <sup>. goldmark hands each tag over as a separate node
// with the text between them as ordinary siblings, so a tag is paired with its
// closing tag among those siblings and what lies between is restyled.
//
// Only tags with an obvious terminal rendering are understood. Anything else,
// and any tag left unclosed, is shown as it is written, as before.

// nbsp is the non-breaking space: a space the wrapper will not break a line
// at or merge with its neighbors.
const nbsp = "\u00a0"

// htmlTag is an inline HTML tag, parsed just far enough to act on.
type htmlTag struct {
	name    string // lower case
	closing bool
	attrs   string
}

var tagPattern = regexp.MustCompile(`^<(/?)([A-Za-z][A-Za-z0-9-]*)(\s[^>]*?)?\s*/?>$`)

func parseTag(raw string) (htmlTag, bool) {
	m := tagPattern.FindStringSubmatch(raw)
	if m == nil {
		return htmlTag{}, false
	}
	return htmlTag{name: strings.ToLower(m[2]), closing: m[1] == "/", attrs: m[3]}, true
}

var attrPattern = regexp.MustCompile(`(?:^|\s)([A-Za-z_:][-A-Za-z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)

// attr returns an attribute's value, with character references decoded.
func (t htmlTag) attr(name string) string {
	for _, m := range attrPattern.FindAllStringSubmatch(t.attrs, -1) {
		if strings.EqualFold(m[1], name) {
			// A reference can spell a control character, so the decoded
			// value is sanitized like any other text from the document.
			return Sanitize(html.UnescapeString(m[2] + m[3] + m[4]))
		}
	}
	return ""
}

// rawHTML returns the source text of an inline HTML node.
func rawHTML(n *ast.RawHTML, src []byte) string {
	var b strings.Builder
	for i := 0; i < n.Segments.Len(); i++ {
		seg := n.Segments.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

// spanStyle is the style an HTML element applies to its content, and whether
// the element is one that is understood at all.
func (r *renderer) spanStyle(name string) (theme.Style, bool) {
	th := r.th
	switch name {
	case "b", "strong":
		return th.Strong, true
	case "i", "em", "cite", "var", "dfn":
		return th.Emphasis, true
	case "s", "del", "strike":
		return th.Strike, true
	case "u", "ins":
		return theme.Style{Underline: true}, true
	case "mark":
		return th.Mark, true
	case "code", "tt", "samp":
		return th.Code, true
	case "kbd":
		return th.Kbd, true
	case "sub", "sup", "a", "span", "small", "abbr":
		// Styled some other way, or not at all: the tags go and the text
		// stays.
		return theme.Style{}, true
	}
	return theme.Style{}, false
}

// html renders an inline HTML node, given the siblings that follow it. It
// reports how many of those it used up - the content and closing tag of an
// element - and false if the node is not something it understands, in which
// case the caller shows it raw.
func (r *renderer) html(n *ast.RawHTML, rest []ast.Node, base theme.Style, link string) ([]Run, int, bool) {
	raw := rawHTML(n, r.src)
	if strings.HasPrefix(raw, "<!--") {
		// A comment: hidden, as it is in a browser.
		return nil, 0, true
	}
	tag, ok := parseTag(raw)
	if !ok || tag.closing {
		return nil, 0, false
	}

	switch tag.name {
	case "br":
		return []Run{hardBreak}, 0, true
	case "wbr":
		return nil, 0, true
	case "img":
		return r.htmlImage(tag, base), 0, true
	}

	style, known := r.spanStyle(tag.name)
	if !known {
		return nil, 0, false
	}
	end := closingTag(tag.name, rest, r.src)
	if end < 0 {
		return nil, 0, false
	}

	inner := rest[:end]
	switch tag.name {
	case "a":
		dest := r.resolve(tag.attr("href"))
		if dest == "" {
			return r.inlineNodes(inner, base, link), end + 1, true
		}
		runs := r.inlineNodes(inner, base.Merge(r.th.Link), dest)
		return append(runs, r.linkSuffix(dest, base)...), end + 1, true
	case "sup", "sub":
		return scriptRuns(r.inlineNodes(inner, base, link), tag.name == "sup"), end + 1, true
	case "kbd":
		return r.keyRuns(r.inlineNodes(inner, base.Merge(style), link), base.Merge(style)), end + 1, true
	}
	return r.inlineNodes(inner, base.Merge(style), link), end + 1, true
}

// closingTag finds the tag closing an element named name among nodes,
// allowing for the same element nested inside it. It returns -1 if there is
// none.
func closingTag(name string, nodes []ast.Node, src []byte) int {
	depth := 0
	for i, n := range nodes {
		raw, ok := n.(*ast.RawHTML)
		if !ok {
			continue
		}
		tag, ok := parseTag(rawHTML(raw, src))
		if !ok || tag.name != name {
			continue
		}
		if !tag.closing {
			depth++
			continue
		}
		if depth == 0 {
			return i
		}
		depth--
	}
	return -1
}

// htmlImage shows an <img> the way an image that cannot be drawn is shown:
// its alt text, linking to the picture.
func (r *renderer) htmlImage(tag htmlTag, base theme.Style) []Run {
	alt := strings.TrimSpace(tag.attr("alt"))
	if alt == "" {
		alt = "image"
	}
	icon := r.th.Glyphs.ImageIcon
	if icon != "" {
		icon += " "
	}
	dest := r.resolve(tag.attr("src"))
	runs := []Run{{Text: icon + alt, Style: base.Merge(r.th.ImageAlt), Link: dest}}
	return append(runs, r.linkSuffix(dest, base)...)
}

// keyRuns draws the content of a <kbd> as a key: padded inside its
// background where the theme gives it one, and in brackets where it does not,
// so the key still stands out without color.
//
// The padding, and any space inside the key, is non-breaking. Ordinary spaces
// would let a line break inside "Page Down", and would be merged with the
// spaces either side of the key, taking its padding with them.
func (r *renderer) keyRuns(runs []Run, style theme.Style) []Run {
	left, right := nbsp, nbsp
	if !r.th.Kbd.BG.IsSet() {
		left, right = "[", "]"
	}
	out := []Run{{Text: left, Style: style}}
	for _, run := range runs {
		run.Text = strings.ReplaceAll(run.Text, " ", nbsp)
		out = append(out, run)
	}
	return append(out, Run{Text: right, Style: style})
}

// scriptRuns raises or lowers text. Where Unicode has no small form for every
// character, a caret or underscore marks it instead, the way it would be
// written without markup: note^[1], or x^(see below) for more than a word.
func scriptRuns(runs []Run, up bool) []Run {
	var text strings.Builder
	for _, r := range runs {
		if !isBreak(r) {
			text.WriteString(r.Text)
		}
	}
	if text.Len() == 0 {
		return nil
	}
	convert, mark := subscript, "_"
	if up {
		convert, mark = superscript, "^"
	}
	if _, ok := convert(text.String()); ok {
		out := make([]Run, 0, len(runs))
		for _, r := range runs {
			if !isBreak(r) {
				r.Text, _ = convert(r.Text)
			}
			out = append(out, r)
		}
		return out
	}
	open, close := mark, ""
	if strings.ContainsAny(text.String(), " \t") {
		open, close = mark+"(", ")"
	}
	first := runs[0]
	out := append([]Run{{Text: open, Style: first.Style, Link: first.Link}}, runs...)
	if close != "" {
		last := runs[len(runs)-1]
		out = append(out, Run{Text: close, Style: last.Style, Link: last.Link})
	}
	return out
}
