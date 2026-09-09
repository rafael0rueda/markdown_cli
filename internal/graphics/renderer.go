package graphics

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rafael0rueda/markdown_cli/internal/render"
)

// Protocol is how images are drawn.
type Protocol int

const (
	// None draws no images; the caller falls back to alt text.
	None Protocol = iota
	// Kitty uses the kitty graphics protocol.
	Kitty
	// Sixel uses sixel graphics.
	Sixel
)

func (p Protocol) String() string {
	switch p {
	case Kitty:
		return "kitty"
	case Sixel:
		return "sixel"
	}
	return "none"
}

// ParseProtocol maps an --images flag value onto a protocol, reporting whether
// the choice should follow the detected terminal capabilities.
func ParseProtocol(s string) (p Protocol, auto bool, err error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "auto":
		return None, true, nil
	case "none", "off", "never", "alt":
		return None, false, nil
	case "kitty":
		return Kitty, false, nil
	case "sixel":
		return Sixel, false, nil
	}
	return None, false, fmt.Errorf("unknown image mode %q (want auto, none, kitty or sixel)", s)
}

// Renderer measures and encodes the images in a document.
type Renderer struct {
	// Protocol selects the wire format. None disables images entirely.
	Protocol Protocol
	// Loader reads and decodes the image files.
	Loader *Loader
	// CellWidth and CellHeight are the pixel size of a terminal cell. Zero
	// means unknown, and the defaults are used.
	CellWidth, CellHeight int

	// nextID hands out kitty image identifiers.
	nextID atomic.Uint32
	seedID sync.Once
}

// imageID returns the next kitty image identifier.
//
// Identifiers are global to the terminal session, not to this process, and
// transmitting an image under an existing one replaces it. Counting from one
// would therefore make a second run of mdv quietly erase the pictures from the
// first as soon as it drew its own - visible as images disappearing from the
// scrollback. Starting from a random base makes that collision unlikely.
func (r *Renderer) imageID() uint32 {
	r.seedID.Do(func() {
		var buf [4]byte
		if _, err := rand.Read(buf[:]); err != nil {
			// A predictable base is still better than always starting at one,
			// and there is nothing useful to do with this error.
			r.nextID.Store(uint32(time.Now().UnixNano()) & idMask)
			return
		}
		r.nextID.Store(binary.BigEndian.Uint32(buf[:]) & idMask)
	})
	// Kitty treats identifiers as 32-bit, but keeping them well clear of the
	// top of the range leaves room to increment without wrapping.
	return r.nextID.Add(1)&idMask + 1
}

// idMask keeps generated identifiers inside a 24-bit range.
const idMask = 0xffffff

// Enabled reports whether this renderer will draw anything.
func (r *Renderer) Enabled() bool {
	return r != nil && r.Protocol != None && r.Loader != nil
}

// Measure reports the cell footprint an image needs.
//
// An error means the image cannot be drawn - it is missing, or in a format
// that cannot be decoded - and the caller should fall back to showing the alt
// text. That is a normal outcome, not a failure of the render.
func (r *Renderer) Measure(ref string, maxCols, maxRows int, hint render.SizeHint) (cols, rows int, err error) {
	if !r.Enabled() {
		return 0, 0, errors.New("images are disabled")
	}
	src, err := r.Loader.Load(ref)
	if err != nil {
		return 0, 0, err
	}
	maxCols, maxRows = r.applyHint(maxCols, maxRows, hint)
	geo := r.fit(src, maxCols, maxRows)
	if geo.Cols < 1 || geo.Rows < 1 {
		return 0, 0, errors.New("image is too small to draw")
	}
	return geo.Cols, geo.Rows, nil
}

// Encode returns the escape sequence that draws the image in a box of exactly
// cols by rows cells, indented by the given number of columns, including the
// cursor movement to step past it.
//
// Passing back the footprint Measure produced reproduces the same geometry:
// Fit never chooses a size larger than the box it is given, and the box here
// is the one it chose last time, so the constraint is no tighter than before.
func (r *Renderer) Encode(ref string, cols, rows, indent int) (string, error) {
	if !r.Enabled() {
		return "", errors.New("images are disabled")
	}
	src, err := r.Loader.Load(ref)
	if err != nil {
		return "", err
	}
	geo := r.fit(src, cols, rows)
	geo.Cols, geo.Rows = cols, rows

	var payload string
	switch r.Protocol {
	case Kitty:
		payload, err = encodeKitty(src, geo, r.imageID())
	case Sixel:
		payload, err = encodeSixel(src, geo)
	default:
		return "", errors.New("no image protocol selected")
	}
	if err != nil {
		return "", err
	}
	if payload == "" {
		return "", errors.New("image encoded to nothing")
	}
	return place(payload, rows, indent), nil
}

// applyHint narrows the box to a size the document asked for.
//
// A hint only ever shrinks the result. Honouring a request for 800 pixels in a
// 40-column terminal would push the image off the right edge, and the document
// author had no way to know how wide the reader's window is.
func (r *Renderer) applyHint(maxCols, maxRows int, hint render.SizeHint) (int, int) {
	cellW, cellH := Constraints{CellWidth: r.CellWidth, CellHeight: r.CellHeight}.cellSize()
	if hint.Width > 0 {
		if cols := ceilDiv(hint.Width, cellW); cols < maxCols {
			maxCols = cols
		}
	}
	if hint.Height > 0 {
		if rows := ceilDiv(hint.Height, cellH); maxRows <= 0 || rows < maxRows {
			maxRows = rows
		}
	}
	return maxCols, maxRows
}

// fit computes the geometry for an image within the given box.
func (r *Renderer) fit(src *Source, maxCols, maxRows int) Geometry {
	return Fit(src.Width, src.Height, Constraints{
		MaxCols:    maxCols,
		MaxRows:    maxRows,
		CellWidth:  r.CellWidth,
		CellHeight: r.CellHeight,
	})
}

// place wraps a graphics payload in the cursor movement that draws it over
// rows lines that the caller has already written.
//
// The rows are written first, as ordinary blank lines, and only then is the
// image drawn back over them. Doing it in that order matters twice. It forces
// any scrolling to happen up front, so the lines the image occupies certainly
// exist and nothing shifts under it. And it means whatever decorates those
// lines - a blockquote bar, a list indent - is on screen before the picture
// lands beside it.
//
// The cursor is saved at the left margin, before the indent is applied, so
// that restoring returns to column zero. Saving after the indent would leave
// every following line shifted right by that many columns.
//
// All the movement is relative, so this holds whether or not the screen
// scrolled.
func place(payload string, rows, indent int) string {
	if rows < 1 {
		rows = 1
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[%dA", rows) // back up to the first reserved line
	b.WriteString("\x1b7")            // save at the left margin
	if indent > 0 {
		// Line up with the surrounding text, so an image inside a list item or
		// a blockquote does not start under the marker or the bar.
		fmt.Fprintf(&b, "\x1b[%dC", indent)
	}
	b.WriteString(payload)
	b.WriteString("\x1b8") // restore, undoing whatever the image did to the cursor
	fmt.Fprintf(&b, "\x1b[%dB", rows)
	return b.String()
}
