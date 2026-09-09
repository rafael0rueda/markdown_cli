# mdv

A markdown viewer for the terminal. Built for [kitty](https://sw.kovidgoyal.net/kitty/)
first, and it degrades cleanly everywhere else.

```console
$ mdv README.md
```

![mdv rendering its own README in kitty](docs/mdv_example.png)

Syntax-highlighted code, box-drawn tables, real inline images, clickable links,
and first-class support for [Obsidian](https://obsidian.md) vaults.

## Features

- **Full CommonMark + GFM** — tables, task lists, strikethrough, footnotes,
  autolinks.
- **Syntax highlighting** for fenced code, via [chroma](https://github.com/alecthomas/chroma),
  in a palette matched to the surrounding theme.
- **Inline images** using the kitty graphics protocol, with a sixel fallback
  and alt text as a last resort. PNG, JPEG, GIF, WebP, BMP and TIFF.
- **Clickable links** via OSC 8, where the terminal supports them.
- **Obsidian vaults** — `![[embeds]]`, `[[wikilinks]]` and YAML frontmatter.
- **Adapts to your terminal** by asking it what it can do, rather than guessing
  from `TERM`.
- **Safe to pipe.** Output to anything other than a terminal contains no escape
  sequences at all.
- **Single static binary**, no runtime dependencies.

## Install

```sh
go install github.com/rafael0rueda/markdown_cli/cmd/mdv@latest
```

Or build from source:

```sh
git clone https://github.com/rafael0rueda/markdown_cli
cd markdown_cli
make build          # ./bin/mdv
make install        # $GOBIN/mdv
```

Building needs Go 1.25 or newer (chroma sets that floor). The binary itself has
no runtime requirements — `CGO_ENABLED=0` throughout, so it runs on any
distribution regardless of its libc.

```sh
make dist           # static binaries for linux and darwin, amd64 and arm64
```

## Usage

```
mdv [flags] [file...]
```

With no file, or with `-`, mdv reads markdown from standard input.

| Flag | Default | Meaning |
|------|---------|---------|
| `-w`, `-width` | detect | Layout width in columns |
| `-theme` | `auto` | `dark`, `light`, `plain`, `auto` |
| `-color` | `auto` | `none`, `16`, `256`, `truecolor`, `always` |
| `-links` | `auto` | `inline` shows URLs, `hide` shows only link text |
| `-images` | `auto` | `none`, `kitty`, `sixel` |
| `-remote-images` | off | Fetch images over http |
| `-frontmatter` | `meta` | `meta`, `hide`, `raw` |
| `-vault` | detect | Obsidian vault root |
| `-no-vault` | off | Do not resolve `[[wikilinks]]` |
| `-ascii` | off | ASCII instead of Unicode box drawing |
| `-caps` | | Report what the terminal supports, and exit |
| `-no-probe` | off | Never query the terminal; use the environment alone |
| `-probe-timeout` | `300ms` | How long to wait for the terminal to answer |
| `-version` | | Print version and exit |

### Piping is safe

When output is not a terminal, mdv emits **no escape sequences at all** — no
color, no hyperlinks, no images. `mdv doc.md > out.txt` gives you plain text.
Use `-color=always` to override. [`NO_COLOR`](https://no-color.org) is honoured.

Width is capped at 100 columns when detected from the terminal, because prose
set much wider is measurably harder to read. `-width` overrides.

## Obsidian vaults

mdv reads Obsidian notes with no setup. If a document has a `.obsidian`
directory in one of its parent folders, that folder is treated as a vault and
the syntax plain markdown does not understand starts working:

| Syntax | Rendered as |
|--------|-------------|
| `![[image.png]]` | An inline picture |
| `![[image.png\|300]]` | The same, capped at 300 pixels wide |
| `[[Some Note]]` | A clickable link to the note's file |
| `[[Some Note\|alias]]` | The same, labelled `alias` |
| `[[Some Note#Section]]` | Labelled `Some Note > Section` |

Embed targets are resolved **the way Obsidian resolves them**, which is why
this needs the vault and not just the document: a bare `![[Pasted image 01.png]]`
means *find this in the vault*, and the folder such images are filed into is
`attachmentFolderPath` in `.obsidian/app.json`. mdv checks the attachment
folder, then the note's own folder, then the vault root, then searches the
vault by name, preferring the shallowest match.

A link or embed that resolves to nothing still shows its label. Pointing at a
note you have not written yet is a normal thing to do in a vault, and a missing
attachment should not take the rest of the note down with it.

YAML frontmatter becomes a compact aligned header instead of the two horizontal
rules and stray bullet list that CommonMark makes of it:

```
tags   security, linux
date   2026-09-03
```

Use `-frontmatter=hide` to drop it, or `-frontmatter=raw` to see what a plain
renderer would have done.

## Terminal support

Run `mdv -caps` to see what was detected, and whether it was measured or
inferred. It is the first thing to try when something renders wrongly.

| Terminal | Images | Hyperlinks |
|----------|--------|------------|
| kitty | kitty protocol | yes |
| Ghostty | kitty protocol | yes |
| WezTerm | kitty protocol | yes |
| Konsole | kitty protocol | yes |
| foot | sixel | yes |
| Contour | sixel | yes |
| iTerm2, Alacritty, Windows Terminal, VS Code | — | yes |
| GNOME Terminal (VTE ≥ 0.50) | — | yes |
| xterm and friends | — | — |

Everything degrades: a terminal without graphics shows alt text, one without
OSC 8 shows the URL in parentheses, and one without color gets plain text.

Nothing is probed inside **tmux or screen** — the multiplexer intercepts the
replies, and graphics need passthrough wrapping that is not implemented yet.
mdv reports the environment's view and says so.

## How it works

A few decisions that are not obvious from the outside:

**The renderer does not produce a string.** It produces a `Doc`: a flat slice
of lines, each a sequence of styled runs. Serializing to a pipe walks it once,
but the interactive pager needs to draw an arbitrary window of it repeatedly,
and to know which screen row a given image lands on.

**Colors degrade by hue, not by RGB distance.** On a 16-color terminal, nearest
by distance collapses almost every pastel onto white — technically the closest
match, and unreadable. Mapping to the nearest *hue* keeps headings, links and
code distinguishable.

**Terminal capabilities are measured, not guessed.** mdv writes a short string
of queries and reads the replies. Primary device attributes goes last on
purpose: every terminal answers it, so its reply marks the point where the
earlier queries have been processed. Without that terminator there is no way to
tell "does not support graphics" from "has not replied yet". Queries go to
`/dev/tty`, so this works even when stdin and stdout are both redirected.

**Image rows are reserved before the picture is drawn.** mdv writes the blank
lines, moves the cursor back up, and draws over them. That forces any scrolling
to happen up front, so the image cannot be scrolled halfway off the screen as
it lands.

**Blocks are split at images on their own source line.** Markdown runs a
paragraph until a blank line, so a sentence followed by an embed on the next
line is a *single* paragraph — and rendering it as one block would strand the
picture mid-sentence.

## Development

```sh
make test           # go test ./...
make race           # under the race detector
make lint           # fmt, vet, test
make golden         # regenerate golden files after a layout change
```

Two testing approaches worth knowing about, since both cover things that are
otherwise hard to check without a terminal:

- **The sixel encoder is verified by round-trip.** The tests contain a sixel
  decoder, so encoded output is decoded again and compared against the source.
  Asserting on the shape of the escape sequence would only confirm it looks
  plausible; decoding it confirms it means the right thing.
- **The capability probe is driven against a real pseudo-terminal**, with a
  fake terminal answering on the other end. That covers raw mode, the read
  timeout, replies arriving in fragments, and restoring the terminal to its
  original state. Those tests are Linux-only and skip elsewhere.

### Layout

```
cmd/mdv/            flag parsing, input dispatch
internal/render/    markdown -> a line-addressed document of styled runs
internal/theme/     colors, styles, palettes, glyph sets
internal/term/      terminal capability detection and probing
internal/graphics/  decoding, scaling, and the kitty and sixel protocols
internal/vault/     Obsidian vault detection and wikilink resolution
docs/               screenshots and other documentation assets
```

## Status

Working and usable. An interactive pager — scrolling, search, resize, with
images redrawn as you move — is the next planned piece; for now, pipe to
`less -R` for long documents.

## Prior art

[`mdcat`](https://github.com/swsnr/mdcat) does markdown with kitty graphics in
Rust, and [`glow`](https://github.com/charmbracelet/glow) is an excellent Go
markdown renderer without inline images. mdv differs mainly in the Obsidian
support and in measuring terminal capabilities rather than inferring them.
