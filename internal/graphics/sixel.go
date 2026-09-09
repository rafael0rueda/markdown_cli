package graphics

import (
	"fmt"
	"image"
	"strings"
)

const (
	// sixelMaxColors is the palette size. The format allows up to 256 and
	// every terminal that implements it supports that many.
	sixelMaxColors = 256

	// alphaThreshold is where a pixel stops counting as visible. Sixel has no
	// alpha channel, so partial transparency has to become fully opaque or
	// fully absent; the midpoint is the least surprising place to cut.
	alphaThreshold = 128

	// sixelRunLimit is the shortest run worth encoding with a repeat
	// introducer. "!4?" is four characters and replaces four, so runs below
	// that length only make the output longer.
	sixelRunLimit = 4

	// transparent marks a pixel that should not be drawn at all.
	transparent = -1
)

// encodeSixel renders an image as a sixel escape sequence.
//
// Sixel encodes six vertical pixels per character, so the image is walked in
// horizontal bands six rows tall. Within a band each color is drawn in its own
// pass over the row, returning to the left margin between passes, because the
// format can only select one color at a time.
func encodeSixel(src *Source, geo Geometry) (string, error) {
	img := toRGBA(scaleTo(src.Image, geo.PixelWidth, geo.PixelHeight))
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	if width == 0 || height == 0 {
		return "", fmt.Errorf("image scaled to nothing")
	}

	palette := medianCut(img, sixelMaxColors)
	if len(palette) == 0 {
		// Every pixel was transparent, so there is nothing to draw.
		return "", nil
	}
	indexed := ditherToPalette(img, palette)

	var b strings.Builder
	// P2=1 leaves undrawn pixels showing whatever is behind them, which is how
	// transparency is expressed; without it they would be painted black.
	b.WriteString("\x1bP0;1;0q")
	fmt.Fprintf(&b, `"1;1;%d;%d`, width, height)

	for i, c := range palette {
		// Sixel color components are percentages, not bytes.
		fmt.Fprintf(&b, "#%d;2;%d;%d;%d", i,
			scaleToPercent(c[0]), scaleToPercent(c[1]), scaleToPercent(c[2]))
	}

	writeBands(&b, indexed, width, height, len(palette))

	b.WriteString("\x1b\\")
	return b.String(), nil
}

// writeBands emits the pixel data, one six-row band at a time.
func writeBands(b *strings.Builder, indexed []int, width, height, colors int) {
	// Reused across bands to keep this from allocating per band per color.
	bits := make([]byte, width)
	present := make([]bool, colors)

	for top := 0; top < height; top += 6 {
		for i := range present {
			present[i] = false
		}
		rows := min(6, height-top)

		// Find which colors appear in this band, so the passes below cover
		// only those rather than the whole palette.
		for y := top; y < top+rows; y++ {
			row := indexed[y*width : (y+1)*width]
			for _, idx := range row {
				if idx != transparent {
					present[idx] = true
				}
			}
		}

		first := true
		for color := 0; color < colors; color++ {
			if !present[color] {
				continue
			}
			// Each color pass starts back at the left margin.
			if !first {
				b.WriteByte('$')
			}
			first = false

			for x := 0; x < width; x++ {
				var mask byte
				for row := 0; row < rows; row++ {
					if indexed[(top+row)*width+x] == color {
						mask |= 1 << row
					}
				}
				// Sixel data characters are offset by '?' so that the six bits
				// land in the printable range.
				bits[x] = '?' + mask
			}
			fmt.Fprintf(b, "#%d", color)
			writeRuns(b, bits)
		}

		if top+6 < height {
			b.WriteByte('-') // move to the next band
		}
	}
}

// writeRuns emits a row of sixel characters, collapsing repeats.
func writeRuns(b *strings.Builder, bits []byte) {
	// Trailing empty sixels need not be written at all: the band is already
	// blank there, and dropping them shortens output on any image with a
	// transparent or uniform right edge.
	end := len(bits)
	for end > 0 && bits[end-1] == '?' {
		end--
	}

	for i := 0; i < end; {
		run := 1
		for i+run < end && bits[i+run] == bits[i] {
			run++
		}
		if run >= sixelRunLimit {
			fmt.Fprintf(b, "!%d%c", run, bits[i])
		} else {
			b.WriteString(strings.Repeat(string(bits[i]), run))
		}
		i += run
	}
}

// ditherToPalette maps every pixel to a palette index, spreading the error of
// each approximation into the neighbours that have not been decided yet.
//
// Without this, reducing a photograph or a gradient to 256 colors produces
// visible banding. Floyd-Steinberg trades that for a fine speckle, which at
// terminal image sizes is far less noticeable.
func ditherToPalette(img *image.RGBA, palette [][3]uint8) []int {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	lookup := newPaletteLookup(palette)
	out := make([]int, width*height)

	// Error carried into the current and the next row, three channels each.
	curr := make([]int32, (width+2)*3)
	next := make([]int32, (width+2)*3)

	for y := 0; y < height; y++ {
		row := img.Pix[y*img.Stride:]
		for i := range next {
			next[i] = 0
		}

		for x := 0; x < width; x++ {
			p := row[x*4:]
			if p[3] < alphaThreshold {
				out[y*width+x] = transparent
				continue
			}

			// The error buffers are offset by one so that spreading to x-1 at
			// the left edge does not need a bounds check.
			e := (x + 1) * 3
			r := clamp8(int(p[0]) + int(curr[e+0]))
			g := clamp8(int(p[1]) + int(curr[e+1]))
			bl := clamp8(int(p[2]) + int(curr[e+2]))

			idx := lookup.nearest(r, g, bl)
			out[y*width+x] = idx

			chosen := palette[idx]
			errR := int32(r - int(chosen[0]))
			errG := int32(g - int(chosen[1]))
			errB := int32(bl - int(chosen[2]))

			// The Floyd-Steinberg distribution: 7/16 right, then 3/16, 5/16
			// and 1/16 across the row below.
			spread := func(buf []int32, at int, num int32) {
				buf[at+0] += errR * num / 16
				buf[at+1] += errG * num / 16
				buf[at+2] += errB * num / 16
			}
			spread(curr, e+3, 7)
			spread(next, e-3, 3)
			spread(next, e+0, 5)
			spread(next, e+3, 1)
		}
		curr, next = next, curr
	}
	return out
}

// clamp8 constrains a value to a single byte's range.
func clamp8(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

// scaleToPercent converts a color component to the 0-100 scale sixel uses.
func scaleToPercent(v uint8) int {
	return (int(v)*100 + 127) / 255
}
