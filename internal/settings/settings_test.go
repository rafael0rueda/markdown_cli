package settings

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	src := `# A comment
theme = light

   width=90
ascii
--links=hide
vault = "/home/me/My Notes"
frontmatter = 'raw'
empty =
`
	got, err := Parse(strings.NewReader(src), "cfg")
	if err != nil {
		t.Fatal(err)
	}
	want := []Setting{
		{Key: "theme", Value: "light", Line: 2},
		{Key: "width", Value: "90", Line: 4},
		{Key: "ascii", Bare: true, Line: 5},
		{Key: "links", Value: "hide", Line: 6},
		{Key: "vault", Value: "/home/me/My Notes", Line: 7},
		{Key: "frontmatter", Value: "raw", Line: 8},
		{Key: "empty", Value: "", Line: 9},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestParseErrorsNameTheLine(t *testing.T) {
	tests := []struct {
		src  string
		line int
	}{
		{"theme = dark\nthe me = light\n", 2},
		{"\n\n= light\n", 3},
		{"--\n", 1},
	}
	for _, tt := range tests {
		_, err := Parse(strings.NewReader(tt.src), "cfg")
		var perr *Error
		if !errors.As(err, &perr) {
			t.Errorf("%q: want *Error, got %v", tt.src, err)
			continue
		}
		if perr.Line != tt.line || perr.Path != "cfg" {
			t.Errorf("%q: got %s:%d, want cfg:%d", tt.src, perr.Path, perr.Line, tt.line)
		}
		if !strings.HasPrefix(err.Error(), "cfg:") {
			t.Errorf("%q: message should start with the location: %v", tt.src, err)
		}
	}
}

func TestParseKeepsInnerQuotesAndHashes(t *testing.T) {
	got, err := Parse(strings.NewReader("vault = ~/notes/#archive\nx = \"a\n"), "cfg")
	if err != nil {
		t.Fatal(err)
	}
	// Comments take whole lines only, so a # inside a value is kept; and an
	// unbalanced quote is left alone rather than half-stripped.
	if got[0].Value != "~/notes/#archive" {
		t.Errorf("hash in value: got %q", got[0].Value)
	}
	if got[1].Value != `"a` {
		t.Errorf("unbalanced quote: got %q", got[1].Value)
	}
}

func TestParseStripsByteOrderMark(t *testing.T) {
	got, err := Parse(strings.NewReader("\ufefftheme = dark\n"), "cfg")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "theme" {
		t.Errorf("got %+v", got)
	}
}

func TestParseCRLF(t *testing.T) {
	got, err := Parse(strings.NewReader("theme = dark\r\nascii\r\n"), "cfg")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Value != "dark" || got[1].Key != "ascii" {
		t.Errorf("got %+v", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "config"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("want ErrNotExist, got %v", err)
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("theme = light\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Value != "light" {
		t.Errorf("got %+v", got)
	}
}

func TestPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	t.Setenv("MDV_CONFIG", "/somewhere/mdv.conf")
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if p, explicit := Path(); p != "/somewhere/mdv.conf" || !explicit {
		t.Errorf("MDV_CONFIG: got %q, %v", p, explicit)
	}

	t.Setenv("MDV_CONFIG", "")
	if p, explicit := Path(); p != filepath.Join("/xdg", "mdv", "config") || explicit {
		t.Errorf("XDG_CONFIG_HOME: got %q, %v", p, explicit)
	}

	// A relative XDG_CONFIG_HOME is invalid per the spec and ignored.
	t.Setenv("XDG_CONFIG_HOME", "relative/dir")
	if p, _ := Path(); p != filepath.Join(home, ".config", "mdv", "config") {
		t.Errorf("relative XDG_CONFIG_HOME: got %q", p)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	if p, explicit := Path(); p != filepath.Join(home, ".config", "mdv", "config") || explicit {
		t.Errorf("default: got %q, %v", p, explicit)
	}
}
