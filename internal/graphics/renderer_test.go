package graphics

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// regexpFind returns the first capture group of pattern in s, or "".
func regexpFind(s, pattern string) string {
	m := regexp.MustCompile(pattern).FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func testRenderer(protocol Protocol) *Renderer {
	return &Renderer{
		Protocol:   protocol,
		Loader:     &Loader{},
		CellWidth:  9,
		CellHeight: 18,
	}
}

func TestParseProtocol(t *testing.T) {
	tests := []struct {
		in   string
		want Protocol
		auto bool
	}{
		{"auto", None, true},
		{"", None, true},
		{"none", None, false},
		{"off", None, false},
		{"kitty", Kitty, false},
		{"KITTY", Kitty, false},
		{"sixel", Sixel, false},
	}
	for _, tt := range tests {
		got, auto, err := ParseProtocol(tt.in)
		if err != nil {
			t.Errorf("ParseProtocol(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want || auto != tt.auto {
			t.Errorf("ParseProtocol(%q) = %v, auto=%v; want %v, auto=%v",
				tt.in, got, auto, tt.want, tt.auto)
		}
	}
	if _, _, err := ParseProtocol("iterm"); err == nil {
		t.Error("an unsupported protocol should be rejected")
	}
}

func TestRendererMeasure(t *testing.T) {
	r := testRenderer(Kitty)
	cols, rows, err := r.Measure(filepath.Join("testdata", "gradient.png"), 40, 30)
	if err != nil {
		t.Fatal(err)
	}
	if cols < 1 || rows < 1 || cols > 40 || rows > 30 {
		t.Errorf("footprint %dx%d is outside the box", cols, rows)
	}
}

// TestRendererMeasureFailsSoftly covers the fallback path: a broken image must
// be reported as an error the caller can turn into alt text, not a panic.
func TestRendererMeasureFailsSoftly(t *testing.T) {
	r := testRenderer(Kitty)
	if _, _, err := r.Measure(filepath.Join("testdata", "does-not-exist.png"), 40, 30); err == nil {
		t.Error("expected an error for a missing image")
	}
}

func TestRendererDisabled(t *testing.T) {
	for _, r := range []*Renderer{
		{Protocol: None, Loader: &Loader{}},
		{Protocol: Kitty, Loader: nil},
		nil,
	} {
		if r.Enabled() {
			t.Errorf("%+v should not be enabled", r)
		}
		if _, _, err := r.Measure("x.png", 10, 10); err == nil {
			t.Error("a disabled renderer should refuse to measure")
		}
		if _, err := r.Encode("x.png", 4, 2, 0); err == nil {
			t.Error("a disabled renderer should refuse to encode")
		}
	}
}

// TestEncodeMatchesMeasure is the contract between the two passes: the layout
// reserves what Measure reported, so Encode must fill exactly that box.
func TestEncodeMatchesMeasure(t *testing.T) {
	for _, protocol := range []Protocol{Kitty, Sixel} {
		for _, name := range []string{"gradient.png", "alpha.png", "wide.png", "tiny.png"} {
			t.Run(protocol.String()+"/"+name, func(t *testing.T) {
				r := testRenderer(protocol)
				ref := filepath.Join("testdata", name)

				cols, rows, err := r.Measure(ref, 40, 20)
				if err != nil {
					t.Fatal(err)
				}
				out, err := r.Encode(ref, cols, rows, 0)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.HasPrefix(out, "\x1b[") || !strings.HasSuffix(out, "B") {
					t.Errorf("output is not wrapped in cursor movement: %q", truncate(out))
				}
				// The reserved height has to match, or the text below the
				// image is drawn over it.
				wantUp := "\x1b[" + itoa(rows) + "A"
				if !strings.HasPrefix(out, wantUp) {
					t.Errorf("expected the sequence to start with %q, got %q", wantUp, truncate(out))
				}
			})
		}
	}
}

func TestEncodeKittyCarriesFootprint(t *testing.T) {
	r := testRenderer(Kitty)
	ref := filepath.Join("testdata", "gradient.png")

	cols, rows, err := r.Measure(ref, 30, 20)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Encode(ref, cols, rows, 0)
	if err != nil {
		t.Fatal(err)
	}
	// The terminal is told the cell box explicitly, so it fits the picture to
	// exactly the space the layout reserved even if the cell size was guessed.
	if !strings.Contains(out, "c="+itoa(cols)+",r="+itoa(rows)) {
		t.Errorf("footprint not sent to the terminal: %q", truncate(out))
	}
}

// TestEncodeIDsAreDistinct matters because a repeated identifier replaces the
// earlier image rather than adding a second one.
func TestEncodeIDsAreDistinct(t *testing.T) {
	r := testRenderer(Kitty)
	ref := filepath.Join("testdata", "alpha.png")

	seen := map[string]bool{}
	for i := 0; i < 5; i++ {
		out, err := r.Encode(ref, 4, 2, 0)
		if err != nil {
			t.Fatal(err)
		}
		id := regexpFind(out, `i=(\d+)`)
		if id == "" {
			t.Fatalf("no image id in %q", truncate(out))
		}
		if seen[id] {
			t.Errorf("image id %s was reused", id)
		}
		seen[id] = true
	}
}

func TestProtocolString(t *testing.T) {
	for p, want := range map[Protocol]string{Kitty: "kitty", Sixel: "sixel", None: "none"} {
		if got := p.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", p, got, want)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// TestImageIDsAvoidCollisionAcrossRuns checks that identifiers do not start
// from a fixed point. They are global to the terminal session, so two runs of
// mdv beginning at the same number would have the second erase the first run's
// images from the scrollback.
func TestImageIDsAvoidCollisionAcrossRuns(t *testing.T) {
	seen := map[uint32]bool{}
	for i := 0; i < 8; i++ {
		r := testRenderer(Kitty)
		seen[r.imageID()] = true
	}
	if len(seen) < 6 {
		t.Errorf("independent renderers produced only %d distinct first ids: %v", len(seen), seen)
	}
	for id := range seen {
		if id == 0 || id > idMask+1 {
			t.Errorf("id %d is outside the usable range", id)
		}
	}
}

func TestImageIDsIncrementWithinARun(t *testing.T) {
	r := testRenderer(Kitty)
	seen := map[uint32]bool{}
	for i := 0; i < 100; i++ {
		id := r.imageID()
		if seen[id] {
			t.Fatalf("id %d was handed out twice", id)
		}
		seen[id] = true
	}
}
