//go:build unix

package term

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
	xterm "golang.org/x/term"
)

// directQueries is the string sent to the terminal to ask what it supports.
//
// Everything goes out in one write and the replies are read back together.
// The order matters only at the end: primary device attributes is last
// because every terminal answers it, so its reply marks the point where all
// earlier queries have been processed. Without that terminator there would be
// no way to distinguish "does not support graphics" from "has not replied
// yet", and the only option would be to always wait out the full timeout.
const directQueries = "" +
	// Kitty graphics: transmit a one-pixel RGB image in query mode. A
	// supporting terminal replies ESC _ G i=31;OK ST and displays nothing.
	"\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" +
	tmuxQueries

// tmuxQueries is what can be asked from inside tmux. It leaves out the kitty
// graphics query, which tmux would take for a new pane title; tmux itself says
// whether images can get through.
const tmuxQueries = "" +
	// Kitty keyboard protocol: ask for the current flags.
	"\x1b[?u" +
	// Background color.
	"\x1b]11;?\x1b\\" +
	// Cell size in pixels.
	"\x1b[16t" +
	// Primary device attributes, the terminator.
	"\x1b[c"

// probe asks the terminal what it supports and parses the replies.
//
// It talks to /dev/tty rather than to stdin and stdout, so it works when
// either has been redirected - which is the common case, since markdown is
// often piped in and the output often piped onward.
func probe(queries string, timeout time.Duration) (response, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return response{}, errors.New("no controlling terminal")
	}
	defer tty.Close()
	return queryTTY(tty, queries, timeout)
}

// queryTTY writes the queries to an open terminal and reads back the replies.
// It is separated from opening /dev/tty so tests can drive it with a
// pseudo-terminal on the other end.
func queryTTY(tty *os.File, queries string, timeout time.Duration) (response, error) {
	fd := int(tty.Fd())

	// Raw mode is required so replies arrive as bytes instead of being eaten
	// by line editing, and so they are not echoed back to the user.
	state, err := xterm.MakeRaw(fd)
	if err != nil {
		return response{}, err
	}
	defer xterm.Restore(fd, state)

	if _, err := tty.WriteString(queries); err != nil {
		return response{}, err
	}
	return readReplies(fd, timeout)
}

// readReplies collects bytes until the device attributes reply arrives or the
// deadline passes.
func readReplies(fd int, timeout time.Duration) (response, error) {
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 0, 1024)
	chunk := make([]byte, 256)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}

		// Poll before reading so the wait can be bounded. A blocking read on
		// the tty could not be cancelled, and abandoning a goroutine inside
		// one would leave it holding the user's next keystroke.
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, int(remaining.Milliseconds()))
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return parseResponses(buf), err
		}
		if n == 0 {
			break // timed out
		}

		count, err := unix.Read(fd, chunk)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				continue
			}
			return parseResponses(buf), err
		}
		if count <= 0 {
			break
		}
		buf = append(buf, chunk[:count]...)

		// Stop as soon as the terminator has arrived rather than waiting out
		// the full timeout on every run.
		if res := parseResponses(buf); res.deviceAttrs {
			return res, nil
		}
	}
	return parseResponses(buf), nil
}

// pixelSize reports the terminal window's size in pixels, which divided by the
// cell grid gives the size of one cell. Many terminals report zero here, in
// which case the CSI 16 t query is the fallback.
func pixelSize(f *os.File) (width, height int, ok bool) {
	if f == nil {
		return 0, 0, false
	}
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return 0, 0, false
	}
	return int(ws.Xpixel), int(ws.Ypixel), true
}
