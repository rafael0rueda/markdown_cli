package render

import (
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

func TestDisplayPath(t *testing.T) {
	const work, home = "/home/ann/src/proj", "/home/ann"
	tests := []struct{ dest, want string }{
		{"/home/ann/src/proj/docs/a.md", "docs/a.md"},
		{"/home/ann/src/proj/a.md#intro", "a.md#intro"},
		{"/home/ann/src/other.md", "../other.md"},
		// Climbing out is longer here than naming it from home.
		{"/home/ann/notes/vault/Note.md", "~/notes/vault/Note.md"},
		// Neither is shorter than the path itself.
		{"/etc/hosts", "/etc/hosts"},
		{"https://example.com/a", "https://example.com/a"},
		{"#section", "#section"},
		{"relative/as/given.md", "relative/as/given.md"},
	}
	for _, tt := range tests {
		if got := displayPath(tt.dest, work, home); got != tt.want {
			t.Errorf("displayPath(%q) = %q, want %q", tt.dest, got, tt.want)
		}
	}
	// Without a working directory or home, nothing can be shortened.
	if got := displayPath("/home/ann/src/proj/a.md", "", ""); got != "/home/ann/src/proj/a.md" {
		t.Errorf("got %q", got)
	}
}

func TestHyperlinkTarget(t *testing.T) {
	host := hostname()
	tests := []struct{ link, want string }{
		{"/tmp/a b.md", "file://" + host + "/tmp/a%20b.md"},
		{"/tmp/a.md#intro", "file://" + host + "/tmp/a.md#intro"},
		{"https://example.com/x", "https://example.com/x"},
		{"#top", "#top"},
	}
	for _, tt := range tests {
		if got := hyperlinkTarget(tt.link); got != tt.want {
			t.Errorf("hyperlinkTarget(%q) = %q, want %q", tt.link, got, tt.want)
		}
	}
}

// TestLocalLinks follows a relative link through rendering and out: shown
// short, and handed to the terminal as a file:// URL it can open from
// anywhere.
func TestLocalLinks(t *testing.T) {
	doc, err := Render([]byte("See [the notes](notes/a.md).\n"), Options{
		Width:    80,
		Theme:    theme.Dark(),
		LinkMode: LinkInline,
		BaseDir:  "/work/proj",
		WorkDir:  "/work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := doc.Text(), "See the notes (proj/notes/a.md)."; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	var out strings.Builder
	if err := Write(&out, doc, WriteOptions{Color: theme.ColorTrue, Hyperlinks: true}); err != nil {
		t.Fatal(err)
	}
	want := "\x1b]8;;file://" + hostname() + "/work/proj/notes/a.md\x1b\\"
	if !strings.Contains(out.String(), want) {
		t.Errorf("hyperlink should open the absolute file, got %q", out.String())
	}
}
