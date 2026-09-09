package pager

import (
	"os"
)

// readKeys decodes keypresses from the terminal onto a channel.
//
// Reading happens on its own goroutine because the main loop also has to react
// to window-resize signals, and there is no portable way to wait on a file
// descriptor and a signal at once. The goroutine ends when the terminal is
// closed, which the screen does on teardown.
func readKeys(tty *os.File) (<-chan key, func()) {
	out := make(chan key, 16)
	done := make(chan struct{})

	go func() {
		defer close(out)

		buf := make([]byte, 0, 256)
		chunk := make([]byte, 128)

		for {
			n, err := tty.Read(chunk)
			if n > 0 {
				buf = append(buf, chunk[:n]...)
				// A single read can carry several keypresses, and an escape
				// sequence can be split across reads, so decode as much as is
				// complete and keep the remainder.
				for len(buf) > 0 {
					k, used, ok := decodeKey(buf)
					if !ok {
						break // incomplete; wait for more
					}
					buf = buf[used:]
					select {
					case out <- k:
					case <-done:
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return out, func() { close(done) }
}
