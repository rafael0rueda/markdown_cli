package term

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// Caps is what the attached terminal can do.
//
// Fields are filled from two sources: the environment, which is free but only
// as honest as the terminal's own advertising, and a live probe, which asks
// the terminal directly. Probing wins where the two disagree, because a
// terminal that answers a graphics query is demonstrating support rather than
// claiming it.
type Caps struct {
	// TTY reports whether output is going to a terminal at all. When it is
	// false every other field holds its conservative default.
	TTY bool

	Cols, Rows int

	// CellWidth and CellHeight are the size of one character cell in pixels,
	// needed to scale an image to a given number of rows. Zero means unknown.
	CellWidth, CellHeight int

	Color theme.ColorMode

	KittyGraphics bool
	Sixel         bool
	KittyKeyboard bool
	Hyperlinks    bool

	// Background is the terminal's background color, when it could be read.
	Background    theme.Color
	HasBackground bool

	// Terminal is a normalized name for the emulator, e.g. "kitty".
	Terminal string
	// Multiplexer is "tmux", "screen", or empty.
	Multiplexer string

	// Probed records whether the terminal actually answered a query.
	Probed bool
	// ProbeErr explains why probing was skipped or failed.
	ProbeErr string
	// ProbeTime is how long the terminal took to answer. It is worth showing
	// because a slow reply over ssh is the most likely reason for a probe to
	// time out and silently fall back to alt text.
	ProbeTime time.Duration
}

// DetectOptions controls a capability scan.
type DetectOptions struct {
	// Out is the stream mdv will write rendered output to.
	Out *os.File
	// Probe enables querying the terminal directly. Callers disable it when
	// the user passed --no-probe.
	Probe bool
	// Timeout bounds the wait for replies. Zero means DefaultProbeTimeout.
	Timeout time.Duration
}

// DefaultProbeTimeout bounds how long mdv waits for a terminal to answer.
//
// Replies are typically instant on a local terminal; the budget is set for a
// slow ssh link, since giving up early on a remote kitty session means falling
// back to alt text for every image. It is still short enough not to be felt as
// a delay before output appears.
const DefaultProbeTimeout = 300 * time.Millisecond

// Detect reports what the terminal can do.
func Detect(opts DetectOptions) Caps {
	caps := Caps{
		Terminal:    identifyTerminal(),
		Multiplexer: identifyMultiplexer(),
		TTY:         IsTerminal(opts.Out),
	}
	caps.Color = DetectColor(opts.Out)
	caps.Cols, caps.Rows = Size(opts.Out)

	if !caps.TTY {
		caps.ProbeErr = "output is not a terminal"
		return caps
	}

	// Start from what the environment claims, so there is still an answer if
	// the terminal declines to reply.
	applyKnownTerminal(&caps)

	// A window size report may carry pixel dimensions, which gives the cell
	// size without a round trip.
	if w, h, ok := pixelSize(opts.Out); ok && caps.Cols > 0 && caps.Rows > 0 {
		caps.CellWidth, caps.CellHeight = w/caps.Cols, h/caps.Rows
	}

	if !opts.Probe {
		caps.ProbeErr = "probing disabled"
		return caps
	}
	if caps.Multiplexer != "" {
		// Inside tmux or screen the replies are intercepted by the
		// multiplexer, and graphics need passthrough wrapping that is not
		// implemented yet. Reporting the environment's view is more useful
		// than reporting a probe that cannot succeed.
		caps.ProbeErr = "not probing inside " + caps.Multiplexer
		caps.KittyGraphics = false
		caps.Sixel = false
		return caps
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}
	start := time.Now()
	res, err := probe(timeout)
	caps.ProbeTime = time.Since(start)
	if err != nil {
		caps.ProbeErr = err.Error()
		return caps
	}
	if !res.deviceAttrs {
		// Without the device attributes reply the terminal never got to the
		// end of the query string, so a missing graphics reply proves nothing.
		caps.ProbeErr = "terminal did not answer"
		return caps
	}

	caps.Probed = true
	caps.KittyGraphics = res.kittyGraphics
	caps.Sixel = res.sixel
	caps.KittyKeyboard = res.kittyKeyboard
	if res.hasBackground {
		caps.Background, caps.HasBackground = res.background, true
	}
	if res.cellWidth > 0 && res.cellHeight > 0 {
		caps.CellWidth, caps.CellHeight = res.cellWidth, res.cellHeight
	}
	return caps
}

