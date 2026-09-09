//go:build unix

package pager

import (
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

// watchResize reports when the terminal window changes size.
//
// The channel is buffered and sends are dropped when it is full: during a drag
// the signal arrives continuously, and only the most recent size matters.
func watchResize() (<-chan struct{}, func()) {
	raw := make(chan os.Signal, 1)
	signal.Notify(raw, unix.SIGWINCH)

	out := make(chan struct{}, 1)
	done := make(chan struct{})

	go func() {
		defer close(out)
		for {
			select {
			case <-raw:
				select {
				case out <- struct{}{}:
				default:
				}
			case <-done:
				return
			}
		}
	}()

	return out, func() {
		signal.Stop(raw)
		close(done)
	}
}
