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
	"time"

	"github.com/rafael0rueda/markdown_cli/internal/graphics"
	"github.com/rafael0rueda/markdown_cli/internal/pager"
	"github.com/rafael0rueda/markdown_cli/internal/render"
	"github.com/rafael0rueda/markdown_cli/internal/term"
	"github.com/rafael0rueda/markdown_cli/internal/theme"
	"github.com/rafael0rueda/markdown_cli/internal/vault"
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
	showCaps  bool
	noProbe   bool
	probeWait time.Duration
	images    string
	remoteImg bool
	vaultDir  string
	noVault   bool
	frontStr  string
	pagerMode string
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
	fs.StringVar(&cfg.images, "images", "auto", "image drawing: auto, none, kitty, sixel")
	fs.BoolVar(&cfg.remoteImg, "remote-images", false, "fetch images over http, off by default")
	fs.StringVar(&cfg.vaultDir, "vault", "", "Obsidian vault root (default: found from the document)")
	fs.BoolVar(&cfg.noVault, "no-vault", false, "do not resolve [[wikilinks]] against a vault")
	fs.StringVar(&cfg.frontStr, "frontmatter", "meta", "YAML frontmatter: meta, hide, raw")
	fs.StringVar(&cfg.pagerMode, "pager", "auto", "interactive pager: auto, always, never")
	fs.BoolVar(&cfg.showCaps, "caps", false, "report what the terminal supports and exit")
	fs.BoolVar(&cfg.noProbe, "no-probe", false, "do not query the terminal; use the environment alone")
	fs.DurationVar(&cfg.probeWait, "probe-timeout", term.DefaultProbeTimeout, "how long to wait for the terminal to answer")
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

	// One capability scan serves everything below: color depth, which theme
	// suits the background, and whether hyperlinks are safe to emit.
	caps := term.Detect(term.DetectOptions{
		Out:     out,
		Probe:   !cfg.noProbe,
		Timeout: cfg.probeWait,
	})

	colorMode, err := resolveColor(cfg.colorName, caps)
	if err != nil {
		return err
	}
	th, err := resolveTheme(cfg.themeName, caps)
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
	linkMode, err := resolveLinkMode(cfg.linkName, caps.Hyperlinks)
	if err != nil {
		return err
	}
	hyperlinks := caps.Hyperlinks

	width := cfg.width
	if width <= 0 {
		width = autoWidth(caps)
	}

	imageHandler, err := resolveImages(cfg, caps)
	if err != nil {
		return err
	}
	frontMode, err := render.ParseFrontmatterMode(cfg.frontStr)
	if err != nil {
		return err
	}

	writeOpts := render.WriteOptions{
		Color:      colorMode,
		Hyperlinks: hyperlinks,
		Images:     imageHandler,
	}

	if cfg.showCaps {
		doc, err := render.Render([]byte(caps.Report()), render.Options{
			Width: width, Theme: th, LinkMode: linkMode,
		})
		if err != nil {
			return err
		}
		return render.Write(stdout, doc, writeOpts)
	}

	inputs := fs.Args()
	if len(inputs) == 0 {
		inputs = []string{"-"}
	}

	docs, err := loadInputs(inputs)
	if err != nil {
		return err
	}

	// build lays the whole session out at a given width. The pager calls it
	// again on every resize, and the streaming path calls it once.
	build := func(w int) (*render.Doc, error) {
		return renderAll(docs, render.Options{
			Width:        w,
			Theme:        th,
			LinkMode:     linkMode,
			Images:       imageHandler,
			MaxImageRows: maxImageRows(caps),
			Frontmatter:  frontMode,
		}, cfg, writeOpts)
	}

	if usePager(cfg.pagerMode, caps, out) {
		err := runPager(build, caps, th, writeOpts, imageHandler, docs)
		// Not having a terminal to draw on is a reason to write the document
		// out instead, not a reason to fail - unless paging was asked for
		// explicitly, in which case silently doing something else would be
		// the wrong answer.
		if err == nil || !errors.Is(err, pager.ErrNoTerminal) || pagerRequired(cfg.pagerMode) {
			return err
		}
	}

	doc, err := build(width)
	if err != nil {
		return err
	}
	return render.Write(stdout, doc, writeOpts)
}

