package render

import (
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// markedText renders src and returns the text of the runs drawn in the
// theme's mark style, joined with "|" between separate stretches.
func markedText(t *testing.T, src string, th *theme.Theme) (text, marked string) {
	t.Helper()
	doc, err := Render([]byte(src), Options{Width: 60, Theme: th})
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	inMark := false
	for _, line := range doc.Lines {
		for _, r := range line.Runs {
			isMark := r.Style.BG == th.Mark.BG && r.Style.Reverse == th.Mark.Reverse
			switch {
			case isMark && inMark:
				parts[len(parts)-1] += r.Text
			case isMark:
				parts = append(parts, r.Text)
			}
			inMark = isMark
		}
	}
	return doc.Text(), strings.Join(parts, "|")
}

func TestMark(t *testing.T) {
	tests := []struct{ src, text, marked string }{
		{"Some ==important== words.\n", "Some important words.", "important"},
		{"==one== and ==two==\n", "one and two", "one|two"},
		{"in==side==word\n", "insideword", "side"},
		{"==**bold** too==\n", "bold too", "bold too"},
		// Code keeps its own background, so it still reads as code.
		{"==a `code` b==\n", "a code b", "a | b"},
		{"~~==struck==~~\n", "struck", "struck"},
		{"# Title ==draft==\n", "# Title draft", "draft"},
	}
	for _, tt := range tests {
		text, marked := markedText(t, tt.src, theme.Dark())
		if text != tt.text || marked != tt.marked {
			t.Errorf("%q: got text %q marked %q, want %q marked %q",
				tt.src, text, marked, tt.text, tt.marked)
		}
	}
}

func TestNotMark(t *testing.T) {
	for _, src := range []string{
		"a == b\n",           // an equation: the delimiters do not hug text
		"x==y\n",             // never closed
		"==> an arrow\n",     // likewise
		"===three===\n",      // three is not two
		"=one=\n",            // one is not two
		"`==code==`\n",       // code is literal
		"== spaced out ==\n", // delimiters must touch the text
	} {
		text, marked := markedText(t, src, theme.Dark())
		if marked != "" {
			t.Errorf("%q: marked %q, want nothing", src, marked)
		}
		if !strings.Contains(text, "=") {
			t.Errorf("%q: the = signs were dropped: %q", src, text)
		}
	}
}

func TestMarkInEveryTheme(t *testing.T) {
	for _, th := range []*theme.Theme{theme.Dark(), theme.Light(), theme.Plain()} {
		if th.Mark.IsPlain() {
			t.Errorf("%s theme draws ==marks== like plain text", th.Name)
		}
		// A search match inside marked text has to stay visible.
		if th.Mark.Merge(th.SearchMatch) == th.Mark {
			t.Errorf("%s theme: a search match inside a mark looks like the mark", th.Name)
		}
	}
}

func TestMarkIsSearchable(t *testing.T) {
	doc, err := Render([]byte("Find ==this== here.\n"), Options{Width: 60})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Search("find this here")) != 1 {
		t.Error("marked text should read as ordinary text to search")
	}
}
