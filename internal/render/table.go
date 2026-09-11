package render

import (
	"strings"

	extast "github.com/yuin/goldmark/extension/ast"
)

// cellPadding is the number of spaces on each side of a cell's content.
const cellPadding = 1

// minColWidth is the narrowest a column may be squeezed to before the table
// gives up on shrinking it further.
const minColWidth = 3

type tableCell struct {
	runs  []Run
	align extast.Alignment
}

type tableRow struct {
	cells  []tableCell
	header bool
}

// table renders a GFM table as a box-drawn grid, wrapping cell content and
// shrinking columns as needed to fit the available width.
func (r *renderer) table(n *extast.Table) {
	rows := r.collectRows(n)
	if len(rows) == 0 {
		return
	}

	cols := 0
	for _, row := range rows {
		if len(row.cells) > cols {
			cols = len(row.cells)
		}
	}
	if cols == 0 {
		return
	}

	widths := r.columnWidths(rows, cols)
	g := r.th.Glyphs

	r.emitRaw(Line{Runs: r.tableEdge(widths, g.TableTL, g.TableTopT, g.TableTR)})
	for i, row := range rows {
		if i == 1 && rows[0].header {
			r.emitRaw(Line{Runs: r.tableEdge(widths, g.TableLeftT, g.TableCross, g.TableRightT)})
		}
		for _, line := range r.tableRowLines(row, widths) {
			r.emitRaw(line)
		}
	}
	r.emitRaw(Line{Runs: r.tableEdge(widths, g.TableBL, g.TableBottomT, g.TableBR)})
}

// collectRows flattens the table's header and body into a single row list.
func (r *renderer) collectRows(n *extast.Table) []tableRow {
	var rows []tableRow
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		isHeader := false
		switch child.(type) {
		case *extast.TableHeader:
			isHeader = true
		case *extast.TableRow:
		default:
			continue
		}

		base := r.base
		if isHeader {
			base = base.Merge(r.th.TableHeader)
		}

		row := tableRow{header: isHeader}
		for c := child.FirstChild(); c != nil; c = c.NextSibling() {
			cell, ok := c.(*extast.TableCell)
			if !ok {
				continue
			}
			row.cells = append(row.cells, tableCell{
				runs:  r.inlineChildren(cell, base, ""),
				align: cell.Alignment,
			})
		}
		rows = append(rows, row)
	}
	return rows
}

// lineCost is how many cells of width an extra line has to save before a
// column that could fit its content is narrowed to wrap it. It is what lets
// one long cell - a note in a column of single words - wrap rather than set
// the width of every row, while a column of long descriptions, where
// narrowing would wrap row after row, keeps one line per row.
const lineCost = 16

// splitCost outweighs any number of lines, so that no word is broken while
// some column could still wrap between words instead.
const splitCost = 1 << 30

// column holds what sizing a table column needs to know about its cells.
type column struct {
	cells   [][]Run
	tokens  [][]token
	widths  []int // each cell's widest line, unwrapped
	breaks  []int // each cell's line count unwrapped: more than one after <br>
	natural int   // the widest cell: the width that wraps nothing
	word    int   // the longest word, below which words have to be split
	lines   map[int]int
}

func newColumn(rows []tableRow, i int) *column {
	c := &column{lines: map[int]int{}}
	for _, row := range rows {
		var runs []Run
		if i < len(row.cells) {
			runs = row.cells[i].runs
		}
		tokens := tokenize(runs)
		// Measured the way wrapRuns lays a line out: space counts only
		// between words, and a <br> starts a line of its own.
		widest, line, space, lines := 0, 0, 0, 1
		for _, t := range tokens {
			switch {
			case t.isBreak:
				line, space = 0, 0
				lines++
			case t.isSpace:
				if line > 0 {
					space = t.width
				}
			default:
				line += space + t.width
				space = 0
				widest = max(widest, line)
				c.word = max(c.word, t.width)
			}
		}
		c.cells = append(c.cells, runs)
		c.tokens = append(c.tokens, tokens)
		c.widths = append(c.widths, widest)
		c.breaks = append(c.breaks, lines)
		c.natural = max(c.natural, widest)
	}
	return c
}

// linesAt reports how many lines the column's cells take at width w, summed
// over its rows.
func (c *column) linesAt(w int) int {
	if n, ok := c.lines[w]; ok {
		return n
	}
	n := 0
	for i, cell := range c.cells {
		if c.widths[i] <= w {
			n += c.breaks[i]
			continue
		}
		if lines, ok := countLines(c.tokens[i], w); ok {
			n += lines
			continue
		}
		n += len(wrapRuns(cell, w, nil, nil))
	}
	c.lines[w] = n
	return n
}

// countLines reports how many lines wrapRuns would lay tokens out in, without
// building them: sizing asks this of every cell at many widths, and building
// the lines each time made a long table slow to lay out. It handles only
// text whose words all fit the width, and reports false otherwise, leaving
// words split mid-way to wrapRuns itself.
func countLines(tokens []token, width int) (int, bool) {
	lines, curW, started, pending := 0, 0, false, -1
	for _, t := range tokens {
		switch {
		case t.isBreak:
			lines++
			curW, started, pending = 0, false, -1
		case t.isSpace:
			if started {
				pending = t.width
			}
		default:
			if t.width > width {
				return 0, false
			}
			if started && curW+max(pending, 0)+t.width > width {
				lines++
				curW, pending = 0, -1
			}
			curW += max(pending, 0) + t.width
			started, pending = true, -1
		}
	}
	if started || lines == 0 {
		lines++
	}
	return lines, true
}