// document is one input, loaded and ready to render.
type document struct {
	name    string
	source  []byte
	baseDir string
}

// loadInputs reads every input up front.
//
// The pager needs to re-render on resize, so the sources have to outlive the
// first pass; reading them once also means a document arriving on stdin can be
// laid out repeatedly.
func loadInputs(names []string) ([]document, error) {
	docs := make([]document, 0, len(names))
	for _, name := range names {
		source, baseDir, err := readInput(name)
		if err != nil {
			return nil, err
		}
		docs = append(docs, document{name: name, source: source, baseDir: baseDir})
	}
	return docs, nil
}

// renderAll lays every input out into a single document, labelling them when
// there is more than one.
func renderAll(docs []document, opts render.Options, cfg config, writeOpts render.WriteOptions) (*render.Doc, error) {
	combined := &render.Doc{Width: opts.Width}

	for i, d := range docs {
		if len(docs) > 1 {
			if i > 0 {
				combined.Lines = append(combined.Lines, render.Line{})
			}
			combined.Lines = append(combined.Lines, fileHeader(d.name, opts.Width, opts.Theme))
		}

		docOpts := opts
		docOpts.BaseDir = d.baseDir
		docOpts.Links = resolveVault(cfg, d.name, d.baseDir)

		doc, err := render.Render(d.source, docOpts)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.name, err)
		}

		// Image placements are line-numbered, so they have to be shifted onto
		// their position within the combined document.
		offset := len(combined.Lines)
		for _, img := range doc.Images {
			img.Line += offset
			combined.Images = append(combined.Images, img)
		}
		combined.Lines = append(combined.Lines, doc.Lines...)
	}
	return combined, nil
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

// fileHeader labels a document when more than one was given.
func fileHeader(name string, width int, th *theme.Theme) render.Line {
	label := " " + name + " "
	rule := width - render.NewRun(label).Width() - 1
	if rule < 0 {
		rule = 0
	}
	return render.Line{Runs: []render.Run{
		{Text: th.Glyphs.Rule, Style: th.Rule},
		{Text: label, Style: th.Headings[0]},
		{Text: strings.Repeat(th.Glyphs.Rule, rule), Style: th.Rule},
	}}
}

// usePager decides whether to page rather than stream.
//
// Paging needs a terminal to draw on, so it is off whenever output is
// redirected - which also keeps piping into another program working.
func usePager(mode string, caps term.Caps, out *os.File) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "never", "off", "no":
		return false
	case "always", "yes":
		return true
	}
	return caps.TTY
}

// pagerRequired reports whether the user insisted on paging.
func pagerRequired(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "always", "yes":
		return true
	}
	return false
}

// runPager displays the document interactively.
func runPager(build pager.Renderer, caps term.Caps, th *theme.Theme,
	writeOpts render.WriteOptions, images render.ImageHandler, docs []document) error {

	title := "mdv"
	if len(docs) == 1 && docs[0].name != "-" {
		title = filepath.Base(docs[0].name)
	} else if len(docs) > 1 {
		title = fmt.Sprintf("%d documents", len(docs))
	}

	// The pager needs to draw parts of images, which the streaming interface
	// cannot express; only the graphics renderer can do it.
	drawer, _ := images.(pager.ImageDrawer)

	return pager.Run(pager.Options{
		Render:        build,
		Write:         writeOpts,
		Images:        drawer,
		Theme:         th,
		Title:         title,
		MaxWidth:      maxAutoWidth,
		KittyKeyboard: caps.KittyKeyboard,
	})
}

// resolveColor turns the --color flag into a mode, deferring to the detected
// capabilities when it is left on auto.
func resolveColor(name string, caps term.Caps) (theme.ColorMode, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "auto":
		return caps.Color, nil
	case "always", "force", "yes":
		return term.ForcedColor(), nil
	}
	return theme.ParseColorMode(name)
}

