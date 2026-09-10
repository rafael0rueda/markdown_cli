package main

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runWithConfig runs the command with a configuration file holding the given
// text, supplied through MDV_CONFIG.
func runWithConfig(t *testing.T, config string, args ...string) (string, error) {
	t.Helper()
	path := writeTemp(t, "config", config)
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("MDV_CONFIG", path)

	var out, errBuf strings.Builder
	err := run(args, &out, &errBuf)
	return out.String(), err
}

// maxLineWidth reports the widest line in plain-text output.
func maxLineWidth(s string) int {
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		widest = max(widest, len(line))
	}
	return widest
}

var longDoc = strings.Repeat("word ", 200)

func TestConfigSetsDefaults(t *testing.T) {
	doc := writeTemp(t, "doc.md", longDoc)
	out, err := runWithConfig(t, "# narrow\nwidth = 30\n", doc)
	if err != nil {
		t.Fatal(err)
	}
	if w := maxLineWidth(out); w > 30 || w < 25 {
		t.Errorf("config width 30 not applied: widest line is %d", w)
	}
}

func TestCommandLineBeatsConfig(t *testing.T) {
	doc := writeTemp(t, "doc.md", longDoc)
	// -w is shorthand for width; giving it has to count as giving width, or
	// the file would quietly overwrite what was typed.
	for _, flagName := range []string{"-width", "-w"} {
		out, err := runWithConfig(t, "width = 30\n", flagName, "60", doc)
		if err != nil {
			t.Fatal(err)
		}
		if w := maxLineWidth(out); w <= 30 || w > 60 {
			t.Errorf("%s 60 should beat the file's 30: widest line is %d", flagName, w)
		}
	}
}

func TestConfigShorthandKey(t *testing.T) {
	doc := writeTemp(t, "doc.md", longDoc)
	out, err := runWithConfig(t, "w = 30\n", doc)
	if err != nil {
		t.Fatal(err)
	}
	if w := maxLineWidth(out); w > 30 {
		t.Errorf("w = 30 not applied: widest line is %d", w)
	}
}

func TestConfigBareBoolean(t *testing.T) {
	doc := writeTemp(t, "doc.md", "- bullet\n")
	out, err := runWithConfig(t, "ascii\n", doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "•") || !strings.Contains(out, "*") {
		t.Errorf("bare ascii not applied: %q", out)
	}
}

func TestConfigBooleanCanBeTurnedBackOff(t *testing.T) {
	doc := writeTemp(t, "doc.md", "- bullet\n")
	out, err := runWithConfig(t, "ascii\n", "-ascii=false", doc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "•") {
		t.Errorf("-ascii=false should override the file: %q", out)
	}
}

func TestConfigAcceptsFlagSyntax(t *testing.T) {
	doc := writeTemp(t, "doc.md", longDoc)
	out, err := runWithConfig(t, "--width=30\n", doc)
	if err != nil {
		t.Fatal(err)
	}
	if w := maxLineWidth(out); w > 30 {
		t.Errorf("--width=30 not applied: widest line is %d", w)
	}
}

func TestConfigErrors(t *testing.T) {
	doc := writeTemp(t, "doc.md", "text\n")
	tests := []struct {
		config string
		line   string
		want   string
	}{
		{"theme = dark\nthme = light\n", ":2:", `unknown setting "thme"`},
		{"width = wide\n", ":1:", "width"},
		{"\n\ntheme\n", ":3:", "needs a value"},
		{"version\n", ":1:", "command line"},
		{"caps = true\n", ":1:", "command line"},
		{"config = /etc/other\n", ":1:", "command line"},
	}
	for _, tt := range tests {
		_, err := runWithConfig(t, tt.config, doc)
		if err == nil {
			t.Errorf("%q: expected an error", tt.config)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "config"+tt.line) || !strings.Contains(msg, tt.want) {
			t.Errorf("%q: error should name the line and say %q, got: %v", tt.config, tt.want, err)
		}
		// A bad file is not a bad command line, so it must not take the
		// usage-error exit path, which prints nothing of its own.
		var uerr usageError
		if errors.As(err, &uerr) {
			t.Errorf("%q: reported as a usage error", tt.config)
		}
	}
}

