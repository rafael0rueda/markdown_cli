package render

import (
	"bytes"
	"strings"
)

// FrontmatterMode selects how a document's YAML frontmatter is shown.
type FrontmatterMode int

const (
	// FrontmatterMeta renders it as a compact key/value header. This is the
	// default: the properties are usually worth seeing, but not worth the
	// vertical space the raw block takes.
	FrontmatterMeta FrontmatterMode = iota
	// FrontmatterHide drops it entirely.
	FrontmatterHide
	// FrontmatterRaw leaves it in the document, where it renders as the rules
	// and stray text that plain markdown makes of it.
	FrontmatterRaw
)

// ParseFrontmatterMode maps a flag value onto a mode.
func ParseFrontmatterMode(s string) (FrontmatterMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "meta", "auto", "show":
		return FrontmatterMeta, nil
	case "hide", "none", "off":
		return FrontmatterHide, nil
	case "raw", "keep":
		return FrontmatterRaw, nil
	}
	return FrontmatterMeta, errUnknownFrontmatter(s)
}

type errUnknownFrontmatter string

func (e errUnknownFrontmatter) Error() string {
	return "unknown frontmatter mode " + string(e) + " (want meta, hide or raw)"
}

// metaEntry is one property from the frontmatter.
type metaEntry struct {
	key string
	// values holds one entry for a scalar and several for a list.
	values []string
	// raw is the source line, kept when the value is something this does not
	// model - a nested map, or a block scalar - so it is shown rather than
	// silently dropped.
	raw string
}

// splitFrontmatter separates a leading YAML block from the document body.
//
// The block is recognized the way every tool that reads it does: three dashes
// on the very first line, and a closing line of three dashes or dots. As a
// guard against a document that simply opens with a horizontal rule, at least
// one line between them has to look like a property.
//
// When there is no frontmatter, body is the source unchanged. Returning a
// stripped body alongside ok=false would leave any caller that forgot to check
// silently dropping the first section of the document.
func splitFrontmatter(src []byte) (meta, body []byte, ok bool) {
	rest, found := bytes.CutPrefix(src, []byte("---\n"))
	if !found {
		if rest, found = bytes.CutPrefix(src, []byte("---\r\n")); !found {
			return nil, src, false
		}
	}

	// Find the closing delimiter at the start of a line.
	offset := 0
	for {
		line, remainder, more := cutLine(rest[offset:])
		trimmed := strings.TrimRight(string(line), "\r")
		if trimmed == "---" || trimmed == "..." {
			meta = rest[:offset]
			if !looksLikeProperties(meta) {
				return nil, src, false
			}
			if !more {
				return meta, nil, true
			}
			return meta, remainder, true
		}
		if !more {
			// No closing delimiter, so this was never frontmatter.
			return nil, src, false
		}
		offset = len(rest) - len(remainder)
	}
}

// cutLine splits off the first line, reporting whether anything followed it.
func cutLine(b []byte) (line, rest []byte, more bool) {
	i := bytes.IndexByte(b, '\n')
	if i < 0 {
		return b, nil, false
	}
	return b[:i], b[i+1:], true
}

// looksLikeProperties reports whether a block plausibly holds YAML properties
// rather than prose that happened to sit between two horizontal rules.
func looksLikeProperties(meta []byte) bool {
	for _, raw := range strings.Split(string(meta), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "- ") {
			continue
		}
		if key, _, found := strings.Cut(line, ":"); found && key != "" && !strings.Contains(key, " ") {
			return true
		}
		return false
	}
	return false
}

// parseFrontmatter reads the properties out of a YAML block.
//
// This understands the shapes frontmatter actually takes - a scalar, a bracketed
// list, or an indented sequence - and no more. It is a display summary, not a
// YAML implementation: anything it does not recognize is carried through as its
// source line so it is shown rather than quietly lost.
func parseFrontmatter(meta []byte) []metaEntry {
	var entries []metaEntry
	lines := strings.Split(string(meta), "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimRight(lines[i], "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Indented lines belong to the entry above and are consumed with it.
		if line != strings.TrimLeft(line, " \t") {
			continue
		}

		key, value, found := strings.Cut(line, ":")
		if !found {
			entries = append(entries, metaEntry{raw: trimmed})
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		entry := metaEntry{key: key}
		switch {
		case value == "":
			// A sequence follows on the indented lines beneath.
			for i+1 < len(lines) {
				next := strings.TrimRight(lines[i+1], "\r")
				if next == strings.TrimLeft(next, " \t") || strings.TrimSpace(next) == "" {
					break
				}
				item := strings.TrimSpace(next)
				if !strings.HasPrefix(item, "- ") {
					// Nested structure; show the key and stop rather than
					// pretending to have understood it.
					entry.values = append(entry.values, "…")
					i++
					continue
				}
				entry.values = append(entry.values, unquote(strings.TrimPrefix(item, "- ")))
				i++
			}
		case strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]"):
			for _, item := range strings.Split(value[1:len(value)-1], ",") {
				if item = strings.TrimSpace(item); item != "" {
					entry.values = append(entry.values, unquote(item))
				}
			}
		default:
			entry.values = []string{unquote(value)}
		}
		entries = append(entries, entry)
	}
	return entries
}

// unquote strips the surrounding quotes YAML allows around a scalar.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// frontmatter renders the properties as a compact header.
//
// Keys are aligned into a column so the values line up, which makes a block of
// five or six properties scannable instead of a wall of text.
func (r *renderer) frontmatter(entries []metaEntry) {
	if len(entries) == 0 {
		return
	}

	width := 0
	for _, e := range entries {
		if n := uniWidth(e.key); e.key != "" && n > width {
			width = n
		}
	}

	for _, e := range entries {
		if e.key == "" {
			r.emit([]Run{{Text: e.raw, Style: r.th.MetaValue}})
			continue
		}
		key := e.key + strings.Repeat(" ", width-uniWidth(e.key))
		runs := []Run{
			{Text: key + "  ", Style: r.th.MetaKey},
			{Text: strings.Join(e.values, ", "), Style: r.th.MetaValue},
		}
		// Continuation lines line up under the values rather than under the
		// key, so a long list of tags stays in its own column.
		indent := []Run{{Text: strings.Repeat(" ", width+2)}}
		r.doc.Lines = append(r.doc.Lines,
			wrapRuns(runs, r.opts.Width, r.rest, append(append([]Run(nil), r.rest...), indent...))...)
	}
}
