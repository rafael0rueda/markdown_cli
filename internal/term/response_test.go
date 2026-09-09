package term

import (
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

// Replies as real terminals send them, used to build test inputs.
const (
	replyKittyOK  = "\x1b_Gi=31;OK\x1b\\"
	replyKittyErr = "\x1b_Gi=31;ENOTSUPPORTED:no graphics\x1b\\"
	replyKeyboard = "\x1b[?0u"
	replyDASixel  = "\x1b[?62;4;22c"
	replyDAPlain  = "\x1b[?62;22c"
	replyBGDark   = "\x1b]11;rgb:1e1e/1e1e/2e2e\x1b\\"
	replyBGLight  = "\x1b]11;rgb:efef/f1f1/f5f5\x1b\\"
	replyBGBel    = "\x1b]11;rgb:0000/0000/0000\x07"
	replyCellSize = "\x1b[6;18;9t"
	replyTextSize = "\x1b[4;600;800t" // a different report, must be ignored
)

func TestParseKittyGraphics(t *testing.T) {
	got := parseResponses([]byte(replyKittyOK + replyDAPlain))
	if !got.kittyGraphics {
		t.Error("kitty graphics reply not recognized")
	}
	if !got.deviceAttrs {
		t.Error("device attributes reply not recognized")
	}
}

// TestParseKittyGraphicsRefused covers the terminal answering the query but
// declining support. Treating any reply as success would turn every image
// into garbage on those terminals.
func TestParseKittyGraphicsRefused(t *testing.T) {
	got := parseResponses([]byte(replyKittyErr + replyDAPlain))
	if got.kittyGraphics {
		t.Error("an ENOTSUPPORTED reply was read as support")
	}
	if !got.deviceAttrs {
		t.Error("device attributes should still be seen")
	}
}

func TestParseSixel(t *testing.T) {
	if got := parseResponses([]byte(replyDASixel)); !got.sixel {
		t.Error("sixel attribute 4 not detected")
	}
	if got := parseResponses([]byte(replyDAPlain)); got.sixel {
		t.Error("sixel reported without attribute 4")
	}
}

// TestParseSixelNotFooledBySubstring guards against matching "4" inside a
// longer parameter such as "24" or "64".
func TestParseSixelNotFooledBySubstring(t *testing.T) {
	for _, da := range []string{"\x1b[?64;22c", "\x1b[?62;24c", "\x1b[?41c", "\x1b[?14c"} {
		if got := parseResponses([]byte(da)); got.sixel {
			t.Errorf("%q was misread as advertising sixel", da)
		}
	}
}

func TestParseKeyboard(t *testing.T) {
	if got := parseResponses([]byte(replyKeyboard + replyDAPlain)); !got.kittyKeyboard {
		t.Error("kitty keyboard reply not recognized")
	}
	if got := parseResponses([]byte(replyDAPlain)); got.kittyKeyboard {
		t.Error("keyboard support reported without a reply")
	}
}

func TestParseCellSize(t *testing.T) {
	got := parseResponses([]byte(replyCellSize + replyDAPlain))
	if got.cellWidth != 9 || got.cellHeight != 18 {
		t.Errorf("cell size = %dx%d, want 9x18", got.cellWidth, got.cellHeight)
	}
}

// TestParseIgnoresOtherSizeReports checks that only the CSI 6 report, which
// carries the cell size, is read; CSI 4 carries the window size in pixels.
func TestParseIgnoresOtherSizeReports(t *testing.T) {
	got := parseResponses([]byte(replyTextSize + replyDAPlain))
	if got.cellWidth != 0 || got.cellHeight != 0 {
		t.Errorf("a text-area report was read as a cell size: %dx%d", got.cellWidth, got.cellHeight)
	}
}

func TestParseBackground(t *testing.T) {
	tests := []struct {
		name  string
		reply string
		want  theme.Color
		dark  bool
	}{
		{"catppuccin mocha", replyBGDark, theme.RGB(0x1e, 0x1e, 0x2e), true},
		{"catppuccin latte", replyBGLight, theme.RGB(0xef, 0xf1, 0xf5), false},
		{"black with BEL terminator", replyBGBel, theme.RGB(0, 0, 0), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseResponses([]byte(tt.reply + replyDAPlain))
			if !got.hasBackground {
				t.Fatal("no background parsed")
			}
			if got.background != tt.want {
				t.Errorf("background = %+v, want %+v", got.background, tt.want)
			}
			caps := Caps{Background: got.background, HasBackground: true}
			if caps.Dark() != tt.dark {
				t.Errorf("Dark() = %v, want %v", caps.Dark(), tt.dark)
			}
		})
	}
}

