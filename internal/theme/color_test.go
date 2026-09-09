package theme

import "testing"

func TestHex(t *testing.T) {
	tests := []struct {
		in      string
		want    Color
		wantSet bool
	}{
		{"#ff0000", RGB(255, 0, 0), true},
		{"ff0000", RGB(255, 0, 0), true},
		{"#f00", RGB(255, 0, 0), true},
		{"  #00ff7f  ", RGB(0, 255, 127), true},
		{"#gggggg", Color{}, false},
		{"#ff00", Color{}, false},
		{"", Color{}, false},
	}
	for _, tt := range tests {
		got := Hex(tt.in)
		if got.IsSet() != tt.wantSet || (tt.wantSet && got != tt.want) {
			t.Errorf("Hex(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestZeroColorIsUnset(t *testing.T) {
	var c Color
	if c.IsSet() {
		t.Error("the zero Color should be unset so it inherits the terminal default")
	}
	if got := (Style{FG: c}).SGR(ColorTrue); got != "" {
		t.Errorf("unset color emitted %q", got)
	}
}

func TestSGRTrueColor(t *testing.T) {
	s := Style{FG: RGB(1, 2, 3), BG: RGB(4, 5, 6), Bold: true, Italic: true}
	if got, want := s.SGR(ColorTrue), "\x1b[1;3;38;2;1;2;3;48;2;4;5;6m"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSGRColorNoneIsEmpty(t *testing.T) {
	s := Style{FG: RGB(1, 2, 3), Bold: true, Underline: true, Strike: true}
	if got := s.SGR(ColorNone); got != "" {
		t.Errorf("ColorNone emitted %q, want empty", got)
	}
}

func TestTo256(t *testing.T) {
	tests := []struct {
		in   Color
		want int
	}{
		{RGB(0, 0, 0), 16},        // cube origin
		{RGB(255, 255, 255), 231}, // cube corner
		{RGB(255, 0, 0), 196},
		{RGB(0, 255, 0), 46},
		{RGB(0, 0, 255), 21},
		{RGB(128, 128, 128), 244}, // mid gray comes off the gray ramp
	}
	for _, tt := range tests {
		if got := tt.in.to256(); got != tt.want {
			t.Errorf("to256(%+v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestTo256InRange(t *testing.T) {
	for r := 0; r < 256; r += 17 {
		for g := 0; g < 256; g += 17 {
			for b := 0; b < 256; b += 17 {
				if got := RGB(uint8(r), uint8(g), uint8(b)).to256(); got < 16 || got > 255 {
					t.Fatalf("to256(%d,%d,%d) = %d, outside 16-255", r, g, b, got)
				}
			}
		}
	}
}

// TestTo16Hues checks the property that motivates hue-based mapping: pastel
// colors keep their hue instead of all collapsing onto white.
func TestTo16Hues(t *testing.T) {
	tests := []struct {
		name string
		in   Color
		want int
	}{
		{"pure red", RGB(255, 0, 0), 9},
		{"pure green", RGB(0, 255, 0), 10},
		{"pure blue", RGB(0, 0, 255), 12},
		{"black", RGB(0, 0, 0), 0},
		{"white", RGB(255, 255, 255), 15},
		{"mid gray", RGB(128, 128, 128), 7},
		{"dark gray", RGB(60, 60, 60), 8},
		{"catppuccin mauve", Hex("#cba6f7"), 12},
		{"catppuccin green", Hex("#a6e3a1"), 10},
		{"catppuccin teal", Hex("#94e2d5"), 14},
		{"catppuccin peach", Hex("#fab387"), 9},
		{"catppuccin surface0", Hex("#313244"), 8},
	}
	for _, tt := range tests {
		if got := tt.in.to16(); got != tt.want {
			t.Errorf("%s: to16 = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestTo16DistinguishesPastels(t *testing.T) {
	// The whole point of the hue mapping: these must not all become white.
	pastels := []Color{
		Hex("#cba6f7"), Hex("#a6e3a1"), Hex("#f38ba8"), Hex("#94e2d5"), Hex("#f9e2af"),
	}
	seen := map[int]bool{}
	for _, c := range pastels {
		seen[c.to16()] = true
	}
	if len(seen) < 4 {
		t.Errorf("pastels collapsed onto %d colors: %v", len(seen), seen)
	}
}

func TestTo16InRange(t *testing.T) {
	for r := 0; r < 256; r += 17 {
		for g := 0; g < 256; g += 17 {
			for b := 0; b < 256; b += 17 {
				if got := RGB(uint8(r), uint8(g), uint8(b)).to16(); got < 0 || got > 15 {
					t.Fatalf("to16(%d,%d,%d) = %d, outside 0-15", r, g, b, got)
				}
			}
		}
	}
}

func TestStyleMerge(t *testing.T) {
	base := Style{FG: RGB(1, 1, 1), Bold: true}
	over := Style{FG: RGB(2, 2, 2), Italic: true}
	got := base.Merge(over)
	if got.FG != RGB(2, 2, 2) {
		t.Errorf("FG = %+v, want the overriding color", got.FG)
	}
	if !got.Bold || !got.Italic {
		t.Errorf("attributes did not accumulate: %+v", got)
	}
}

func TestStyleMergeKeepsBaseWhenUnset(t *testing.T) {
	base := Style{FG: RGB(1, 1, 1)}
	if got := base.Merge(Style{Bold: true}); got.FG != RGB(1, 1, 1) {
		t.Errorf("an unset override replaced the base color: %+v", got.FG)
	}
}

func TestParseColorMode(t *testing.T) {
	tests := map[string]ColorMode{
		"none": ColorNone, "off": ColorNone, "16": Color16,
		"256": Color256, "truecolor": ColorTrue, "24bit": ColorTrue, "TrueColor": ColorTrue,
	}
	for in, want := range tests {
		got, err := ParseColorMode(in)
		if err != nil || got != want {
			t.Errorf("ParseColorMode(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseColorMode("magenta"); err == nil {
		t.Error("ParseColorMode should reject unknown values")
	}
}

func TestGetTheme(t *testing.T) {
	for _, name := range []string{"dark", "light", "plain", "auto", ""} {
		th, err := Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		if th == nil || len(th.Glyphs.Bullets) == 0 {
			t.Errorf("Get(%q) returned an incomplete theme", name)
		}
	}
	if _, err := Get("solarized"); err == nil {
		t.Error("Get should reject unknown theme names")
	}
}

// TestThemesAreIndependent guards against the constructors handing out a
// shared pointer: mdv mutates the returned theme for --ascii and --color=none.
func TestThemesAreIndependent(t *testing.T) {
	a, b := Dark(), Dark()
	a.Glyphs = ASCIIGlyphs
	if b.Glyphs.QuoteBar == ASCIIGlyphs.QuoteBar {
		t.Error("mutating one theme affected another")
	}
}

func TestDetectDark(t *testing.T) {
	tests := map[string]bool{
		"":         true, // nothing to go on: assume dark
		"15;0":     true,
		"0;15":     false,
		"default;": true, // unparseable: assume dark
		"7;8":      true,
	}
	for env, want := range tests {
		t.Setenv("COLORFGBG", env)
		if got := DetectDark(); got != want {
			t.Errorf("COLORFGBG=%q: DetectDark = %v, want %v", env, got, want)
		}
	}
}
