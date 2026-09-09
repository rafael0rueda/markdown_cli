# mdv

A markdown viewer for the terminal. Built for kitty first, but it degrades
cleanly everywhere else.

```
mdv README.md
```

## Status

Phase 1 of 6 is complete: markdown renders to stdout with syntax-highlighted
code, wrapped prose, and box-drawn tables, adapting to the terminal's color
depth. Images are shown as alt text for now.

Still to come:

| Phase | What it adds |
|-------|--------------|
| 2 | Terminal capability probing (`--caps`) for graphics, keyboard and background |
| 3 | OSC 8 hyperlinks, so link text is clickable |
| 4 | Inline images via the kitty graphics protocol, with a sixel fallback |
| 5 | An interactive pager: scrolling, search, resize |
| 6 | Config file, man page, packaging |

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

## Layout

```
cmd/mdv/          flag parsing, input dispatch
internal/theme/   colors, styles, palettes, glyph sets
internal/term/    terminal capability detection
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
