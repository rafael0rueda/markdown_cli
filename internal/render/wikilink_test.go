package render

import (
	"strings"
	"testing"

	"mdv/internal/theme"
)

// stubLinks resolves any target that appears in its map.
type stubLinks struct {
	embeds map[string]string
	notes  map[string]string
}

func (s stubLinks) ResolveEmbed(target string) (string, bool) {
	path, ok := s.embeds[target]
	return path, ok
}

func (s stubLinks) ResolveNote(target string) (string, bool) {
	path, ok := s.notes[target]
	return path, ok
}

func TestParseWikilinkBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		embed    bool
		target   string
		fragment string
		display  string
		width    int
		height   int
	}{
		{name: "plain note", body: "Some Note", target: "Some Note"},
		{name: "with alias", body: "Some Note|display text", target: "Some Note", display: "display text"},
		{name: "with heading", body: "Some Note#Heading", target: "Some Note", fragment: "Heading"},
		{name: "heading and alias", body: "Note#Head|label", target: "Note", fragment: "Head", display: "label"},
		{name: "block reference", body: "Note#^block-id", target: "Note", fragment: "^block-id"},
		{name: "path", body: "folder/Some Note", target: "folder/Some Note"},

		{name: "embed", body: "pic.png", embed: true, target: "pic.png"},
		{name: "embed with width", body: "pic.png|300", embed: true, target: "pic.png", width: 300},
		{name: "embed with both", body: "pic.png|300x200", embed: true, target: "pic.png", width: 300, height: 200},
		{name: "embed with spaces", body: "Pasted image 01.png", embed: true, target: "Pasted image 01.png"},
		// A pipe value that is not a size is a caption, which is what Obsidian
		// does with it too.
		{name: "embed with caption", body: "pic.png|the big one", embed: true, target: "pic.png", display: "the big one"},
		{name: "embed with bad size", body: "pic.png|0", embed: true, target: "pic.png", display: "0"},
		// In a link the pipe is always a label, never a size.
		{name: "link with numeric alias", body: "Note|300", target: "Note", display: "300"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseWikilinkBody(tt.body, tt.embed)
			if got == nil {
				t.Fatal("parse returned nil")
			}
			if got.Target != tt.target {
				t.Errorf("Target = %q, want %q", got.Target, tt.target)
			}
			if got.Fragment != tt.fragment {
				t.Errorf("Fragment = %q, want %q", got.Fragment, tt.fragment)
			}
			if got.Display != tt.display {
				t.Errorf("Display = %q, want %q", got.Display, tt.display)
			}
			if got.Width != tt.width || got.Height != tt.height {
				t.Errorf("size = %dx%d, want %dx%d", got.Width, got.Height, tt.width, tt.height)
			}
		})
	}

	if parseWikilinkBody("", false) != nil {
		t.Error("an empty body should not parse")
	}
	if parseWikilinkBody("|alias", false) != nil {
		t.Error("a body with no target should not parse")
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		in     string
		w, h   int
		wantOK bool
	}{
		{"300", 300, 0, true},
		{"300x200", 300, 200, true},
		{" 300 x 200 ", 300, 200, true},
		{"0", 0, 0, false},
		{"-5", 0, 0, false},
		{"300x0", 0, 0, false},
		{"big", 0, 0, false},
		{"", 0, 0, false},
		{"300px", 0, 0, false},
	}
	for _, tt := range tests {
		w, h, ok := parseSize(tt.in)
		if ok != tt.wantOK || w != tt.w || h != tt.h {
			t.Errorf("parseSize(%q) = %d, %d, %v; want %d, %d, %v",
				tt.in, w, h, ok, tt.w, tt.h, tt.wantOK)
		}
	}
}

