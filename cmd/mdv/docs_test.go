package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// These tests keep the documentation honest: a flag added to the code but not
// to the man page or the README fails the build rather than going unnoticed.

func allFlags(t *testing.T) []string {
	t.Helper()
	var cfg config
	fs := newFlagSet(&cfg, io.Discard)
	var names []string
	fs.VisitAll(func(f *flag.Flag) { names = append(names, f.Name) })
	if len(names) < 10 {
		t.Fatalf("suspiciously few flags: %v", names)
	}
	return names
}

func readDoc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestManPageDocumentsEveryFlag(t *testing.T) {
	page := readDoc(t, "../../docs/mdv.1")
	for _, name := range allFlags(t) {
		// roff needs hyphens escaped to print as a real minus sign, which is
		// also what makes them searchable and copyable from the rendered page.
		want := `\-` + strings.ReplaceAll(name, "-", `\-`)
		if !strings.Contains(page, want) {
			t.Errorf("docs/mdv.1 does not mention -%s", name)
		}
	}
}

func TestREADMEDocumentsEveryFlag(t *testing.T) {
	readme := readDoc(t, "../../README.md")
	for _, name := range allFlags(t) {
		if !strings.Contains(readme, "`-"+name+"`") {
			t.Errorf("README.md does not mention `-%s`", name)
		}
	}
}

func TestManPageRendersWithoutWarnings(t *testing.T) {
	groff, err := exec.LookPath("groff")
	if err != nil {
		t.Skip("groff not installed")
	}
	cmd := exec.Command(groff, "-man", "-ww", "-z", "../../docs/mdv.1")
	cmd.Env = append(os.Environ(), "LC_ALL=C.UTF-8")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("groff failed: %v\n%s", err, stderr.String())
	}
	if stderr.Len() > 0 {
		t.Errorf("groff warnings:\n%s", stderr.String())
	}
}
