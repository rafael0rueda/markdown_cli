package render

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mdv/internal/theme"
)

// update rewrites the golden files instead of comparing against them:
//
//	go test ./internal/render -update
var update = flag.Bool("update", false, "rewrite golden files")

// TestGolden renders every fixture and compares the unstyled result against a
// checked-in golden file. Comparing plain text rather than escape sequences
// keeps the goldens readable and lets the palette change without churning
// them; escape output is covered separately in ansi_test.go.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/*.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no fixtures found in testdata")
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			source, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := Render(source, Options{
				Width:    78,
				Theme:    theme.Plain(),
				LinkMode: LinkInline,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := doc.Text() + "\n"

			golden := strings.TrimSuffix(file, ".md") + ".golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run: go test ./internal/render -update)", err)
			}
			if got != string(want) {
				t.Errorf("output differs from %s\n--- got ---\n%s\n--- want ---\n%s",
					golden, got, want)
			}
		})
	}
}

// TestNoLineExceedsWidth is the invariant that matters most for a terminal
// renderer: nothing may overflow the layout width, or every long line wraps
// twice and the output turns to noise.
func TestNoLineExceedsWidth(t *testing.T) {
	source, err := os.ReadFile("testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, width := range []int{20, 30, 40, 60, 78, 120, 200} {
		doc, err := Render(source, Options{Width: width, Theme: theme.Dark(), LinkMode: LinkInline})
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range doc.Lines {
			if w := line.Width(); w > width {
				t.Errorf("width=%d: line %d is %d cells: %q", width, i+1, w, line.Text())
			}
		}
	}
}

func TestRenderDefaults(t *testing.T) {
	doc, err := Render([]byte("# hi"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Width != defaultWidth {
		t.Errorf("Width = %d, want %d", doc.Width, defaultWidth)
	}
	if !strings.Contains(doc.Text(), "hi") {
		t.Errorf("heading text missing from %q", doc.Text())
	}
}

func TestRenderClampsTinyWidth(t *testing.T) {
	doc, err := Render([]byte("some prose here"), Options{Width: 3, Theme: theme.Plain()})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Width < 20 {
		t.Errorf("Width = %d, want it clamped up to at least 20", doc.Width)
	}
}

func TestLinkModes(t *testing.T) {
	source := []byte("[text](https://example.com)")
	tests := []struct {
		mode LinkMode
		want bool // is the URL present in the output?
	}{
		{LinkInline, true},
		{LinkHide, false},
	}
	for _, tt := range tests {
		doc, err := Render(source, Options{Width: 78, Theme: theme.Plain(), LinkMode: tt.mode})
		if err != nil {
			t.Fatal(err)
		}
		got := strings.Contains(doc.Text(), "https://example.com")
		if got != tt.want {
			t.Errorf("mode %v: URL present = %v, want %v (%q)", tt.mode, got, tt.want, doc.Text())
		}
		if !strings.Contains(doc.Text(), "text") {
			t.Errorf("mode %v: link text missing from %q", tt.mode, doc.Text())
		}
	}
}

func TestResolveRelativePaths(t *testing.T) {
	tests := []struct {
		dest string
		want string
	}{
		{"./cat.png", "/docs/cat.png"},
		{"img/cat.png", "/docs/img/cat.png"},
		{"../cat.png", "/cat.png"},
		{"/abs/cat.png", "/abs/cat.png"},
		{"https://example.com/x.png", "https://example.com/x.png"},
		{"mailto:a@b.c", "mailto:a@b.c"},
		{"#anchor", "#anchor"},
		{"other.md#section", "/docs/other.md#section"},
		{"", ""},
	}
	r := &renderer{opts: Options{BaseDir: "/docs"}}
	for _, tt := range tests {
		if got := r.resolve(tt.dest); got != tt.want {
			t.Errorf("resolve(%q) = %q, want %q", tt.dest, got, tt.want)
		}
	}
}

func TestResolveWithoutBaseDir(t *testing.T) {
	r := &renderer{opts: Options{}}
	if got := r.resolve("./cat.png"); got != "./cat.png" {
		t.Errorf("resolve = %q, want it left alone when BaseDir is empty", got)
	}
}

func TestParseLinkMode(t *testing.T) {
	for _, s := range []string{"", "auto", "inline", "hide", "off", "none", "INLINE"} {
		if _, err := ParseLinkMode(s); err != nil {
			t.Errorf("ParseLinkMode(%q) failed: %v", s, err)
		}
	}
	if _, err := ParseLinkMode("sideways"); err == nil {
		t.Error("ParseLinkMode(\"sideways\") should fail")
	}
}

// TestMalformedInput checks that documents which stress the parser render
// without panicking, whatever they produce.
func TestMalformedInput(t *testing.T) {
	inputs := []string{
		"",
		"\n\n\n",
		"| broken | table\n|---",
		"```\nunclosed fence",
		"> > > deeply nested quote",
		strings.Repeat("- item\n", 200),
		"# " + strings.Repeat("word ", 500),
		"[unclosed](",
		"![](  )",
		"\x00\x01\x02",
		strings.Repeat("*", 300),
	}
	for i, in := range inputs {
		if _, err := Render([]byte(in), Options{Width: 40, Theme: theme.Dark()}); err != nil {
			t.Errorf("input %d: %v", i, err)
		}
	}
}
