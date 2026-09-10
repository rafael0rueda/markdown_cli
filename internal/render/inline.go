package render

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// inlineChildren renders every inline child of n into a run slice.
//
// base is the style inherited from the enclosing block; link is the URL of the
// enclosing anchor, if any, so nested emphasis inside a link stays clickable.
func (r *renderer) inlineChildren(n ast.Node, base theme.Style, link string) []Run {
	var out []Run
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.inline(c, base, link)...)
	}
	return out
}

// inlineNodes renders an explicit list of inline nodes. Splitting a paragraph
// around a standalone image leaves runs of siblings that are no longer a
// single node's children.
func (r *renderer) inlineNodes(nodes []ast.Node, base theme.Style, link string) []Run {
	var out []Run
	for _, n := range nodes {
		out = append(out, r.inline(n, base, link)...)
	}
	return out
}

func (r *renderer) inline(n ast.Node, base theme.Style, link string) []Run {
	th := r.th

	switch n := n.(type) {
	case *ast.Text:
		runs := []Run{{Text: textValue(n, r.src), Style: base, Link: link}}
		switch {
		case n.HardLineBreak():
			runs = append(runs, hardBreak)
		case n.SoftLineBreak():
			// A soft break is a wrap opportunity, not a line break: emit a
			// space and let the wrapper decide where the line ends.
			runs = append(runs, Run{Text: " ", Style: base, Link: link})
		}
		return runs

	case *ast.String:
		return []Run{{Text: string(n.Value), Style: base, Link: link}}

	case *ast.CodeSpan:
		// Code spans hold raw Text children; concatenate them so the whole
		// span shares one background.
		var b strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			b.Write(c.Text(r.src))
		}
		return []Run{{Text: b.String(), Style: base.Merge(th.Code), Link: link}}

	case *ast.Emphasis:
		style := th.Emphasis
		if n.Level >= 2 {
			style = th.Strong
		}
		return r.inlineChildren(n, base.Merge(style), link)

	case *extast.Strikethrough:
		return r.inlineChildren(n, base.Merge(th.Strike), link)

	case *ast.Link:
		dest := r.resolve(unescape(n.Destination))
		runs := r.inlineChildren(n, base.Merge(th.Link), dest)
		return append(runs, r.linkSuffix(dest, base)...)

	case *ast.AutoLink:
		url := r.resolve(string(n.URL(r.src)))
		label := string(n.Label(r.src))
		if label == "" {
			label = url
		}
		return []Run{{Text: label, Style: base.Merge(th.Link), Link: url}}

	case *ast.Image:
		// Phase 1 shows alt text and the source. Inline graphics replace this
		// once terminal capability detection lands.
		alt := strings.TrimSpace(nodeText(n, r.src))
		if alt == "" {
			alt = "image"
		}
		icon := th.Glyphs.ImageIcon
		if icon != "" {
			icon += " "
		}
		dest := r.resolve(unescape(n.Destination))
		runs := []Run{{Text: icon + alt, Style: base.Merge(th.ImageAlt), Link: dest}}
		return append(runs, r.linkSuffix(dest, base)...)

	case *ast.RawHTML:
		var b strings.Builder
		for i := 0; i < n.Segments.Len(); i++ {
			seg := n.Segments.At(i)
			b.Write(seg.Value(r.src))
		}
		return []Run{{Text: b.String(), Style: base.Merge(th.HTML), Link: link}}

	case *Wikilink:
		return r.wikilink(n, base)

	case *extast.TaskCheckBox:
		style, glyph := th.TaskTodo, th.Glyphs.TaskTodo
		if n.IsChecked {
			style, glyph = th.TaskDone, th.Glyphs.TaskDone
		}
		return []Run{{Text: glyph + " ", Style: base.Merge(style)}}

	case *extast.FootnoteLink:
		return []Run{{
			Text:  "[" + strconv.Itoa(n.Index) + "]",
			Style: base.Merge(th.Footnote),
		}}

	case *extast.FootnoteBacklink:
		// The backlink is navigation for HTML output; in a terminal it is
		// noise, so it is dropped.
		return nil

	default:
		if n.Type() == ast.TypeInline {
			return r.inlineChildren(n, base, link)
		}
		return nil
	}
}

// wikilink renders an Obsidian [[link]] or an ![[embed]] that could not be
// drawn as a picture.
//
// An unresolved link still shows its label rather than vanishing: a link to a
// note that has not been written yet is a normal thing to have in a vault, and
// the reader wants to see the name.
func (r *renderer) wikilink(n *Wikilink, base theme.Style) []Run {
	th := r.th
	label := n.Label()

	if n.Embed {
		// Reaching here means the picture could not be drawn, so it is shown
		// the way any other undrawable image is.
		icon := th.Glyphs.ImageIcon
		if icon != "" {
			icon += " "
		}
		style := base.Merge(th.ImageAlt)
		path, ok := r.resolveEmbed(n.Target)
		if !ok {
			return []Run{{Text: icon + label, Style: style}}
		}
		runs := []Run{{Text: icon + label, Style: style, Link: path}}
		return append(runs, r.linkSuffix(path, base)...)
	}

	path, ok := r.resolveNote(n.Target)
	if !ok {
		// Nothing to point at, so it is styled as a link but is not one.
		return []Run{{Text: label, Style: base.Merge(th.Link)}}
	}
	runs := []Run{{Text: label, Style: base.Merge(th.Link), Link: path}}
	return append(runs, r.linkSuffix(path, base)...)
}

// linkSuffix returns the parenthesized URL shown after link text, or nil when
// the link mode hides it.
func (r *renderer) linkSuffix(dest string, base theme.Style) []Run {
	if dest == "" || r.opts.LinkMode == LinkHide {
		return nil
	}
	return []Run{{Text: " (" + dest + ")", Style: base.Merge(r.th.LinkURL)}}
}

// resolve turns a document-relative path into one relative to the process's
// working directory, so links printed to the terminal are actually openable.
// Absolute paths, URLs and fragments are left alone.
func (r *renderer) resolve(dest string) string {
	if r.opts.BaseDir == "" || dest == "" {
		return dest
	}
	if strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "/") {
		return dest
	}
	if i := strings.Index(dest, ":"); i > 0 && !strings.ContainsAny(dest[:i], "/\\.") {
		return dest // has a URL scheme
	}
	// Clean so a link written as "./img.png" does not print as
	// "/abs/dir/./img.png"; the fragment is preserved separately because
	// Clean would mangle it.
	path, frag, hasFrag := strings.Cut(dest, "#")
	out := filepath.Join(r.opts.BaseDir, path)
	if hasFrag {
		out += "#" + frag
	}
	return out
}

// textValue returns what a text node says, with backslash escapes and
// character references resolved. Raw text - the inside of a code span - is
// taken literally, since escapes mean nothing there.
func textValue(n *ast.Text, src []byte) string {
	if n.IsRaw() {
		return string(n.Value(src))
	}
	return unescape(n.Value(src))
}

// nodeText collects the plain text of an inline subtree.
func nodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch c := c.(type) {
		case *ast.Text:
			b.WriteString(textValue(c, src))
		case *ast.String:
			b.Write(c.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}