func renderWikilinks(t *testing.T, source string, links LinkResolver, images ImageHandler) *Doc {
	t.Helper()
	doc, err := Render([]byte(source), Options{
		Width:    72,
		Theme:    theme.Plain(),
		LinkMode: LinkInline,
		Links:    links,
		Images:   images,
	})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestWikilinkRendersAsLink(t *testing.T) {
	links := stubLinks{notes: map[string]string{"Some Note": "/vault/Some Note.md"}}
	doc := renderWikilinks(t, "See [[Some Note]] for details.\n", links, nil)

	text := doc.Text()
	if !strings.Contains(text, "Some Note") {
		t.Errorf("link label missing: %q", text)
	}
	if !strings.Contains(text, "/vault/Some Note.md") {
		t.Errorf("resolved path missing: %q", text)
	}
	// The wikilink brackets themselves must not survive into the output.
	if strings.Contains(text, "[[") {
		t.Errorf("raw wikilink syntax leaked: %q", text)
	}
}

func TestWikilinkAliasIsShown(t *testing.T) {
	links := stubLinks{notes: map[string]string{"Some Note": "/vault/Some Note.md"}}
	doc := renderWikilinks(t, "See [[Some Note|the note]].\n", links, nil)
	if !strings.Contains(doc.Text(), "the note") {
		t.Errorf("alias not used as the label: %q", doc.Text())
	}
}

// TestWikilinkUnresolvedStillShowsLabel covers a link to a note that has not
// been written yet, which is a normal thing to have in a vault.
func TestWikilinkUnresolvedStillShowsLabel(t *testing.T) {
	doc := renderWikilinks(t, "See [[Future Note]].\n", stubLinks{}, nil)
	if !strings.Contains(doc.Text(), "Future Note") {
		t.Errorf("label missing for an unresolved link: %q", doc.Text())
	}
}

func TestWikilinkWithoutResolver(t *testing.T) {
	// Outside a vault there is nothing to resolve against, but the label is
	// still the useful part of the text.
	doc := renderWikilinks(t, "See [[Some Note]].\n", nil, nil)
	if !strings.Contains(doc.Text(), "Some Note") {
		t.Errorf("label missing with no resolver: %q", doc.Text())
	}
	if strings.Contains(doc.Text(), "[[") {
		t.Errorf("raw syntax leaked: %q", doc.Text())
	}
}

func TestEmbedBecomesAnImage(t *testing.T) {
	links := stubLinks{embeds: map[string]string{"pic.png": "/vault/Assets/pic.png"}}
	images := &fakeImages{cols: 20, rows: 6}
	doc := renderWikilinks(t, "![[pic.png]]\n", links, images)

	if len(doc.Images) != 1 {
		t.Fatalf("got %d placements, want 1", len(doc.Images))
	}
	if doc.Images[0].Ref != "/vault/Assets/pic.png" {
		t.Errorf("ref = %q", doc.Images[0].Ref)
	}
}

// TestEmbedOnItsOwnLineInAParagraph is the case from a real vault: markdown
// joins the sentence and the embed into one paragraph, but the embed is on its
// own source line and must still become a block image.
func TestEmbedOnItsOwnLineInAParagraph(t *testing.T) {
	links := stubLinks{embeds: map[string]string{"pic.png": "/vault/pic.png"}}
	images := &fakeImages{cols: 20, rows: 4}

	source := "With a 's' in the group execute position.\n![[pic.png]]\n"
	doc := renderWikilinks(t, source, links, images)

	if len(doc.Images) != 1 {
		t.Fatalf("got %d placements, want 1", len(doc.Images))
	}
	// The sentence must not have been swallowed into the image block.
	if !strings.Contains(doc.Text(), "group execute position") {
		t.Errorf("the sentence went missing: %q", doc.Text())
	}
	// The image has to start after the sentence, not on the same line.
	sentenceLine := -1
	for i, line := range doc.Lines {
		if strings.Contains(line.Text(), "group execute") {
			sentenceLine = i
		}
	}
	if sentenceLine < 0 {
		t.Fatal("sentence not found in any line")
	}
	if doc.Images[0].Line <= sentenceLine {
		t.Errorf("image starts at line %d, on or before the sentence at %d",
			doc.Images[0].Line, sentenceLine)
	}
}

func TestEmbedBetweenTwoSentences(t *testing.T) {
	links := stubLinks{embeds: map[string]string{"pic.png": "/vault/pic.png"}}
	images := &fakeImages{cols: 20, rows: 3}

	source := "Before the picture.\n![[pic.png]]\nAfter the picture.\n"
	doc := renderWikilinks(t, source, links, images)

	if len(doc.Images) != 1 {
		t.Fatalf("got %d placements, want 1", len(doc.Images))
	}
	text := doc.Text()
	if !strings.Contains(text, "Before the picture.") || !strings.Contains(text, "After the picture.") {
		t.Errorf("surrounding sentences lost: %q", text)
	}
	// Both sentences must be their own lines, not run together across the
	// image's rows.
	for _, line := range doc.Lines {
		if strings.Contains(line.Text(), "Before") && strings.Contains(line.Text(), "After") {
			t.Errorf("the sentences were joined: %q", line.Text())
		}
	}
}

// TestEmbedInASentenceStaysText keeps the rule that only a lone image on its
// line becomes a picture.
func TestEmbedInASentenceStaysText(t *testing.T) {
	links := stubLinks{embeds: map[string]string{"pic.png": "/vault/pic.png"}}
	images := &fakeImages{cols: 20, rows: 3}

	doc := renderWikilinks(t, "Look at ![[pic.png]] closely.\n", links, images)
	if len(doc.Images) != 0 {
		t.Errorf("an embed inside a sentence was placed as a picture")
	}
	if !strings.Contains(doc.Text(), "pic.png") {
		t.Errorf("embed label missing: %q", doc.Text())
	}
}

func TestEmbedSizeHintPassedThrough(t *testing.T) {
	links := stubLinks{embeds: map[string]string{"pic.png": "/vault/pic.png"}}
	images := &fakeImages{cols: 20, rows: 3}

	renderWikilinks(t, "![[pic.png|300x200]]\n", links, images)
	if len(images.hints) != 1 {
		t.Fatalf("got %d measure calls, want 1", len(images.hints))
	}
	if images.hints[0].Width != 300 || images.hints[0].Height != 200 {
		t.Errorf("hint = %+v, want 300x200", images.hints[0])
	}
}

// TestUnresolvedEmbedShowsLabel covers an attachment that has been deleted or
// renamed, which must not take the rest of the note down with it.
func TestUnresolvedEmbedShowsLabel(t *testing.T) {
	images := &fakeImages{cols: 20, rows: 3}
	doc := renderWikilinks(t, "![[gone.png]]\n", stubLinks{}, images)

	if len(doc.Images) != 0 {
		t.Error("an unresolvable embed was placed")
	}
	if !strings.Contains(doc.Text(), "gone.png") {
		t.Errorf("label missing: %q", doc.Text())
	}
}

// TestOrdinaryMarkdownStillWorks guards against the wikilink parser, which
// triggers on the same characters, swallowing normal links and images.
func TestOrdinaryMarkdownStillWorks(t *testing.T) {
	tests := []struct {
		source string
		want   []string
	}{
		{"[a link](http://example.com)\n", []string{"a link", "http://example.com"}},
		{"![an image](pic.png)\n", []string{"an image", "pic.png"}},
		{"text [with] brackets\n", []string{"with"}},
		{"an array a[i][j] index\n", []string{"a[i][j]"}},
		{"[not a wikilink]\n", []string{"[not a wikilink]"}},
		{"![[unclosed\n", []string{"unclosed"}},
		{"[[unclosed\n", []string{"unclosed"}},
	}
	for _, tt := range tests {
		doc := renderWikilinks(t, tt.source, stubLinks{}, nil)
		for _, want := range tt.want {
			if !strings.Contains(doc.Text(), want) {
				t.Errorf("%q: output %q is missing %q", tt.source, doc.Text(), want)
			}
		}
	}
}

func TestWikilinkLabel(t *testing.T) {
	tests := []struct {
		name string
		link Wikilink
		want string
	}{
		{"target only", Wikilink{Target: "Note"}, "Note"},
		{"alias wins", Wikilink{Target: "Note", Display: "alias"}, "alias"},
		// Two links into different sections of one note must not read the same.
		{"heading shown", Wikilink{Target: "Note", Fragment: "Section"}, "Note > Section"},
		{"alias beats heading", Wikilink{Target: "Note", Fragment: "Section", Display: "x"}, "x"},
		{"heading only", Wikilink{Fragment: "Section"}, "Section"},
	}
	for _, tt := range tests {
		if got := tt.link.Label(); got != tt.want {
			t.Errorf("%s: Label() = %q, want %q", tt.name, got, tt.want)
		}
	}
}
