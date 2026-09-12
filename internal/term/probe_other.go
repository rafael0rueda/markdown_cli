//go:build !unix

package term

import (
	"errors"
	"os"
	"time"
)

// probe is unavailable off Unix: it needs a controlling terminal at /dev/tty
// and a bounded read. Capabilities fall back to what the environment says.
func probe(string, time.Duration) (response, error) {
	return response{}, errors.New("probing is not supported on this platform")
}

// The queries are only ever sent by the Unix probe.
const directQueries, tmuxQueries = "", ""

// pixelSize is likewise unavailable, so the cell size stays unknown.
func pixelSize(*os.File) (width, height int, ok bool) { return 0, 0, false }
