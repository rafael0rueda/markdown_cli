//go:build linux

package term

import (
	"strings"
	"testing"
)

// TestDetectInsideTmux runs the whole detection on a terminal inside tmux. The
// environment says kitty, as it does when the tmux server was started from
// kitty, but only tmux can say whether images will get through.
func TestDetectInsideTmux(t *testing.T) {
	_, slave := openPTY(t)
	clearTerminalEnv(t)
	t.Setenv("TERM", "tmux-256color")
	t.Setenv("TMUX", "/tmp/tmux-1000/default,1,0")
	t.Setenv("TMUX_PANE", "%1")
	t.Setenv("KITTY_WINDOW_ID", "1")

	fakeTmux(t, "xterm-kitty\tkitty(0.47.1)\t256,RGB\toff\n", nil)
	caps := Detect(DetectOptions{Out: slave, Probe: false})
	if caps.KittyGraphics || caps.Passthrough {
		t.Errorf("graphics claimed with passthrough off: %+v", caps)
	}
	if !strings.Contains(caps.Report(), "allow-passthrough on") {
		t.Error("the report should say how to let images through")
	}

	fakeTmux(t, "xterm-kitty\tkitty(0.47.1)\t256,RGB\ton\n", nil)
	caps = Detect(DetectOptions{Out: slave, Probe: false})
	if !caps.KittyGraphics || !caps.Passthrough || caps.Terminal != "kitty" {
		t.Errorf("kitty through tmux not detected: %+v", caps)
	}
	if !strings.Contains(caps.Report(), "kitty, through tmux") {
		t.Errorf("the report should say images go through tmux:\n%s", caps.Report())
	}
}

// TestDetectInsideScreenClaimsNoGraphics: screen cannot pass images on, whatever
// the environment says about the terminal - including with probing off, which
// used to return before that was taken into account.
func TestDetectInsideScreenClaimsNoGraphics(t *testing.T) {
	_, slave := openPTY(t)
	clearTerminalEnv(t)
	t.Setenv("TERM", "screen-256color")
	t.Setenv("STY", "1234.pts-0")
	t.Setenv("KITTY_WINDOW_ID", "1")

	for _, probe := range []bool{false, true} {
		caps := Detect(DetectOptions{Out: slave, Probe: probe})
		if caps.KittyGraphics || caps.Sixel {
			t.Errorf("probe=%v: graphics claimed inside screen: %+v", probe, caps)
		}
	}
}

// TestTmuxQueriesLeaveOutGraphics: tmux takes the kitty graphics query for a
// pane title, so it must never be sent from inside tmux.
func TestTmuxQueriesLeaveOutGraphics(t *testing.T) {
	if strings.Contains(tmuxQueries, "\x1b_G") {
		t.Errorf("tmuxQueries holds a graphics command: %q", tmuxQueries)
	}
	if !strings.HasSuffix(tmuxQueries, "\x1b[c") {
		t.Error("the device attributes query has to come last, as the terminator")
	}
}
