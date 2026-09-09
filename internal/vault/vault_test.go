package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// buildVault creates a vault on disk from a map of relative paths to contents.
// A path with empty content is created as an empty file.
func buildVault(t *testing.T, config string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()

	if err := os.MkdirAll(filepath.Join(root, markerDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if config != "" {
		path := filepath.Join(root, markerDir, configFile)
		if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestFindWalksUp(t *testing.T) {
	root := buildVault(t, "", map[string]string{
		"a/b/c/note.md": "hi",
	})

	// From the note's own directory, and from every directory above it.
	for _, dir := range []string{"a/b/c", "a/b", "a", "."} {
		v := Find(filepath.Join(root, dir))
		if v == nil {
			t.Fatalf("no vault found from %q", dir)
		}
		if v.Root != root {
			t.Errorf("from %q: root = %q, want %q", dir, v.Root, root)
		}
	}
}

func TestFindFromAFilePath(t *testing.T) {
	root := buildVault(t, "", map[string]string{"note.md": "hi"})
	v := Find(filepath.Join(root, "note.md"))
	if v == nil || v.Root != root {
		t.Fatalf("Find on a file path did not locate the vault: %+v", v)
	}
}

// TestFindOutsideAVault is the ordinary case for a markdown file that is not
// part of a vault at all.
func TestFindOutsideAVault(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "note.md"), []byte("hi"), 0o644)
	if v := Find(filepath.Join(dir, "note.md")); v != nil {
		t.Errorf("found a vault where there is none: %+v", v)
	}
}

func TestFindNearestVaultWins(t *testing.T) {
	outer := buildVault(t, "", map[string]string{"inner/.obsidian/app.json": "{}", "inner/note.md": "hi"})
	inner := filepath.Join(outer, "inner")

	v := Find(filepath.Join(inner, "note.md"))
	if v == nil {
		t.Fatal("no vault found")
	}
	// A vault nested inside another is its own vault; links in its notes
	// resolve against it, not the parent.
	if v.Root != inner {
		t.Errorf("root = %q, want the nearer vault %q", v.Root, inner)
	}
}

func TestOpenReadsAttachmentFolder(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   string
	}{
		{"configured", `{"attachmentFolderPath":"04_Varios/Assets"}`, filepath.FromSlash("04_Varios/Assets")},
		{"absent", `{"vimMode":true}`, ""},
		{"same folder as note", `{"attachmentFolderPath":"./"}`, ""},
		{"dot", `{"attachmentFolderPath":"."}`, ""},
		{"empty", `{"attachmentFolderPath":""}`, ""},
		// A broken config must not stop the vault from working at all.
		{"malformed json", `{not json`, ""},
		{"no config file", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := buildVault(t, tt.config, nil)
			v := Open(root)
			if v.AttachmentDir != tt.want {
				t.Errorf("AttachmentDir = %q, want %q", v.AttachmentDir, tt.want)
			}
		})
	}
}

