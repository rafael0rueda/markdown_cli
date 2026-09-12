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
	// Tmux draws kitty images from inside tmux: through its passthrough, and
	// with Unicode placeholders that tmux keeps in place as text. Sixel needs
	// nothing of the kind, since tmux draws sixel itself.
	Tmux bool

	// nextID hands out kitty image identifiers.
	nextID atomic.Uint32
	seedID sync.Once

	mu       sync.Mutex
	prepared map[prepareKey]preparedEntry
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
	if r.placeholders() {
		// Placeholders can only number so many rows and columns.
		maxCols = min(maxCols, maxPlaceholderCells)
		if maxRows <= 0 || maxRows > maxPlaceholderCells {
			maxRows = maxPlaceholderCells
		}
	}
	geo := r.fit(src, maxCols, maxRows)
	if geo.Cols < 1 || geo.Rows < 1 {
		return 0, 0, errors.New("image is too small to draw")
	}
	return geo.Cols, geo.Rows, nil
}

// Prepared is an image made ready to draw, repeatedly and in pieces.
//
// Preparing is the expensive step - decoding, scaling, and for sixel a median
// cut and a dither - and it depends only on the image and the box it goes in.
// The pager redraws on every scroll step, so that work is done once and each
// frame only asks for the rows it needs.
type Prepared struct {
	protocol Protocol
	cols     int
	rows     int

	kitty *kittyImage
	sixel *sixelImage

	// pixelHeight is the height of the drawable content, which the row crop is
	// converted into.
	pixelHeight int

	// sent records whether the image data has reached the terminal yet. Only
	// the kitty protocol has anything to send ahead of drawing.
	sent bool
	// placement numbers each drawing of this image. Placements are cleared at
	// the start of every frame, so this only has to be unique among the
	// placements alive at one time.
	placement atomic.Uint32
}

// Cols and Rows are the footprint the image was prepared for.
func (p *Prepared) Cols() int { return p.cols }
func (p *Prepared) Rows() int { return p.rows }

// prepareKey identifies a prepared image, so that the same picture drawn at
// the same size is only ever built once.
type prepareKey struct {
	ref        string
	cols, rows int
}

// Prepare makes an image ready to draw in a box of cols by rows cells.
func (r *Renderer) Prepare(ref string, cols, rows int) (*Prepared, error) {
	if !r.Enabled() {
		return nil, errors.New("images are disabled")
	}
	key := prepareKey{ref: ref, cols: cols, rows: rows}

	r.mu.Lock()
	if p, ok := r.prepared[key]; ok {
		r.mu.Unlock()
		return p.prep, p.err
	}
	r.mu.Unlock()

	prep, err := r.prepare(ref, cols, rows)

	r.mu.Lock()
	if r.prepared == nil {
		r.prepared = make(map[prepareKey]preparedEntry)
	}
	r.prepared[key] = preparedEntry{prep: prep, err: err}
	r.mu.Unlock()

	return prep, err
}

type preparedEntry struct {
	prep *Prepared
	err  error
}

func (r *Renderer) prepare(ref string, cols, rows int) (*Prepared, error) {
	src, err := r.Loader.Load(ref)
	if err != nil {
		return nil, err
	}
	geo := r.fit(src, cols, rows)
	geo.Cols, geo.Rows = cols, rows

	p := &Prepared{protocol: r.Protocol, cols: cols, rows: rows}

	switch r.Protocol {
	case Kitty:
		img, err := prepareKitty(src, geo, r.imageID())
		if err != nil {
			return nil, err
		}
		img.tmux = r.Tmux
		p.kitty, p.pixelHeight = img, img.pixelHeight

	case Sixel:
		img, err := prepareSixel(src, geo)
		if err != nil {
			return nil, err
		}
		if img == nil {
			return nil, errors.New("image is entirely transparent")
		}
		p.sixel, p.pixelHeight = img, img.height

	default:
		return nil, errors.New("no image protocol selected")
	}
	return p, nil
}

// draw returns the sequence that renders rows [skip, skip+visible) of the
// image at the cursor, indented by indent columns, without any surrounding
// cursor movement.
//
// Cropping in rows is what lets an image be scrolled through rather than
// appearing and disappearing whole. The row range is converted to pixels
// against the prepared height, so it stays correct whatever the terminal's
// cell size turned out to be.
func (p *Prepared) draw(skip, visible, indent int) string {
	if p == nil || visible < 1 || skip >= p.rows {
		return ""
	}
	visible = min(visible, p.rows-skip)

	if p.protocol == Kitty && p.kitty.tmux {
		// Placeholder cells name their rows, so cropping is just leaving
		// the others out.
		var b strings.Builder
		if !p.sent {
			b.WriteString(p.kitty.transmit())
			b.WriteString(p.kitty.wrap(p.kitty.virtualPlacement(p.cols, p.rows)))
			p.sent = true
		}
		b.WriteString(p.kitty.placeholders(p.cols, skip, visible, indent))
		return b.String()
	}

	top := skip * p.pixelHeight / p.rows
	bottom := (skip + visible) * p.pixelHeight / p.rows
	if bottom <= top {
		return ""
	}

	switch p.protocol {
	case Kitty:
		var b strings.Builder
		if !p.sent {
			b.WriteString(p.kitty.transmit())
			p.sent = true
		}
		b.WriteString(p.kitty.place(p.placement.Add(1)&idMask, p.cols, visible, top, bottom-top))
		return b.String()
	case Sixel:
		return p.sixel.encode(top, bottom)
	}
	return ""
}

// ClearPlacements returns the sequence that removes the images currently on
// screen without discarding the data behind them, so the next frame can draw
// them again without retransmitting.
//
// Sixel has no notion of a placement - the pixels were written into the screen
// like text - so there is nothing to clear and redrawing the frame is what
// erases them. Placeholders are text outright, and go the same way.
func (r *Renderer) ClearPlacements() string {
	if r != nil && r.Protocol == Kitty && !r.Tmux {
		return kittyClearPlacements
	}
	return ""
}

// placeholders reports whether images are drawn as placeholder cells.
func (r *Renderer) placeholders() bool {
	return r.Protocol == Kitty && r.Tmux
}

// DrawCropped returns the escape sequence drawing part of an image at the
// cursor, for a caller that positions the cursor itself. The pager uses this;
// it does no cursor movement of its own beyond what the protocol requires,
// and indent is where the image's rows after the first begin.
func (r *Renderer) DrawCropped(ref string, cols, rows, skip, visible, indent int) (string, error) {
	prep, err := r.Prepare(ref, cols, rows)
	if err != nil {
		return "", err
	}
	seq := prep.draw(skip, visible, indent)
	if seq == "" {
		return "", errors.New("nothing to draw")
	}
	return seq, nil
}

// Encode returns the escape sequence that draws a whole image in a box of
// exactly cols by rows cells, indented by the given columns, including the
// cursor movement to step past it.
//
// Passing back the footprint Measure produced reproduces the same geometry:
// Fit never chooses a size larger than the box it is given, and the box here
// is the one it chose last time, so the constraint is no tighter than before.
func (r *Renderer) Encode(ref string, cols, rows, indent int) (string, error) {
	if !r.Enabled() {
		return "", errors.New("images are disabled")
	}
	prep, err := r.Prepare(ref, cols, rows)
	if err != nil {
		return "", err
	}
	payload := prep.draw(0, rows, indent)
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
