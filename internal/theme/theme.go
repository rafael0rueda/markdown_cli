package theme

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Glyphs are the box-drawing and marker characters used to draw structure.
// Keeping them in the theme lets an ASCII-only variant swap them wholesale for
// terminals or fonts that render the Unicode set badly.
type Glyphs struct {
	QuoteBar     string
	Bullets      []string // cycled by nesting depth
	TaskDone     string
	TaskTodo     string
	Rule         string
	Ellipsis     string
	LinkIcon     string
	ImageIcon    string
	TableH       string
	TableV       string
	TableTL      string
	TableTR      string
	TableBL      string
	TableBR      string
	TableLeftT   string
	TableRightT  string
	TableTopT    string
	TableBottomT string
	TableCross   string
}

// UnicodeGlyphs is the default glyph set.
var UnicodeGlyphs = Glyphs{
	QuoteBar:     "▌",
	Bullets:      []string{"•", "◦", "▸", "·"},
	TaskDone:     "☑",
	TaskTodo:     "☐",
	Rule:         "─",
	Ellipsis:     "…",
	LinkIcon:     "↗",
	ImageIcon:    "🖼",
	TableH:       "─",
	TableV:       "│",
	TableTL:      "┌",
	TableTR:      "┐",
	TableBL:      "└",
	TableBR:      "┘",
	TableLeftT:   "├",
	TableRightT:  "┤",
	TableTopT:    "┬",
	TableBottomT: "┴",
	TableCross:   "┼",
}

// ASCIIGlyphs is the fallback for terminals without dependable Unicode.
var ASCIIGlyphs = Glyphs{
	QuoteBar:     "|",
	Bullets:      []string{"*", "-", "+", "."},
	TaskDone:     "[x]",
	TaskTodo:     "[ ]",
	Rule:         "-",
	Ellipsis:     "...",
	LinkIcon:     "",
	ImageIcon:    "[img]",
	TableH:       "-",
	TableV:       "|",
	TableTL:      "+",
	TableTR:      "+",
	TableBL:      "+",
	TableBR:      "+",
	TableLeftT:   "+",
	TableRightT:  "+",
	TableTopT:    "+",
	TableBottomT: "+",
	TableCross:   "+",
}

// Theme is the complete set of styles the renderer draws with. Every field is
// a Style so that any element can be recolored without touching the renderer.
type Theme struct {
	Name string
	Dark bool

	// ChromaStyle names the chroma style used for fenced code blocks. It is
	// chosen to share a palette with the surrounding text styles.
	ChromaStyle string

	Glyphs Glyphs

	Text       Style
	Headings   [6]Style // index 0 is h1
	HeadingNum Style    // the "1.2" prefix when heading numbers are enabled

	Emphasis Style
	Strong   Style
	Strike   Style

	Code      Style // inline code span
	CodeBlock Style // fenced/indented block background
	CodeLang  Style // the language tag shown above a block
	CodeLine  Style // gutter line numbers

	Link    Style // the visible link text
	LinkURL Style // the URL, when shown separately

	Quote    Style
	QuoteBar Style

	ListMarker Style
	TaskDone   Style
	TaskTodo   Style

	Rule Style

	TableBorder Style
	TableHeader Style
	TableCell   Style

	ImageAlt Style
	HTML     Style
	Footnote Style

	// MetaKey and MetaValue style the compact header rendered from a
	// document's YAML frontmatter.
	MetaKey   Style
	MetaValue Style

	// Status is the pager's bar along the bottom of the screen, and
	// SearchMatch marks the text a search found.
	Status      Style
	SearchMatch Style
}

