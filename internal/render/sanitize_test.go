package render

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

func TestSanitize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"tab\tand\nnewline", "tab\tand\nnewline"},
		{"esc\x1b]0;title\x07", "esc␛]0;title␇"},
		{"del\x7f", "del␡"},
		{"cr\r", "cr␍"},
		{"nul\x00", "nul�"},
		{"csi\u009b31m", "csi�31m"}, // C1 CSI, which some terminals obey
		{"bad\xffbyte", "bad�byte"},
		{"unicode é ✓ 👍", "unicode é ✓ 👍"},
	}
	for _, tt := range tests {
		if got := Sanitize(tt.in); got != tt.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSanitizeOutput(t *testing.T) {
	if got := sanitizeOutput("a\tb\nc\x1b"); got != "a b␊c␛" {
		t.Errorf("got %q", got)
	}
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://example.com/a?b=c#d", "https://example.com/a?b=c#d"},
		{"http://x/\x1b]52;c;aGk=\x07", "http://x/%1B]52;c;aGk=%07"},
		{"http://x/\u009b", "http://x/%C2%9B"},
		{"http://x/é", "http://x/é"},
	}
	for _, tt := range tests {
		if got := sanitizeURL(tt.in); got != tt.want {
			t.Errorf("sanitizeURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestUnescape(t *testing.T) {
	tests := []struct{ in, want string }{
		{`plain`, `plain`},
		{`\*not emphasis\*`, `*not emphasis*`},
		{`\\`, `\`},
		{`\a\1`, `\a\1`},    // only ASCII punctuation can be escaped
		{`\&amp;`, `&amp;`}, // an escaped & does not start a reference
		{`&amp; &lt; &gt; &quot;`, `& < > "`},
		{`&copy; &nbsp;`, "©  "},
		{`&#35; &#x22; &#X22;`, `# " "`},
		{`&#0;`, "�"},
		{`&#1234567;`, "�"},            // seven digits, but past U+10FFFF
		{`&#12345678;`, `&#12345678;`}, // eight digits is not a reference
		{`&#xD800;`, "�"},              // a surrogate
		{`&bogus; &amp &; &#; &#x;`, `&bogus; &amp &; &#; &#x;`},
		// References can name control characters; they must come out inert.
		{`&#27;]0;x&#7;`, `␛]0;x␇`},
		{`&#x9b;`, "�"},
		{`a&#10;b&#9;c`, `a b c`},
	}
	for _, tt := range tests {
		if got := unescape([]byte(tt.in)); got != tt.want {
			t.Errorf("unescape(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// ownSequences matches the escape sequences mdv itself writes: SGR styling
// and OSC 8 hyperlinks. The URL part excludes ESC and BEL, so a hyperlink
// whose URL smuggled in either one will not match and will be caught.
var ownSequences = regexp.MustCompile(`\x1b\[[0-9;]*m|\x1b\]8;;[^\x1b\x07]*\x1b\\`)

var osc8URL = regexp.MustCompile(`\x1b\]8;;([^\x1b\x07]*)\x1b\\`)

// assertInert fails if the output holds any control character other than a
// newline, outside the sequences mdv writes on purpose.
func assertInert(t *testing.T, label, out string) {
	t.Helper()
	for _, m := range osc8URL.FindAllStringSubmatch(out, -1) {
		if strings.ContainsFunc(m[1], isControl) {
			t.Errorf("%s: control character inside a hyperlink URL: %q", label, m[1])
		}
	}
	rest := ownSequences.ReplaceAllString(out, "")
	for i, r := range rest {
		if r != '\n' && isControl(r) {
			lo, hi := max(0, i-20), min(len(rest), i+20)
			t.Errorf("%s: control %U reached the output: %q", label, r, rest[lo:hi])
			return
		}
	}
}

// TestNoControlCharacterReachesTheTerminal puts every character a terminal
// would act on into every place a document can hold text, written both
// literally and as a character reference, and checks that none of them comes
// out as anything but inert text.
func TestNoControlCharacterReachesTheTerminal(t *testing.T) {
	const template = "---\ntitle: aXb\n---\n" +
		"# Head aXb\n\n" +
		"Para aXb *em aXb* `code aXb` [link aXb](http://h/aXb) ![alt aXb](imgXb.png) <span>aXb</span>\n\n" +
		"| h aXb | h |\n|---|---|\n| cell aXb | c |\n\n" +
		"> quote aXb\n\n" +
		"- item aXb\n- [[Note aXb]]\n\n" +
		"```go\ncode aXb\n```\n\n" +
		"    indented aXb\n\n" +
		"<div>\nblock aXb\n</div>\n\n" +
		"Foot[^1] <http://auto/aXb>\n\n[^1]: note aXb\n"

	var payloads []string
	for c := 0; c < 0x20; c++ {
		payloads = append(payloads, string(rune(c)), fmt.Sprintf("&#%d;", c), fmt.Sprintf("&#x%x;", c))
	}
	for c := 0x7f; c <= 0x9f; c++ {
		payloads = append(payloads, string(rune(c)), fmt.Sprintf("&#%d;", c))
	}
	payloads = append(payloads, "\x1b]52;c;aGk=\x07", "\x1b[2J", "\xff\xfe")

	for _, p := range payloads {
		src := strings.ReplaceAll(template, "X", p)
		doc, err := Render([]byte(src), Options{Width: 60, LinkMode: LinkInline})
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		out := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue, Hyperlinks: true})
		assertInert(t, fmt.Sprintf("payload %q", p), out)
	}
}

// TestWriteSanitizesTextFromOutsideTheDocument covers the output-side net: a
// run whose text never passed through Render - a file name, a path found on
// disk - is made safe too.
func TestWriteSanitizesTextFromOutsideTheDocument(t *testing.T) {
	doc := &Doc{Width: 40, Lines: []Line{{Runs: []Run{
		{Text: "name\x1b]0;pwned\x07\nmore", Link: "file:///tmp/\x1b]52;c;aGk=\x07"},
	}}}}
	out := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue, Hyperlinks: true})
	assertInert(t, "raw run", out)
	if !strings.Contains(out, "name␛]0;pwned␇␊more") {
		t.Errorf("controls should be shown, not dropped: %q", out)
	}
	if !strings.Contains(out, "file:///tmp/%1B]52;c;aGk=%07") {
		t.Errorf("URL controls should be percent-encoded: %q", out)
	}
}

func TestAdjacentLinksKeepTheirOwnURLs(t *testing.T) {
	doc, err := Render([]byte("[a](http://a.example)[b](http://b.example)\n"), Options{Width: 60, LinkMode: LinkHide})
	if err != nil {
		t.Fatal(err)
	}
	out := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue, Hyperlinks: true})
	b := strings.Index(out, "b")
	lastOpen := strings.LastIndex(out[:b], "\x1b]8;;")
	if lastOpen < 0 || !strings.HasPrefix(out[lastOpen:], "\x1b]8;;http://b.example\x1b\\") {
		t.Errorf("the second link's text is not under its own URL: %q", out)
	}
}

func TestEscapesAndEntitiesInContext(t *testing.T) {
	src := "# Issue \\#1 &amp; more\n\n" +
		"| a \\| b | c |\n|---|---|\n| d | e |\n\n" +
		"![alt &amp; more](p.png)\n\n" +
		"[x](a\\_b.md) [y](<c&amp;d.md>)\n\n" +
		"line\\\nnext\n\n" +
		"`raw \\* &amp;` and\n\n" +
		"```\nblock \\* &amp;\n```\n"
	doc, err := Render([]byte(src), Options{Width: 60, LinkMode: LinkInline})
	if err != nil {
		t.Fatal(err)
	}
	text := doc.Text()
	for _, want := range []string{
		"Issue #1 & more",
		"a | b",
		"alt & more",
		"(a_b.md)",
		"(c&d.md)",
		"raw \\* &amp;",   // code spans are raw
		"block \\* &amp;", // and so are code blocks
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	// A backslash at the end of a line is a hard break, not a character.
	if strings.Contains(text, "line\\") || !strings.Contains(text, "line\nnext") {
		t.Errorf("backslash hard break mishandled:\n%s", text)
	}
}

func TestLineEndingsAreNormalized(t *testing.T) {
	for name, src := range map[string]string{
		"CRLF":    "one\r\ntwo\r\n\r\n```\nx\r\ny\r\n```\n",
		"lone CR": "one\rtwo\r\r```\rx\ry\r```\r",
	} {
		doc, err := Render([]byte(src), Options{Width: 40})
		if err != nil {
			t.Fatal(err)
		}
		out := renderToString(t, doc, WriteOptions{Color: theme.ColorTrue})
		if strings.ContainsAny(out, "\r␍") {
			t.Errorf("%s: carriage return survived: %q", name, out)
		}
		text := doc.Text()
		if !strings.Contains(text, "one two") || !strings.Contains(text, "x") || !strings.Contains(text, "y") {
			t.Errorf("%s: content lost:\n%s", name, text)
		}
	}
}
