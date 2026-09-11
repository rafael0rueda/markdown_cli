package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// Options configures a render pass.
type Options struct {
	// Width is the column count to lay out for.
	Width int
	// Theme supplies every style and glyph. Required.
	Theme *theme.Theme
	// LinkMode decides how link destinations are surfaced.
	LinkMode LinkMode
	// BaseDir is the directory the document was loaded from. Relative image
	// and link paths resolve against it.
	BaseDir string
	// WorkDir and HomeDir shorten the local paths shown after link text:
	// relative to WorkDir, or with ~ for HomeDir, whichever is shortest.
	// Empty leaves paths absolute.
	WorkDir, HomeDir string
	// Images draws pictures. Nil shows alt text instead.
	Images ImageHandler
	// Links resolves Obsidian-style wikilinks. Nil renders them as plain
	// text, which is what a document outside a vault should get.
	Links LinkResolver
	// MaxImageRows caps how tall any single image may be. Zero uses a default.
	MaxImageRows int
	// Frontmatter selects how a leading YAML block is shown.
	Frontmatter FrontmatterMode
}

// LinkMode selects how a link's destination is presented.
type LinkMode int

const (
	// LinkAuto shows the URL inline after the text unless the serializer is
	// emitting OSC 8 hyperlinks, in which case the caller sets LinkHide.
	LinkAuto LinkMode = iota
	// LinkInline always writes the URL in parentheses after the link text.
	LinkInline
	// LinkHide shows only the link text.
	LinkHide
)

// ParseLinkMode maps a --links flag value onto a LinkMode.
func ParseLinkMode(s string) (LinkMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return LinkAuto, nil
	case "inline":
		return LinkInline, nil
	case "hide", "off", "none":
		return LinkHide, nil
	}
	return LinkAuto, fmt.Errorf("unknown link mode %q (want auto, inline or hide)", s)
}

// defaultWidth is used when the caller does not supply one.
const defaultWidth = 80

// Render parses markdown and lays it out.
func Render(source []byte, opts Options) (*Doc, error) {
	if opts.Theme == nil {
		opts.Theme = theme.Dark()
	}
	if opts.Width <= 0 {
		opts.Width = defaultWidth
	}
	// Below this the output is unreadable anyway, and several block renderers
	// assume they have room for a prefix plus at least one cell of content.
	if opts.Width < 20 {
		opts.Width = 20
	}

	// Before anything else looks at the text, so that no path through the
	// renderer - code, raw HTML, frontmatter, link targets - can carry a
	// terminal control sequence through to the output.
	source = normalizeSource(source)

	// Frontmatter is separated before parsing rather than after. CommonMark
	// has no concept of it, so leaving it in the source makes the opening
	// delimiter a horizontal rule and the properties a stray list.
	var entries []metaEntry
	if opts.Frontmatter != FrontmatterRaw {
		if meta, body, ok := splitFrontmatter(source); ok {
			source = body
			if opts.Frontmatter == FrontmatterMeta {
				entries = parseFrontmatter(meta)
			}
		}
	}

	md := goldmark.New(goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		wikilinkExtension{},
		commentExtension{},
		markExtension{},
		mathExtension{},
	))
	root := md.Parser().Parse(text.NewReader(source))

	r := &renderer{
		src:  source,
		th:   opts.Theme,
		opts: opts,
		doc:  &Doc{Width: opts.Width},
	}
	if len(entries) > 0 {
		r.frontmatter(entries)
		r.blank()
	}
	r.renderChildren(root, false)
	r.doc.trimTrailingBlanks()
	return r.doc, nil
}

// renderer walks the AST and appends lines to doc.
type renderer struct {
	src  []byte
	th   *theme.Theme
	opts Options
	doc  *Doc

	// rest is the prefix repeated on every line of the current block context:
	// blockquote bars and list indentation accumulate here.
	rest []Run
	// pending, when non-nil, replaces rest on the next line emitted. List
	// markers use it so the marker lands on the item's first line only.
	pending []Run
	// base is the style inherited by inline content, so a blockquote can tint
	// everything inside it without each inline case knowing why.
	base theme.Style
}

