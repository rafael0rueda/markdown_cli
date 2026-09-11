package render

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Obsidian marks text as highlighted with a pair of == on each side:
// "some ==important== words". It follows the rules emphasis does, so the
// delimiters have to hug the text - "a == b" is an equation, not a mark - and
// it is parsed the same way goldmark parses ~~strikethrough~~.

// Mark is ==highlighted== text. Its children are the highlighted inlines.
type Mark struct {
	ast.BaseInline
}

// KindMark is the node kind of highlighted text.
var KindMark = ast.NewNodeKind("Mark")

// Kind implements ast.Node.
func (n *Mark) Kind() ast.NodeKind { return KindMark }

// Dump implements ast.Node.
func (n *Mark) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type markDelimiter struct{}

func (markDelimiter) IsDelimiter(b byte) bool { return b == '=' }

func (markDelimiter) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}

func (markDelimiter) OnMatch(consumes int) ast.Node { return &Mark{} }

type markParser struct{}

func (markParser) Trigger() []byte { return []byte{'='} }

func (markParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	d := parser.ScanDelimiter(line, before, 2, markDelimiter{})
	// Exactly two: a single = is ordinary text, and a run of three or more is
	// more likely an ASCII rule or an arrow ("==>") than a mark.
	if d == nil || d.OriginalLength != 2 || before == '=' {
		return nil
	}
	d.Segment = segment.WithStop(segment.Start + d.OriginalLength)
	block.Advance(d.OriginalLength)
	pc.PushDelimiter(d)
	return d
}

type markExtension struct{}

func (markExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(util.Prioritized(markParser{}, 500)))
}
