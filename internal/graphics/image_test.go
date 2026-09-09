package graphics

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testLoader() *Loader { return &Loader{} }

func TestLoadFormats(t *testing.T) {
	tests := []struct {
		file   string
		format string
		width  int
		height int
	}{
		{"gradient.png", "png", 240, 160},
		{"photo.jpg", "jpeg", 240, 160},
		{"anim.gif", "gif", 240, 160},
		{"alpha.png", "png", 64, 64},
		{"tiny.png", "png", 1, 1},
		{"wide.png", "png", 800, 20},
	}
	l := testLoader()
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			src, err := l.Load(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if src.Format != tt.format {
				t.Errorf("format = %q, want %q", src.Format, tt.format)
			}
			if src.Width != tt.width || src.Height != tt.height {
				t.Errorf("size = %dx%d, want %dx%d", src.Width, src.Height, tt.width, tt.height)
			}
			if src.Image == nil || len(src.Data) == 0 {
				t.Error("decoded image or original bytes missing")
			}
		})
	}
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()

	notImage := filepath.Join(dir, "notes.txt")
	os.WriteFile(notImage, []byte("this is not an image"), 0o644)

	truncated := filepath.Join(dir, "truncated.png")
	full, _ := os.ReadFile(filepath.Join("testdata", "gradient.png"))
	os.WriteFile(truncated, full[:len(full)/3], 0o644)

	empty := filepath.Join(dir, "empty.png")
	os.WriteFile(empty, nil, 0o644)

	l := testLoader()
	for _, path := range []string{
		filepath.Join(dir, "missing.png"),
		notImage,
		truncated,
		empty,
		dir, // a directory
	} {
		if _, err := l.Load(path); err == nil {
			t.Errorf("Load(%q) should have failed", path)
		}
	}
}

// TestLoadCaches matters because every image is looked at twice - once to
// measure, once to encode - and decoding is the expensive step.
func TestLoadCaches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	data, _ := os.ReadFile(filepath.Join("testdata", "gradient.png"))
	os.WriteFile(path, data, 0o644)

	l := testLoader()
	first, err := l.Load(path)
	if err != nil {
		t.Fatal(err)
	}

	// Removing the file must not affect a second load if the cache works.
	os.Remove(path)
	second, err := l.Load(path)
	if err != nil {
		t.Fatalf("second load failed, so the result was not cached: %v", err)
	}
	if first != second {
		t.Error("cache returned a different object")
	}
}

func TestLoadCachesFailures(t *testing.T) {
	l := testLoader()
	path := filepath.Join(t.TempDir(), "missing.png")
	if _, err1 := l.Load(path); err1 == nil {
		t.Fatal("expected an error")
	}
	if _, err2 := l.Load(path); err2 == nil {
		t.Error("expected the failure to be remembered")
	}
}

// TestRemoteImagesDisabledByDefault is a privacy property, not a performance
// one: rendering a document must not tell a third party your address and when
// you read it unless you asked for that.
func TestRemoteImagesDisabledByDefault(t *testing.T) {
	var reached bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Write([]byte("nope"))
	}))
	defer server.Close()

	l := testLoader()
	if _, err := l.Load(server.URL + "/img.png"); err == nil {
		t.Error("remote image loaded with AllowRemote off")
	}
	if reached {
		t.Error("a network request was made without opting in")
	}
}

func TestRemoteImagesWhenEnabled(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "gradient.png"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer server.Close()

	l := &Loader{AllowRemote: true}
	src, err := l.Load(server.URL + "/img.png")
	if err != nil {
		t.Fatal(err)
	}
	if src.Width != 240 {
		t.Errorf("width = %d, want 240", src.Width)
	}
}

func TestRemoteImageErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer server.Close()

	l := &Loader{AllowRemote: true}
	if _, err := l.Load(server.URL + "/img.png"); err == nil {
		t.Error("a 404 should be an error")
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention the status: %v", err)
	}
}

func TestIsRemote(t *testing.T) {
	remote := []string{"http://x/y.png", "https://x/y.png", "HTTPS://X/Y.PNG"}
	local := []string{"/tmp/a.png", "a.png", "./a.png", "file:///tmp/a.png", ""}
	for _, ref := range remote {
		if !isRemote(ref) {
			t.Errorf("%q should be remote", ref)
		}
	}
	for _, ref := range local {
		if isRemote(ref) {
			t.Errorf("%q should not be remote", ref)
		}
	}
}