// prefixes returns the first-line and continuation prefixes, consuming any
// pending first-line prefix.
func (r *renderer) prefixes() (first, rest []Run) {
	if r.pending != nil {
		first = r.pending
		r.pending = nil
		return first, r.rest
	}
	return r.rest, r.rest
}

// emit wraps runs into lines and appends them.
func (r *renderer) emit(runs []Run) {
	first, rest := r.prefixes()
	r.doc.Lines = append(r.doc.Lines, wrapRuns(runs, r.opts.Width, first, rest)...)
}

// emitRaw appends one line verbatim, without wrapping, behind the current
// prefix. Code blocks and table borders use it: their content is already laid
// out and must not be re-flowed.
func (r *renderer) emitRaw(line Line) {
	first, _ := r.prefixes()
	line.Runs = append(append([]Run(nil), first...), line.Runs...)
	r.doc.Lines = append(r.doc.Lines, line)
}

// blank appends an empty line, keeping the continuation prefix so blockquote
// bars stay unbroken across paragraph gaps.
func (r *renderer) blank() {
	r.doc.Lines = append(r.doc.Lines, Line{Runs: append([]Run(nil), r.rest...)})
}

// contentWidth is the space left for content after the current prefix.
func (r *renderer) contentWidth() int {
	w := r.opts.Width - runsWidth(r.rest)
	if w < 1 {
		return 1
	}
	return w
}

// withPrefix runs fn with additional prefix runs pushed onto the context.
func (r *renderer) withPrefix(firstAdd, restAdd []Run, base theme.Style, fn func()) {
	savedRest, savedPending, savedBase := r.rest, r.pending, r.base

	r.rest = append(append([]Run(nil), savedRest...), restAdd...)
	if firstAdd != nil {
		head := savedPending
		if head == nil {
			head = savedRest
		}
		r.pending = append(append([]Run(nil), head...), firstAdd...)
	} else {
		r.pending = nil
	}
	r.base = savedBase.Merge(base)

	fn()

	r.rest, r.pending, r.base = savedRest, savedPending, savedBase
}

// renderChildren renders every child block of n, separating them with blank
// lines unless tight is set.
func (r *renderer) renderChildren(n ast.Node, tight bool) {
	first := true
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if hiddenBlock(c, r.src) {
			continue
		}
		if !first && !tight {
			r.blank()
		}
		r.renderBlock(c)
		first = false
	}
}

func (r *renderer) renderBlock(n ast.Node) {
	switch n := n.(type) {
	case *ast.Heading:
		r.heading(n)
	case *ast.Paragraph:
		r.inlineBlock(n)
	case *ast.TextBlock:
		r.inlineBlock(n)
	case *ast.Blockquote:
		r.blockquote(n)
	case *ast.List:
		r.list(n)
	case *ast.ListItem:
		// Reached only when a list item is walked directly; normally list()
		// drives these so it can compute markers.
		r.renderChildren(n, true)
	case *ast.FencedCodeBlock:
		r.codeBlock(n.Lines(), string(n.Language(r.src)))
	case *ast.CodeBlock:
		r.codeBlock(n.Lines(), "")
	case *ast.ThematicBreak:
		r.rule()
	case *ast.HTMLBlock:
		r.htmlBlock(n)
	case *MathBlock:
		r.mathBlock(n)
	case *extast.Table:
		r.table(n)
	case *extast.FootnoteList:
		r.footnoteList(n)
	case *extast.Footnote:
		r.footnote(n)
	default:
		// Any block type not handled above still has its inline content shown
		// rather than silently vanishing.
		if n.Type() == ast.TypeBlock {
			r.renderChildren(n, false)
		}
	}
}

func (r *renderer) heading(n *ast.Heading) {
	level := n.Level
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	style := r.th.Headings[level-1]

	// The hash prefix is drawn in the heading's own color but faint, so the
	// level stays visible without competing with the text.
	marker := Run{
		Text:  strings.Repeat("#", level) + " ",
		Style: theme.Style{FG: style.FG, Faint: true},
	}
	runs := append([]Run{marker}, r.inlineChildren(n, r.base.Merge(style), "")...)
	r.emit(runs)
}

func (r *renderer) blockquote(n *ast.Blockquote) {
	if c, ok := r.detectCallout(n); ok {
		r.calloutBlock(n, c)
		return
	}
	bar := []Run{
		{Text: r.th.Glyphs.QuoteBar, Style: r.th.QuoteBar},
		{Text: " "},
	}
	r.withPrefix(nil, bar, r.th.Quote, func() {
		r.renderChildren(n, false)
	})
}

