package render

import (
	"strings"
	"testing"

	"mdv/internal/theme"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		wantOK   bool
		wantMeta string
		wantBody string
	}{
		{
			name:     "simple",
			source:   "---\ntags: a\n---\n# Title\n",
			wantOK:   true,
			wantMeta: "tags: a\n",
			wantBody: "# Title\n",
		},
		{
			name:     "list values",
			source:   "---\ntags:\n  - security\n  - linux\n---\n\nBody\n",
			wantOK:   true,
			wantMeta: "tags:\n  - security\n  - linux\n",
			wantBody: "\nBody\n",
		},
		{
			name:     "closed with dots",
			source:   "---\nkey: v\n...\nBody\n",
			wantOK:   true,
			wantMeta: "key: v\n",
			wantBody: "Body\n",
		},
		{
			name:     "nothing after the block",
			source:   "---\nkey: v\n---",
			wantOK:   true,
			wantMeta: "key: v\n",
			wantBody: "",
		},
		{
			name:     "carriage returns",
			source:   "---\r\nkey: v\r\n---\r\nBody\r\n",
			wantOK:   true,
			wantBody: "Body\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, body, ok := splitFrontmatter([]byte(tt.source))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if tt.wantMeta != "" && string(meta) != tt.wantMeta {
				t.Errorf("meta = %q, want %q", meta, tt.wantMeta)
			}
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", body, tt.wantBody)
			}
		})
	}
}

// TestSplitFrontmatterRejectsProse is the guard against a document that simply
// opens with a horizontal rule, which would otherwise have its first paragraph
// eaten as properties.
func TestSplitFrontmatterRejectsProse(t *testing.T) {
	tests := []string{
		"---\nJust some prose here.\n---\nMore.\n",
		"---\n\n---\nBody\n",
		"# Title\n\n---\nkey: v\n---\n", // not at the start of the document
		"---\nkey: v\n",                 // never closed
		"----\nkey: v\n----\n",          // four dashes is not a delimiter
		"",
		"no frontmatter at all\n",
	}
	for _, source := range tests {
		meta, body, ok := splitFrontmatter([]byte(source))
		if ok {
			t.Errorf("%q was treated as frontmatter: meta=%q", source, meta)
		}
		if string(body) != source {
			t.Errorf("%q: body was altered to %q", source, body)
		}
	}
}

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		meta string
		want []metaEntry
	}{
		{
			name: "scalar",
			meta: "title: My Note\n",
			want: []metaEntry{{key: "title", values: []string{"My Note"}}},
		},
		{
			name: "sequence",
			meta: "tags:\n  - security\n  - linux\n",
			want: []metaEntry{{key: "tags", values: []string{"security", "linux"}}},
		},
		{
			name: "inline list",
			meta: "tags: [security, linux]\n",
			want: []metaEntry{{key: "tags", values: []string{"security", "linux"}}},
		},
		{
			name: "quoted values",
			meta: "title: \"Quoted\"\nalias: 'Single'\n",
			want: []metaEntry{
				{key: "title", values: []string{"Quoted"}},
				{key: "alias", values: []string{"Single"}},
			},
		},
		{
			name: "several keys",
			meta: "title: T\ntags:\n  - a\ndate: 2026-09-03\n",
			want: []metaEntry{
				{key: "title", values: []string{"T"}},
				{key: "tags", values: []string{"a"}},
				{key: "date", values: []string{"2026-09-03"}},
			},
		},
		{
			name: "comments and blanks are skipped",
			meta: "# a comment\n\ntitle: T\n",
			want: []metaEntry{{key: "title", values: []string{"T"}}},
		},
		{
			name: "empty value",
			meta: "tags:\n",
			want: []metaEntry{{key: "tags"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseFrontmatter([]byte(tt.meta))
			if len(got) != len(tt.want) {
				t.Fatalf("got %d entries, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i].key != tt.want[i].key {
					t.Errorf("entry %d: key = %q, want %q", i, got[i].key, tt.want[i].key)
				}
				if strings.Join(got[i].values, "|") != strings.Join(tt.want[i].values, "|") {
					t.Errorf("entry %d: values = %v, want %v", i, got[i].values, tt.want[i].values)
				}
			}
		})
	}
}

