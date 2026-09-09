package graphics

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func loadTestImage(t *testing.T, name string) *Source {
	t.Helper()
	src, err := (&Loader{}).Load(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return src
}

// --- kitty ---

var kittySeq = regexp.MustCompile(`\x1b_G([^;]*);([^\x1b]*)\x1b\\`)

// parseKitty splits an encoded image into its escape sequences, returning the
// control data of each and the concatenated payload.
func parseKitty(t *testing.T, s string) (controls []string, payload []byte) {
	t.Helper()
	matches := kittySeq.FindAllStringSubmatch(s, -1)
	if matches == nil {
		t.Fatalf("no kitty sequences found in %q", truncate(s))
	}
	var encoded strings.Builder
	for _, m := range matches {
		controls = append(controls, m[1])
		encoded.WriteString(m[2])
	}
	data, err := base64.StdEncoding.DecodeString(encoded.String())
	if err != nil {
		t.Fatalf("payload is not valid base64: %v", err)
	}
	return controls, data
}

func truncate(s string) string {
	if len(s) > 120 {
		return s[:120] + "..."
	}
	return s
}

func TestEncodeKittyStructure(t *testing.T) {
	src := loadTestImage(t, "gradient.png")
	geo := Fit(src.Width, src.Height, Constraints{MaxCols: 40, CellWidth: 9, CellHeight: 18})

	out, err := encodeKitty(src, geo, 7)
	if err != nil {
		t.Fatal(err)
	}
	controls, payload := parseKitty(t, out)

	first := controls[0]
	for _, want := range []string{
		"a=T",   // transmit and display
		"f=100", // PNG
		"t=d",   // data is inline, not a file path
		"i=7",   // the identifier we asked for
		"C=1",   // do not move the cursor
		"q=2",   // no replies, which would land in the user's input
		fmt.Sprintf("c=%d", geo.Cols),
		fmt.Sprintf("r=%d", geo.Rows),
	} {
		if !strings.Contains(first, want) {
			t.Errorf("control data %q is missing %q", first, want)
		}
	}

	// The payload must actually be a PNG the terminal can decode.
	img, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("payload is not a valid PNG: %v", err)
	}
	if img.Bounds().Dx() > src.Width {
		t.Errorf("payload was enlarged to %d px", img.Bounds().Dx())
	}
}

// TestEncodeKittyChunking checks the continuation flags. Getting m wrong
// leaves the terminal waiting for data that never comes, and it stops drawing
// anything at all afterwards.
func TestEncodeKittyChunking(t *testing.T) {
	src := loadTestImage(t, "gradient.png")
	geo := Fit(src.Width, src.Height, Constraints{MaxCols: 200, CellWidth: 9, CellHeight: 18})

	out, err := encodeKitty(src, geo, 1)
	if err != nil {
		t.Fatal(err)
	}
	controls, _ := parseKitty(t, out)
	if len(controls) < 2 {
		t.Fatalf("expected the image to need several chunks, got %d", len(controls))
	}

	for i, c := range controls {
		last := i == len(controls)-1
		wantMore := "m=1"
		if last {
			wantMore = "m=0"
		}
		if !strings.Contains(c, wantMore) {
			t.Errorf("chunk %d of %d: control %q should carry %s", i, len(controls), c, wantMore)
		}
		// Only the first chunk carries the image parameters; repeating them
		// is not allowed by the protocol.
		if i > 0 && strings.Contains(c, "a=T") {
			t.Errorf("chunk %d repeats the image parameters: %q", i, c)
		}
	}

	// No single escape sequence may exceed the protocol's payload limit.
	for _, m := range kittySeq.FindAllStringSubmatch(out, -1) {
		if len(m[2]) > kittyChunkSize {
			t.Errorf("chunk payload is %d bytes, over the %d limit", len(m[2]), kittyChunkSize)
		}
	}
}