func (r *renderer) list(n *ast.List) {
	depth := listDepth(n)
	num := n.Start
	if num == 0 {
		num = 1
	}
	first := true
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		if !first && !n.IsTight {
			r.blank()
		}
		first = false

		var marker string
		if n.IsOrdered() {
			marker = strconv.Itoa(num) + string(n.Marker) + " "
			num++
		} else {
			bullets := r.th.Glyphs.Bullets
			marker = bullets[depth%len(bullets)] + " "
		}

		markerRuns := []Run{{Text: marker, Style: r.th.ListMarker}}
		indent := []Run{{Text: strings.Repeat(" ", uniWidth(marker))}}
		r.withPrefix(markerRuns, indent, theme.Style{}, func() {
			r.renderChildren(item, true)
		})
	}
}

// listDepth counts how many lists enclose n, so nested bullets can differ.
func listDepth(n ast.Node) int {
	depth := 0
	for p := n.Parent(); p != nil; p = p.Parent() {
		if _, ok := p.(*ast.List); ok {
			depth++
		}
	}
	return depth
}

func (r *renderer) rule() {
	w := r.contentWidth()
	r.emitRaw(Line{Runs: []Run{{
		Text:  strings.Repeat(r.th.Glyphs.Rule, w),
		Style: r.th.Rule,
	}}})
}

func (r *renderer) htmlBlock(n *ast.HTMLBlock) {
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		r.htmlLine(string(seg.Value(r.src)))
	}
	if n.ClosureLine.Start >= 0 {
		r.htmlLine(string(n.ClosureLine.Value(r.src)))
	}
}

// htmlLine emits one line of raw HTML. Like code, it is not re-flowed as
// prose, but it still has to be broken to fit: a long tag left intact would
// overflow the layout width and be wrapped a second time by the terminal.
func (r *renderer) htmlLine(text string) {
	text = strings.TrimRight(text, "\r\n")
	runs := []Run{{Text: strings.ReplaceAll(text, "\t", "    "), Style: r.th.HTML}}
	width := r.contentWidth()
	for {
		head, tail := splitRuns(runs, width)
		r.emitRaw(Line{Runs: head})
		if len(tail) == 0 {
			return
		}
		runs = tail
	}
}

func (r *renderer) footnoteList(n *extast.FootnoteList) {
	// The caller has already emitted a separating blank line; the rule marks
	// the notes off from the body text above them.
	r.rule()
	r.blank()
	r.renderChildren(n, false)
}

func (r *renderer) footnote(n *extast.Footnote) {
	marker := []Run{{Text: "[" + strconv.Itoa(n.Index) + "] ", Style: r.th.Footnote}}
	indent := []Run{{Text: strings.Repeat(" ", runsWidth(marker))}}
	r.withPrefix(marker, indent, theme.Style{}, func() {
		r.renderChildren(n, true)
	})
}

// LinkResolver turns a wikilink target into a path on disk.
//
// Obsidian links name a file rather than giving its location, so resolving one
// needs to know where the vault is and where it keeps attachments. That is not
// this package's concern, so it arrives through this interface.
type LinkResolver interface {
	// ResolveEmbed locates the target of an ![[...]] embed.
	ResolveEmbed(target string) (path string, ok bool)
	// ResolveNote locates the target of a [[...]] link, supplying the .md
	// extension that Obsidian leaves off.
	ResolveNote(target string) (path string, ok bool)
}

// resolveEmbed locates an embed target, reporting failure when there is no
// resolver or the file cannot be found.
func (r *renderer) resolveEmbed(target string) (string, bool) {
	if r.opts.Links == nil || target == "" {
		return "", false
	}
	return r.opts.Links.ResolveEmbed(target)
}

// resolveNote locates a link target.
func (r *renderer) resolveNote(target string) (string, bool) {
	if r.opts.Links == nil || target == "" {
		return "", false
	}
	return r.opts.Links.ResolveNote(target)
}

// uniWidth is a shorthand for the display width of a plain string.
func uniWidth(s string) int { return Run{Text: s}.Width() }
