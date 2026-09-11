package render

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Local links resolve to absolute paths, because only an absolute path means
// the same thing to every program that sees it. Two places want something
// else from one:
//
//   - A reader who sees the path printed after the link text wants it short
//     enough to read and still openable from the shell, so it is shown
//     relative to where they are standing when that is shorter.
//   - A terminal opening an OSC 8 hyperlink has no idea where mdv is running
//     and expects a URL, so it gets a file:// one with the host named.

// displayPath returns the shortest form of an absolute local path that still
// opens from the working directory: relative to workDir, abbreviated under
// home with ~, or as it is. Anything that is not an absolute path - a URL, a
// fragment - is returned unchanged.
func displayPath(dest, workDir, home string) string {
	if !filepath.IsAbs(dest) {
		return dest
	}
	path, frag, hasFrag := strings.Cut(dest, "#")
	best := path
	consider := func(candidate string) {
		if len(candidate) < len(best) {
			best = candidate
		}
	}
	if workDir != "" {
		if rel, err := filepath.Rel(workDir, path); err == nil {
			consider(rel)
		}
	}
	if home != "" && home != "/" {
		if rel, err := filepath.Rel(home, path); err == nil && !escapes(rel) {
			consider("~/" + rel)
		}
	}
	if hasFrag {
		best += "#" + frag
	}
	return best
}

// escapes reports whether a relative path climbs out of the directory it is
// relative to.
func escapes(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, "../")
}

// hyperlinkTarget turns a link into something a terminal can open: absolute
// paths become file:// URLs naming this host, and everything else passes
// through. Naming the host is what the OSC 8 convention asks for, so that a
// link printed by mdv on a remote machine over ssh is not opened as a file on
// the local one.
func hyperlinkTarget(link string) string {
	if !filepath.IsAbs(link) {
		return link
	}
	path, frag, _ := strings.Cut(link, "#")
	u := url.URL{Scheme: "file", Host: hostname(), Path: path, Fragment: frag}
	return u.String()
}

var hostname = sync.OnceValue(func() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
})