// resolveLinkMode decides how link destinations are shown.
//
// On auto, the presence of OSC 8 support settles it: with hyperlinks the URL
// is carried by the link text itself and printing it again in parentheses is
// noise, while without them the visible URL is the only way a reader can act
// on the link.
func resolveLinkMode(name string, hyperlinks bool) (render.LinkMode, error) {
	mode, err := render.ParseLinkMode(name)
	if err != nil {
		return mode, err
	}
	if mode == render.LinkAuto {
		if hyperlinks {
			return render.LinkHide, nil
		}
		return render.LinkInline, nil
	}
	return mode, nil
}

// resolveImages builds the image handler, or returns nil to fall back to alt
// text everywhere.
//
// A nil interface value is returned rather than a typed nil, because the
// renderer tests the interface against nil to decide whether to attempt
// images at all.
func resolveImages(cfg config, caps term.Caps) (render.ImageHandler, error) {
	protocol, auto, err := graphics.ParseProtocol(cfg.images)
	if err != nil {
		return nil, err
	}
	if auto {
		// On auto, only draw what the terminal was found to support. This is
		// also what keeps a redirected stream clean: nothing is detected for a
		// pipe, so nothing is drawn into it.
		switch {
		case caps.KittyGraphics:
			protocol = graphics.Kitty
		case caps.Sixel:
			protocol = graphics.Sixel
		default:
			protocol = graphics.None
		}
	}
	if protocol == graphics.None {
		return nil, nil
	}
	return &graphics.Renderer{
		Protocol:   protocol,
		Loader:     &graphics.Loader{AllowRemote: cfg.remoteImg},
		CellWidth:  caps.CellWidth,
		CellHeight: caps.CellHeight,
	}, nil
}

// resolveVault finds the note vault a document belongs to, so that
// Obsidian-style [[links]] and ![[embeds]] can be resolved.
//
// Detection is automatic because a vault is discoverable: it is the nearest
// ancestor directory holding a .obsidian folder. Requiring a flag would mean
// the common case - opening a note from inside a vault - needed configuration
// to work at all.
//
// A nil result is returned rather than an empty resolver, because the renderer
// takes nil to mean the document is not in a vault and renders wikilinks as
// plain text.
func resolveVault(cfg config, docPath, docDir string) render.LinkResolver {
	if cfg.noVault {
		return nil
	}
	var v *vault.Vault
	switch {
	case cfg.vaultDir != "":
		v = vault.Open(cfg.vaultDir)
	case docPath == "-" || docDir == "":
		// Markdown arriving on stdin has no location, so there is no vault to
		// search from.
		return nil
	default:
		v = vault.Find(docDir)
	}
	if v == nil {
		return nil
	}
	// A typed nil inside the interface would not compare equal to nil, and the
	// renderer's check would pass a resolver that resolves nothing.
	if r := v.For(docDir); r != nil {
		return r
	}
	return nil
}

// maxImageRows caps image height at nearly a full screen, so a picture cannot
// push the text it belongs to entirely out of view.
func maxImageRows(caps term.Caps) int {
	if caps.Rows > 4 {
		return caps.Rows - 2
	}
	return 0
}

// resolveTheme picks the theme, using the terminal's actual background color
// to choose between dark and light when the name is left on auto. Reading the
// background beats guessing from COLORFGBG, which most terminals never set.
func resolveTheme(name string, caps term.Caps) (*theme.Theme, error) {
	if n := strings.ToLower(strings.TrimSpace(name)); n == "" || n == "auto" {
		if caps.Dark() {
			return theme.Dark(), nil
		}
		return theme.Light(), nil
	}
	return theme.Get(name)
}

// autoWidth picks a layout width from the terminal, or a readable default when
// output is not going to one.
func autoWidth(caps term.Caps) int {
	if !caps.TTY {
		return 80
	}
	if caps.Cols > maxAutoWidth {
		return maxAutoWidth
	}
	return caps.Cols
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
