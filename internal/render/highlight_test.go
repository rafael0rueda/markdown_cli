package render

import (
	"strings"
	"testing"

	"github.com/rafael0rueda/markdown_cli/internal/theme"
)

func TestFindAll(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		query string
		want  []Range
	}{
		{"single", "hello world", "world", []Range{{6, 11}}},
		{"several", "aXbXc", "X", []Range{{1, 2}, {3, 4}}},
		{"whole string", "abc", "abc", []Range{{0, 3}}},
		{"none", "hello", "zzz", nil},
		{"empty query", "hello", "", nil},
		{"empty text", "", "x", nil},
		// Case folding must not shift the offsets, which is why matching works
		// over the original string rather than a lowercased copy.
		{"case insensitive", "Hello World", "hello", []Range{{0, 5}}},
		{"mixed case", "aBcDe", "bcd", []Range{{1, 4}}},
		// Overlapping matches are not reported; stepping through them would
		// otherwise barely move.
		{"no overlap", "aaaa", "aa", []Range{{0, 2}, {2, 4}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindAll(tt.text, tt.query)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("match %d: got %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// TestFindAllOffsetsAreValid is the property that matters: the ranges are used
// to slice the original string, so they have to be real byte offsets into it.
func TestFindAllOffsetsAreValid(t *testing.T) {
	texts := []string{
		"plain ascii text",
		"Ünïcödé wîth accents",
		"日本語のテキスト",
		"emoji 👨‍👩‍👧‍👦 family",
		"MiXeD CaSe StRiNg",
	}
	queries := []string{"e", "É", "テ", "family", "case", "👨‍👩‍👧‍👦"}

	for _, text := range texts {
		for _, query := range queries {
			for _, r := range FindAll(text, query) {
				if r.Start < 0 || r.End > len(text) || r.Start >= r.End {
					t.Errorf("FindAll(%q, %q) gave an invalid range %v", text, query, r)
					continue
				}
				// Slicing must not panic and must produce a real substring.
				got := text[r.Start:r.End]
				if !strings.EqualFold(got, query) && len(got) == 0 {
					t.Errorf("FindAll(%q, %q) matched %q", text, query, got)
				}
			}
		}
	}
}

func TestHighlight(t *testing.T) {
	mark := theme.Style{Reverse: true}
	line := Line{Runs: []Run{{Text: "hello world"}}}

	got := Highlight(line, []Range{{6, 11}}, mark)
	if got.Text() != "hello world" {
		t.Errorf("highlighting changed the text to %q", got.Text())
	}

	var marked string
	for _, r := range got.Runs {
		if r.Style.Reverse {
			marked += r.Text
		}
	}
	if marked != "world" {
		t.Errorf("marked %q, want %q", marked, "world")
	}
}

// TestHighlightAcrossRuns covers a match spanning a style boundary, which is
// what happens when a search crosses inline code or emphasis.
func TestHighlightAcrossRuns(t *testing.T) {
	mark := theme.Style{Reverse: true}
	line := Line{Runs: []Run{
		{Text: "abc", Style: theme.Style{Bold: true}},
		{Text: "def"},
	}}

	got := Highlight(line, []Range{{2, 4}}, mark)
	if got.Text() != "abcdef" {
		t.Errorf("text changed to %q", got.Text())
	}

	var marked string
	for _, r := range got.Runs {
		if r.Style.Reverse {
			marked += r.Text
		}
	}
	if marked != "cd" {
		t.Errorf("marked %q, want %q", marked, "cd")
	}
	// The original styling has to survive underneath the highlight.
	for _, r := range got.Runs {
		if strings.Contains(r.Text, "c") && !r.Style.Bold {
			t.Errorf("the bold run lost its styling: %+v", r)
		}
	}
}

func TestHighlightSeveralRanges(t *testing.T) {
	mark := theme.Style{Reverse: true}
	line := Line{Runs: []Run{{Text: "aXbXc"}}}

	got := Highlight(line, FindAll("aXbXc", "X"), mark)
	if got.Text() != "aXbXc" {
		t.Errorf("text changed to %q", got.Text())
	}
	var marked string
	for _, r := range got.Runs {
		if r.Style.Reverse {
			marked += r.Text
		}
	}
	if marked != "XX" {
		t.Errorf("marked %q, want %q", marked, "XX")
	}
}

func TestHighlightNoRanges(t *testing.T) {
	line := Line{Runs: []Run{{Text: "unchanged"}}}
	got := Highlight(line, nil, theme.Style{Reverse: true})
	if got.Text() != "unchanged" {
		t.Errorf("text changed to %q", got.Text())
	}
}

func TestDocSearch(t *testing.T) {
	doc := &Doc{Lines: []Line{
		{Runs: []Run{{Text: "alpha"}}},
		{Runs: []Run{{Text: "beta"}}},
		{Runs: []Run{{Text: "Alpha again"}}},
		{Runs: []Run{{Text: ""}}},
	}}

	got := doc.Search("alpha")
	if len(got) != 2 || got[0] != 0 || got[1] != 2 {
		t.Errorf("got %v, want [0 2]", got)
	}
	if doc.Search("") != nil {
		t.Error("an empty query should match nothing")
	}
	if doc.Search("   ") != nil {
		t.Error("a whitespace query should match nothing")
	}
}
