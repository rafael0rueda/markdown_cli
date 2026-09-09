// Package vault resolves Obsidian-style wikilinks against a note vault.
//
// Obsidian writes links as [[note]] and embeds as ![[image.png]], naming the
// target rather than giving a path. Resolving one means knowing where the
// vault begins and where its attachments are kept, neither of which can be
// worked out from the document alone.
package vault

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// markerDir is the directory Obsidian keeps its configuration in. Its presence
// is what defines the root of a vault.
const markerDir = ".obsidian"

// configFile holds the vault's settings, including where attachments live.
const configFile = "app.json"

// maxIndexFiles bounds the vault-wide search index. A vault is someone's notes
// directory, but nothing stops it being pointed at a home directory, and
// walking that on every run would be a surprise.
const maxIndexFiles = 200000

// Vault is a directory of notes with an Obsidian configuration at its root.
type Vault struct {
	// Root is the vault's top directory, the one holding .obsidian.
	Root string
	// AttachmentDir is where Obsidian files pasted images, relative to Root.
	// It is empty when the setting is absent, in which case attachments sit
	// beside the note that references them.
	AttachmentDir string

	// index maps a lowercased file name to the paths carrying it, built on
	// first use because most documents resolve everything without it.
	indexOnce sync.Once
	index     map[string][]string
}

// config is the subset of Obsidian's app.json that matters here.
type config struct {
	AttachmentFolderPath string `json:"attachmentFolderPath"`
}

// Find locates the vault containing the given document by walking up the
// directory tree looking for a .obsidian directory.
//
// It returns nil when there is none, which is the ordinary case for a markdown
// file that is not part of a vault.
func Find(docPath string) *Vault {
	dir, err := filepath.Abs(docPath)
	if err != nil {
		return nil
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	for {
		if info, err := os.Stat(filepath.Join(dir, markerDir)); err == nil && info.IsDir() {
			return Open(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil // reached the filesystem root
		}
		dir = parent
	}
}

// Open builds a Vault for a known root directory, reading its configuration if
// there is one.
func Open(root string) *Vault {
	v := &Vault{Root: root}

	data, err := os.ReadFile(filepath.Join(root, markerDir, configFile))
	if err != nil {
		return v
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		// A malformed config is not worth failing over; attachments simply
		// fall back to being looked for beside the note.
		return v
	}
	// The setting may be "./" or "" for "same folder as the note", or a
	// vault-relative path.
	path := strings.TrimSpace(cfg.AttachmentFolderPath)
	if path != "" && path != "." && path != "./" {
		v.AttachmentDir = filepath.Clean(path)
	}
	return v
}

// Resolve turns a wikilink target into an absolute path.
//
// The order follows what Obsidian itself does closely enough to agree in
// practice: an explicit path is honoured first, then the configured attachment
// folder, then the folder holding the note, then the vault root, and finally a
// search of the whole vault by file name. docDir is the directory of the note
// the link appears in.
func (v *Vault) Resolve(target, docDir string) (string, bool) {
	if v == nil {
		return "", false
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return "", false
	}
	// A target may name a heading or block after a #; only the file part is a
	// path. Callers usually strip this, but not always.
	if i := strings.IndexByte(target, '#'); i > 0 {
		target = target[:i]
	}

	// Windows-style separators appear in vaults synced from other machines.
	target = filepath.FromSlash(strings.ReplaceAll(target, "\\", "/"))

	candidates := []string{}
	if v.AttachmentDir != "" {
		candidates = append(candidates, filepath.Join(v.Root, v.AttachmentDir, target))
	}
	if docDir != "" {
		candidates = append(candidates, filepath.Join(docDir, target))
	}
	candidates = append(candidates, filepath.Join(v.Root, target))

	for _, path := range candidates {
		if isFile(path) {
			return path, true
		}
	}

	// Nothing matched a path, so fall back to finding the name anywhere in the
	// vault. This is what makes a link written as [[note]] work regardless of
	// which folder the note was later moved to.
	return v.search(filepath.Base(target))
}

// search looks a bare file name up in the vault index.
func (v *Vault) search(name string) (string, bool) {
	v.indexOnce.Do(v.buildIndex)

	matches := v.index[strings.ToLower(name)]
	if len(matches) == 0 {
		return "", false
	}
	// Obsidian resolves an ambiguous name to the shallowest match; with two at
	// the same depth the shorter path is the more likely intent.
	best := matches[0]
	for _, m := range matches[1:] {
		if betterMatch(m, best) {
			best = m
		}
	}
	return best, true
}

// betterMatch reports whether a should be preferred over b.
func betterMatch(a, b string) bool {
	da, db := strings.Count(a, string(filepath.Separator)), strings.Count(b, string(filepath.Separator))
	if da != db {
		return da < db
	}
	return len(a) < len(b)
}

// buildIndex walks the vault once, recording where each file name lives.
func (v *Vault) buildIndex() {
	v.index = make(map[string][]string)
	count := 0

	filepath.WalkDir(v.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable directory should not abort the walk
		}
		if d.IsDir() {
			// Skip Obsidian's own configuration and the usual hidden
			// directories, which hold no notes and can be very large.
			if name := d.Name(); path != v.Root && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if count >= maxIndexFiles {
			return fs.SkipAll
		}
		count++
		key := strings.ToLower(d.Name())
		v.index[key] = append(v.index[key], path)
		return nil
	})
}

// isFile reports whether path names an existing regular file.
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// ResolveNote resolves a link target that names a note, supplying the .md
// extension Obsidian leaves off.
func (v *Vault) ResolveNote(target, docDir string) (string, bool) {
	if path, ok := v.Resolve(target, docDir); ok {
		return path, true
	}
	if filepath.Ext(target) == "" {
		return v.Resolve(target+".md", docDir)
	}
	return "", false
}

// Resolver binds a vault to the directory of one particular note, which is
// what a link's resolution depends on beyond the vault itself.
//
// It satisfies the renderer's link-resolving interface without that package
// having to know what a vault is.
type Resolver struct {
	vault  *Vault
	docDir string
}

// For returns a resolver for links appearing in a note in docDir. It returns
// nil when there is no vault, so callers can pass the result straight through
// to a renderer that treats nil as "no wikilink support".
func (v *Vault) For(docDir string) *Resolver {
	if v == nil {
		return nil
	}
	return &Resolver{vault: v, docDir: docDir}
}

// ResolveEmbed locates the target of an ![[...]] embed.
func (r *Resolver) ResolveEmbed(target string) (string, bool) {
	if r == nil {
		return "", false
	}
	return r.vault.Resolve(target, r.docDir)
}

// ResolveNote locates the target of a [[...]] link.
func (r *Resolver) ResolveNote(target string) (string, bool) {
	if r == nil {
		return "", false
	}
	return r.vault.ResolveNote(target, r.docDir)
}
