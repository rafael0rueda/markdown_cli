package render

import (
	"bytes"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Obsidian treats text between a pair of %% as a comment: it is kept in the
// file but not shown when the note is read. People use it for notes to
// themselves, so showing it would be showing something the author chose to
// keep out of view.
//
// A comment can sit inside a line - "text %%aside%% more" - or take whole
// lines of its own:
//
//	%%
//	Several lines,
//
//	even paragraphs.
//	%%
//
// Code is exempt: %% inside a code span or block is just text, which falls
// out of goldmark trying code first.

var commentDelim = []byte("%%")

// Comment is an inline %%...%% comment. It renders as nothing.
type Comment struct {
	ast.BaseInline
}

// KindComment is the node kind of an inline comment.
var KindComment = ast.NewNodeKind("Comment")

// Kind implements ast.Node.
func (n *Comment) Kind() ast.NodeKind { return KindComment }

// Dump implements ast.Node.
func (n *Comment) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

// CommentBlock is a comment occupying whole lines. It renders as nothing.
type CommentBlock struct {
	ast.BaseBlock
	// closed is set when the comment ended on the line it started on.
	closed bool
}

// KindCommentBlock is the node kind of a block comment.
var KindCommentBlock = ast.NewNodeKind("CommentBlock")

// Kind implements ast.Node.
func (n *CommentBlock) Kind() ast.NodeKind { return KindCommentBlock }

// Dump implements ast.Node.
func (n *CommentBlock) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

// commentParser finds %%...%% within a paragraph, including one that spans
// several of its lines.
type commentParser struct{}

func (commentParser) Trigger() []byte { return []byte{'%'} }

func (commentParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	if !bytes.HasPrefix(line, commentDelim) {
		return nil
	}
	startLine, startPos := block.Position()
	block.Advance(len(commentDelim))
	for {
		line, _ := block.PeekLine()
		if line == nil {
			// Never closed within the paragraph: then it is not a comment,
			// and the %% is ordinary text.
			block.SetPosition(startLine, startPos)
			return nil
		}
		if i := bytes.Index(line, commentDelim); i >= 0 {
			block.Advance(i + len(commentDelim))
			return &Comment{}
		}
		block.AdvanceLine()
	}
}

// commentBlockParser handles a comment that starts a block with %% and
// either ends on the same line or runs across lines to a closing %%.
type commentBlockParser struct{}

func (commentBlockParser) Trigger() []byte { return []byte{'%'} }

func (commentBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, seg := reader.PeekLine()
	indent, _ := util.IndentWidth(line, reader.LineOffset())
	if indent > 3 {
		return nil, parser.NoChildren
	}
	rest := bytes.TrimLeft(line, " \t")
	if !bytes.HasPrefix(rest, commentDelim) {
		return nil, parser.NoChildren
	}
	after := rest[len(commentDelim):]

	if i := bytes.Index(after, commentDelim); i >= 0 {
		// Closed on the same line. With text after it, the line is a
		// paragraph that happens to start with a comment, and the inline
		// parser will deal with it.
		if len(bytes.TrimSpace(after[i+len(commentDelim):])) > 0 {
			return nil, parser.NoChildren
		}
		reader.Advance(seg.Len() - 1)
		return &CommentBlock{closed: true}, parser.NoChildren
	}

	// Open until a later %%. If there is none, this is not a comment: hiding
	// everything to the end of the file, as Obsidian would, turns one stray
	// %% into a document that silently loses its second half.
	if !closesLater(reader) {
		return nil, parser.NoChildren
	}
	reader.Advance(seg.Len() - 1)
	return &CommentBlock{}, parser.NoChildren
}

// closesLater reports whether a %% appears on some line after the current
// one, leaving the reader where it was.
func closesLater(reader text.Reader) bool {
	line, pos := reader.Position()
	defer reader.SetPosition(line, pos)
	reader.AdvanceLine()
	for {
		l, _ := reader.PeekLine()
		if l == nil {
			return false
		}
		if bytes.Contains(l, commentDelim) {
			return true
		}
		reader.AdvanceLine()
	}
}

func (commentBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	if node.(*CommentBlock).closed {
		return parser.Close
	}
	line, seg := reader.PeekLine()
	if line == nil {
		return parser.Close
	}
	reader.Advance(seg.Len() - 1)
	if bytes.Contains(line, commentDelim) {
		// Anything after the closing %% on this line is hidden too. Splitting
		// the line would need a block to hand the remainder to, and a comment
		// closed mid-line with more text after it is rare enough not to.
		return parser.Close
	}
	return parser.Continue | parser.NoChildren
}

func (commentBlockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {}

// A comment block has to follow a blank line or another block. Letting one
// interrupt a paragraph would misread the closing line of a multi-line
// inline comment - "text %%\naside\n%% more" - as the start of a new one.
func (commentBlockParser) CanInterruptParagraph() bool { return false }

func (commentBlockParser) CanAcceptIndentedLine() bool { return false }

type commentExtension struct{}

func (commentExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(util.Prioritized(commentParser{}, 100)),
		// Ahead of the paragraph parser, which would otherwise take the line.
		parser.WithBlockParsers(util.Prioritized(commentBlockParser{}, 150)),
	)
}

// hiddenBlock reports whether a block renders as nothing at all, so that it
// can be skipped without leaving the blank line that separates blocks.
func hiddenBlock(n ast.Node, src []byte) bool {
	switch n := n.(type) {
	case *CommentBlock:
		return true
	case *ast.Paragraph:
		// A paragraph made only of comments, and the whitespace between them.
		sawComment := false
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			switch c := c.(type) {
			case *Comment:
				sawComment = true
			case *ast.Text:
				if len(bytes.TrimSpace(c.Value(src))) > 0 {
					return false
				}
			default:
				return false
			}
		}
		return sawComment
	}
	return false
}
