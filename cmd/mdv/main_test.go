package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"mdv/internal/render"
)

// runCLI invokes the command with a clean environment and captures its output.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	// Output goes to buffers rather than a terminal, so color detection lands
	// on "none" and the assertions can be made on plain text.
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")

	var out, errBuf bytes.Buffer
	err = run(args, &out, &errBuf)
	return out.String(), errBuf.String(), err
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunRendersFile(t *testing.T) {
	path := writeTemp(t, "doc.md", "# Title\n\nSome *text*.\n")
	out, _, err := runCLI(t, "--width", "40", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Title") || !strings.Contains(out, "Some text.") {
		t.Errorf("unexpected output: %q", out)
	}
}

// TestRunPlainWhenNotATerminal is the property that makes piping safe: writing
// to anything other than a terminal must produce no escape sequences.
func TestRunPlainWhenNotATerminal(t *testing.T) {
	path := writeTemp(t, "doc.md", "# Title\n\n`code` and **bold**\n")
	out, _, err := runCLI(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("escape sequences leaked into piped output: %q", out)
	}
}

func TestRunForcedColor(t *testing.T) {
	path := writeTemp(t, "doc.md", "# Title\n")
	out, _, err := runCLI(t, "--color", "truecolor", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.ContainsRune(out, 0x1b) {
		t.Errorf("forcing color produced no escapes: %q", out)
	}
}

func TestRunRespectsWidth(t *testing.T) {
	path := writeTemp(t, "doc.md", strings.Repeat("word ", 200))
	for _, width := range []int{30, 50, 90} {
		out, _, err := runCLI(t, "--width", strconv.Itoa(width), path)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(out, "\n") {
			if len(line) > width {
				t.Errorf("width=%d: line of %d chars: %q", width, len(line), line)
			}
		}
	}
}

func TestRunMultipleFilesGetHeaders(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	os.WriteFile(a, []byte("alpha\n"), 0o644)
	os.WriteFile(b, []byte("bravo\n"), 0o644)

	out, _, err := runCLI(t, "--width", "60", a, b)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a.md", "b.md", "alpha", "bravo"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestRunSingleFileHasNoHeader(t *testing.T) {
	path := writeTemp(t, "solo.md", "content\n")
	out, _, err := runCLI(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "solo.md") {
		t.Errorf("a lone file should not be labelled: %q", out)
	}
}

func TestRunMissingFile(t *testing.T) {
	_, _, err := runCLI(t, filepath.Join(t.TempDir(), "nope.md"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !strings.Contains(err.Error(), "nope.md") {
		t.Errorf("error should name the file: %v", err)
	}
}

func TestRunVersion(t *testing.T) {
	out, _, err := runCLI(t, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "mdv ") {
		t.Errorf("got %q", out)
	}
}

func TestRunRejectsBadFlags(t *testing.T) {
	tests := [][]string{
		{"--color", "chartreuse"},
		{"--theme", "solarized"},
		{"--links", "sideways"},
	}
	for _, args := range tests {
		if _, _, err := runCLI(t, args...); err == nil {
			t.Errorf("%v should have failed", args)
		}
	}
}

func TestRunStdin(t *testing.T) {
	old := os.Stdin
	defer func() { os.Stdin = old }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		io.WriteString(w, "# From stdin\n")
		w.Close()
	}()
	os.Stdin = r

	out, _, err := runCLI(t, "--width", "40")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "From stdin") {
		t.Errorf("got %q", out)
	}
}

func TestRunASCIIGlyphs(t *testing.T) {
	path := writeTemp(t, "doc.md", "- bullet\n")
	out, _, err := runCLI(t, "--ascii", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "•") {
		t.Errorf("--ascii still emitted Unicode glyphs: %q", out)
	}
	if !strings.Contains(out, "*") {
		t.Errorf("--ascii bullet missing: %q", out)
	}
}

func TestResolveLinkMode(t *testing.T) {
	tests := []struct {
		name       string
		hyperlinks bool
		want       render.LinkMode
	}{
		// On auto the decision follows the terminal: hide the URL when the
		// text itself is clickable, show it when it is not.
		{"auto", true, render.LinkHide},
		{"auto", false, render.LinkInline},
		{"", true, render.LinkHide},
		{"", false, render.LinkInline},
		// An explicit choice is honoured either way.
		{"inline", true, render.LinkInline},
		{"hide", false, render.LinkHide},
	}
	for _, tt := range tests {
		got, err := resolveLinkMode(tt.name, tt.hyperlinks)
		if err != nil {
			t.Fatalf("resolveLinkMode(%q, %v): %v", tt.name, tt.hyperlinks, err)
		}
		if got != tt.want {
			t.Errorf("resolveLinkMode(%q, hyperlinks=%v) = %v, want %v",
				tt.name, tt.hyperlinks, got, tt.want)
		}
	}
	if _, err := resolveLinkMode("sideways", false); err == nil {
		t.Error("an unknown link mode should be rejected")
	}
}

func TestRunCaps(t *testing.T) {
	out, _, err := runCLI(t, "--caps")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Terminal capabilities", "Kitty graphics", "Hyperlinks"} {
		if !strings.Contains(out, want) {
			t.Errorf("caps report missing %q:\n%s", want, out)
		}
	}
}

// TestRunCapsIsPlainWhenPiped checks that --caps obeys the same rule as
// document output: no escape sequences when the target is not a terminal, so
// it can be pasted into a bug report.
func TestRunCapsIsPlainWhenPiped(t *testing.T) {
	out, _, err := runCLI(t, "--caps")
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("caps report leaked escape sequences: %q", out)
	}
}

func TestRunCapsIgnoresFileArguments(t *testing.T) {
	path := writeTemp(t, "doc.md", "# Should not be rendered\n")
	out, _, err := runCLI(t, "--caps", path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Should not be rendered") {
		t.Error("--caps should report capabilities and exit, not render files")
	}
}

// TestRunNoProbe checks the escape hatch for terminals that misbehave when
// queried: it must still produce a report rather than failing.
func TestRunNoProbe(t *testing.T) {
	out, _, err := runCLI(t, "--no-probe", "--caps")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Terminal capabilities") {
		t.Errorf("no report produced:\n%s", out)
	}
}

func TestRunNoProbeStillRenders(t *testing.T) {
	path := writeTemp(t, "doc.md", "# Title\n")
	out, _, err := runCLI(t, "--no-probe", path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Title") {
		t.Errorf("got %q", out)
	}
}
