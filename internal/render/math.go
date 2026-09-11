package render

import (
	"bytes"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Math is written in TeX between dollar signs: $E = mc^2$ inline, and
//
//	$$
//	\sum_{i=1}^n i = \frac{n(n+1)}{2}
//	$$
//
// as a display of its own. A terminal cannot typeset it, but a good deal of
// everyday math has a faithful Unicode spelling - Greek letters, operators,
// arrows, small raised and lowered digits - and that is what it is shown as,
// in its own style. What has no such spelling is left as TeX, which the
// people who write it can read.
//
// A dollar sign is also money, so the rules pandoc and GitHub use decide
// which are math: the opening $ has to be followed by something other than a
// space, and the closing one has to follow something other than a space and
// not be followed by a digit. "$5 and $10" is not math.

// Math is an inline $...$ or $$...$$ expression.
type Math struct {
	ast.BaseInline
	Source string
}

// KindMath is the node kind of inline math.
var KindMath = ast.NewNodeKind("Math")

// Kind implements ast.Node.
func (n *Math) Kind() ast.NodeKind { return KindMath }

// Dump implements ast.Node.
func (n *Math) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

// MathBlock is a $$ display taking whole lines.
type MathBlock struct {
	ast.BaseBlock
	Source string
	closed bool
}

// KindMathBlock is the node kind of display math.
var KindMathBlock = ast.NewNodeKind("MathBlock")

// Kind implements ast.Node.
func (n *MathBlock) Kind() ast.NodeKind { return KindMathBlock }

// Dump implements ast.Node.
func (n *MathBlock) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

var displayDelim = []byte("$$")

type mathParser struct{}

func (mathParser) Trigger() []byte { return []byte{'$'} }

func (mathParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, _ := block.PeekLine()
	display := bytes.HasPrefix(line, displayDelim)
	delim := 1
	if display {
		delim = 2
	} else if len(line) < 2 || isSpaceByte(line[1]) {
		return nil
	}

	startLine, startPos := block.Position()
	block.Advance(delim)
	var source strings.Builder
	for {
		l, _ := block.PeekLine()
		if l == nil {
			// Never closed within the paragraph: a dollar sign, not math.
			block.SetPosition(startLine, startPos)
			return nil
		}
		if i := mathClose(l, display, source.Len() == 0); i >= 0 {
			source.Write(l[:i])
			block.Advance(i + delim)
			if strings.TrimSpace(source.String()) == "" {
				block.SetPosition(startLine, startPos)
				return nil
			}
			return &Math{Source: source.String()}
		}
		source.Write(l)
		block.AdvanceLine()
	}
}

// mathClose finds where math ends on a line, or -1. atStart reports whether
// nothing of the expression precedes the line.
func mathClose(line []byte, display, atStart bool) int {
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++ // \$ is a dollar sign inside math, not the end of it
		case '$':
			if display {
				if i+1 < len(line) && line[i+1] == '$' {
					return i
				}
				continue
			}
			if (i == 0 && atStart) || (i > 0 && isSpaceByte(line[i-1])) {
				continue
			}
			if i+1 < len(line) && line[i+1] >= '0' && line[i+1] <= '9' {
				continue
			}
			return i
		}
	}
	return -1
}