func TestParseXColor(t *testing.T) {
	tests := []struct {
		in   string
		want theme.Color
		ok   bool
	}{
		{"rgb:ffff/0000/0000", theme.RGB(255, 0, 0), true},
		{"rgb:ff/00/00", theme.RGB(255, 0, 0), true},
		{"rgb:f/0/0", theme.RGB(255, 0, 0), true},
		{"rgb:1e1e/1e1e/2e2e", theme.RGB(0x1e, 0x1e, 0x2e), true},
		{"  rgb:0/0/0  ", theme.RGB(0, 0, 0), true},
		{"rgba:ffff/0/0/0", theme.Color{}, false},
		{"rgb:ffff/0000", theme.Color{}, false},
		{"rgb:zzzz/0000/0000", theme.Color{}, false},
		{"rgb:fffff/0/0", theme.Color{}, false},
		{"#ff0000", theme.Color{}, false},
		{"", theme.Color{}, false},
	}
	for _, tt := range tests {
		got, ok := parseXColor(tt.in)
		if ok != tt.ok || (tt.ok && got != tt.want) {
			t.Errorf("parseXColor(%q) = %+v, %v; want %+v, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// TestParseFullExchange runs everything a supporting terminal would send, in
// one buffer, as it would actually arrive.
func TestParseFullExchange(t *testing.T) {
	buf := replyKittyOK + replyKeyboard + replyBGDark + replyCellSize + replyDASixel
	got := parseResponses([]byte(buf))
	if !got.kittyGraphics || !got.kittyKeyboard || !got.sixel || !got.deviceAttrs {
		t.Errorf("missed a capability: %+v", got)
	}
	if !got.hasBackground || got.cellWidth != 9 {
		t.Errorf("missed background or cell size: %+v", got)
	}
}

// TestParseSurvivesNoise checks that stray bytes between replies - a keystroke
// typed while the probe was running, say - do not derail parsing.
func TestParseSurvivesNoise(t *testing.T) {
	buf := "x" + replyKittyOK + "\n\r" + replyDASixel + "junk"
	got := parseResponses([]byte(buf))
	if !got.kittyGraphics || !got.sixel || !got.deviceAttrs {
		t.Errorf("noise defeated parsing: %+v", got)
	}
}

// TestParseTruncated is the timeout case: the read deadline passed while the
// terminal was mid-reply. Whatever completed must still be usable.
func TestParseTruncated(t *testing.T) {
	buf := replyKittyOK + "\x1b[?62;4" // device attributes cut short
	got := parseResponses([]byte(buf))
	if !got.kittyGraphics {
		t.Error("a complete earlier reply was lost")
	}
	if got.deviceAttrs {
		t.Error("a truncated reply was treated as complete")
	}
}

func TestParseEmpty(t *testing.T) {
	got := parseResponses(nil)
	if got.deviceAttrs || got.kittyGraphics || got.sixel {
		t.Errorf("empty input produced %+v", got)
	}
}

// TestParseNeverPanics feeds malformed input, since the buffer comes straight
// off a terminal and mdv cannot control what lands in it.
func TestParseNeverPanics(t *testing.T) {
	inputs := []string{
		"\x1b", "\x1b[", "\x1b]", "\x1b_", "\x1bP", "\x1b^",
		"\x1b]11;", "\x1b]11;rgb:", "\x1b[6;t", "\x1b[6;a;bt",
		"\x1b_G\x1b\\", "\x1b_G;\x1b\\", "\x1b[?u", "\x1b[c",
		"\x00\x1b\x00", "\x1b\x1b\x1b\x1b",
	}
	for _, in := range inputs {
		_ = parseResponses([]byte(in))
	}
}

func TestScanSequences(t *testing.T) {
	buf := []byte("a" + replyDAPlain + "b" + replyBGDark + "c")
	got := scanSequences(buf)
	if len(got) != 2 {
		t.Fatalf("found %d sequences, want 2: %q", len(got), got)
	}
	if string(got[0]) != replyDAPlain || string(got[1]) != replyBGDark {
		t.Errorf("got %q", got)
	}
}