// TestEncodeKittyPassesThroughSmallPNG checks the shortcut: a PNG already no
// larger than its display size is sent byte for byte, which is both faster and
// lossless.
func TestEncodeKittyPassesThroughSmallPNG(t *testing.T) {
	src := loadTestImage(t, "alpha.png")
	geo := Fit(src.Width, src.Height, Constraints{MaxCols: 80, CellWidth: 9, CellHeight: 18})

	out, err := encodeKitty(src, geo, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, payload := parseKitty(t, out)
	if !bytes.Equal(payload, src.Data) {
		t.Error("a small PNG was re-encoded instead of being sent as-is")
	}
}

func TestEncodeKittyReencodesJPEG(t *testing.T) {
	src := loadTestImage(t, "photo.jpg")
	geo := Fit(src.Width, src.Height, Constraints{MaxCols: 40, CellWidth: 9, CellHeight: 18})

	out, err := encodeKitty(src, geo, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, payload := parseKitty(t, out)
	// The protocol only accepts PNG among compressed formats, so a JPEG has
	// to be converted rather than forwarded.
	if _, err := png.Decode(bytes.NewReader(payload)); err != nil {
		t.Errorf("JPEG was not converted to PNG: %v", err)
	}
}

// --- sixel ---

// decodeSixel parses a sixel sequence back into an image.
//
// Round-tripping is the only way to check the encoder without a terminal:
// asserting on the shape of the escape sequence would confirm it looks
// plausible, while decoding it confirms it means the right thing.
func decodeSixel(t *testing.T, s string) *image.RGBA {
	t.Helper()

	body, ok := strings.CutPrefix(s, "\x1bP")
	if !ok {
		t.Fatalf("missing DCS introducer in %q", truncate(s))
	}
	q := strings.IndexByte(body, 'q')
	if q < 0 {
		t.Fatal("missing 'q' after the DCS parameters")
	}
	body = body[q+1:]
	body, ok = strings.CutSuffix(body, "\x1b\\")
	if !ok {
		t.Fatal("missing string terminator")
	}

	var width, height int
	palette := map[int]color.RGBA{}
	current := 0
	x, bandTop := 0, 0
	var img *image.RGBA

	readInt := func(i int) (int, int) {
		start := i
		for i < len(body) && body[i] >= '0' && body[i] <= '9' {
			i++
		}
		if start == i {
			return -1, i
		}
		n, _ := strconv.Atoi(body[start:i])
		return n, i
	}

	for i := 0; i < len(body); {
		switch c := body[i]; {
		case c == '"':
			// Raster attributes: pan;pad;width;height
			i++
			var vals []int
			for len(vals) < 4 {
				n, next := readInt(i)
				if n < 0 {
					break
				}
				vals = append(vals, n)
				i = next
				if i < len(body) && body[i] == ';' {
					i++
				}
			}
			if len(vals) == 4 {
				width, height = vals[2], vals[3]
				img = image.NewRGBA(image.Rect(0, 0, width, height))
			}

		case c == '#':
			i++
			n, next := readInt(i)
			i = next
			if i < len(body) && body[i] == ';' {
				// Defining a color: #n;2;r;g;b as percentages.
				i++
				var vals []int
				for len(vals) < 4 {
					v, nx := readInt(i)
					if v < 0 {
						break
					}
					vals = append(vals, v)
					i = nx
					if i < len(body) && body[i] == ';' {
						i++
					}
				}
				if len(vals) == 4 && vals[0] == 2 {
					palette[n] = color.RGBA{
						uint8(vals[1] * 255 / 100),
						uint8(vals[2] * 255 / 100),
						uint8(vals[3] * 255 / 100),
						255,
					}
				}
			} else {
				current = n
				x = 0
			}

		case c == '!':
			i++
			count, next := readInt(i)
			i = next
			if i >= len(body) {
				t.Fatal("repeat introducer at end of data")
			}
			ch := body[i]
			i++
			for r := 0; r < count; r++ {
				putSixel(t, img, x, bandTop, ch, palette[current])
				x++
			}

		case c == '$':
			x = 0
			i++

		case c == '-':
			bandTop += 6
			x = 0
			i++

		case c >= '?' && c <= '~':
			putSixel(t, img, x, bandTop, c, palette[current])
			x++
			i++

		default:
			i++
		}
	}

	if img == nil {
		t.Fatal("no raster attributes, so the image size is unknown")
	}
	return img
}

// putSixel paints the six vertical pixels encoded in one data character.
func putSixel(t *testing.T, img *image.RGBA, x, bandTop int, ch byte, c color.RGBA) {
	if img == nil {
		return
	}
	bits := ch - '?'
	for row := 0; row < 6; row++ {
		if bits&(1<<row) == 0 {
			continue
		}
		y := bandTop + row
		if x < img.Bounds().Dx() && y < img.Bounds().Dy() {
			img.SetRGBA(x, y, c)
		}
	}
}

func TestEncodeSixelRoundTrip(t *testing.T) {
	for _, name := range []string{"gradient.png", "photo.jpg", "wide.png"} {
		t.Run(name, func(t *testing.T) {
			src := loadTestImage(t, name)
			geo := Fit(src.Width, src.Height, Constraints{MaxCols: 40, CellWidth: 9, CellHeight: 18})

			out, err := encodeSixel(src, geo)
			if err != nil {
				t.Fatal(err)
			}
			got := decodeSixel(t, out)

			if got.Bounds().Dx() != geo.PixelWidth || got.Bounds().Dy() != geo.PixelHeight {
				t.Fatalf("decoded %dx%d, want %dx%d",
					got.Bounds().Dx(), got.Bounds().Dy(), geo.PixelWidth, geo.PixelHeight)
			}

			want := toRGBA(scaleTo(src.Image, geo.PixelWidth, geo.PixelHeight))
			if mae := meanAbsError(got, want); mae > 12 {
				t.Errorf("mean error per channel is %.1f, too high for a 256-color reduction", mae)
			}
		})
	}
}

// TestEncodeSixelTransparency checks that fully transparent pixels are left
// undrawn. Sixel has no alpha, so the only way to express it is to skip them.
func TestEncodeSixelTransparency(t *testing.T) {
	src := loadTestImage(t, "alpha.png")
	geo := Fit(src.Width, src.Height, Constraints{MaxCols: 40, CellWidth: 9, CellHeight: 18})

	out, err := encodeSixel(src, geo)
	if err != nil {
		t.Fatal(err)
	}
	got := decodeSixel(t, out)

	// The source is an opaque circle on a transparent field; the corners must
	// have been left untouched and the centre painted.
	corner := got.RGBAAt(0, 0)
	if corner.A != 0 {
		t.Errorf("a transparent corner was painted %v", corner)
	}
	centre := got.RGBAAt(got.Bounds().Dx()/2, got.Bounds().Dy()/2)
	if centre.A == 0 {
		t.Error("the opaque centre was not painted")
	}
}

func TestEncodeSixelFullyTransparent(t *testing.T) {
	blank := image.NewRGBA(image.Rect(0, 0, 32, 32))
	src := &Source{Image: blank, Width: 32, Height: 32, Format: "png"}
	geo := Fit(32, 32, Constraints{MaxCols: 40, CellWidth: 9, CellHeight: 18})

	out, err := encodeSixel(src, geo)
	if err != nil {
		t.Fatal(err)
	}
	if out != "" {
		t.Errorf("an entirely transparent image should encode to nothing, got %q", truncate(out))
	}
}

func TestEncodeSixelPaletteBounded(t *testing.T) {
	src := loadTestImage(t, "gradient.png")
	geo := Fit(src.Width, src.Height, Constraints{MaxCols: 60, CellWidth: 9, CellHeight: 18})

	out, err := encodeSixel(src, geo)
	if err != nil {
		t.Fatal(err)
	}
	defs := regexp.MustCompile(`#(\d+);2;`).FindAllStringSubmatch(out, -1)
	if len(defs) == 0 {
		t.Fatal("no palette entries defined")
	}
	if len(defs) > sixelMaxColors {
		t.Errorf("defined %d colors, over the %d limit", len(defs), sixelMaxColors)
	}
	for _, d := range defs {
		if n, _ := strconv.Atoi(d[1]); n >= sixelMaxColors {
			t.Errorf("palette index %d is out of range", n)
		}
	}
}

// TestEncodeSixelRunLength checks that repeats are only used where they
// actually shorten the output.
func TestEncodeSixelRunLength(t *testing.T) {
	var b strings.Builder
	writeRuns(&b, []byte("???")) // trailing blanks are dropped entirely
	if got := b.String(); got != "" {
		t.Errorf("trailing empty sixels were emitted: %q", got)
	}

	b.Reset()
	writeRuns(&b, []byte("AAA@"))
	if got := b.String(); got != "AAA@" {
		t.Errorf("a run of 3 should be written out: got %q", got)
	}

	b.Reset()
	writeRuns(&b, []byte("AAAA@"))
	if got := b.String(); got != "!4A@" {
		t.Errorf("a run of 4 should be collapsed: got %q", got)
	}
}

func meanAbsError(a, b *image.RGBA) float64 {
	bounds := a.Bounds()
	var total, n float64
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			p, q := a.RGBAAt(x, y), b.RGBAAt(x, y)
			total += absDiff(p.R, q.R) + absDiff(p.G, q.G) + absDiff(p.B, q.B)
			n += 3
		}
	}
	if n == 0 {
		return 0
	}
	return total / n
}

