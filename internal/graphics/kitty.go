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

// KittyDeleteAll is the sequence that removes every image the terminal is
// holding. The pager uses it when redrawing from scratch.
const KittyDeleteAll = "\x1b_Ga=d,d=A\x1b\\"

// encodeKitty renders an image as kitty graphics protocol escape sequences.
//
// The image is transmitted as PNG, the only compressed format the protocol
// accepts, and is scaled down beforehand to bound how much data is sent. The
// cell footprint is sent as well, so the terminal fits the image to exactly
// the box that was reserved for it during layout - which keeps the picture and
// the text agreeing about where the image ends even when the cell size had to
// be guessed.
func encodeKitty(src *Source, geo Geometry, imageID uint32) (string, error) {
	payload, err := kittyPayload(src, geo)
	if err != nil {
		return "", err
	}

	encoded := base64.StdEncoding.EncodeToString(payload)

	// a=T transmits and displays in one step. C=1 stops the terminal moving
	// the cursor, so the caller stays in control of the layout: mdv has
	// already reserved the rows and will step past them itself. q=2 suppresses
	// both the success and the error replies, which would otherwise be
	// delivered to the terminal's input and end up in the user's shell.
	controls := fmt.Sprintf("a=T,f=100,t=d,i=%d,c=%d,r=%d,C=1,q=2",
		imageID, geo.Cols, geo.Rows)

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

		b.WriteString("\x1b_G")
		if first {
			b.WriteString(controls)
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "m=%d;", more)
		b.WriteString(chunk)
		b.WriteString("\x1b\\")
	}
	return b.String(), nil
}

// kittyPayload produces the PNG bytes to transmit.
//
// A PNG that is already no larger than its display size is sent untouched,
// which is both faster and lossless. Anything else is scaled and re-encoded.
func kittyPayload(src *Source, geo Geometry) ([]byte, error) {
	if src.Format == "png" && src.Width <= geo.PixelWidth && src.Height <= geo.PixelHeight {
		return src.Data, nil
	}
	img := fitForTransfer(src.Image, geo)

	var buf bytes.Buffer
	// Best speed rather than best compression: this runs while the user waits
	// for output, and the data is going a few centimetres down a pipe.
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encoding png for transfer: %w", err)
	}
	return buf.Bytes(), nil
}