// Dark reports whether the terminal has a dark background, preferring the
// color read from the terminal over the environment's hint.
func (c Caps) Dark() bool {
	if c.HasBackground {
		// Mid-gray is the dividing line; anything darker reads as a dark theme.
		return c.Background.Luminance() < 0.5
	}
	return theme.DetectDark()
}

// Graphics names the best available image protocol: "kitty", "sixel" or
// "none".
func (c Caps) Graphics() string {
	switch {
	case c.KittyGraphics:
		return "kitty"
	case c.Sixel:
		return "sixel"
	}
	return "none"
}

// identifyTerminal names the emulator from the environment.
//
// The checks are ordered most to least specific: a terminal setting its own
// marker variable is trusted over TERM_PROGRAM, which is trusted over TERM,
// because TERM is routinely set to xterm-256color by terminals that are
// nothing of the sort.
func identifyTerminal() string {
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "":
		return "kitty"
	case os.Getenv("GHOSTTY_RESOURCES_DIR") != "", os.Getenv("GHOSTTY_BIN_DIR") != "":
		return "ghostty"
	case os.Getenv("WEZTERM_PANE") != "", os.Getenv("WEZTERM_EXECUTABLE") != "":
		return "wezterm"
	case os.Getenv("KONSOLE_VERSION") != "":
		return "konsole"
	case os.Getenv("ALACRITTY_WINDOW_ID") != "":
		return "alacritty"
	case os.Getenv("CONTOUR_PROFILE") != "":
		return "contour"
	case os.Getenv("WT_SESSION") != "":
		return "windows-terminal"
	}

	switch prog := os.Getenv("TERM_PROGRAM"); prog {
	case "iTerm.app":
		return "iterm2"
	case "Apple_Terminal":
		return "apple-terminal"
	case "WezTerm":
		return "wezterm"
	case "ghostty":
		return "ghostty"
	case "vscode":
		return "vscode"
	case "Hyper":
		return "hyper"
	case "":
	default:
		return strings.ToLower(prog)
	}

	term := os.Getenv("TERM")
	for _, name := range []string{"kitty", "ghostty", "foot", "contour", "alacritty", "rio"} {
		if strings.Contains(term, name) {
			return name
		}
	}
	if os.Getenv("VTE_VERSION") != "" {
		return "vte"
	}
	if term == "" {
		return "unknown"
	}
	return term
}

// identifyMultiplexer reports whether output is passing through tmux or screen.
func identifyMultiplexer() string {
	term := os.Getenv("TERM")
	switch {
	case os.Getenv("TMUX") != "", strings.HasPrefix(term, "tmux"):
		return "tmux"
	case os.Getenv("STY") != "", strings.HasPrefix(term, "screen"):
		// screen's TERM is also what tmux sets by default in older configs,
		// but TMUX above has already claimed that case.
		return "screen"
	}
	return ""
}

// applyKnownTerminal fills in capabilities from the identified terminal.
//
// This is the fallback for when probing is off, impossible, or unanswered.
// Only well-established support is listed: guessing wrong here means emitting
// escape sequences that print as garbage, which is worse than plain output.
func applyKnownTerminal(caps *Caps) {
	switch caps.Terminal {
	case "kitty":
		caps.KittyGraphics, caps.KittyKeyboard, caps.Hyperlinks = true, true, true
	case "ghostty":
		caps.KittyGraphics, caps.KittyKeyboard, caps.Hyperlinks = true, true, true
	case "wezterm":
		caps.KittyGraphics, caps.Sixel, caps.Hyperlinks = true, true, true
	case "konsole":
		caps.KittyGraphics, caps.Hyperlinks = true, true
	case "foot":
		caps.Sixel, caps.Hyperlinks = true, true
	case "contour":
		caps.Sixel, caps.Hyperlinks = true, true
	case "iterm2":
		caps.Hyperlinks = true
	case "rio", "alacritty", "windows-terminal", "hyper", "vscode":
		caps.Hyperlinks = true
	case "vte":
		// GNOME Terminal and relatives gained OSC 8 in VTE 0.50, which reports
		// itself as 5000.
		if v, err := strconv.Atoi(os.Getenv("VTE_VERSION")); err == nil && v >= 5000 {
			caps.Hyperlinks = true
		}
	case "mlterm":
		caps.Sixel = true
	}
}
