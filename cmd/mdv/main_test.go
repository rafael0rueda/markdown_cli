package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/graphics"
	"github.com/rafael0rueda/markdown_cli/internal/render"
	"github.com/rafael0rueda/markdown_cli/internal/term"
	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// runCLI invokes the command with a clean environment and captures its output.
func runCLI(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	// Output goes to buffers rather than a terminal, so color detection lands
	// on "none" and the assertions can be made on plain text.
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	// Keep the developer's own configuration file out of the results.
	t.Setenv("MDV_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

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

func TestResolveImages(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		caps    term.Caps
		wantNil bool
		want    graphics.Protocol
	}{
		{"auto with kitty", "auto", term.Caps{KittyGraphics: true}, false, graphics.Kitty},
		{"auto with sixel", "auto", term.Caps{Sixel: true}, false, graphics.Sixel},
		{"auto prefers kitty", "auto", term.Caps{KittyGraphics: true, Sixel: true}, false, graphics.Kitty},
		// Nothing is detected for a pipe, so auto draws nothing into it.
		{"auto with neither", "auto", term.Caps{}, true, graphics.None},
		{"explicitly off", "none", term.Caps{KittyGraphics: true}, true, graphics.None},
		{"explicitly kitty", "kitty", term.Caps{}, false, graphics.Kitty},
		{"explicitly sixel", "sixel", term.Caps{}, false, graphics.Sixel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveImages(config{images: tt.flag}, tt.caps)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantNil {
				if got != nil {
					t.Errorf("got %+v, want nil so the renderer falls back to alt text", got)
				}
				return
			}
			r, ok := got.(*graphics.Renderer)
			if !ok {
				t.Fatalf("got %T, want *graphics.Renderer", got)
			}
			if r.Protocol != tt.want {
				t.Errorf("protocol = %v, want %v", r.Protocol, tt.want)
			}
		})
	}

	if _, err := resolveImages(config{images: "iterm"}, term.Caps{}); err == nil {
		t.Error("an unknown image mode should be rejected")
	}
}

func TestResolveImagesCarriesCellSize(t *testing.T) {
	got, err := resolveImages(config{images: "kitty"}, term.Caps{CellWidth: 9, CellHeight: 18})
	if err != nil {
		t.Fatal(err)
	}
	r := got.(*graphics.Renderer)
	if r.CellWidth != 9 || r.CellHeight != 18 {
		t.Errorf("cell size = %dx%d, want 9x18", r.CellWidth, r.CellHeight)
	}
}

func TestResolveImagesRemoteOptIn(t *testing.T) {
	off, _ := resolveImages(config{images: "kitty"}, term.Caps{})
	if off.(*graphics.Renderer).Loader.AllowRemote {
		t.Error("remote images should be off unless asked for")
	}
	on, _ := resolveImages(config{images: "kitty", remoteImg: true}, term.Caps{})
	if !on.(*graphics.Renderer).Loader.AllowRemote {
		t.Error("--remote-images was not honoured")
	}
}

