# mdv

A markdown viewer for the terminal. Built for kitty first, but it degrades
cleanly everywhere else.

```
mdv README.md
```

## Status

Phases 1 and 2 are complete. Markdown renders to stdout with syntax-highlighted
code, wrapped prose, and box-drawn tables. mdv asks the terminal what it
supports and adapts: color depth, dark or light theme from the actual
background color, and clickable OSC 8 hyperlinks. Images are still shown as
alt text.

Still to come:

| Phase | What it adds |
|-------|--------------|
| 4 | Inline images via the kitty graphics protocol, with a sixel fallback |
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

Capability detection is tested at two levels. The reply parser is a pure
function over a byte buffer, so it is exercised against replies captured from
real terminals, including malformed and truncated ones. The probe itself is
driven against a genuine pseudo-terminal with a fake terminal answering on the
other end, which covers raw mode, the read timeout, replies arriving in
fragments, and restoring the terminal to its original state. Those tests are
Linux-only and are skipped elsewhere.