// Catppuccin Mocha, used for the dark theme.
var (
	mochaPink     = Hex("#f5c2e7")
	mochaMauve    = Hex("#cba6f7")
	mochaRed      = Hex("#f38ba8")
	mochaPeach    = Hex("#fab387")
	mochaGreen    = Hex("#a6e3a1")
	mochaTeal     = Hex("#94e2d5")
	mochaSky      = Hex("#89dceb")
	mochaSapphire = Hex("#74c7ec")
	mochaBlue     = Hex("#89b4fa")
	mochaLavender = Hex("#b4befe")
	mochaSubtext0 = Hex("#a6adc8")
	mochaOverlay1 = Hex("#7f849c")
	mochaOverlay0 = Hex("#6c7086")
	mochaSurface1 = Hex("#45475a")
	mochaSurface0 = Hex("#313244")
	mochaMantle   = Hex("#181825")
	mochaYellow   = Hex("#f9e2af")
)

// Catppuccin Latte, used for the light theme.
var (
	lattePink     = Hex("#ea76cb")
	latteMauve    = Hex("#8839ef")
	lattePeach    = Hex("#fe640b")
	latteGreen    = Hex("#40a02b")
	latteTeal     = Hex("#179299")
	latteSky      = Hex("#04a5e5")
	latteSapphire = Hex("#209fb5")
	latteBlue     = Hex("#1e66f5")
	latteLavender = Hex("#7287fd")
	latteSubtext0 = Hex("#6c6f85")
	latteOverlay1 = Hex("#8c8fa1")
	latteOverlay0 = Hex("#9ca0b0")
	latteSurface1 = Hex("#bcc0cc")
	latteSurface0 = Hex("#ccd0da")
	latteMantle   = Hex("#e6e9ef")
	latteYellow   = Hex("#df8e1d")
)

// Dark returns the default dark theme. Body text is deliberately left unstyled
// so it inherits the terminal's own foreground color.
func Dark() *Theme {
	return &Theme{
		Name:        "dark",
		Dark:        true,
		ChromaStyle: "catppuccin-mocha",
		Glyphs:      UnicodeGlyphs,

		Headings: [6]Style{
			{FG: mochaMauve, Bold: true},
			{FG: mochaBlue, Bold: true},
			{FG: mochaSapphire, Bold: true},
			{FG: mochaTeal, Bold: true},
			{FG: mochaGreen, Bold: true},
			{FG: mochaSubtext0, Bold: true},
		},
		HeadingNum: Style{FG: mochaOverlay1},

		Emphasis: Style{Italic: true},
		Strong:   Style{Bold: true},
		Strike:   Style{Strike: true, Faint: true},

		Code:      Style{FG: mochaPeach, BG: mochaSurface0},
		CodeBlock: Style{BG: mochaMantle},
		CodeLang:  Style{FG: mochaOverlay1, Italic: true},
		CodeLine:  Style{FG: mochaSurface1},

		Link:    Style{FG: mochaBlue, Underline: true},
		LinkURL: Style{FG: mochaOverlay1},

		Quote:    Style{FG: mochaSubtext0, Italic: true},
		QuoteBar: Style{FG: mochaMauve},

		ListMarker: Style{FG: mochaPeach},
		TaskDone:   Style{FG: mochaGreen},
		TaskTodo:   Style{FG: mochaOverlay1},

		Rule: Style{FG: mochaSurface1},

		TableBorder: Style{FG: mochaSurface1},
		TableHeader: Style{FG: mochaLavender, Bold: true},

		ImageAlt: Style{FG: mochaPink},
		HTML:     Style{FG: mochaOverlay0, Faint: true},
		Footnote: Style{FG: mochaSky},

		MetaKey:   Style{FG: mochaOverlay1},
		MetaValue: Style{FG: mochaSubtext0},

		Status:      Style{FG: mochaSubtext0, BG: mochaSurface0},
		SearchMatch: Style{FG: mochaMantle, BG: mochaYellow, Bold: true},
	}
}