// TestParseFrontmatterKeepsUnknownShapes checks the fallback. This is a display
// summary rather than a YAML implementation, so anything it cannot model has to
// be shown rather than silently dropped.
func TestParseFrontmatterKeepsUnknownShapes(t *testing.T) {
	got := parseFrontmatter([]byte("nested:\n  inner: value\n"))
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1: %+v", len(got), got)
	}
	if got[0].key != "nested" {
		t.Errorf("key = %q", got[0].key)
	}
	if len(got[0].values) == 0 {
		t.Error("a nested map produced no indication that anything was there")
	}
}

func TestFrontmatterModes(t *testing.T) {
	source := "---\ntags:\n  - security\n  - linux\n---\n\n# Title\n\nBody.\n"

	tests := []struct {
		mode       FrontmatterMode
		wantTags   bool
		wantDashes bool
	}{
		{FrontmatterMeta, true, false},
		{FrontmatterHide, false, false},
		// Raw leaves it to the markdown parser, which makes the delimiters
		// into horizontal rules.
		{FrontmatterRaw, true, true},
	}

	for _, tt := range tests {
		doc, err := Render([]byte(source), Options{
			Width: 60, Theme: theme.Plain(), Frontmatter: tt.mode,
		})
		if err != nil {
			t.Fatal(err)
		}
		text := doc.Text()

		if got := strings.Contains(text, "security"); got != tt.wantTags {
			t.Errorf("mode %v: tags present = %v, want %v\n%s", tt.mode, got, tt.wantTags, text)
		}
		if got := strings.Contains(text, "────"); got != tt.wantDashes {
			t.Errorf("mode %v: rules present = %v, want %v\n%s", tt.mode, got, tt.wantDashes, text)
		}
		// The body must survive in every mode.
		if !strings.Contains(text, "Title") || !strings.Contains(text, "Body.") {
			t.Errorf("mode %v: body lost\n%s", tt.mode, text)
		}
	}
}

func TestFrontmatterRendersCompactly(t *testing.T) {
	source := "---\ntitle: A Note\ntags:\n  - security\n  - linux\n---\n\n# Title\n"
	doc, err := Render([]byte(source), Options{Width: 60, Theme: theme.Plain()})
	if err != nil {
		t.Fatal(err)
	}
	text := doc.Text()

	if !strings.Contains(text, "security, linux") {
		t.Errorf("list values should be joined onto one line:\n%s", text)
	}
	// Keys are aligned into a column, so the shorter key is padded out to the
	// width of the longest.
	if !strings.Contains(text, "title  ") || !strings.Contains(text, "tags   ") {
		t.Errorf("keys are not aligned:\n%s", text)
	}
}

func TestParseFrontmatterMode(t *testing.T) {
	for _, s := range []string{"", "meta", "show", "hide", "none", "raw", "RAW"} {
		if _, err := ParseFrontmatterMode(s); err != nil {
			t.Errorf("ParseFrontmatterMode(%q): %v", s, err)
		}
	}
	if _, err := ParseFrontmatterMode("yaml"); err == nil {
		t.Error("an unknown mode should be rejected")
	}
}

func TestUnquote(t *testing.T) {
	tests := map[string]string{
		`"quoted"`:   "quoted",
		`'quoted'`:   "quoted",
		`plain`:      "plain",
		`"unclosed`:  `"unclosed`,
		`""`:         "",
		`"`:          `"`,
		`  spaced  `: "spaced",
	}
	for in, want := range tests {
		if got := unquote(in); got != want {
			t.Errorf("unquote(%q) = %q, want %q", in, got, want)
		}
	}
}
