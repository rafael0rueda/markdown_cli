// Package settings reads mdv's configuration file.
//
// The file holds standing defaults for command-line flags, one per line:
//
//	# Comments take a whole line.
//	theme = light
//	width = 90
//	ascii
//
// A key is a flag's long name. A bare key turns a boolean flag on. Anything
// given on the command line wins over the file, so the file only ever changes
// what mdv does when nothing more specific was asked for.
//
// The format is deliberately the command line and nothing more. Every setting
// already has a name, a type and validation as a flag, and a second vocabulary
// for the same options would be two things to document and keep in step.
package settings

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Setting is one line of the file.
type Setting struct {
	Key   string
	Value string
	// Bare is set when the line held only a key, which switches a boolean
	// flag on.
	Bare bool
	// Line is the 1-based line number, for error messages.
	Line int
}

// Error reports a problem at a specific line of the file.
type Error struct {
	Path string
	Line int
	Err  error
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s:%d: %v", e.Path, e.Line, e.Err)
}

func (e *Error) Unwrap() error { return e.Err }

// Path returns where the configuration file is looked for, and whether that
// location was asked for explicitly.
//
// The distinction matters to the caller: no file at the default location is
// the normal state of a fresh install, while no file where MDV_CONFIG points
// is a mistake worth reporting.
func Path() (path string, explicit bool) {
	if p := os.Getenv("MDV_CONFIG"); p != "" {
		return p, true
	}
	// The XDG base directory spec says a relative XDG_CONFIG_HOME is invalid
	// and must be ignored.
	if dir := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(dir) {
		return filepath.Join(dir, "mdv", "config"), false
	}
	// ~/.config rather than os.UserConfigDir, which is ~/Library/Application
	// Support on macOS: command-line tools keep their dotfiles in ~/.config
	// there too, and one documented location beats a per-platform one.
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, ".config", "mdv", "config"), false
}

// Load reads the settings in the file at path. A missing file is reported as
// an error satisfying errors.Is(err, fs.ErrNotExist), for the caller to
// ignore or not.
func Load(path string) ([]Setting, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Parse(f, path)
}

// Parse reads settings from r. The name is used in error messages.
func Parse(r io.Reader, name string) ([]Setting, error) {
	var settings []Setting
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if n == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		s, err := parseLine(line)
		if err != nil {
			return nil, &Error{Path: name, Line: n, Err: err}
		}
		s.Line = n
		settings = append(settings, s)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return settings, nil
}

// parseLine splits one non-blank, non-comment line into a setting.
func parseLine(line string) (Setting, error) {
	key, value, hasValue := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	// Leading dashes are tolerated so that a flag copied from a shell command
	// works as it is: --theme=light means the same as theme = light.
	key = strings.TrimLeft(key, "-")
	if key == "" {
		return Setting{}, fmt.Errorf("missing setting name in %q", line)
	}
	if strings.ContainsAny(key, " \t") {
		return Setting{}, fmt.Errorf("expected \"name = value\", got %q", line)
	}
	if !hasValue {
		return Setting{Key: key, Bare: true}, nil
	}
	return Setting{Key: key, Value: unquote(strings.TrimSpace(value))}, nil
}

// unquote strips one pair of matching quotes, so a value can keep leading or
// trailing spaces, or be written the way a shell would need it.
func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
