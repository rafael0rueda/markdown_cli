package render

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Wikilink is an Obsidian-style link or embed: [[note]] or ![[image.png]].
//
// It is not part of CommonMark, so goldmark needs an extension to see it at
// all; without one the whole construct is passed through as literal text,
// which is what a plain markdown renderer shows.
type Wikilink struct {
	ast.BaseInline

	// Target is the file being referred to, without any heading or display
	// text.
	Target string
	// Fragment is the heading or block reference after a #, if any.
	Fragment string
	// Display is the text after a |, when it is a label rather than a size.
	Display string
	// Width and Height are the pixel dimensions after a |, when it is a size.
	// Obsidian writes these as ![[img.png|300]] or ![[img.png|300x200]].
	Width, Height int
	// Embed distinguishes ![[...]] from [[...]].
	Embed bool
}

// KindWikilink identifies the node type.
var KindWikilink = ast.NewNodeKind("Wikilink")

// Kind implements ast.Node.
func (n *Wikilink) Kind() ast.NodeKind { return KindWikilink }

// Dump implements ast.Node.
func (n *Wikilink) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Target":   n.Target,
		"Fragment": n.Fragment,
		"Display":  n.Display,
		"Embed":    strconv.FormatBool(n.Embed),
	}, nil)
}

// Label is the text to show for the link.
//
// With no alias given, a link carrying a heading is shown as "Note > Section",
// which is how Obsidian renders it. Dropping the heading would make two links
// into different parts of the same note look identical.
func (n *Wikilink) Label() string {
	switch {
	case n.Display != "":
		return n.Display
	case n.Target == "":
		return n.Fragment
	case n.Fragment != "":
		return n.Target + " > " + n.Fragment
	}
	return n.Target
}

// wikilinkParser recognizes [[...]] and ![[...]] in inline text.
type wikilinkParser struct{}

// Trigger implements parser.InlineParser. Both introducing characters are
// claimed: '[' for links and '!' for embeds.
func (p *wikilinkParser) Trigger() []byte { return []byte{'[', '!'} }

// Parse implements parser.InlineParser.
func (p *wikilinkParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()

	offset := 0
	embed := false
	if len(line) > 0 && line[0] == '!' {
		embed = true
		offset = 1
	}
	// Only a doubled bracket is a wikilink; a single one is an ordinary
	// markdown link and belongs to goldmark's own parser.
	if len(line) < offset+4 || line[offset] != '[' || line[offset+1] != '[' {
		return nil
	}

	body := line[offset+2:]
	end := bytes.Index(body, []byte("]]"))
	if end < 0 {
		// Wikilinks do not span lines, so an unclosed one is just text.
		return nil
	}

	node := parseWikilinkBody(string(body[:end]), embed)
	if node == nil {
		return nil
	}
	block.Advance(offset + 2 + end + 2)
	return node
}

// parseWikilinkBody splits the inside of a wikilink into its parts.
func parseWikilinkBody(body string, embed bool) *Wikilink {
	node := &Wikilink{Embed: embed}

	// The display text or size comes after the first pipe.
	target := body
	if i := strings.IndexByte(body, '|'); i >= 0 {
		target = body[:i]
		suffix := strings.TrimSpace(body[i+1:])
		// In an embed the pipe usually carries a size; in a link it is always
		// a label. A size that fails to parse is treated as a label, which is
		// what Obsidian does with, say, ![[diagram.png|the big one]].
		if embed {
			if w, h, ok := parseSize(suffix); ok {
				node.Width, node.Height = w, h
			} else {
				node.Display = suffix
			}
		} else {
			node.Display = suffix
		}
	}

	// A heading or block reference comes after a hash.
	if i := strings.IndexByte(target, '#'); i >= 0 {
		node.Fragment = strings.TrimSpace(target[i+1:])
		target = target[:i]
	}
	node.Target = strings.TrimSpace(target)

	if node.Target == "" && node.Fragment == "" {
		return nil
	}
	return node
}

// parseSize reads an embed size, which is either a width or "widthxheight".
func parseSize(s string) (width, height int, ok bool) {
	w, h, hasHeight := strings.Cut(s, "x")
	width, err := strconv.Atoi(strings.TrimSpace(w))
	if err != nil || width <= 0 {
		return 0, 0, false
	}
	if !hasHeight {
		return width, 0, true
	}
	height, err = strconv.Atoi(strings.TrimSpace(h))
	if err != nil || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// wikilinkExtension registers the parser with goldmark.
type wikilinkExtension struct{}

// Extend implements goldmark.Extender.
func (wikilinkExtension) Extend(m goldmark.Markdown) {
	// The priority puts this ahead of the built-in link and image parsers,
	// which trigger on the same characters and would otherwise consume the
	// first bracket.
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&wikilinkParser{}, 100),
	))
}
