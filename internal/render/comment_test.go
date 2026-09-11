package render

import (
	"strings"
	"testing"
)

func TestCommentsAreHidden(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"inline", "Visible %%secret%% text.\n", "Visible text."},
		{"several inline", "a %%x%% b %%y%% c\n", "a b c"},
		{"spanning lines", "Text %%spans\nlines%% continues.\n", "Text continues."},
		{"block", "Before.\n\n%%\nsecret\n%%\n\nAfter.\n", "Before.\n\nAfter."},
		{"block across paragraphs", "Before.\n\n%%\none\n\ntwo\n%%\n\nAfter.\n", "Before.\n\nAfter."},
		{"one-line block", "Before.\n\n%% secret %%\n\nAfter.\n", "Before.\n\nAfter."},
		{"paragraph of comments", "Before.\n\n%%x%% %%y%%\n\nAfter.\n", "Before.\n\nAfter."},
		{"leading comment", "%%x%% then visible\n", "then visible"},
		{"in a heading", "# Title %%draft%%\n", "# Title"},
		{"in a list", "- one %%x%%\n- two\n", "• one\n• two"},
		{"in a quote", "> quoted %%x%% text\n", "▌ quoted text"},
		{"empty", "a%%%%b\n", "ab"},
	}
	for _, tt := range tests {
		if got := renderText(t, tt.src, 60); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestCommentsInTables(t *testing.T) {
	got := renderText(t, "| a %%x%% | b |\n|---|---|\n| c | d %%y%% |\n", 60)
	if strings.Contains(got, "x") || strings.Contains(got, "y") {
		t.Errorf("comment shown in a table:\n%s", got)
	}
}

func TestNotComments(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"single percent", "100% sure, 50% done\n", "100% sure, 50% done"},
		{"unclosed inline", "A lone %% stays.\n", "A lone %% stays."},
		// Hiding the rest of the file over one stray %% would lose content
		// silently, so an unclosed block is shown instead.
		{"unclosed block", "Before.\n\n%%\nnever closed\n", "Before.\n\n%% never closed"},
		{"code span", "`a %%b%% c`\n", "a %%b%% c"},
	}
	for _, tt := range tests {
		if got := renderText(t, tt.src, 60); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
	got := renderText(t, "```\n%%\nin code\n%%\n```\n", 60)
	if !strings.Contains(got, "%%") || !strings.Contains(got, "in code") {
		t.Errorf("code block content hidden:\n%s", got)
	}
}

func TestCommentsAreNotSearchable(t *testing.T) {
	doc, err := Render([]byte("Public %%private%% words.\n"), Options{Width: 60})
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Search("private")) != 0 {
		t.Error("search found text inside a comment")
	}
}
