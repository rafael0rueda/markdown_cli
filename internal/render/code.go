package render

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/rivo/uniseg"
	"github.com/yuin/goldmark/text"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// codeIndent is the left padding inside a code block's background.
const codeIndent = 2

// codeBlock renders a fenced or indented code block: a padded rectangle of
// background color with syntax-highlighted content inside it.
func (r *renderer) codeBlock(segments *text.Segments, lang string) {
	var b strings.Builder
	for i := 0; i < segments.Len(); i++ {
		seg := segments.At(i)
		b.Write(seg.Value(r.src))
	}
	source := strings.TrimRight(b.String(), "\n")

	bg := r.th.CodeBlock.BG
	width := r.contentWidth()
	inner := width - codeIndent*2
	if inner < 1 {
		inner = 1
	}
	pad := Run{Text: strings.Repeat(" ", codeIndent), Style: theme.Style{BG: bg}}

	// Top padding row, carrying the language tag at the right edge.
	top := []Run{pad}
	if lang != "" {
		tag := lang
		if uniWidth(tag) > inner {
			tag = tag[:inner]
		}
		gap := width - codeIndent - uniWidth(tag)
		if gap > codeIndent {
			top = append(top,
				Run{Text: strings.Repeat(" ", gap-codeIndent), Style: theme.Style{BG: bg}},
				Run{Text: tag, Style: r.th.CodeLang.WithBG(bg)},
			)
		}
	}
	r.emitRaw(Line{Runs: top, Fill: bg})

	for _, srcLine := range strings.Split(source, "\n") {
		runs := r.highlight(srcLine, lang, bg)
		// Long lines are wrapped rather than truncated: losing code off the
		// right edge is worse than a ragged wrap.
		for {
			head, tail := splitRuns(runs, inner)
			line := append([]Run{pad}, head...)
			r.emitRaw(Line{Runs: line, Fill: bg})
			if len(tail) == 0 {
				break
			}
			runs = tail
		}
	}

	r.emitRaw(Line{Runs: []Run{pad}, Fill: bg})
}

// highlight tokenizes one line of code and maps chroma's tokens onto runs.
// Highlighting is best-effort: an unknown language or a tokenizer error simply
// yields unstyled text on the block background.
func (r *renderer) highlight(line, lang string, bg theme.Color) []Run {
	line = strings.ReplaceAll(line, "\t", "    ")
	plain := []Run{{Text: line, Style: theme.Style{BG: bg}}}

	if r.th.ChromaStyle == "" || strings.TrimSpace(line) == "" {
		return plain
	}
	lexer := lexers.Get(lang)
	if lexer == nil {
		return plain
	}
	style := styles.Get(r.th.ChromaStyle)
	if style == nil {
		return plain
	}
	// Coalesce merges adjacent same-type tokens, which keeps the run count
	// and therefore the escape-sequence count down.
	iter, err := chroma.Coalesce(lexer).Tokenise(nil, line)
	if err != nil {
		return plain
	}

	var out []Run
	for tok := iter(); tok != chroma.EOF; tok = iter() {
		text := strings.TrimRight(tok.Value, "\n")
		if text == "" {
			continue
		}
		out = appendRun(out, Run{Text: text, Style: chromaStyle(style, tok.Type, bg)})
	}
	if len(out) == 0 {
		return plain
	}
	return out
}

// chromaStyle converts a chroma style entry into one of ours. The token's own
// background is discarded so the block keeps a single flat backdrop.
func chromaStyle(style *chroma.Style, tt chroma.TokenType, bg theme.Color) theme.Style {
	entry := style.Get(tt)
	s := theme.Style{BG: bg}
	if entry.Colour.IsSet() {
		s.FG = theme.RGB(entry.Colour.Red(), entry.Colour.Green(), entry.Colour.Blue())
	}
	s.Bold = entry.Bold == chroma.Yes
	s.Italic = entry.Italic == chroma.Yes
	s.Underline = entry.Underline == chroma.Yes
	return s
}

// splitRuns cuts a run slice at the given cell width, returning the part that
// fits and the remainder. A nil tail means everything fit.
//
// The split always consumes at least one grapheme cluster. Without that, a
// double-width character in a column only one cell wide would be carried to
// the next line forever, and callers loop until the tail is empty.
func splitRuns(runs []Run, width int) (head, tail []Run) {
	used := 0
	for i, r := range runs {
		w := r.Width()
		if used+w <= width {
			head = append(head, r)
			used += w
			continue
		}
		h, t, hw := splitToWidth(r.Text, width-used)
		if h == "" && len(head) == 0 {
			h, t, hw = firstCluster(r.Text)
		}
		if h != "" {
			head = append(head, Run{Text: h, Style: r.Style, Link: r.Link})
			used += hw
		}
		if t != "" {
			tail = append(tail, Run{Text: t, Style: r.Style, Link: r.Link})
		}
		tail = append(tail, runs[i+1:]...)
		return head, tail
	}
	return head, nil
}

// firstCluster peels one grapheme cluster off the front of s.
func firstCluster(s string) (head, tail string, width int) {
	g := uniseg.NewGraphemes(s)
	if !g.Next() {
		return "", "", 0
	}
	_, end := g.Positions()
	return s[:end], s[end:], uniseg.StringWidth(s[:end])
}
