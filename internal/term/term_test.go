package term

import (
	"os"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

func TestDetectColorRespectsNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("TERM", "xterm-kitty")
	if got := DetectColor(os.Stdout); got != theme.ColorNone {
		t.Errorf("NO_COLOR ignored: got %v", got)
	}
}

func TestDetectColorOnPipe(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("COLORTERM", "truecolor")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	// A pipe is not a terminal, so nothing should be colored even though the
	// environment advertises full support.
	if got := DetectColor(w); got != theme.ColorNone {
		t.Errorf("piped output got %v, want none", got)
	}
}

func TestDetectColorDumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")
	if got := DetectColor(os.Stdout); got != theme.ColorNone {
		t.Errorf("TERM=dumb got %v, want none", got)
	}
}

func TestDepthFromEnv(t *testing.T) {
	tests := []struct {
		term      string
		colorterm string
		want      theme.ColorMode
	}{
		{"xterm-kitty", "truecolor", theme.ColorTrue},
		{"xterm-kitty", "", theme.ColorTrue},
		{"xterm-ghostty", "", theme.ColorTrue},
		{"xterm-256color", "", theme.Color256},
		{"screen-256color", "", theme.Color256},
		{"xterm-direct", "", theme.ColorTrue},
		{"xterm", "", theme.Color16},
		{"vt100", "", theme.Color16},
		{"xterm", "24bit", theme.ColorTrue},
		{"", "", theme.ColorNone},
	}
	for _, tt := range tests {
		t.Setenv("TERM", tt.term)
		t.Setenv("COLORTERM", tt.colorterm)
		if got := depthFromEnv(); got != tt.want {
			t.Errorf("TERM=%q COLORTERM=%q: got %v, want %v",
				tt.term, tt.colorterm, got, tt.want)
		}
	}
}

func TestForcedColorNeverNone(t *testing.T) {
	t.Setenv("TERM", "")
	t.Setenv("COLORTERM", "")
	if got := ForcedColor(); got == theme.ColorNone {
		t.Error("forcing color should never resolve to none")
	}
}

func TestIsTerminalOnPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if IsTerminal(w) {
		t.Error("a pipe reported as a terminal")
	}
	if IsTerminal(nil) {
		t.Error("nil reported as a terminal")
	}
}

func TestSizeFallback(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if width, height := Size(w); width != 80 || height != 24 {
		t.Errorf("got %dx%d, want the 80x24 fallback", width, height)
	}
	if width, _ := Size(nil); width != 80 {
		t.Errorf("nil file got width %d, want 80", width)
	}
}