func isSpaceByte(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

// mathBlockParser handles $$ displays that take lines of their own.
type mathBlockParser struct{}

func (mathBlockParser) Trigger() []byte { return []byte{'$'} }

func (mathBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, seg := reader.PeekLine()
	indent, _ := util.IndentWidth(line, reader.LineOffset())
	if indent > 3 {
		return nil, parser.NoChildren
	}
	rest := bytes.TrimSpace(line)
	if !bytes.HasPrefix(rest, displayDelim) {
		return nil, parser.NoChildren
	}
	after := rest[len(displayDelim):]
	if i := bytes.Index(after, displayDelim); i >= 0 {
		// $$ ... $$ on one line. With text after it, the line is a
		// paragraph and the inline parser takes it.
		if len(bytes.TrimSpace(after[i+len(displayDelim):])) > 0 {
			return nil, parser.NoChildren
		}
		reader.Advance(seg.Len() - 1)
		return &MathBlock{Source: string(after[:i]), closed: true}, parser.NoChildren
	}
	// Only a display that is closed later is one: otherwise a stray $$ would
	// swallow the rest of the document.
	if !closesLaterWith(reader, displayDelim) {
		return nil, parser.NoChildren
	}
	reader.Advance(seg.Len() - 1)
	return &MathBlock{Source: string(after)}, parser.NoChildren
}

func (mathBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	m := node.(*MathBlock)
	if m.closed {
		return parser.Close
	}
	line, seg := reader.PeekLine()
	if line == nil {
		return parser.Close
	}
	reader.Advance(seg.Len() - 1)
	if i := bytes.Index(line, displayDelim); i >= 0 {
		m.Source += "\n" + string(line[:i])
		return parser.Close
	}
	m.Source += "\n" + strings.TrimRight(string(line), "\r\n")
	return parser.Continue | parser.NoChildren
}

func (mathBlockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {}

// Like a comment block, a display has to start after a blank line or
// another block; letting it interrupt a paragraph would misread the last
// line of $$ math written inside one.
func (mathBlockParser) CanInterruptParagraph() bool { return false }

func (mathBlockParser) CanAcceptIndentedLine() bool { return false }

// closesLaterWith reports whether delim appears on a line after the current
// one, leaving the reader where it was.
func closesLaterWith(reader text.Reader, delim []byte) bool {
	line, pos := reader.Position()
	defer reader.SetPosition(line, pos)
	reader.AdvanceLine()
	for {
		l, _ := reader.PeekLine()
		if l == nil {
			return false
		}
		if bytes.Contains(l, delim) {
			return true
		}
		reader.AdvanceLine()
	}
}

type mathExtension struct{}

func (mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(util.Prioritized(mathParser{}, 150)),
		parser.WithBlockParsers(util.Prioritized(mathBlockParser{}, 150)),
	)
}

// mathBlock draws a display: each line converted, and centered where it fits.
func (r *renderer) mathBlock(n *MathBlock) {
	width := r.contentWidth()
	for _, line := range strings.Split(texToUnicode(n.Source, true), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runs := []Run{{Text: line, Style: r.base.Merge(r.th.Math)}}
		if w := uniWidth(line); w < width {
			runs = append([]Run{{Text: strings.Repeat(" ", (width-w)/2)}}, runs...)
			r.emitRaw(Line{Runs: runs})
			continue
		}
		r.emit(runs)
	}
}

// texToUnicode spells TeX math in Unicode where it can. In a display, \\
// starts a new line; inline, it is a space.
func texToUnicode(src string, display bool) string {
	t := &texConv{src: []rune(src), display: display}
	out := t.sequence(false)
	// TeX ignores spacing in math and people space it as they please; runs
	// of it are collapsed, leaving the author's single spaces.
	var b strings.Builder
	space := false
	for _, r := range out {
		if r == ' ' {
			space = true
			continue
		}
		if space && b.Len() > 0 && r != '\n' {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	return b.String()
}

type texConv struct {
	src     []rune
	pos     int
	display bool
}

// sequence converts until the end of input or, inside a group, its closing
// brace.
func (t *texConv) sequence(inGroup bool) string {
	var b strings.Builder
	for t.pos < len(t.src) {
		c := t.src[t.pos]
		switch c {
		case '}':
			if inGroup {
				t.pos++
				return b.String()
			}
			t.pos++
			b.WriteRune(c)
		case '{':
			t.pos++
			b.WriteString(t.sequence(true))
		case '^', '_':
			t.pos++
			arg := t.argument()
			b.WriteString(script(arg, c == '^'))
		case '\\':
			b.WriteString(t.command())
		case '&':
			// Alignment points in an aligned display.
			t.pos++
		case '~':
			t.pos++
			b.WriteByte(' ')
		case '\n', '\t':
			t.pos++
			b.WriteByte(' ')
		default:
			t.pos++
			b.WriteRune(c)
		}
	}
	return b.String()
}

// argument reads what a command or ^ applies to: a braced group, a command,
// or a single character.
func (t *texConv) argument() string {
	for t.pos < len(t.src) && t.src[t.pos] == ' ' {
		t.pos++
	}
	if t.pos >= len(t.src) {
		return ""
	}
	switch c := t.src[t.pos]; c {
	case '{':
		t.pos++
		return t.sequence(true)
	case '\\':
		return t.command()
	default:
		t.pos++
		return string(c)
	}
}

// optional reads a [bracketed] argument if one comes next.
func (t *texConv) optional() string {
	if t.pos >= len(t.src) || t.src[t.pos] != '[' {
		return ""
	}
	end := t.pos + 1
	for end < len(t.src) && t.src[end] != ']' {
		end++
	}
	if end >= len(t.src) {
		return ""
	}
	inner := texToUnicode(string(t.src[t.pos+1:end]), false)
	t.pos = end + 1
	return inner
}

// command converts a backslash command, leaving any it does not know as
// written.
func (t *texConv) command() string {
	t.pos++ // the backslash
	if t.pos >= len(t.src) {
		return "\\"
	}
	start := t.pos
	if !unicode.IsLetter(t.src[t.pos]) {
		// A one-character command: an escaped symbol or a spacing command.
		c := t.src[t.pos]
		t.pos++
		switch c {
		case '\\':
			if t.display {
				return "\n"
			}
			return " "
		case ',', ':', ';', ' ':
			return " "
		case '!':
			return ""
		case '{', '}', '$', '%', '#', '&', '_', '|':
			if c == '|' {
				return "‖"
			}
			return string(c)
		}
		return "\\" + string(c)
	}
	for t.pos < len(t.src) && unicode.IsLetter(t.src[t.pos]) {
		t.pos++
	}
	name := string(t.src[start:t.pos])

	if sym, ok := texSymbols[name]; ok {
		return sym
	}
	if texFunctions[name] {
		return name
	}
	switch name {
	case "frac", "dfrac", "tfrac":
		num, den := t.argument(), t.argument()
		return grouped(num) + "/" + grouped(den)
	case "sqrt":
		index := t.optional()
		root := "√"
		if index != "" {
			if s, ok := superscript(index); ok {
				root = s + root
			}
		}
		return root + grouped(t.argument())
	case "text", "textrm", "textit", "textbf", "mathrm", "mathit", "mathbf",
		"mathsf", "mathtt", "operatorname", "boldsymbol", "mathcal", "mathfrak", "mbox":
		return t.argument()
	case "mathbb":
		arg := t.argument()
		if bb, ok := blackboard[arg]; ok {
			return bb
		}
		return arg
	case "hat", "widehat", "bar", "overline", "vec", "dot", "ddot", "tilde", "widetilde":
		return accent(t.argument(), name)
	case "left", "right", "big", "Big", "bigg", "Bigg", "bigl", "bigr", "Bigl", "Bigr",
		"displaystyle", "textstyle", "limits", "nolimits":
		// Sizing that a terminal cannot do; the delimiter that follows stays.
		return ""
	case "quad":
		return "  "
	case "qquad":
		return "    "
	case "begin", "end":
		// Environment markers: the environment's content is shown, its
		// name is not.
		t.argument()
		return ""
	}
	// Unknown: left as written, braces and all, so it reads as the TeX it
	// is rather than running into its argument.
	out := "\\" + name
	for t.pos < len(t.src) && t.src[t.pos] == '{' {
		t.pos++
		out += "{" + t.sequence(true) + "}"
	}
	return out
}

// grouped parenthesizes an expression used as one operand when it has an
// operator or space outside any brackets, so a/b, n(n+1)/2 and (a+b)/2 each
// read the way the fraction meant.
func grouped(s string) string {
	s = strings.TrimSpace(s)
	depth := 0
	for _, r := range s {
		switch {
		case strings.ContainsRune("([{⟨", r):
			depth++
		case strings.ContainsRune(")]}⟩", r):
			depth--
		case depth == 0 && (r == ' ' || strings.ContainsRune("+-−±∓=<>≤≥≠≈·×÷/,", r)):
			return "(" + s + ")"
		}
	}
	return s
}

// script raises or lowers an operand, falling back to TeX's own ^ and _
// where Unicode has no small form for it.
func script(s string, up bool) string {
	s = strings.TrimSpace(s)
	// x^\circ is a degree sign and x^\prime a prime, not a raised ring or
	// a raised prime.
	if up && (s == "∘" || s == "′") {
		if s == "∘" {
			return "°"
		}
		return s
	}
	convert, mark := subscript, "_"
	if up {
		convert, mark = superscript, "^"
	}
	if small, ok := convert(s); ok {
		return small
	}
	if len([]rune(s)) == 1 {
		return mark + s
	}
	return mark + "(" + s + ")"
}

// accent puts a combining mark over a single character, or leaves a longer
// operand as it is.
func accent(s, name string) string {
	marks := map[string]string{
		"hat": "̂", "widehat": "̂", "bar": "̄", "overline": "̄",
		"vec": "⃗", "dot": "̇", "ddot": "̈", "tilde": "̃", "widetilde": "̃",
	}
	if len([]rune(s)) != 1 {
		return s
	}
	return s + marks[name]
}

var blackboard = map[string]string{
	"N": "ℕ", "Z": "ℤ", "Q": "ℚ", "R": "ℝ", "C": "ℂ", "P": "ℙ", "H": "ℍ",
}

// texFunctions are the operator names TeX sets upright; they are shown as
// the plain words they are.
var texFunctions = map[string]bool{
	"sin": true, "cos": true, "tan": true, "sec": true, "csc": true, "cot": true,
	"arcsin": true, "arccos": true, "arctan": true, "sinh": true, "cosh": true, "tanh": true,
	"log": true, "ln": true, "lg": true, "exp": true, "lim": true, "liminf": true, "limsup": true,
	"max": true, "min": true, "sup": true, "inf": true, "det": true, "dim": true,
	"gcd": true, "deg": true, "arg": true, "ker": true, "hom": true, "Pr": true, "mod": true,
}

var texSymbols = map[string]string{
	// Greek
	"alpha": "α", "beta": "β", "gamma": "γ", "delta": "δ", "epsilon": "ε", "varepsilon": "ε",
	"zeta": "ζ", "eta": "η", "theta": "θ", "vartheta": "ϑ", "iota": "ι", "kappa": "κ",
	"lambda": "λ", "mu": "μ", "nu": "ν", "xi": "ξ", "pi": "π", "varpi": "ϖ", "rho": "ρ",
	"varrho": "ϱ", "sigma": "σ", "varsigma": "ς", "tau": "τ", "upsilon": "υ", "phi": "φ",
	"varphi": "φ", "chi": "χ", "psi": "ψ", "omega": "ω",
	"Gamma": "Γ", "Delta": "Δ", "Theta": "Θ", "Lambda": "Λ", "Xi": "Ξ", "Pi": "Π",
	"Sigma": "Σ", "Upsilon": "Υ", "Phi": "Φ", "Psi": "Ψ", "Omega": "Ω",
	// Operators
	"times": "×", "div": "÷", "cdot": "·", "pm": "±", "mp": "∓", "ast": "∗", "star": "⋆",
	"circ": "∘", "bullet": "•", "oplus": "⊕", "otimes": "⊗", "cap": "∩", "cup": "∪",
	"setminus": "∖", "land": "∧", "wedge": "∧", "lor": "∨", "vee": "∨", "neg": "¬", "lnot": "¬",
	// Relations
	"leq": "≤", "le": "≤", "geq": "≥", "ge": "≥", "neq": "≠", "ne": "≠", "approx": "≈",
	"equiv": "≡", "sim": "∼", "simeq": "≃", "cong": "≅", "propto": "∝", "ll": "≪", "gg": "≫",
	"subset": "⊂", "supset": "⊃", "subseteq": "⊆", "supseteq": "⊇", "in": "∈", "notin": "∉",
	"ni": "∋", "perp": "⊥", "parallel": "∥", "mid": "∣", "coloneqq": "≔",
	// Arrows
	"to": "→", "rightarrow": "→", "leftarrow": "←", "gets": "←", "leftrightarrow": "↔",
	"Rightarrow": "⇒", "Leftarrow": "⇐", "Leftrightarrow": "⇔", "implies": "⟹", "iff": "⟺",
	"mapsto": "↦", "uparrow": "↑", "downarrow": "↓", "longrightarrow": "⟶", "longleftarrow": "⟵",
	// Large operators
	"sum": "∑", "prod": "∏", "coprod": "∐", "int": "∫", "iint": "∬", "iiint": "∭",
	"oint": "∮", "bigcup": "⋃", "bigcap": "⋂",
	// Everything else
	"infty": "∞", "partial": "∂", "nabla": "∇", "forall": "∀", "exists": "∃", "nexists": "∄",
	"emptyset": "∅", "varnothing": "∅", "angle": "∠", "triangle": "△", "ldots": "…",
	"dots": "…", "cdots": "⋯", "vdots": "⋮", "ddots": "⋱", "prime": "′", "hbar": "ℏ",
	"ell": "ℓ", "Re": "ℜ", "Im": "ℑ", "aleph": "ℵ", "langle": "⟨", "rangle": "⟩",
	"lfloor": "⌊", "rfloor": "⌋", "lceil": "⌈", "rceil": "⌉", "vert": "|", "lvert": "|",
	"rvert": "|", "Vert": "‖", "therefore": "∴", "because": "∵", "top": "⊤", "bot": "⊥",
	"colon": ":", "lbrace": "{", "rbrace": "}",
}
