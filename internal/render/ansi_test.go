package render

import (
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

func renderToString(t *testing.T, doc *Doc, opts WriteOptions) string {
	t.Helper()
	var b strings.Builder
	if err := Write(&b, doc, opts); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestWriteNoColorHasNoEscapes(t *testing.T) {
	doc := &Doc{Width: 20, Lines: []Line{{Runs: []Run{
		{Text: "loud", Style: theme.Style{Bold: true, FG: theme.RGB(255, 0, 0)}},
		{Text: " link", Link: "https://example.com"},
	}}}}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorNone, Hyperlinks: true})
	if strings.ContainsRune(got, 0x1b) {
		t.Errorf("ColorNone emitted an escape sequence: %q", got)
	}
	if got != "loud link\n" {
		t.Errorf("got %q, want %q", got, "loud link\n")
	}
}

func TestWriteResetsAtLineEnd(t *testing.T) {
	doc := &Doc{Width: 20, Lines: []Line{{Runs: []Run{
		{Text: "red", Style: theme.Style{FG: theme.RGB(255, 0, 0)}},
	}}}}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue})
	if !strings.HasSuffix(got, theme.Reset+"\n") {
		t.Errorf("line does not end with a reset: %q", got)
	}
}

// TestWriteCoalescesStyles checks that a repeated style is not re-emitted per
// run. Without this the escape output balloons on syntax-highlighted code.
func TestWriteCoalescesStyles(t *testing.T) {
	style := theme.Style{FG: theme.RGB(1, 2, 3)}
	doc := &Doc{Width: 20, Lines: []Line{{Runs: []Run{
		{Text: "a", Style: style},
		{Text: "b", Style: style},
		{Text: "c", Style: style},
	}}}}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue})
	if n := strings.Count(got, "38;2;1;2;3"); n != 1 {
		t.Errorf("style emitted %d times, want 1: %q", n, got)
	}
}

func TestWriteTrimsTrailingSpace(t *testing.T) {
	doc := &Doc{Width: 20, Lines: []Line{{Runs: []Run{{Text: "text    "}}}}}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorNone})
	if got != "text\n" {
		t.Errorf("got %q, want %q", got, "text\n")
	}
}

// TestWriteKeepsSpaceInsideBackground checks that padding which is visible as
// color is not mistaken for trailing whitespace.
func TestWriteKeepsSpaceInsideBackground(t *testing.T) {
	doc := &Doc{Width: 20, Lines: []Line{{Runs: []Run{
		{Text: "x   ", Style: theme.Style{BG: theme.RGB(10, 10, 10)}},
	}}}}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue})
	if !strings.Contains(got, "x   ") {
		t.Errorf("background padding was trimmed: %q", got)
	}
}

func TestWriteFillExtendsToWidth(t *testing.T) {
	bg := theme.RGB(20, 20, 30)
	doc := &Doc{Width: 10, Lines: []Line{{
		Runs: []Run{{Text: "ab", Style: theme.Style{BG: bg}}},
		Fill: bg,
	}}}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue})
	// Two content cells plus eight of fill.
	if !strings.Contains(got, strings.Repeat(" ", 8)) {
		t.Errorf("fill did not pad out to the document width: %q", got)
	}
}

func TestWriteFillSkippedWithoutColor(t *testing.T) {
	bg := theme.RGB(20, 20, 30)
	doc := &Doc{Width: 10, Lines: []Line{{
		Runs: []Run{{Text: "ab"}},
		Fill: bg,
	}}}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorNone})
	if got != "ab\n" {
		t.Errorf("got %q, want %q", got, "ab\n")
	}
}

func TestWriteHyperlinks(t *testing.T) {
	doc := &Doc{Width: 20, Lines: []Line{{Runs: []Run{
		{Text: "here", Link: "https://example.com"},
		{Text: " plain"},
	}}}}

	on := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue, Hyperlinks: true})
	if !strings.Contains(on, "\x1b]8;;https://example.com\x1b\\here") {
		t.Errorf("hyperlink not opened: %q", on)
	}
	if !strings.Contains(on, osc8End) {
		t.Errorf("hyperlink not closed: %q", on)
	}

	off := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue})
	if strings.Contains(off, "\x1b]8;") {
		t.Errorf("hyperlink emitted while disabled: %q", off)
	}
}

func TestWriteHyperlinkClosedAtLineEnd(t *testing.T) {
	doc := &Doc{Width: 20, Lines: []Line{{Runs: []Run{
		{Text: "here", Link: "https://example.com"},
	}}}}
	got := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue, Hyperlinks: true})
	if strings.Count(got, "\x1b]8;;") != 2 {
		t.Errorf("expected one open and one close: %q", got)
	}
}

func TestRenderLineMatchesWrite(t *testing.T) {
	line := Line{Runs: []Run{{Text: "hi", Style: theme.Style{Bold: true}}}}
	opts := WriteOptions{Color: theme.ColorTrue}
	doc := &Doc{Width: 10, Lines: []Line{line}}
	if got, want := RenderLine(line, 10, opts), strings.TrimSuffix(renderToString(t, doc, opts), "\n"); got != want {
		t.Errorf("RenderLine = %q, Write = %q", got, want)
	}
}

func TestDocText(t *testing.T) {
	doc := &Doc{Lines: []Line{
		{Runs: []Run{{Text: "one"}}},
		{Runs: []Run{{Text: "two"}, {Text: "three"}}},
	}}
	if got := doc.Text(); got != "one\ntwothree" {
		t.Errorf("got %q", got)
	}
}
