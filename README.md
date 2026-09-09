# mdv

A markdown viewer for the terminal. Built for kitty first, but it degrades
cleanly everywhere else.

```
mdv README.md
```

## Status

Phases 1 to 4 are complete. Markdown renders to stdout with syntax-highlighted
code, wrapped prose, box-drawn tables, clickable hyperlinks, and inline images
drawn with the kitty graphics protocol or sixel. mdv asks the terminal what it
supports and adapts to the answer.

Still to come:

| Phase | What it adds |
|-------|--------------|
| 5 | An interactive pager: scrolling, search, resize |
| 6 | Config file, man page, packaging |

Phase 3 was hyperlinks; detecting support was the whole cost, so it landed
with phase 2.

## Building

Requires Go 1.25 or newer to build. The chroma syntax highlighter sets that
floor; the resulting binary itself has no runtime requirements.

```
make build      # ./bin/mdv
make install    # $GOBIN/mdv
make test
make dist       # static binaries for linux and darwin, amd64 and arm64
```

`CGO_ENABLED=0` throughout, so the binary is static and runs on any
distribution regardless of its libc.

## Usage

```
mdv [flags] [file...]
```

With no file, or with `-`, mdv reads from standard input.

| Flag | Default | Meaning |
|------|---------|---------|
| `-w`, `-width` | detect | Layout width in columns |
| `-theme` | `auto` | `dark`, `light`, `plain`, or `auto` |
| `-color` | `auto` | `none`, `16`, `256`, `truecolor`, `always` |
| `-links` | `auto` | `inline` shows URLs, `hide` shows only link text |
| `-ascii` | off | ASCII instead of Unicode box drawing |
| `-images` | `auto` | `none`, `kitty`, `sixel` |
| `-remote-images` | off | Fetch images over http |
| `-caps` | | Report what the terminal supports, and exit |
| `-no-probe` | off | Never query the terminal; use the environment alone |
| `-probe-timeout` | `300ms` | How long to wait for the terminal to answer |
| `-version` | | Print version and exit |

## Behaviour worth knowing

**Piping is safe.** When output is not a terminal, mdv emits no escape
sequences at all — no color, no hyperlinks. `mdv doc.md > out.txt` gives you
plain text. Use `-color=always` to override.

**`NO_COLOR` is honoured**, per [no-color.org](https://no-color.org).

**Width is capped at 100 columns** when detected from the terminal, because
prose set much wider than that is measurably harder to read. `-width` overrides.

**Colors degrade by hue, not by distance.** On a 16-color terminal the pastel
theme is mapped to the nearest *hue* rather than the nearest RGB value. Nearest
RGB is the obvious approach and it is wrong: it collapses almost every pastel
onto white, which is technically accurate and useless to read.

## How images work

An image on a line of its own becomes a picture:

```markdown
![a diagram](diagram.png)
```

An image inside a sentence stays as alt text. Both graphics protocols draw into
a rectangle of whole character cells, so a picture placed mid-line would either
overwrite the words beside it or force the line to be as tall as the image.
Markdown that means to show a picture puts it on its own line anyway.

PNG, JPEG, GIF, WebP, BMP and TIFF are supported. Paths are relative to the
document, not to your shell's working directory.

**Remote images are not fetched** unless you pass `-remote-images`. Rendering a
document should not tell a third party your address and when you read it.

Anything that cannot be drawn — a missing file, an unreadable format, a
terminal without graphics — falls back to the alt text. A broken picture never
breaks the document.

Some details that took care to get right:

- **Rows are reserved before the picture is drawn.** mdv writes the blank lines
  first, then moves the cursor back up and draws over them. That forces any
  scrolling to happen up front, so the image cannot be scrolled halfway off the
  screen as it is drawn.
- **The cursor is saved at the left margin**, before the indent for an image
  inside a list or blockquote is applied. Saving after would leave every
  following line shifted right.
- **Kitty image identifiers start from a random base.** They are global to the
  terminal session and re-using one replaces the earlier image, so counting
  from one would make a second run of mdv erase the first run's pictures from
  your scrollback.
- **Sixel is a great deal more work than kitty.** Kitty takes a PNG and scales
  it itself. Sixel has no compression, no alpha and a 256-color limit, so the
  image has to be scaled, reduced with a median cut and dithered before it can
  be sent. The same picture is typically five times larger on the wire.

## How capability detection works

`mdv -caps` shows what was found, and whether it was measured or guessed. It is
the first thing to run when something renders wrongly.

Detection has two sources. The environment is free but only as honest as the
terminal's own advertising — `TERM` is routinely set to `xterm-256color` by
terminals that are nothing of the sort. So mdv also asks the terminal directly,
writing a short string of queries and reading the replies:

```
kitty graphics query -> does it answer OK?
kitty keyboard query -> does it answer at all?
OSC 11               -> what is the background color?
CSI 16 t             -> how large is a character cell, in pixels?
CSI c                -> primary device attributes
```

Device attributes goes last on purpose. Every terminal answers it, so its reply
marks the point at which all the earlier queries have been processed. Without
that terminator there is no way to tell "does not support graphics" from "has
not replied yet", and the only option would be to wait out the full timeout on
every run.

Queries go to `/dev/tty`, not to stdin and stdout, so probing still works when
markdown is piped in or output is piped onward.

Some things worth knowing:

- **A terminal that never answers costs the full timeout**, once, before output
  appears. Use `-no-probe` to skip it, or shorten `-probe-timeout`.
- **Nothing is probed inside tmux or screen.** The multiplexer intercepts the
  replies, and graphics need passthrough wrapping that is not implemented yet.
  mdv reports the environment's view and says so.
- **Probing writes escape sequences to your terminal.** Terminals are supposed
  to silently consume sequences they do not understand, and modern ones do. If
  yours prints them as text, `-no-probe` is the fix.

## Layout

```
cmd/mdv/          flag parsing, input dispatch
internal/theme/   colors, styles, palettes, glyph sets
internal/term/    terminal capability detection and probing
internal/render/  markdown -> a line-addressed document of styled runs
internal/graphics/ decoding, scaling, and the kitty and sixel protocols
```

The renderer does not produce a string. It produces a `Doc`: a flat slice of
`Line`, each a sequence of styled `Run`. Serializing to a pipe walks it once;
the interactive pager will need to draw an arbitrary window of it, repeatedly,
and later to know which screen row a given image lands on. Committing to a
finished string early would have closed off the second use.

## Testing

```
make test
make race
```

Golden files under `internal/render/testdata` hold the expected plain-text
rendering of each fixture. After an intentional change to layout:

```
make golden     # then read the diff before committing it
```

Goldens compare unstyled text so the palette can change without churning them;
escape-sequence output is asserted separately in `ansi_test.go`.

The sixel encoder is verified by round-trip: the tests contain a sixel decoder,
and the encoded image is decoded again and compared against the source. Checking
the shape of the escape sequence would only confirm it looks plausible; decoding
it confirms it means the right thing.

Capability detection is tested at two levels. The reply parser is a pure
function over a byte buffer, so it is exercised against replies captured from
real terminals, including malformed and truncated ones. The probe itself is
driven against a genuine pseudo-terminal with a fake terminal answering on the
other end, which covers raw mode, the read timeout, replies arriving in
fragments, and restoring the terminal to its original state. Those tests are
Linux-only and are skipped elsewhere.
