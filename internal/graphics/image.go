// Package graphics turns images into escape sequences a terminal can draw.
//
// Two wire protocols are supported. The kitty graphics protocol is preferred
// where it exists: it takes the encoded file more or less as-is, scales it
// itself, and composites it over the text without disturbing the grid. Sixel
// is the fallback, and is a great deal more work - the image has to be scaled,
// reduced to a 256-color palette and dithered before it can be sent.
package graphics

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	// Registering the decoders is the whole reason for these imports.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const (
	// maxFileBytes caps how much will be read for a single image. Documents
	// are not always trusted input, and a viewer should not be able to be
	// pointed at a huge file and made to exhaust memory.
	maxFileBytes = 64 << 20

	// maxPixels caps the decoded dimensions. A small file can decode to an
	// enormous bitmap, so the header is inspected before committing to the
	// decode.
	maxPixels = 50 << 20

	// httpTimeout bounds fetching a remote image, which only happens when the
	// user has explicitly opted in.
	httpTimeout = 10 * time.Second
)

// Source is a decoded image together with the bytes it was decoded from.
//
// The encoded form is kept because kitty can take PNG directly, which avoids
// re-encoding and lets the terminal do the scaling. Sixel needs the decoded
// pixels instead.
type Source struct {
	Ref    string
	Format string
	Data   []byte
	Image  image.Image
	Width  int
	Height int
}

// Loader reads and decodes images, remembering what it has already done.
//
// The same image is looked at twice in a normal run - once to measure how many
// cells it needs during layout, once to encode it during output - and a
// document may well use the same image more than once. Decoding is by far the
// most expensive step, so results are cached.
type Loader struct {
	// AllowRemote permits fetching http and https URLs. It is off by default:
	// rendering a document should not make network requests that tell a third
	// party your address and when you read it.
	AllowRemote bool

	mu    sync.Mutex
	cache map[string]*cacheEntry
}

type cacheEntry struct {
	src *Source
	err error
}

// Load decodes the image at ref, which may be a filesystem path or, when
// remote images are enabled, an http or https URL.
func (l *Loader) Load(ref string) (*Source, error) {
	l.mu.Lock()
	if entry, ok := l.cache[ref]; ok {
		l.mu.Unlock()
		return entry.src, entry.err
	}
	l.mu.Unlock()

	src, err := l.load(ref)

	l.mu.Lock()
	if l.cache == nil {
		l.cache = make(map[string]*cacheEntry)
	}
	l.cache[ref] = &cacheEntry{src: src, err: err}
	l.mu.Unlock()

	return src, err
}

func (l *Loader) load(ref string) (*Source, error) {
	data, err := l.read(ref)
	if err != nil {
		return nil, err
	}

	// Check the dimensions from the header before decoding, so a small file
	// claiming to be enormous is rejected rather than allocated.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("unsupported image format: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errors.New("image has no size")
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxPixels {
		return nil, fmt.Errorf("image is too large: %dx%d pixels", cfg.Width, cfg.Height)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}
	bounds := img.Bounds()

	return &Source{
		Ref:    ref,
		Format: format,
		Data:   data,
		Image:  img,
		Width:  bounds.Dx(),
		Height: bounds.Dy(),
	}, nil
}

// read fetches the raw bytes for a reference.
func (l *Loader) read(ref string) ([]byte, error) {
	if isRemote(ref) {
		if !l.AllowRemote {
			return nil, errors.New("remote images are disabled")
		}
		return fetch(ref)
	}

	f, err := os.Open(ref)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err == nil && info.Size() > maxFileBytes {
		return nil, fmt.Errorf("image is too large: %d bytes", info.Size())
	}
	// The limit is applied to the read as well, since the stat above says
	// nothing about a named pipe or a file being written concurrently.
	return io.ReadAll(io.LimitReader(f, maxFileBytes))
}

// fetch retrieves a remote image.
func fetch(url string) ([]byte, error) {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching image: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxFileBytes))
}

// isRemote reports whether a reference names a network location.
func isRemote(ref string) bool {
	lower := strings.ToLower(ref)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}