// Light returns the default light theme.
func Light() *Theme {
	return &Theme{
		Name:        "light",
		Dark:        false,
		ChromaStyle: "catppuccin-latte",
		Glyphs:      UnicodeGlyphs,

		Headings: [6]Style{
			{FG: latteMauve, Bold: true},
			{FG: latteBlue, Bold: true},
			{FG: latteSapphire, Bold: true},
			{FG: latteTeal, Bold: true},
			{FG: latteGreen, Bold: true},
			{FG: latteSubtext0, Bold: true},
		},
		HeadingNum: Style{FG: latteOverlay1},

		Emphasis: Style{Italic: true},
		Strong:   Style{Bold: true},
		Strike:   Style{Strike: true, Faint: true},

		Code:      Style{FG: lattePeach, BG: latteSurface0},
		CodeBlock: Style{BG: latteMantle},
		CodeLang:  Style{FG: latteOverlay1, Italic: true},
		CodeLine:  Style{FG: latteSurface1},

		Link:    Style{FG: latteBlue, Underline: true},
		LinkURL: Style{FG: latteOverlay1},

		Quote:    Style{FG: latteSubtext0, Italic: true},
		QuoteBar: Style{FG: latteMauve},

		ListMarker: Style{FG: lattePeach},
		TaskDone:   Style{FG: latteGreen},
		TaskTodo:   Style{FG: latteOverlay1},

		Rule: Style{FG: latteSurface1},

		TableBorder: Style{FG: latteSurface1},
		TableHeader: Style{FG: latteLavender, Bold: true},

		ImageAlt: Style{FG: lattePink},
		HTML:     Style{FG: latteOverlay0, Faint: true},
		Footnote: Style{FG: latteSky},

		MetaKey:   Style{FG: latteOverlay1},
		MetaValue: Style{FG: latteSubtext0},

		Status:      Style{FG: latteSubtext0, BG: latteSurface0},
		SearchMatch: Style{FG: latteMantle, BG: latteYellow, Bold: true},
	}
}

// Plain returns a theme with no colors, keeping only structural attributes.
// It is what --color=none renders with, and it is also the baseline for
// golden-file tests.
func Plain() *Theme {
	t := &Theme{
		Name:        "plain",
		Dark:        true,
		ChromaStyle: "",
		Glyphs:      UnicodeGlyphs,
		Emphasis:    Style{Italic: true},
		Strong:      Style{Bold: true},
		Strike:      Style{Strike: true},
		Link:        Style{Underline: true},
		MetaKey:     Style{Faint: true},
		Status:      Style{Reverse: true},
		SearchMatch: Style{Reverse: true},
	}
	for i := range t.Headings {
		t.Headings[i] = Style{Bold: true}
	}
	return t
}

// builtin maps theme names to constructors.
var builtin = map[string]func() *Theme{
	"dark":  Dark,
	"light": Light,
	"plain": Plain,
}

// Names lists the built-in theme names, plus "auto", in a stable order.
func Names() []string {
	out := []string{"auto"}
	for name := range builtin {
		out = append(out, name)
	}
	sort.Strings(out[1:])
	return out
}

// Get resolves a theme by name. The name "auto" picks between dark and light
// using DetectDark.
func Get(name string) (*Theme, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || name == "auto" {
		if DetectDark() {
			return Dark(), nil
		}
		return Light(), nil
	}
	if fn, ok := builtin[name]; ok {
		return fn(), nil
	}
	return nil, fmt.Errorf("unknown theme %q (want one of %s)", name, strings.Join(Names(), ", "))
}

// DetectDark guesses whether the terminal has a dark background.
//
// This is the cheap environment-only check: COLORFGBG is set by a handful of
// terminals and carries "<fg>;<bg>" as palette indices. Everything else falls
// through to dark, which is both the common case and the safer failure mode -
// a dark theme on a light background stays readable, while the reverse washes
// out. Querying the terminal directly with OSC 11 is a terminal-capability
// concern and belongs with the rest of that probing.
func DetectDark() bool {
	v := os.Getenv("COLORFGBG")
	if v == "" {
		return true
	}
	parts := strings.Split(v, ";")
	bg, err := strconv.Atoi(strings.TrimSpace(parts[len(parts)-1]))
	if err != nil {
		return true
	}
	// Palette indices 0-6 and 8 are the dark half of the 16-color set.
	return bg <= 6 || bg == 8
}
