//go:build linux

package term

import (
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// TestDetectColorOnATerminal runs detection against a real terminal, the only
// target where NO_COLOR decides anything: everywhere else color is off anyway.
func TestDetectColorOnATerminal(t *testing.T) {
	_, slave := openPTY(t)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("COLORTERM", "truecolor")

	t.Setenv("NO_COLOR", "")
	if got := DetectColor(slave); got != theme.ColorTrue {
		t.Errorf("empty NO_COLOR: got %v, want truecolor", got)
	}
	t.Setenv("NO_COLOR", "1")
	if got := DetectColor(slave); got != theme.ColorNone {
		t.Errorf("NO_COLOR=1: got %v, want none", got)
	}
}