func TestMaxImageRows(t *testing.T) {
	if got := maxImageRows(term.Caps{Rows: 40}); got != 38 {
		t.Errorf("got %d, want 38 so the image cannot fill the whole screen", got)
	}
	// An implausible terminal height falls back to the renderer's own default.
	if got := maxImageRows(term.Caps{Rows: 2}); got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

// TestRunPipedOutputHasNoImages is the safety property for redirection: a file
// on disk must not end up with graphics escape sequences in it.
func TestRunPipedOutputHasNoImages(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	os.WriteFile(doc, []byte("![alt text](pic.png)\n"), 0o644)

	out, _, err := runCLI(t, doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("escape sequences in piped output: %q", out)
	}
	if !strings.Contains(out, "alt text") {
		t.Errorf("alt text missing: %q", out)
	}
}

// noWrap is a layout width no test document reaches, for tests that assert on
// content rather than layout.
const noWrap = "10000"

// buildTestVault writes a minimal Obsidian vault and returns its root.
func buildTestVault(t *testing.T, attachmentDir string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".obsidian"), 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"attachmentFolderPath":"` + attachmentDir + `"}`
	if err := os.WriteFile(filepath.Join(root, ".obsidian", "app.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunResolvesWikilinksInAVault(t *testing.T) {
	root := buildTestVault(t, "Assets", map[string]string{
		"notes/note.md":       "See [[Other Note]] and ![[pic.png]].\n",
		"notes/Other Note.md": "hi",
		"Assets/pic.png":      "not really a png",
	})

	// Wide enough that no path wraps: the assertions below look for resolved
	// paths in the text, and a path broken across lines - as it is at 100
	// columns under some temp directory lengths, macOS's among them - would
	// hide a correct result.
	out, _, err := runCLI(t, "--width", noWrap, filepath.Join(root, "notes", "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[[") {
		t.Errorf("raw wikilink syntax reached the output:\n%s", out)
	}
	if !strings.Contains(out, "Other Note") || !strings.Contains(out, "pic.png") {
		t.Errorf("wikilink labels missing:\n%s", out)
	}
	// The embed target has to resolve through the configured attachment
	// folder, which is the whole reason for reading the vault config.
	if !strings.Contains(out, filepath.Join("Assets", "pic.png")) {
		t.Errorf("embed did not resolve to the attachment folder:\n%s", out)
	}
}

func TestRunNoVault(t *testing.T) {
	root := buildTestVault(t, "Assets", map[string]string{
		"note.md":        "![[pic.png]]\n",
		"Assets/pic.png": "x",
	})

	out, _, err := runCLI(t, "--no-vault", filepath.Join(root, "note.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "Assets") {
		t.Errorf("--no-vault still resolved the embed:\n%s", out)
	}
	// The label is still worth showing even with resolution turned off.
	if !strings.Contains(out, "pic.png") {
		t.Errorf("label missing:\n%s", out)
	}
}

func TestRunExplicitVault(t *testing.T) {
	root := buildTestVault(t, "Assets", map[string]string{"Assets/pic.png": "x"})

	// A note outside the vault, pointed at it explicitly.
	outside := t.TempDir()
	note := filepath.Join(outside, "note.md")
	os.WriteFile(note, []byte("![[pic.png]]\n"), 0o644)

	out, _, err := runCLI(t, "--width", noWrap, "--vault", root, note)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, filepath.Join("Assets", "pic.png")) {
		t.Errorf("--vault was not used:\n%s", out)
	}
}

func TestRunOutsideAVault(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.md")
	os.WriteFile(note, []byte("See [[Some Note]].\n"), 0o644)

	out, _, err := runCLI(t, note)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[[") {
		t.Errorf("raw syntax leaked outside a vault:\n%s", out)
	}
	if !strings.Contains(out, "Some Note") {
		t.Errorf("label missing:\n%s", out)
	}
}

func TestRunFrontmatterModes(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.md")
	os.WriteFile(note, []byte("---\ntags:\n  - alpha\n---\n\n# Heading\n"), 0o644)

	tests := map[string]bool{"meta": true, "hide": false}
	for mode, wantTags := range tests {
		out, _, err := runCLI(t, "--frontmatter", mode, note)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(out, "alpha"); got != wantTags {
			t.Errorf("--frontmatter=%s: tags present = %v, want %v\n%s", mode, got, wantTags, out)
		}
		if !strings.Contains(out, "Heading") {
			t.Errorf("--frontmatter=%s: body lost\n%s", mode, out)
		}
	}

	if _, _, err := runCLI(t, "--frontmatter", "yaml", note); err == nil {
		t.Error("an unknown frontmatter mode should be rejected")
	}
}

// TestRunStdinHasNoVault checks that markdown arriving on stdin, which has no
// location on disk, does not have wikilinks resolved against some unrelated
// vault near the working directory.
func TestRunStdinHasNoVault(t *testing.T) {
	old := os.Stdin
	defer func() { os.Stdin = old }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		io.WriteString(w, "See [[Some Note]].\n")
		w.Close()
	}()
	os.Stdin = r

	out, _, err := runCLI(t)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Some Note") {
		t.Errorf("label missing: %q", out)
	}
}

func TestUsePager(t *testing.T) {
	tests := []struct {
		mode string
		caps term.Caps
		want bool
	}{
		// On auto it follows the terminal, which is what keeps redirection and
		// piping into another program working.
		{"auto", term.Caps{TTY: true}, true},
		{"auto", term.Caps{TTY: false}, false},
		{"", term.Caps{TTY: true}, true},
		{"never", term.Caps{TTY: true}, false},
		{"off", term.Caps{TTY: true}, false},
		{"always", term.Caps{TTY: false}, true},
	}
	for _, tt := range tests {
		if got := usePager(tt.mode, tt.caps, nil); got != tt.want {
			t.Errorf("usePager(%q, tty=%v) = %v, want %v",
				tt.mode, tt.caps.TTY, got, tt.want)
		}
	}
}

func TestPagerRequired(t *testing.T) {
	for _, mode := range []string{"always", "yes", "ALWAYS"} {
		if !pagerRequired(mode) {
			t.Errorf("%q should require the pager", mode)
		}
	}
	for _, mode := range []string{"auto", "never", ""} {
		if pagerRequired(mode) {
			t.Errorf("%q should not require the pager", mode)
		}
	}
}

// TestRunNeverPagesWhenPiped is the property that keeps mdv usable in a
// pipeline: writing to anything but a terminal streams the document out.
func TestRunNeverPagesWhenPiped(t *testing.T) {
	path := writeTemp(t, "doc.md", "# Title\n\nBody.\n")
	out, _, err := runCLI(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Title") || !strings.Contains(out, "Body.") {
		t.Errorf("document was not written out: %q", out)
	}
	if strings.ContainsRune(out, 0x1b) {
		t.Errorf("escape sequences in piped output: %q", out)
	}
}

// TestRunMultipleFilesAreCombined checks that several inputs become one
// document, which is what lets the pager scroll through them as a unit.
func TestRunMultipleFilesAreCombined(t *testing.T) {
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
	// The header for the second file must come after the first file's body.
	if strings.Index(out, "alpha") > strings.Index(out, "b.md") {
		t.Errorf("documents are out of order:\n%s", out)
	}
}

// TestRenderAllShiftsImageLines guards the bookkeeping that combining
// documents requires: placements are line-numbered, so they have to move with
// the lines they belong to.
func TestRenderAllShiftsImageLines(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.md")
	second := filepath.Join(dir, "second.md")
	os.WriteFile(first, []byte("one\n\ntwo\n\nthree\n"), 0o644)
	os.WriteFile(second, []byte("![alt](pic.png)\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "pic.png"), []byte("x"), 0o644)

	docs, err := loadInputs([]string{first, second})
	if err != nil {
		t.Fatal(err)
	}

	images := &countingHandler{cols: 10, rows: 3}
	doc, err := renderAll(docs, render.Options{
		Width: 60, Theme: theme.Plain(), Images: images,
	}, config{}, render.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Images) != 1 {
		t.Fatalf("got %d placements, want 1", len(doc.Images))
	}

	img := doc.Images[0]
	if img.Line == 0 {
		t.Error("the placement was not shifted past the first document")
	}
	if img.Line+img.Rows > len(doc.Lines) {
		t.Errorf("placement covers lines %d-%d but the document has %d",
			img.Line, img.Line+img.Rows-1, len(doc.Lines))
	}
	// The rows it points at have to be the reserved blank ones.
	if got := strings.TrimSpace(doc.Lines[img.Line+1].Text()); got != "" {
		t.Errorf("line %d should be reserved for the image, got %q", img.Line+1, got)
	}
}

// countingHandler is a stand-in image handler for layout tests.
type countingHandler struct{ cols, rows int }

func (c *countingHandler) Measure(string, int, int, render.SizeHint) (int, int, error) {
	return c.cols, c.rows, nil
}

func (c *countingHandler) Encode(string, int, int, int) (string, error) { return "<img>", nil }
