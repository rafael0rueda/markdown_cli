package term

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseTmuxClient(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want tmuxClient
	}{
		{"kitty by TERM", "xterm-kitty\t\t256,RGB,title\ton\n",
			tmuxClient{terminal: "kitty", passthrough: true}},
		{"kitty by its answer", "xterm-256color\tkitty(0.47.1)\t\tall\n",
			tmuxClient{terminal: "kitty", passthrough: true}},
		{"ghostty", "xterm-ghostty\tghostty 1.2.0\thyperlinks,sixel\ton\n",
			tmuxClient{terminal: "ghostty", passthrough: true, sixel: true, hyperlinks: true}},
		{"passthrough off", "xterm-kitty\tkitty(0.47.1)\t\toff\n",
			tmuxClient{terminal: "kitty"}},
		// Before tmux 3.3 there was no option, and nothing was held back.
		{"tmux before the option", "xterm-kitty\t\t\t\n",
			tmuxClient{terminal: "kitty", passthrough: true}},
		{"a terminal without placeholders", "xterm-256color\tWezTerm 20240203\tsixel\ton\n",
			tmuxClient{passthrough: true, sixel: true}},
		{"no client attached", "\t\t\ton\n", tmuxClient{passthrough: true}},
	}
	for _, tt := range tests {
		if got := parseTmuxClient(tt.out); got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func TestApplyTmux(t *testing.T) {
	var caps Caps
	applyTmux(&caps, tmuxClient{terminal: "kitty", passthrough: true, hyperlinks: true}, nil)
	if !caps.KittyGraphics || !caps.Passthrough || caps.Terminal != "kitty" || !caps.Hyperlinks || caps.Note != "" {
		t.Errorf("kitty with passthrough: %+v", caps)
	}

	caps = Caps{}
	applyTmux(&caps, tmuxClient{terminal: "kitty"}, nil)
	if caps.KittyGraphics || !strings.Contains(caps.Note, "allow-passthrough on") {
		t.Errorf("kitty without passthrough should say how to allow it: %+v", caps)
	}

	caps = Caps{Terminal: "tmux"}
	applyTmux(&caps, tmuxClient{passthrough: true}, nil)
	if caps.KittyGraphics || caps.Terminal != "tmux" || caps.Note != "" {
		t.Errorf("an unknown terminal: %+v", caps)
	}

	caps = Caps{Hyperlinks: true}
	applyTmux(&caps, tmuxClient{}, errors.New("no server"))
	if caps.KittyGraphics || caps.Note == "" || !caps.Hyperlinks {
		t.Errorf("tmux not answering should leave the rest alone and say so: %+v", caps)
	}
}

// fakeTmux stands in for the tmux command, recording the arguments.
func fakeTmux(t *testing.T, out string, err error) *[]string {
	t.Helper()
	var args []string
	saved := runTmux
	runTmux = func(_ context.Context, a ...string) (string, error) {
		args = a
		return out, err
	}
	t.Cleanup(func() { runTmux = saved })
	return &args
}

// TestQueryTmuxAsksAboutThisPane: passthrough is set per pane, so the answer
// has to be about the pane mdv is in rather than whichever is active.
func TestQueryTmuxAsksAboutThisPane(t *testing.T) {
	t.Setenv("TMUX_PANE", "%7")
	args := fakeTmux(t, "xterm-kitty\t\t\ton\n", nil)
	client, err := queryTmux(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if client.terminal != "kitty" {
		t.Errorf("got %+v", client)
	}
	if i := slices.Index(*args, "-t"); i < 0 || i+1 >= len(*args) || (*args)[i+1] != "%7" {
		t.Errorf("tmux was not pointed at the pane: %q", *args)
	}
}