func TestConfigValuesAreValidated(t *testing.T) {
	doc := writeTemp(t, "doc.md", "text\n")
	if _, err := runWithConfig(t, "theme = solarized\n", doc); err == nil {
		t.Error("an unknown theme in the file should fail like it does on the command line")
	}
}

func TestNoConfigIgnoresTheFile(t *testing.T) {
	doc := writeTemp(t, "doc.md", "text\n")
	if _, err := runWithConfig(t, "this is not valid\n", "-no-config", doc); err != nil {
		t.Errorf("-no-config should not read the file: %v", err)
	}
}

func TestVersionIgnoresABrokenConfig(t *testing.T) {
	out, err := runWithConfig(t, "this is not valid\n", "-version")
	if err != nil || !strings.HasPrefix(out, "mdv ") {
		t.Errorf("-version should work regardless of the file: %q, %v", out, err)
	}
}

func TestConfigFlagPicksTheFile(t *testing.T) {
	doc := writeTemp(t, "doc.md", longDoc)
	other := writeTemp(t, "other", "width = 30\n")
	out, err := runWithConfig(t, "width = 70\n", "-config", other, doc)
	if err != nil {
		t.Fatal(err)
	}
	if w := maxLineWidth(out); w > 30 {
		t.Errorf("-config should replace MDV_CONFIG: widest line is %d", w)
	}
}

func TestMissingConfigFile(t *testing.T) {
	doc := writeTemp(t, "doc.md", "text\n")
	missing := filepath.Join(t.TempDir(), "nope")

	// Pointed at explicitly, a missing file is a mistake.
	_, _, err := runCLI(t, "-config", missing, doc)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("-config with a missing file: %v", err)
	}
	t.Setenv("MDV_CONFIG", missing)
	var out, errBuf strings.Builder
	if err := run([]string{doc}, &out, &errBuf); err == nil {
		t.Error("MDV_CONFIG naming a missing file should fail")
	}

	// At the default location, it is how every install starts out.
	if _, _, err := runCLI(t, doc); err != nil {
		t.Errorf("no file at the default location: %v", err)
	}
}

func TestConfigFromXDGConfigHome(t *testing.T) {
	doc := writeTemp(t, "doc.md", longDoc)
	xdg := t.TempDir()
	if err := os.MkdirAll(filepath.Join(xdg, "mdv"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "mdv", "config"), []byte("width = 30\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("MDV_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)

	var out, errBuf strings.Builder
	if err := run([]string{doc}, &out, &errBuf); err != nil {
		t.Fatal(err)
	}
	if w := maxLineWidth(out.String()); w > 30 {
		t.Errorf("file in XDG_CONFIG_HOME not read: widest line is %d", w)
	}
}

func TestBadFlagIsAUsageError(t *testing.T) {
	_, stderr, err := runCLI(t, "-no-such-flag")
	var uerr usageError
	if !errors.As(err, &uerr) {
		t.Fatalf("want a usage error, got %v", err)
	}
	// The flag package prints the complaint itself; main relies on that.
	if !strings.Contains(stderr, "no-such-flag") {
		t.Errorf("stderr should explain the problem: %q", stderr)
	}
}

func TestHelpIsNotAUsageError(t *testing.T) {
	_, stderr, err := runCLI(t, "-h")
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("want flag.ErrHelp, got %v", err)
	}
	if !strings.Contains(stderr, "config") {
		t.Errorf("help should mention the configuration file: %q", stderr)
	}
}

func TestVersionFrom(t *testing.T) {
	tests := []struct{ stamped, module, want string }{
		{"1.2.0", "v1.2.0", "1.2.0"}, // release build: the stamp wins
		{"dev", "v1.2.0", "v1.2.0"},  // go install ...@v1.2.0
		{"dev", "(devel)", "dev"},    // go build from a checkout
		{"dev", "", "dev"},
	}
	for _, tt := range tests {
		if got := versionFrom(tt.stamped, tt.module); got != tt.want {
			t.Errorf("versionFrom(%q, %q) = %q, want %q", tt.stamped, tt.module, got, tt.want)
		}
	}
}