func absDiff(a, b uint8) float64 {
	if a > b {
		return float64(a - b)
	}
	return float64(b - a)
}

// --- placement ---

func TestPlaceSequence(t *testing.T) {
	got := place("PAYLOAD", 4, 0)
	want := "\x1b[4A" + "\x1b7" + "PAYLOAD" + "\x1b8" + "\x1b[4B"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// TestPlaceSavesBeforeIndenting is the fix for text after an indented image
// being shifted right: the cursor must be restored to the left margin, so it
// has to be saved before the indent is applied.
func TestPlaceSavesBeforeIndenting(t *testing.T) {
	got := place("P", 2, 6)
	save := strings.Index(got, "\x1b7")
	indent := strings.Index(got, "\x1b[6C")
	if save < 0 || indent < 0 {
		t.Fatalf("missing save or indent in %q", got)
	}
	if save > indent {
		t.Errorf("cursor saved after indenting, so restoring lands in the wrong column: %q", got)
	}
}

func TestPlaceMovesPastTheImage(t *testing.T) {
	for _, rows := range []int{1, 3, 20} {
		got := place("P", rows, 0)
		up := fmt.Sprintf("\x1b[%dA", rows)
		down := fmt.Sprintf("\x1b[%dB", rows)
		if !strings.HasPrefix(got, up) {
			t.Errorf("rows=%d: should start by moving up: %q", rows, got)
		}
		if !strings.HasSuffix(got, down) {
			t.Errorf("rows=%d: should end by moving back down: %q", rows, got)
		}
	}
}
