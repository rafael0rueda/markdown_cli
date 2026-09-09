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

// columnWidths picks a width for each column: the natural content width where
// it fits, shrinking the widest columns first when it does not.
func (r *renderer) columnWidths(rows []tableRow, cols int) []int {
	widths := make([]int, cols)
	for _, row := range rows {
		for i, cell := range row.cells {
			if w := runsWidth(cell.runs); w > widths[i] {
				widths[i] = w
			}
		}
	}

	// Every column contributes its content plus two padding cells, and there
	// is one vertical rule between each pair plus one at each edge.
	chrome := cols + 1 + cols*cellPadding*2
	budget := r.contentWidth() - chrome
	if budget < cols*minColWidth {
		budget = cols * minColWidth
	}

	total := 0
	for _, w := range widths {
		total += w
	}
	// Shrinking the widest column repeatedly converges on an even split of the
	// overflow, which reads better than scaling every column proportionally.
	for total > budget {
		widest, idx := 0, -1
		for i, w := range widths {
			if w > widest && w > minColWidth {
				widest, idx = w, i
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
