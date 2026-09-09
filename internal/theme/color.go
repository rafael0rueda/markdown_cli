package theme

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ColorMode is how much color the output target can represent. Values are
// ordered from least to most capable so they can be compared.
type ColorMode int

const (
	ColorNone ColorMode = iota // no SGR color at all
	Color16                    // the 8 base colors plus their bright variants
	Color256                   // the xterm 256-color palette
	ColorTrue                  // 24-bit direct color
)

func (m ColorMode) String() string {
	switch m {
	case ColorNone:
		return "none"
	case Color16:
		return "16"
	case Color256:
		return "256"
	case ColorTrue:
		return "truecolor"
	}
	return "unknown"
}

// ParseColorMode maps a --color flag value onto a ColorMode.
func ParseColorMode(s string) (ColorMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "off", "never", "0":
		return ColorNone, nil
	case "16", "basic", "ansi":
		return Color16, nil
	case "256", "ansi256":
		return Color256, nil
	case "true", "truecolor", "24bit", "16m":
		return ColorTrue, nil
	}
	return ColorNone, fmt.Errorf("unknown color mode %q (want none, 16, 256 or truecolor)", s)
}

// Color is a 24-bit color. The zero value means "unset", which renders as the
// terminal's own default foreground or background rather than as black.
type Color struct {
	R, G, B uint8
	set     bool
}

// RGB builds a Color from its components.
func RGB(r, g, b uint8) Color { return Color{R: r, G: g, B: b, set: true} }

// Hex builds a Color from a "#rrggbb" or "#rgb" string. It returns an unset
// Color if the input does not parse, so themes fall back to terminal defaults
// instead of failing.
func Hex(s string) Color {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	if len(s) != 6 {
		return Color{}
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return Color{}
	}
	return RGB(uint8(v>>16), uint8(v>>8), uint8(v))
}

// IsSet reports whether the color carries a value.
func (c Color) IsSet() bool { return c.set }

// Luminance is the perceptual brightness of the color in [0,1]. It is used to
// decide whether a background is light or dark.
func (c Color) Luminance() float64 {
	lin := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.04045 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// sgr returns the SGR parameters that select this color in the given mode.
// fg selects between the foreground and background parameter families.
func (c Color) sgr(mode ColorMode, fg bool) string {
	if !c.set || mode == ColorNone {
		return ""
	}
	switch mode {
	case ColorTrue:
		base := 48
		if fg {
			base = 38
		}
		return fmt.Sprintf("%d;2;%d;%d;%d", base, c.R, c.G, c.B)
	case Color256:
		base := 48
		if fg {
			base = 38
		}
		return fmt.Sprintf("%d;5;%d", base, c.to256())
	default:
		idx := c.to16()
		// 30-37 and 40-47 hold the base colors; the bright variants live at
		// 90-97 and 100-107.
		switch {
		case fg && idx < 8:
			return strconv.Itoa(30 + idx)
		case fg:
			return strconv.Itoa(90 + idx - 8)
		case idx < 8:
			return strconv.Itoa(40 + idx)
		default:
			return strconv.Itoa(100 + idx - 8)
		}
	}
}

// to256 maps the color onto the xterm 256-color palette, choosing between the
// 6x6x6 color cube and the finer 24-step gray ramp by whichever lands closer.
func (c Color) to256() int {
	cubeIdx := func(v uint8) int {
		// Cube levels are 0, 95, 135, 175, 215, 255; the midpoints between
		// consecutive levels are what decide each bucket.
		switch {
		case v < 48:
			return 0
		case v < 115:
			return 1
		default:
			i := (int(v) - 35) / 40
			if i > 5 {
				i = 5
			}
			return i
		}
	}
	cubeVal := func(i int) int {
		if i == 0 {
			return 0
		}
		return 55 + i*40
	}

	ri, gi, bi := cubeIdx(c.R), cubeIdx(c.G), cubeIdx(c.B)
	cubeDist := dist(c, RGB(uint8(cubeVal(ri)), uint8(cubeVal(gi)), uint8(cubeVal(bi))))

	// Gray ramp: 24 steps from 8 to 238 in increments of 10.
	avg := (int(c.R) + int(c.G) + int(c.B)) / 3
	gi2 := (avg - 3) / 10
	if gi2 < 0 {
		gi2 = 0
	}
	if gi2 > 23 {
		gi2 = 23
	}
	grayVal := uint8(8 + gi2*10)
	grayDist := dist(c, RGB(grayVal, grayVal, grayVal))

	if grayDist < cubeDist {
		return 232 + gi2
	}
	return 16 + 36*ri + 6*gi + bi
}

// to16 maps the color onto the 16 ANSI colors by hue rather than by RGB
// distance.
//
// Nearest-RGB is the obvious approach and it is wrong here. Terminal palettes
// are saturated primaries, so a pastel theme measured by distance collapses
// almost entirely onto white and gray - technically the closest match, and
// useless to read. Picking the nearest hue instead keeps headings, links and
// code visually distinct at the cost of being less faithful to the exact
// color, which is the right trade when there are only sixteen slots.
func (c Color) to16() int {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	max := math.Max(r, math.Max(g, b))
	min := math.Min(r, math.Min(g, b))
	lightness := (max + min) / 2
	chroma := max - min

	saturation := 0.0
	if d := 1 - math.Abs(2*lightness-1); d > 0 {
		saturation = chroma / d
	}

	// Grays, near-blacks and near-whites have no meaningful hue to preserve,
	// so they are placed on the achromatic ramp by lightness alone.
	if saturation < achromaticSaturation || chroma == 0 {
		switch {
		case lightness < 0.15:
			return 0 // black
		case lightness < 0.4:
			return 8 // bright black, i.e. dark gray
		case lightness < 0.75:
			return 7 // white, i.e. light gray
		default:
			return 15 // bright white
		}
	}

	// Hue in degrees, then the nearest of the six chromatic ANSI hues.
	var hue float64
	switch max {
	case r:
		hue = math.Mod((g-b)/chroma, 6)
	case g:
		hue = (b-r)/chroma + 2
	default:
		hue = (r-g)/chroma + 4
	}
	hue *= 60
	if hue < 0 {
		hue += 360
	}

	best, bestDist := 0, math.MaxFloat64
	for _, h := range chromaticHues {
		d := math.Abs(hue - h.degrees)
		if d > 180 {
			d = 360 - d
		}
		if d < bestDist {
			best, bestDist = h.index, d
		}
	}
	// The bright half of the palette carries the lighter colors. The
	// comparison is inclusive so that the pure primaries, which sit exactly at
	// 0.5, land on bright red/green/blue - which is what terminals actually
	// render #ff0000 as - rather than on their darker counterparts.
	if lightness >= 0.5 {
		best += 8
	}
	return best
}

// achromaticSaturation is the saturation below which a color is treated as
// gray. It is set high enough that the faintly blue-tinted backgrounds common
// in dark themes land on the gray ramp instead of turning the code block blue.
const achromaticSaturation = 0.25

// chromaticHues maps hue angles to the six non-gray ANSI color indices.
var chromaticHues = []struct {
	degrees float64
	index   int
}{
	{0, 1},   // red
	{60, 3},  // yellow
	{120, 2}, // green
	{180, 6}, // cyan
	{240, 4}, // blue
	{300, 5}, // magenta
}

// dist is the squared distance between two colors, weighted to approximate
// human sensitivity (green matters most, blue least).
func dist(a, b Color) float64 {
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	return 2*dr*dr + 4*dg*dg + 3*db*db
}
