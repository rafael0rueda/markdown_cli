package graphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"strings"
)

// kittyChunkSize is the largest payload the protocol allows in one escape
// sequence. Larger images are split across several, chained with the m key.
const kittyChunkSize = 4096

// kittyClearPlacements removes every image placement currently on screen,
// leaving the transmitted image data in place so it can be drawn again without
// being sent a second time. The pager emits this at the start of each frame.
const kittyClearPlacements = "\x1b_Ga=d,d=a\x1b\\"

// kittyImage is a PNG made ready for the terminal to draw.
//
// The protocol separates sending an image from showing it, and that separation
// is what makes scrolling affordable: the picture is transmitted once, and
// every redraw afterwards is a short placement command rather than a fresh
// base64 copy of the whole file.
type kittyImage struct {
	id   uint32
	data []byte
	// tmux sends every command through tmux's passthrough, and draws with
	// placeholders instead of placements.
	tmux bool
	// pixelWidth and pixelHeight describe the transmitted image, which is what
	// the crop offsets are measured against.
	pixelWidth, pixelHeight int
}

// prepareKitty produces the PNG bytes to send for an image.
func prepareKitty(src *Source, geo Geometry, id uint32) (*kittyImage, error) {
	data, width, height, err := kittyPayload(src, geo)
	if err != nil {
		return nil, err
	}
	return &kittyImage{id: id, data: data, pixelWidth: width, pixelHeight: height}, nil
}

// transmit returns the escape sequences that send the image to the terminal
// without displaying it.
//
// q=2 suppresses both the success and the error replies, which would otherwise
// be delivered to the terminal's input and end up in the user's shell.
func (k *kittyImage) transmit() string {
	encoded := base64.StdEncoding.EncodeToString(k.data)
	controls := fmt.Sprintf("a=t,f=100,t=d,i=%d,q=2", k.id)

	var b strings.Builder
	for first := true; len(encoded) > 0; first = false {
		chunk := encoded
		if len(chunk) > kittyChunkSize {
			chunk = chunk[:kittyChunkSize]
		}
		encoded = encoded[len(chunk):]

		more := 0
		if len(encoded) > 0 {
			more = 1
		}

		start := ""
		if first {
			start = controls + ","
		}
		// Each chunk is wrapped on its own, keeping every sequence tmux has to
		// buffer as small as the protocol's.
		b.WriteString(k.wrap(fmt.Sprintf("\x1b_G%sm=%d;%s\x1b\\", start, more, chunk)))
	}
	return b.String()
}

// wrap prepares a command for the way it reaches the terminal.
func (k *kittyImage) wrap(seq string) string {
	if k.tmux {
		return tmuxPassthrough(seq)
	}
	return seq
}

// place returns the sequence that draws an already-transmitted image at the
// cursor, in a box of cols by rows cells.
//
// srcTop and srcHeight select a horizontal band of the source image in pixels,
// which is how an image scrolled half off the top of the screen is drawn: the
// hidden part is cropped away and what remains is placed in the rows that are
// actually visible.
//
// C=1 stops the terminal moving the cursor, leaving the caller in control of
// the layout.
func (k *kittyImage) place(placementID uint32, cols, rows, srcTop, srcHeight int) string {
	crop := ""
	if srcTop > 0 || srcHeight < k.pixelHeight {
		crop = fmt.Sprintf(",y=%d,h=%d", srcTop, srcHeight)
	}
	// The placement identifier is given explicitly rather than left for the
	// terminal to assign, so that one image drawn twice - the same picture
	// appearing in two places in a document - is two placements for certain
	// rather than depending on how unnamed ones are treated.
	return fmt.Sprintf("\x1b_Ga=p,i=%d,p=%d,c=%d,r=%d%s,C=1,q=2\x1b\\",
		k.id, placementID, cols, rows, crop)
}

// kittyPayload produces the PNG bytes to transmit, and the pixel dimensions of
// what they contain.
//
// A PNG that is already no larger than its display size is sent untouched,
// which is both faster and lossless. Anything else is scaled and re-encoded.
func kittyPayload(src *Source, geo Geometry) (data []byte, width, height int, err error) {
	if src.Format == "png" && src.Width <= geo.PixelWidth && src.Height <= geo.PixelHeight {
		return src.Data, src.Width, src.Height, nil
	}
	img := fitForTransfer(src.Image, geo)

	var buf bytes.Buffer
	// Best speed rather than best compression: this runs while the user waits
	// for output, and the data is going a few centimetres down a pipe.
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, 0, 0, fmt.Errorf("encoding png for transfer: %w", err)
	}
	bounds := img.Bounds()
	return buf.Bytes(), bounds.Dx(), bounds.Dy(), nil
}