// comfortable picks the width that best trades the column's width against
// the lines that wrapping adds, without splitting a word to get there.
func (c *column) comfortable() int {
	rows := len(c.cells)
	lo := max(c.word, minColWidth)
	best, bestCost := c.natural, c.natural
	for w := c.natural - 1; w >= lo; w-- {
		extra := c.linesAt(w) - rows
		// Narrower never takes fewer lines, so once even the narrowest
		// allowed width could not win at this many lines, nothing below can.
		if lo+extra*lineCost >= bestCost {
			break
		}
		if cost := w + extra*lineCost; cost < bestCost {
			best, bestCost = w, cost
		}
	}
	return best
}

// columnWidths picks a width for each column.
//
// Each column starts at the width that suits its own content: its widest
// cell, unless a few long cells are cheaper to wrap. If the table is then
// still too wide, it is narrowed a cell at a time wherever that adds the
// fewest lines, so that words are split only once no column can wrap between
// words any more. Narrowing the widest column instead, as a simpler scheme
// would, splits words in a column of identifiers while a column of prose
// beside it could have wrapped for free.
func (r *renderer) columnWidths(rows []tableRow, cols int) []int {
	columns := make([]*column, cols)
	widths := make([]int, cols)
	total := 0
	for i := range columns {
		columns[i] = newColumn(rows, i)
		widths[i] = columns[i].comfortable()
		total += widths[i]
	}

	// Every column contributes its content plus two padding cells, and there
	// is one vertical rule between each pair plus one at each edge.
	chrome := cols + 1 + cols*cellPadding*2
	budget := r.contentWidth() - chrome
	if budget < cols*minColWidth {
		budget = cols * minColWidth
	}

	for total > budget {
		idx, bestCost := -1, 0
		for i, c := range columns {
			w := widths[i]
			if w <= minColWidth {
				continue
			}
			cost := c.linesAt(w-1) - c.linesAt(w)
			if w-1 < c.word {
				cost += splitCost
			}
			// On a tie the wider column gives way, which splits an overflow
			// between columns of similar text evenly.
			if idx < 0 || cost < bestCost || cost == bestCost && w > widths[idx] {
				idx, bestCost = i, cost
			}
		}
		if idx < 0 {
			break
		}
		widths[idx]--
		total--
	}
	for i := range widths {
		if widths[i] < 1 {
			widths[i] = 1
		}
	}
	return widths
}

// tableEdge draws a horizontal rule using the given corner and junction glyphs.
func (r *renderer) tableEdge(widths []int, left, mid, right string) []Run {
	g := r.th.Glyphs
	var b strings.Builder
	b.WriteString(left)
	for i, w := range widths {
		if i > 0 {
			b.WriteString(mid)
		}
		b.WriteString(strings.Repeat(g.TableH, w+cellPadding*2))
	}
	b.WriteString(right)
	return []Run{{Text: b.String(), Style: r.th.TableBorder}}
}

// tableRowLines lays out one row, wrapping each cell and producing as many
// lines as the tallest cell needs.
func (r *renderer) tableRowLines(row tableRow, widths []int) []Line {
	cells := make([][]Line, len(widths))
	height := 1
	for i := range widths {
		var runs []Run
		align := extast.AlignNone
		if i < len(row.cells) {
			runs = row.cells[i].runs
			align = row.cells[i].align
		}
		wrapped := wrapRuns(runs, widths[i], nil, nil)
		for j := range wrapped {
			wrapped[j].Runs = alignRuns(wrapped[j].Runs, widths[i], align)
		}
		cells[i] = wrapped
		if len(wrapped) > height {
			height = len(wrapped)
		}
	}

	pad := Run{Text: strings.Repeat(" ", cellPadding)}
	bar := Run{Text: r.th.Glyphs.TableV, Style: r.th.TableBorder}

	lines := make([]Line, 0, height)
	for row := 0; row < height; row++ {
		runs := []Run{bar}
		for i := range widths {
			var content []Run
			if row < len(cells[i]) {
				content = cells[i][row].Runs
			} else {
				content = []Run{{Text: strings.Repeat(" ", widths[i])}}
			}
			runs = append(runs, pad)
			runs = append(runs, content...)
			runs = append(runs, pad, bar)
		}
		lines = append(lines, Line{Runs: runs})
	}
	return lines
}

// alignRuns pads a cell's content out to width according to its alignment.
func alignRuns(runs []Run, width int, align extast.Alignment) []Run {
	slack := width - runsWidth(runs)
	if slack <= 0 {
		return runs
	}
	switch align {
	case extast.AlignRight:
		return append([]Run{{Text: strings.Repeat(" ", slack)}}, runs...)
	case extast.AlignCenter:
		left := slack / 2
		out := append([]Run{{Text: strings.Repeat(" ", left)}}, runs...)
		return append(out, Run{Text: strings.Repeat(" ", slack-left)})
	default:
		return append(runs, Run{Text: strings.Repeat(" ", slack)})
	}
}
