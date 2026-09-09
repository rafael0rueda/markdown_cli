package term

import (
	"fmt"
	"strings"
	"time"
)

// Report renders the detected capabilities as a markdown document.
//
// Emitting markdown rather than formatted text means --caps goes through the
// same renderer as everything else: it picks up the theme, wraps to the
// terminal, and turns into plain text when piped, with no separate formatting
// code to keep in step.
func (c Caps) Report() string {
	var b strings.Builder

	b.WriteString("# Terminal capabilities\n\n")

	b.WriteString("| Property | Value |\n|---|---|\n")
	row := func(name, value string) {
		fmt.Fprintf(&b, "| %s | %s |\n", name, value)
	}

	row("Terminal", c.Terminal)
	row("Multiplexer", orNone(c.Multiplexer))
	if c.TTY {
		row("Output", fmt.Sprintf("terminal, %d x %d cells", c.Cols, c.Rows))
	} else {
		row("Output", "not a terminal")
	}
	row("Cell size", cellSize(c))
	row("Color depth", c.Color.String())
	row("Background", background(c))
	row("Theme", themeName(c.Dark()))

	b.WriteString("\n## Features\n\n")
	b.WriteString("| Feature | Supported |\n|---|---|\n")
	row("Kitty graphics", yesNo(c.KittyGraphics))
	row("Sixel graphics", yesNo(c.Sixel))
	row("Image protocol in use", c.Graphics())
	row("Kitty keyboard protocol", yesNo(c.KittyKeyboard))
	row("Hyperlinks (OSC 8)", yesNo(c.Hyperlinks))

	b.WriteString("\n## Probe\n\n")
	switch {
	case c.Probed:
		fmt.Fprintf(&b, "The terminal answered in %s, so the features above are "+
			"measured rather than guessed.\n", c.ProbeTime.Round(100*time.Microsecond))
	case c.ProbeErr != "":
		fmt.Fprintf(&b, "No live probe: %s. The features above come from the "+
			"environment, which reflects what the terminal advertises rather "+
			"than what it does.\n", c.ProbeErr)
	default:
		b.WriteString("No live probe was attempted.\n")
	}

	return b.String()
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func themeName(dark bool) string {
	if dark {
		return "dark"
	}
	return "light"
}

func cellSize(c Caps) string {
	if c.CellWidth > 0 && c.CellHeight > 0 {
		return fmt.Sprintf("%d x %d px", c.CellWidth, c.CellHeight)
	}
	return "unknown"
}

func background(c Caps) string {
	if !c.HasBackground {
		return "unknown"
	}
	return fmt.Sprintf("#%02x%02x%02x", c.Background.R, c.Background.G, c.Background.B)
}
