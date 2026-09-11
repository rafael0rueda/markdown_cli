package render

import (
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

func renderText(t *testing.T, src string, width int) string {
	t.Helper()
	doc, err := Render([]byte(src), Options{Width: width, Theme: theme.Dark()})
	if err != nil {
		t.Fatal(err)
	}
	return doc.Text()
}

func TestCalloutTitleAndBody(t *testing.T) {
	got := renderText(t, "> [!warning]- Disk *almost* full\n> Clear the cache.\n>\n> Then retry.\n", 60)
	// Doc.Text keeps the bar's trailing space on the blank line; the writer
	// trims it on output.
	want := "▌ ⚠ Disk almost full\n▌ Clear the cache.\n▌ \n▌ Then retry."
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestCalloutDefaultTitles(t *testing.T) {
	tests := []struct{ src, want string }{
		// GitHub alerts put the marker alone on its line.
		{"> [!TIP]\n> Body.\n", "▌ ✦ Tip\n▌ Body."},
		{"> [!faq]\n", "▌ ? Faq"},
		{"> [!note]\n>\n> Separate paragraph.\n", "▌ ✎ Note\n▌ Separate paragraph."},
	}
	for _, tt := range tests {
		if got := renderText(t, tt.src, 60); got != tt.want {
			t.Errorf("%q:\ngot\n%s\nwant\n%s", tt.src, got, tt.want)
		}
	}
}

func TestCalloutAliasesAndUnknownKinds(t *testing.T) {
	tests := []struct{ src, want string }{
		{"> [!summary] S\n", "▌ ≡ S"},
		{"> [!caution] C\n", "▌ ⚠ C"},
		{"> [!cite] Q\n", "▌ ❝ Q"},
		// Obsidian draws kinds it does not know as notes.
		{"> [!mystery]\n", "▌ ✎ Mystery"},
	}
	for _, tt := range tests {
		if got := renderText(t, tt.src, 60); got != tt.want {
			t.Errorf("%q: got %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestCalloutColors(t *testing.T) {
	th := theme.Dark()
	doc, err := Render([]byte("> [!danger] Title\n> body\n"), Options{Width: 40, Theme: th})
	if err != nil {
		t.Fatal(err)
	}
	want := th.Callouts["danger"]
	title := doc.Lines[0].Runs
	if title[0].Style != want {
		t.Errorf("bar style = %+v, want %+v", title[0].Style, want)
	}
	for _, r := range title[2:] {
		if r.Style.FG != want.FG || !r.Style.Bold {
			t.Errorf("title run %q = %+v, want bold in the kind's color", r.Text, r.Style)
		}
	}
	// The body is the author's own text, not a quotation, so it is not
	// tinted or italicized the way a quote is.
	for _, r := range doc.Lines[1].Runs[2:] {
		if r.Style.Italic || r.Style.FG == th.Quote.FG {
			t.Errorf("callout body styled as a quote: %+v", r.Style)
		}
	}
}

func TestNestedCallout(t *testing.T) {
	got := renderText(t, "> [!info] Outer\n> > [!bug] Inner\n> > inner body\n", 60)
	want := "▌ ⓘ Outer\n▌ ▌ ✱ Inner\n▌ ▌ inner body"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestNotACallout(t *testing.T) {
	for _, src := range []string{
		"> quoted [!note] mid-line\n",
		"> [!not a kind] spaces are not allowed\n",
		"```\n> [!note] inside code\n```\n",
	} {
		got := renderText(t, src, 60)
		if !strings.Contains(got, "[!") {
			t.Errorf("%q should render literally, got %q", src, got)
		}
	}
}

func TestCalloutWrapsUnderItsBar(t *testing.T) {
	src := "> [!note] " + strings.Repeat("title ", 12) + "\n> " + strings.Repeat("body ", 20) + "\n"
	for _, line := range strings.Split(renderText(t, src, 30), "\n") {
		if !strings.HasPrefix(line, "▌") {
			t.Errorf("line escaped the callout's bar: %q", line)
		}
		if w := uniseg.StringWidth(line); w > 30 {
			t.Errorf("line of width %d exceeds 30: %q", w, line)
		}
	}
}

func TestCalloutASCII(t *testing.T) {
	th := theme.Dark()
	th.Glyphs = theme.ASCIIGlyphs
	doc, err := Render([]byte("> [!warning] Careful\n"), Options{Width: 40, Theme: th})
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Text(); got != "| ! Careful" {
		t.Errorf("got %q", got)
	}
}

// TestEveryCalloutKindIsStyled keeps the kind list and the themes in step: a
// kind added to one without the other would render with no color or icon.
func TestEveryCalloutKindIsStyled(t *testing.T) {
	kinds := map[string]bool{}
	for _, kind := range calloutAliases {
		kinds[kind] = true
	}
	for kind := range kinds {
		for _, th := range []*theme.Theme{theme.Dark(), theme.Light()} {
			if !th.Callouts[kind].FG.IsSet() {
				t.Errorf("%s theme has no color for callout kind %q", th.Name, kind)
			}
		}
		if theme.UnicodeGlyphs.CalloutIcons[kind] == "" || theme.ASCIIGlyphs.CalloutIcons[kind] == "" {
			t.Errorf("callout kind %q is missing an icon", kind)
		}
		// A glyph the terminal draws two cells wide would push the title out
		// of line with the body; emoji are the usual culprits.
		if w := uniseg.StringWidth(theme.UnicodeGlyphs.CalloutIcons[kind]); w != 1 {
			t.Errorf("icon for %q is %d cells wide, want 1", kind, w)
		}
	}
}
