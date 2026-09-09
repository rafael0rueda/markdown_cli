package render

import (
	"bufio"
	"io"
	"strings"

	"mdv/internal/theme"
)

// WriteOptions controls serialization of a Doc to a terminal or a pipe.
type WriteOptions struct {
	// Color is how much of the palette the target can show.
	Color theme.ColorMode
	// Hyperlinks enables OSC 8 escape sequences for runs carrying a Link.
	// Terminals that do not understand them mostly ignore them, but some
	// older ones and some pagers print the payload, so it stays opt-in.
	Hyperlinks bool
}

// links reports whether OSC 8 sequences should be emitted. ColorNone means the
// target wants no escape sequences whatsoever - it is what output redirected
// to a file gets - so it suppresses hyperlinks along with the palette.
func (o WriteOptions) links() bool {
	return o.Hyperlinks && o.Color != theme.ColorNone
}

// osc8 wraps a URL in the start half of an OSC 8 hyperlink sequence. The
// terminator is a String Terminator; BEL also works, but ST is what modern
// terminals document.
func osc8(url string) string {
	return "\x1b]8;;" + url + "\x1b\\"
}

const osc8End = "\x1b]8;;\x1b\\"

// Write serializes the document to w.
func Write(w io.Writer, doc *Doc, opts WriteOptions) error {
	bw := bufio.NewWriter(w)
	for _, line := range doc.Lines {
		if err := writeLine(bw, line, doc.Width, opts); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// RenderLine serializes a single line to a string. The pager draws with this,
// one row at a time.
func RenderLine(line Line, width int, opts WriteOptions) string {
	var b strings.Builder
	_ = writeLine(&b, line, width, opts)
	return b.String()
}

// stringWriter is the subset of bufio.Writer/strings.Builder that writeLine
// needs, so both callers share one implementation.
type stringWriter interface {
	WriteString(string) (int, error)
}

func writeLine(w stringWriter, line Line, width int, opts WriteOptions) error {
	runs := line.Runs
	if !line.Fill.IsSet() {
		runs = trimTrailingSpace(runs)
	}

	// prev tracks the style currently in effect so unchanged styles do not
	// re-emit their escape sequence on every run.
	var prev theme.Style
	var linkOpen bool
	dirty := false

	emit := func(s string) error {
		_, err := w.WriteString(s)
		return err
	}

	for _, r := range runs {
		if r.Text == "" {
			continue
		}
		if opts.links() && r.Link != "" {
			if !linkOpen {
				if err := emit(osc8(r.Link)); err != nil {
					return err
				}
				linkOpen = true
			}
		} else if linkOpen {
			if err := emit(osc8End); err != nil {
				return err
			}
			linkOpen = false
		}

		if r.Style != prev {
			// A reset first, because SGR attributes are additive: going from
			// bold+red to plain red has no "unset bold" parameter we can rely
			// on across terminals.
			if dirty {
				if err := emit(theme.Reset); err != nil {
					return err
				}
				dirty = false
			}
			if sgr := r.Style.SGR(opts.Color); sgr != "" {
				if err := emit(sgr); err != nil {
					return err
				}
				dirty = true
			}
			prev = r.Style
		}
		if err := emit(r.Text); err != nil {
			return err
		}
	}

	if linkOpen {
		if err := emit(osc8End); err != nil {
			return err
		}
	}

	// Extend the block background to the full width.
	if line.Fill.IsSet() && opts.Color != theme.ColorNone {
		if pad := width - line.Width(); pad > 0 {
			fill := theme.Style{BG: line.Fill}
			if fill != prev {
				if dirty {
					if err := emit(theme.Reset); err != nil {
						return err
					}
				}
				if err := emit(fill.SGR(opts.Color)); err != nil {
					return err
				}
				dirty = true
			}
			if err := emit(strings.Repeat(" ", pad)); err != nil {
				return err
			}
		}
	}

	if dirty {
		return emit(theme.Reset)
	}
	return nil
}

// trimTrailingSpace drops trailing blanks so copied output has no ragged
// whitespace. Runs carrying a background are left alone: their spaces are
// visible color, not padding.
func trimTrailingSpace(runs []Run) []Run {
	out := append([]Run(nil), runs...)
	for len(out) > 0 {
		last := &out[len(out)-1]
		if last.Style.BG.IsSet() {
			break
		}
		trimmed := strings.TrimRight(last.Text, " \t")
		if trimmed == last.Text {
			break
		}
		if trimmed == "" {
			out = out[:len(out)-1]
			continue
		}
		last.Text = trimmed
		break
	}
	return out
}