func TestResolveOrder(t *testing.T) {
	root := buildVault(t, `{"attachmentFolderPath":"Assets"}`, map[string]string{
		"Assets/pic.png":       "a",
		"notes/pic.png":        "b",
		"pic.png":              "c",
		"notes/deep/only.png":  "d",
		"Assets/Pasted 01.png": "e",
	})
	v := Open(root)
	docDir := filepath.Join(root, "notes")

	tests := []struct {
		name   string
		target string
		want   string
	}{
		// The attachment folder is searched first, which is where Obsidian
		// puts pasted images.
		{"attachment folder wins", "pic.png", "Assets/pic.png"},
		{"explicit subpath", "notes/pic.png", "notes/pic.png"},
		{"spaces in the name", "Pasted 01.png", "Assets/Pasted 01.png"},
		// Not in the attachment folder or beside the note, so found by
		// searching the vault.
		{"found by search", "only.png", "notes/deep/only.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := v.Resolve(tt.target, docDir)
			if !ok {
				t.Fatalf("Resolve(%q) failed", tt.target)
			}
			if want := filepath.Join(root, filepath.FromSlash(tt.want)); got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

// TestResolvePrefersDocumentFolderWithoutAttachmentDir covers a vault that has
// not configured an attachment folder, where images sit beside their note.
func TestResolvePrefersDocumentFolder(t *testing.T) {
	root := buildVault(t, "{}", map[string]string{
		"notes/pic.png": "a",
		"pic.png":       "b",
	})
	v := Open(root)

	got, ok := v.Resolve("pic.png", filepath.Join(root, "notes"))
	if !ok {
		t.Fatal("resolve failed")
	}
	if want := filepath.Join(root, "notes", "pic.png"); got != want {
		t.Errorf("got %q, want the copy beside the note %q", got, want)
	}
}

func TestResolveMissing(t *testing.T) {
	root := buildVault(t, "{}", map[string]string{"a.png": "x"})
	v := Open(root)
	for _, target := range []string{"nope.png", "", "   ", "deep/nope.png"} {
		if got, ok := v.Resolve(target, root); ok {
			t.Errorf("Resolve(%q) unexpectedly found %q", target, got)
		}
	}
}

// TestResolveStripsFragment covers [[note#heading]], where only the part
// before the hash names a file.
func TestResolveStripsFragment(t *testing.T) {
	root := buildVault(t, "{}", map[string]string{"note.md": "x"})
	v := Open(root)
	if _, ok := v.Resolve("note.md#Some Heading", root); !ok {
		t.Error("a fragment prevented the file from resolving")
	}
}

// TestResolveIgnoresDirectories checks that a directory sharing a link's name
// is not returned as if it were the file.
func TestResolveIgnoresDirectories(t *testing.T) {
	root := buildVault(t, "{}", map[string]string{"pic.png/inside.txt": "x"})
	v := Open(root)
	if got, ok := v.Resolve("pic.png", root); ok {
		t.Errorf("resolved to a directory: %q", got)
	}
}

func TestResolveNoteAddsExtension(t *testing.T) {
	root := buildVault(t, "{}", map[string]string{
		"notes/Some Note.md": "x",
		"other.md":           "y",
	})
	v := Open(root)

	got, ok := v.ResolveNote("Some Note", root)
	if !ok {
		t.Fatal("a bare note name did not resolve")
	}
	if want := filepath.Join(root, "notes", "Some Note.md"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// An explicit extension still works.
	if _, ok := v.ResolveNote("other.md", root); !ok {
		t.Error("an explicit .md did not resolve")
	}
	if _, ok := v.ResolveNote("missing", root); ok {
		t.Error("a note that does not exist should not resolve")
	}
}

// TestSearchPrefersShallowMatch mirrors how Obsidian disambiguates a name that
// appears in more than one folder.
func TestSearchPrefersShallowMatch(t *testing.T) {
	root := buildVault(t, "{}", map[string]string{
		"deep/deeper/dup.png": "a",
		"shallow/dup.png":     "b",
	})
	v := Open(root)

	got, ok := v.Resolve("dup.png", filepath.Join(root, "elsewhere"))
	if !ok {
		t.Fatal("resolve failed")
	}
	if want := filepath.Join(root, "shallow", "dup.png"); got != want {
		t.Errorf("got %q, want the shallower %q", got, want)
	}
}

// TestSearchSkipsHiddenDirectories keeps Obsidian's own configuration and any
// version-control directory out of the index.
func TestSearchSkipsHiddenDirectories(t *testing.T) {
	root := buildVault(t, "{}", map[string]string{
		".obsidian/plugins/hidden.png": "a",
		".git/objects/hidden.png":      "b",
	})
	v := Open(root)
	if got, ok := v.Resolve("hidden.png", root); ok {
		t.Errorf("found a file inside a hidden directory: %q", got)
	}
}

func TestResolveOnNilVault(t *testing.T) {
	var v *Vault
	if _, ok := v.Resolve("x.png", ""); ok {
		t.Error("a nil vault should resolve nothing")
	}
	if r := v.For("/tmp"); r != nil {
		t.Error("a nil vault should produce a nil resolver")
	}
}

func TestResolverDelegates(t *testing.T) {
	root := buildVault(t, `{"attachmentFolderPath":"Assets"}`, map[string]string{
		"Assets/pic.png": "a",
		"note.md":        "b",
	})
	r := Open(root).For(root)

	if got, ok := r.ResolveEmbed("pic.png"); !ok || got != filepath.Join(root, "Assets", "pic.png") {
		t.Errorf("ResolveEmbed = %q, %v", got, ok)
	}
	if got, ok := r.ResolveNote("note"); !ok || got != filepath.Join(root, "note.md") {
		t.Errorf("ResolveNote = %q, %v", got, ok)
	}

	var nilResolver *Resolver
	if _, ok := nilResolver.ResolveEmbed("pic.png"); ok {
		t.Error("a nil resolver should resolve nothing")
	}
	if _, ok := nilResolver.ResolveNote("note"); ok {
		t.Error("a nil resolver should resolve nothing")
	}
}
