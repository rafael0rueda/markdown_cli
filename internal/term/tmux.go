package term

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// Inside tmux the terminal cannot be asked much directly. tmux answers most
// queries itself, discards the rest, and treats the kitty graphics query as a
// request to retitle the pane. The environment is no better: it describes the
// terminal the tmux server was started from, which may not be the one attached
// now. tmux itself knows, though, so mdv asks it.

// tmuxClient is what tmux reports about the terminal attached to it.
type tmuxClient struct {
	// terminal names the emulator outside tmux when it is one mdv knows how
	// to draw images on through tmux, and is empty otherwise.
	terminal string
	// passthrough reports whether tmux forwards the sequences wrapped for it,
	// which kitty graphics need.
	passthrough bool
	// sixel and hyperlinks report whether tmux relays those to the terminal,
	// which it does itself, given a terminal that has them.
	sixel, hyperlinks bool
}

// tmuxFormat asks for the fields a tmuxClient is read from, tab-separated.
//
// allow-passthrough expands to nothing on tmux before 3.3, which had no such
// option and passed everything through.
const tmuxFormat = "#{client_termname}\t#{client_termtype}\t#{client_termfeatures}\t#{allow-passthrough}"

// runTmux runs a tmux command against the server mdv is running under. Tests
// replace it.
var runTmux = func(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", args...).Output()
	return string(out), err
}

// queryTmux asks tmux about the terminal attached to the pane mdv is in.
func queryTmux(timeout time.Duration) (tmuxClient, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{"display-message", "-p"}
	// Passthrough is set per pane, and without a target tmux reports on
	// whichever pane is active, which need not be this one.
	if pane := os.Getenv("TMUX_PANE"); pane != "" {
		args = append(args, "-t", pane)
	}
	out, err := runTmux(ctx, append(args, tmuxFormat)...)
	if err != nil {
		return tmuxClient{}, err
	}
	return parseTmuxClient(out), nil
}

// parseTmuxClient reads the answer to tmuxFormat.
func parseTmuxClient(out string) tmuxClient {
	fields := strings.Split(strings.TrimRight(out, "\r\n"), "\t")
	for len(fields) < 4 {
		fields = append(fields, "")
	}
	name, kind, features, passthrough := fields[0], fields[1], fields[2], fields[3]

	var c tmuxClient
	// termname is TERM outside tmux, such as xterm-kitty; termtype is the
	// terminal's own answer to an identity query, such as kitty(0.47.1).
	// Either is enough.
	id := strings.ToLower(name + " " + kind)
	for _, t := range []string{"kitty", "ghostty"} {
		if strings.Contains(id, t) {
			c.terminal = t
			break
		}
	}
	c.passthrough = passthrough == "" || passthrough == "on" || passthrough == "all"
	list := strings.Split(features, ",")
	c.sixel = slices.Contains(list, "sixel")
	c.hyperlinks = slices.Contains(list, "hyperlinks")
	return c
}

// applyTmux fills in the capabilities that depend on tmux and the terminal
// attached to it.
func applyTmux(caps *Caps, client tmuxClient, err error) {
	if err != nil {
		caps.Note = "tmux did not say which terminal it is attached to, so images are left out"
		return
	}
	caps.Hyperlinks = client.hyperlinks
	if client.terminal == "" {
		return
	}
	caps.Terminal = client.terminal
	if !client.passthrough {
		caps.Note = "tmux is not passing images through to " + client.terminal +
			"; `set -g allow-passthrough on` in tmux.conf lets it"
		return
	}
	caps.KittyGraphics = true
	caps.Passthrough = true
}
