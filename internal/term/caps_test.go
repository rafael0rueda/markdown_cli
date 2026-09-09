package term

import (
	"os"
	"strings"
	"testing"

	"mdv/internal/theme"
)

// terminalEnvVars is every variable the identification logic consults. Tests
// clear all of them so a result never depends on the environment the test
// suite happens to run in.
var terminalEnvVars = []string{
	"KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR", "GHOSTTY_BIN_DIR",
	"WEZTERM_PANE", "WEZTERM_EXECUTABLE", "KONSOLE_VERSION",
	"ALACRITTY_WINDOW_ID", "CONTOUR_PROFILE", "WT_SESSION",
	"TERM_PROGRAM", "TERM", "VTE_VERSION", "TMUX", "STY",
	"COLORTERM", "NO_COLOR", "COLORFGBG",
}

func clearTerminalEnv(t *testing.T) {
	t.Helper()
	for _, v := range terminalEnvVars {
		t.Setenv(v, "")
	}
}

func TestIdentifyTerminal(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"kitty", map[string]string{"KITTY_WINDOW_ID": "1", "TERM": "xterm-kitty"}, "kitty"},
		{"ghostty", map[string]string{"GHOSTTY_RESOURCES_DIR": "/x"}, "ghostty"},
		{"wezterm", map[string]string{"WEZTERM_PANE": "0"}, "wezterm"},
		{"konsole", map[string]string{"KONSOLE_VERSION": "220400"}, "konsole"},
		{"iterm2", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "iterm2"},
		{"apple terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "apple-terminal"},
		{"vscode", map[string]string{"TERM_PROGRAM": "vscode"}, "vscode"},
		{"foot by TERM", map[string]string{"TERM": "foot-extra"}, "foot"},
		{"alacritty by TERM", map[string]string{"TERM": "alacritty"}, "alacritty"},
		{"vte", map[string]string{"VTE_VERSION": "6003", "TERM": "xterm-256color"}, "vte"},
		{"plain xterm", map[string]string{"TERM": "xterm-256color"}, "xterm-256color"},
		{"nothing", map[string]string{}, "unknown"},

		// A terminal's own marker variable beats TERM_PROGRAM, which beats
		// TERM: terminals routinely claim TERM=xterm-256color.
		{"marker beats TERM", map[string]string{
			"KITTY_WINDOW_ID": "1", "TERM": "xterm-256color",
		}, "kitty"},
		{"TERM_PROGRAM beats TERM", map[string]string{
			"TERM_PROGRAM": "WezTerm", "TERM": "xterm-256color",
		}, "wezterm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTerminalEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := identifyTerminal(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIdentifyMultiplexer(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"tmux by env", map[string]string{"TMUX": "/tmp/sock,1,0"}, "tmux"},
		{"tmux by TERM", map[string]string{"TERM": "tmux-256color"}, "tmux"},
		{"screen by env", map[string]string{"STY": "1234.pts-0"}, "screen"},
		{"screen by TERM", map[string]string{"TERM": "screen-256color"}, "screen"},
		{"none", map[string]string{"TERM": "xterm-kitty"}, ""},

		// tmux sets TERM=screen-256color in older configurations, so the TMUX
		// variable has to win or every tmux session is misreported as screen.
		{"tmux wearing screen's TERM", map[string]string{
			"TMUX": "/tmp/sock,1,0", "TERM": "screen-256color",
		}, "tmux"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearTerminalEnv(t)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := identifyMultiplexer(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyKnownTerminal(t *testing.T) {
	tests := []struct {
		terminal   string
		graphics   bool
		sixel      bool
		hyperlinks bool
	}{
		{"kitty", true, false, true},
		{"ghostty", true, false, true},
		{"wezterm", true, true, true},
		{"foot", false, true, true},
		{"iterm2", false, false, true},
		{"apple-terminal", false, false, false},
		{"xterm-256color", false, false, false},
		{"unknown", false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.terminal, func(t *testing.T) {
			clearTerminalEnv(t)
			caps := Caps{Terminal: tt.terminal}
			applyKnownTerminal(&caps)
			if caps.KittyGraphics != tt.graphics {
				t.Errorf("KittyGraphics = %v, want %v", caps.KittyGraphics, tt.graphics)
			}
			if caps.Sixel != tt.sixel {
				t.Errorf("Sixel = %v, want %v", caps.Sixel, tt.sixel)
			}
			if caps.Hyperlinks != tt.hyperlinks {
				t.Errorf("Hyperlinks = %v, want %v", caps.Hyperlinks, tt.hyperlinks)
			}
		})
	}
}

// TestVTEHyperlinkVersion checks the version gate: VTE gained OSC 8 in 0.50,
// reported as 5000. Emitting them to an older VTE prints the escape payload.
func TestVTEHyperlinkVersion(t *testing.T) {
	tests := map[string]bool{"4800": false, "5000": true, "6003": true, "": false, "junk": false}
	for version, want := range tests {
		clearTerminalEnv(t)
		t.Setenv("VTE_VERSION", version)
		caps := Caps{Terminal: "vte"}
		applyKnownTerminal(&caps)
		if caps.Hyperlinks != want {
			t.Errorf("VTE_VERSION=%q: Hyperlinks = %v, want %v", version, caps.Hyperlinks, want)
		}
	}
}

func TestDetectNotATerminal(t *testing.T) {
	clearTerminalEnv(t)
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("KITTY_WINDOW_ID", "1")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	caps := Detect(DetectOptions{Out: w, Probe: true})
	if caps.TTY {
		t.Error("a pipe was reported as a terminal")
	}
	if caps.Color != theme.ColorNone {
		t.Errorf("Color = %v, want none for a pipe", caps.Color)
	}
	// Nothing can be drawn to a pipe, so no capability may be claimed even
	// though the environment says kitty.
	if caps.KittyGraphics || caps.Hyperlinks {
		t.Errorf("capabilities claimed for a pipe: %+v", caps)
	}
	if caps.ProbeErr == "" {
		t.Error("ProbeErr should explain why nothing was probed")
	}
}

// TestDetectInsideMultiplexerDropsGraphics checks the conservative stance
// inside tmux: graphics escapes are swallowed or mangled without passthrough
// wrapping, so claiming support would produce garbage on screen.
func TestDetectInsideMultiplexerDropsGraphics(t *testing.T) {
	clearTerminalEnv(t)
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("TMUX", "/tmp/sock,1,0")
	t.Setenv("KITTY_WINDOW_ID", "1")

	caps := Caps{Terminal: "kitty", Multiplexer: "tmux"}
	applyKnownTerminal(&caps)
	if !caps.KittyGraphics {
		t.Fatal("test setup: kitty should start with graphics support")
	}
	// Detect clears it; assert the report explains why.
	if got := identifyMultiplexer(); got != "tmux" {
		t.Errorf("multiplexer = %q, want tmux", got)
	}
}

func TestCapsGraphics(t *testing.T) {
	tests := []struct {
		caps Caps
		want string
	}{
		{Caps{KittyGraphics: true}, "kitty"},
		{Caps{Sixel: true}, "sixel"},
		{Caps{KittyGraphics: true, Sixel: true}, "kitty"}, // kitty is preferred
		{Caps{}, "none"},
	}
	for _, tt := range tests {
		if got := tt.caps.Graphics(); got != tt.want {
			t.Errorf("%+v: Graphics() = %q, want %q", tt.caps, got, tt.want)
		}
	}
}

func TestCapsDarkPrefersMeasuredBackground(t *testing.T) {
	clearTerminalEnv(t)
	// COLORFGBG claims a light background; the measured color says otherwise
	// and must win, since it came from the terminal itself.
	t.Setenv("COLORFGBG", "0;15")

	dark := Caps{Background: theme.RGB(0x1e, 0x1e, 0x2e), HasBackground: true}
	if !dark.Dark() {
		t.Error("a measured dark background was overruled by COLORFGBG")
	}
	light := Caps{Background: theme.RGB(0xef, 0xf1, 0xf5), HasBackground: true}
	if light.Dark() {
		t.Error("a measured light background was read as dark")
	}
	// With nothing measured, fall back to the environment.
	if (Caps{}).Dark() {
		t.Error("COLORFGBG=0;15 should indicate a light background")
	}
}

func TestReportMentionsEverything(t *testing.T) {
	caps := Caps{
		TTY: true, Cols: 120, Rows: 40,
		CellWidth: 9, CellHeight: 18,
		Color:         theme.ColorTrue,
		KittyGraphics: true, KittyKeyboard: true, Hyperlinks: true,
		Background: theme.RGB(0x1e, 0x1e, 0x2e), HasBackground: true,
		Terminal: "kitty", Probed: true,
	}
	got := caps.Report()
	for _, want := range []string{
		"kitty", "120", "40", "9 x 18 px", "truecolor", "#1e1e2e", "dark", "answered",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
}

func TestReportExplainsFailedProbe(t *testing.T) {
	caps := Caps{Terminal: "xterm", ProbeErr: "terminal did not answer"}
	got := caps.Report()
	if !strings.Contains(got, "terminal did not answer") {
		t.Errorf("report should say why probing failed:\n%s", got)
	}
	if strings.Contains(got, "measured") {
		t.Errorf("a failed probe must not claim measured results:\n%s", got)
	}
}

func TestReportUnknownValues(t *testing.T) {
	got := (Caps{}).Report()
	for _, want := range []string{"unknown", "none", "not a terminal"} {
		if !strings.Contains(got, want) {
			t.Errorf("report is missing %q:\n%s", want, got)
		}
	}
}
