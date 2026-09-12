//go:build linux

package term

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY returns a connected pseudo-terminal pair. The slave end is a real
// terminal as far as the kernel is concerned, so raw mode, poll and read all
// behave exactly as they would against a user's terminal - which is the point:
// it exercises the probe's actual I/O path rather than a mock of it.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()

	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("cannot open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() { master.Close() })

	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlocking pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("getting pty number: %v", err)
	}

	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("opening pty slave: %v", err)
	}
	t.Cleanup(func() { slave.Close() })

	return master, slave
}

// fakeTerminal reads the query from the master end and answers with reply,
// after an optional delay. It reports the query it saw.
func fakeTerminal(t *testing.T, master *os.File, reply string, delay time.Duration) <-chan string {
	t.Helper()
	seen := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, err := master.Read(buf)
		if err != nil {
			seen <- ""
			return
		}
		seen <- string(buf[:n])
		if delay > 0 {
			time.Sleep(delay)
		}
		if reply != "" {
			master.WriteString(reply)
		}
	}()
	return seen
}

func TestProbeAgainstSupportingTerminal(t *testing.T) {
	master, slave := openPTY(t)
	reply := replyKittyOK + replyKeyboard + replyBGDark + replyCellSize + replyDASixel
	seen := fakeTerminal(t, master, reply, 0)

	got, err := queryTTY(slave, directQueries, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if !got.deviceAttrs {
		t.Error("device attributes not seen")
	}
	if !got.kittyGraphics {
		t.Error("kitty graphics not detected")
	}
	if !got.kittyKeyboard {
		t.Error("kitty keyboard not detected")
	}
	if !got.sixel {
		t.Error("sixel not detected")
	}
	if !got.hasBackground {
		t.Error("background not read")
	}
	if got.cellWidth != 9 || got.cellHeight != 18 {
		t.Errorf("cell size = %dx%d, want 9x18", got.cellWidth, got.cellHeight)
	}

	query := <-seen
	if !strings.HasSuffix(query, "\x1b[c") {
		t.Errorf("device attributes must be sent last so it terminates the exchange; got %q", query)
	}
}

// TestProbeAgainstSilentTerminal is the case that motivates the timeout: a
// terminal that ignores everything. It must return promptly and claim nothing.
func TestProbeAgainstSilentTerminal(t *testing.T) {
	master, slave := openPTY(t)
	fakeTerminal(t, master, "", 0)

	start := time.Now()
	got, err := queryTTY(slave, directQueries, 150*time.Millisecond)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatal(err)
	}
	if got.deviceAttrs || got.kittyGraphics || got.sixel {
		t.Errorf("a silent terminal was credited with support: %+v", got)
	}
	if elapsed > time.Second {
		t.Errorf("took %s to give up, want roughly the 150ms timeout", elapsed)
	}
}

// TestProbeAgainstPlainTerminal covers a terminal that answers but supports
// nothing: the reply proves the absence of graphics rather than leaving it
// unknown.
func TestProbeAgainstPlainTerminal(t *testing.T) {
	master, slave := openPTY(t)
	fakeTerminal(t, master, replyDAPlain, 0)

	got, err := queryTTY(slave, directQueries, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !got.deviceAttrs {
		t.Fatal("device attributes not seen")
	}
	if got.kittyGraphics || got.sixel || got.kittyKeyboard {
		t.Errorf("capabilities claimed without replies: %+v", got)
	}
}

// TestProbeReturnsEarly checks that a fast terminal is not made to wait out
// the whole timeout: output would be delayed by it on every single run.
func TestProbeReturnsEarly(t *testing.T) {
	master, slave := openPTY(t)
	fakeTerminal(t, master, replyKittyOK+replyDASixel, 0)

	start := time.Now()
	if _, err := queryTTY(slave, directQueries, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("waited %s after the terminator arrived", elapsed)
	}
}

// TestProbeToleratesSlowTerminal stands in for a terminal at the far end of an
// ssh link, replying well after the query but inside the budget.
func TestProbeToleratesSlowTerminal(t *testing.T) {
	master, slave := openPTY(t)
	fakeTerminal(t, master, replyKittyOK+replyDASixel, 120*time.Millisecond)

	got, err := queryTTY(slave, directQueries, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !got.kittyGraphics || !got.deviceAttrs {
		t.Errorf("a slow but valid reply was missed: %+v", got)
	}
}

// TestProbeRestoresTerminalState is what keeps a user's shell usable: the
// probe puts the terminal into raw mode and must put it back, including when
// it gives up on a timeout.
func TestProbeRestoresTerminalState(t *testing.T) {
	master, slave := openPTY(t)
	fakeTerminal(t, master, "", 0)

	before, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Skipf("cannot read termios: %v", err)
	}

	if _, err := queryTTY(slave, directQueries, 100*time.Millisecond); err != nil {
		t.Fatal(err)
	}

	after, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	if *before != *after {
		t.Errorf("terminal was left in a different mode\nbefore: %+v\nafter:  %+v", before, after)
	}
}

// TestProbeRepliesArrivingInPieces covers a reply split across reads, which is
// what happens when the terminal writes each answer separately or the link
// fragments them.
func TestProbeRepliesArrivingInPieces(t *testing.T) {
	master, slave := openPTY(t)

	go func() {
		buf := make([]byte, 4096)
		if _, err := master.Read(buf); err != nil {
			return
		}
		for _, part := range []string{
			replyKittyOK[:5], replyKittyOK[5:],
			replyKeyboard,
			replyDASixel[:4], replyDASixel[4:],
		} {
			master.WriteString(part)
			time.Sleep(5 * time.Millisecond)
		}
	}()

	got, err := queryTTY(slave, directQueries, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !got.kittyGraphics || !got.kittyKeyboard || !got.sixel || !got.deviceAttrs {
		t.Errorf("fragmented replies were not reassembled: %+v", got)
	}
}

// TestPixelSizeOnPTY exercises the ioctl path. A fresh pty reports no pixel
// dimensions, which is exactly the "unknown" case the caller must handle.
func TestPixelSizeOnPTY(t *testing.T) {
	_, slave := openPTY(t)
	if _, _, ok := pixelSize(slave); ok {
		t.Log("pty reported pixel dimensions, unusual but not wrong")
	}

	want := unix.Winsize{Row: 24, Col: 80, Xpixel: 720, Ypixel: 432}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), unix.TIOCSWINSZ, &want); err != nil {
		t.Skipf("cannot set window size: %v", err)
	}
	w, h, ok := pixelSize(slave)
	if !ok {
		t.Fatal("pixel size not reported after being set")
	}
	if w != 720 || h != 432 {
		t.Errorf("got %dx%d, want 720x432", w, h)
	}
}

func TestProbeWithoutControllingTerminal(t *testing.T) {
	// The package-level probe opens /dev/tty; under `go test` there may or may
	// not be one, so this only asserts it does not panic or hang.
	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := probe(directQueries, 50*time.Millisecond); err != nil && !errors.Is(err, io.EOF) {
			t.Logf("probe returned: %v", err)
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("probe did not return")
	}
}
