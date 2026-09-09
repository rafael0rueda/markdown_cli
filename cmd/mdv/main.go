// Command mdv renders markdown files for the terminal.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mdv/internal/render"
	"mdv/internal/term"
	"mdv/internal/theme"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

// maxAutoWidth caps the layout width when it is taken from the terminal.
// Prose past roughly this many columns is measurably harder to read, and a
// full-screen-width table is worse than a wrapped one.
const maxAutoWidth = 100

type config struct {
	width     int
	themeName string
	colorName string
	linkName  string
	ascii     bool
	showVer   bool
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stderr, "mdv: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	var cfg config

	fs := flag.NewFlagSet("mdv", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.IntVar(&cfg.width, "width", 0, "layout width in columns (0 = detect)")
	fs.IntVar(&cfg.width, "w", 0, "shorthand for -width")
	fs.StringVar(&cfg.themeName, "theme", "auto", "color theme: "+strings.Join(theme.Names(), ", "))
	fs.StringVar(&cfg.colorName, "color", "auto", "color depth: auto, none, 16, 256, truecolor")
	fs.StringVar(&cfg.linkName, "links", "auto", "link display: auto, inline, hide")
	fs.BoolVar(&cfg.ascii, "ascii", false, "use ASCII instead of Unicode box drawing")
	fs.BoolVar(&cfg.showVer, "version", false, "print version and exit")
	fs.Usage = func() { usage(stderr, fs) }

	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.showVer {
		fmt.Fprintf(stdout, "mdv %s\n", version)
		return nil
	}

	out, ok := stdout.(*os.File)
	if !ok {
		out = nil
	}

	colorMode, err := resolveColor(cfg.colorName, out)
	if err != nil {
		return err
	}
	th, err := theme.Get(cfg.themeName)
	if err != nil {
		return err
	}
	if colorMode == theme.ColorNone {
		// Keep the requested theme's glyphs but drop its palette, so
		// --color=none and --theme=plain agree on what they produce.
		plain := theme.Plain()
		plain.Glyphs = th.Glyphs
		th = plain
	}
	if cfg.ascii {
		th.Glyphs = theme.ASCIIGlyphs
	}
	linkMode, err := render.ParseLinkMode(cfg.linkName)
	if err != nil {
		return err
	}
	if linkMode == render.LinkAuto {
		// Until terminal hyperlink support is probed for, showing the URL is
		// the only way a reader can act on a link.
		linkMode = render.LinkInline
	}

	width := cfg.width
	if width <= 0 {
		width = autoWidth(out)
	}

	writeOpts := render.WriteOptions{Color: colorMode}

	inputs := fs.Args()
	if len(inputs) == 0 {
		inputs = []string{"-"}
	}
	for i, name := range inputs {
		source, baseDir, err := readInput(name)
		if err != nil {
			return err
		}
		if len(inputs) > 1 {
			if i > 0 {
				fmt.Fprintln(stdout)
			}
			if err := writeFileHeader(stdout, name, width, th, writeOpts); err != nil {
				return err
			}
		}
		doc, err := render.Render(source, render.Options{
			Width:    width,
			Theme:    th,
			LinkMode: linkMode,
			BaseDir:  baseDir,
		})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := render.Write(stdout, doc, writeOpts); err != nil {
			return err
		}
	}
	return nil
}

// readInput loads a document and reports the directory its relative links
// should resolve against.
func readInput(name string) (source []byte, baseDir string, err error) {
	if name == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, "", fmt.Errorf("reading stdin: %w", err)
		}
		// Relative paths in piped markdown are relative to wherever the user
		// is standing, since there is no document location to anchor to.
		return b, "", nil
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, "", err
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return b, "", nil
	}
	return b, filepath.Dir(abs), nil
}

// writeFileHeader labels a document when more than one was given.
func writeFileHeader(w io.Writer, name string, width int, th *theme.Theme, opts render.WriteOptions) error {
	label := " " + name + " "
	rule := width - len(label) - 1
	if rule < 0 {
		rule = 0
	}
	line := render.Line{Runs: []render.Run{
		{Text: th.Glyphs.Rule, Style: th.Rule},
		{Text: label, Style: th.Headings[0]},
		{Text: strings.Repeat(th.Glyphs.Rule, rule), Style: th.Rule},
	}}
	_, err := fmt.Fprintln(w, render.RenderLine(line, width, opts))
	return err
}

// resolveColor turns the --color flag into a mode, consulting the environment
// when it is left on auto.
func resolveColor(name string, out *os.File) (theme.ColorMode, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return term.DetectColor(out), nil
	case "always", "force", "yes":
		return term.ForcedColor(), nil
	}
	return theme.ParseColorMode(name)
}

// autoWidth picks a layout width from the terminal, or a readable default when
// output is not going to one.
func autoWidth(out *os.File) int {
	if !term.IsTerminal(out) {
		return 80
	}
	w, _ := term.Size(out)
	if w > maxAutoWidth {
		return maxAutoWidth
	}
	return w
}

func usage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprintf(w, `mdv - render markdown in the terminal

Usage:
  mdv [flags] [file...]

With no file, or with "-", mdv reads markdown from standard input.
Output is plain when it is not going to a terminal, so piping into a
file or another program is safe.

Flags:
`)
	fs.PrintDefaults()
}
