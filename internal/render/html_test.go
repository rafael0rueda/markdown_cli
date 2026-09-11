package render

import (
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// plainText renders with the plain theme, the one output without color gets.
func plainText(t *testing.T, src string, width int) string {
	t.Helper()
	doc, err := Render([]byte(src), Options{Width: width, Theme: theme.Plain(), LinkMode: LinkInline})
	if err != nil {
		t.Fatal(err)
	}
	return doc.Text()
}

func TestInlineHTML(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"kbd", "Press <kbd>Ctrl</kbd>+<kbd>C</kbd>.", "Press [Ctrl]+[C]."},
		{"sub", "H<sub>2</sub>O", "H₂O"},
		{"sup", "r<sup>2</sup> and 1<sup>st</sup>", "r² and 1ˢᵗ"},
		{"sup fallback", "note<sup>[1]</sup>", "note^[1]"},
		{"sup fallback, words", "x<sup>see note</sup>", "x^(see note)"},
		{"sub fallback", "log<sub>Q</sub>", "log_Q"},
		{"bold and em", "<b>bold *em*</b> <i>it</i>", "bold em it"},
		{"span", `<span style="color:red">red</span>`, "red"},
		{"comment", "a <!-- hidden --> b", "a b"},
		{"link", `<a href="https://x.org">site</a>`, "site (https://x.org)"},
		{"link without href", `<a name="top">here</a>`, "here"},
		// Mid-sentence: an <img> alone on a line is an HTML block instead.
		{"image", `See <img src="https://x.org/l.png" alt="Logo" width="20">`, "See 🖼 Logo (https://x.org/l.png)"},
		{"image without alt", `See <img src="https://x.org/l.png">`, "See 🖼 image (https://x.org/l.png)"},
		{"entity in attribute", `See <img alt="a &amp; b" src="https://x.org/i">`, "See 🖼 a & b (https://x.org/i)"},
		{"br", "one<br>two<br/>three", "one\ntwo\nthree"},
		{"nested same tag", "<b>a <b>b</b> c</b>", "a b c"},
		{"case", "<KBD>Esc</KBD>", "[Esc]"},
	}
	for _, tt := range tests {
		if got := plainText(t, tt.src+"\n", 60); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestUnknownHTMLIsShown: tags with no terminal meaning, and tags never
// closed, are left as they are written rather than dropped.
func TestUnknownHTMLIsShown(t *testing.T) {
	for _, tt := range []struct{ src, want string }{
		{"<blink>tag</blink>", "<blink>tag</blink>"},
		{"unclosed <b>bold", "unclosed <b>bold"},
		{"stray </b> close", "stray </b> close"},
	} {
		if got := plainText(t, tt.src+"\n", 60); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestHTMLStyles(t *testing.T) {
	th := theme.Dark()
	doc, err := Render([]byte("<kbd>K</kbd> <b>B</b> <mark>M</mark>\n"), Options{Width: 60, Theme: th})
	if err != nil {
		t.Fatal(err)
	}
	styles := map[string]theme.Style{}
	for _, r := range doc.Lines[0].Runs {
		styles[strings.TrimSpace(strings.ReplaceAll(r.Text, nbsp, " "))] = r.Style
	}
	if s := styles["K"]; s != th.Kbd {
		t.Errorf("kbd style = %+v, want %+v", s, th.Kbd)
	}
	if !styles["B"].Bold {
		t.Error("<b> not bold")
	}
	if styles["M"] != th.Mark {
		t.Error("<mark> not marked")
	}
}

// TestKeyStaysWhole: a key with a space in it is not broken across lines,
// and keeps its padding next to the spaces around it.
func TestKeyStaysWhole(t *testing.T) {
	src := "Then press <kbd>Page Down</kbd> twice.\n"
	for width := 12; width <= 40; width++ {
		doc, err := Render([]byte(src), Options{Width: width, Theme: theme.Dark()})
		if err != nil {
			t.Fatal(err)
		}
		text := doc.Text()
		if !strings.Contains(text, nbsp+"Page"+nbsp+"Down"+nbsp) {
			t.Errorf("width %d: key broken or unpadded: %q", width, text)
		}
		if strings.Contains(text, "press"+nbsp) {
			t.Errorf("width %d: space before the key lost: %q", width, text)
		}
	}
}

func TestBreakInTableCell(t *testing.T) {
	src := "| Key | Action |\n|---|---|\n| j<br>↓ | Down one line<br>(or the wheel) |\n"
	rows := tableRows(plainText(t, src, 60))
	want := []string{
		"│ Key │ Action         │",
		"│ j   │ Down one line  │",
		"│ ↓   │ (or the wheel) │",
	}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(rows, "\n"), strings.Join(want, "\n"))
	}
}

func TestHTMLCommentBlockIsHidden(t *testing.T) {
	got := plainText(t, "Before.\n\n<!-- a note\nfor editors -->\n\nAfter.\n", 60)
	if got != "Before.\n\nAfter." {
		t.Errorf("got %q", got)
	}
}

func TestScriptTablesAreNarrow(t *testing.T) {
	// Every replacement has to take one cell, or the text around it shifts.
	for _, table := range []map[rune]rune{superscripts, subscripts} {
		for from, to := range table {
			if w := uniWidth(string(to)); w != 1 {
				t.Errorf("%q -> %q is %d cells wide", from, to, w)
			}
		}
	}
}
