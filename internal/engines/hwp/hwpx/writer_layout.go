package hwpx

// Rough text measurement for the values 한글 files carry as layout caches: table row heights and the width of inline
// tabs. 한글 lays the document out again when it opens it, but simpler readers (and previews) use these values as
// they are, so they should be close to the real layout.

import (
	"fmt"
	"math"
	"strconv"

	"github.com/dseif0x/filee-go/internal/doc"
)

// TextWidth is the estimated width of text at a font size (1/100 pt = HWPUNIT per em).
func TextWidth(text string, size int) float64 {
	width := 0.0
	for _, ch := range text {
		// Average advances (em) of the fonts Korean documents use (맑은 고딕, 굴림, 함초롬, Calibri ...).
		var em float64
		switch {
		case ch == ' ' || ch == ' ':
			em = 0.3
		case ch == '.' || ch == ',' || ch == ':' || ch == ';' || ch == '\'' || ch == '!' || ch == '|' || ch == '(' || ch == ')' || ch == '[' || ch == ']' || ch == '-':
			em = 0.3
		case ch >= ' ' && ch <= '⁯': // general punctuation (dashes, quotes, bullets)
			em = 0.5
		case ch >= 'ᄀ': // Hangul, CJK, full-width forms: about one em
			em = 0.92
		case ch >= '0' && ch <= '9':
			em = 0.55
		case ch >= 'A' && ch <= 'Z':
			em = 0.65
		default:
			em = 0.5
		}
		width += em * float64(size)
	}
	return width
}

// baseSize is the base font size (1/100 pt) of a character shape.
func (w *writer) baseSize(charPrID string) int {
	if c := w.charPrs[charPrID]; c != nil {
		if v, err := strconv.Atoi(c.A("height")); err == nil {
			return v
		}
	}
	return 1000
}

// basePercent is the base line spacing (percent) of a paragraph shape.
func (w *writer) basePercent(paraPrID string) int {
	paraPr := w.paraPrs[paraPrID]
	if paraPr == nil {
		return 160
	}
	line := paraPr.FirstDescendantNS(nsHH, "lineSpacing")
	if line != nil && line.A("type") == "PERCENT" {
		if v, err := strconv.Atoi(line.A("value")); err == nil {
			return v
		}
	}
	return 160
}

// estimateBlocks is the estimated height of blocks laid out in width.
func (w *writer) estimateBlocks(blocks []doc.Block, width int) int {
	total := 0
	for _, block := range blocks {
		switch b := block.(type) {
		case *doc.Paragraph:
			total += w.estimateParagraph(b, width)
		case *doc.Table:
			placed, columns := doc.PlaceCells(b)
			total += sum(w.estimateRowHeights(b, placed, doc.ColumnWidths(b, columns, width)))
		}
	}
	return total
}

func (w *writer) estimateParagraph(p *doc.Paragraph, width int) int {
	style := w.styleOf(p)
	size := largestSize(p.Inlines)
	if size == 0 {
		size = val(p.MarkFormat.Size, w.baseSize(style.charPr))
	}
	var line int
	if ls := p.Format.LineSpacing; ls != nil {
		switch ls.Kind {
		case doc.SpacingFixed:
			line = ls.Value
		case doc.SpacingAtLeast:
			line = max(ls.Value, size*13/10)
		default:
			line = size * ls.Value / 100
		}
	} else {
		line = size * w.basePercent(style.paraPr) / 100
	}
	available := max(1000, width-val(p.Format.Left, 0)-val(p.Format.Right, 0))

	// Line breaks split the text; each piece wraps on its own. Objects add their own height.
	lines := 0
	current := float64(max(0, val(p.Format.FirstLine, 0)))
	objects := 0
	var measure func([]doc.Inline)
	measure = func(inlines []doc.Inline) {
		for _, inline := range inlines {
			switch v := inline.(type) {
			case *doc.Text:
				current += TextWidth(v.Text, val(v.Format.Size, size))
			case *doc.Equation:
				current += TextWidth(v.Text.Text, val(v.Text.Format.Size, size))
			case *doc.Tab:
				current += 4000
			case *doc.LineBreak:
				lines += linesFor(current, available)
				current = 0
			case *doc.Link:
				measure(v.Content)
			case *doc.Image:
				if v.Anchor == nil {
					objects += val(v.Height, 0)
				}
			case *doc.TextBox:
				if v.Anchor == nil {
					objects += v.Height
				}
			}
		}
	}
	measure(p.Inlines)
	lines += linesFor(current, available)
	return lines*line + objects + val(p.Format.Before, 0) + val(p.Format.After, 0)
}

// linesFor is the wrapped lines for a text width; 5% slack so text that just fits is not pushed to a second line.
func linesFor(textWidth float64, available int) int {
	return max(1, int(math.Ceil(textWidth/(float64(available)*1.05))))
}

// largestSize is the largest explicit font size of inlines (0 = none).
func largestSize(inlines []doc.Inline) int {
	m := 0
	for _, inline := range inlines {
		size := 0
		switch v := inline.(type) {
		case *doc.Text:
			size = val(v.Format.Size, 0)
		case *doc.Equation:
			size = val(v.Text.Format.Size, 0)
		case *doc.Link:
			size = largestSize(v.Content)
		}
		if size > m {
			m = size
		}
	}
	return m
}

// estimateRowHeights: at least the row's own height, and tall enough for the estimated content of its cells.
// Cells spanning rows spread their extra height over the last row they span.
func (w *writer) estimateRowHeights(table *doc.Table, placed [][]doc.PlacedCell, widths []int) []int {
	margin := defaultCellMargin
	if table.CellMargin != nil {
		margin = *table.CellMargin
	}
	heights := make([]int, len(table.Rows))
	for i, r := range table.Rows {
		heights[i] = max(val(r.Height, 0), 0)
	}
	outerBase := w.base
	for pass := 0; pass < 2; pass++ {
		for r, row := range placed {
			for _, pc := range row {
				cell, column := pc.Cell, pc.Column
				cellMargin := margin
				if cell.CellMargin != nil {
					cellMargin = *cell.CellMargin
				}
				if (pass == 0) != (cell.RowSpan == 1) {
					continue // single-row cells first, then spans on top of them
				}
				width := 0
				for i := column; i < column+cell.ColSpan && i < len(widths); i++ {
					width += widths[i]
				}
				blocks := cell.Blocks
				if len(blocks) == 0 {
					blocks = []doc.Block{&doc.Paragraph{}}
				}
				needed := w.estimateBlocks(blocks, max(1000, width-cellMargin.Left-cellMargin.Right)) + cellMargin.Top + cellMargin.Bottom
				last := min(r+cell.RowSpan, len(heights)) - 1
				have := 0
				for i := r; i <= last; i++ {
					have += heights[i]
				}
				if needed > have && last >= 0 {
					heights[last] += needed - have
				}
			}
		}
	}
	w.base = outerBase
	for i := range heights {
		heights[i] = max(heights[i], 282)
	}
	return heights
}

// ───────────────────────── Tabs ─────────────────────────

// tabElements are the type, leader and width of each inline tab of a paragraph, in order. Like 한글 and Word, a tab
// moves to the first tab stop right of the text before it; without a stop it advances to the next default stop.
func tabElements(p *doc.Paragraph, size int) []string {
	var result []string
	stops := p.Format.Tabs
	left := val(p.Format.Left, 0)
	x := float64(left + val(p.Format.FirstLine, 0))
	type piece struct {
		inline doc.Inline
		width  float64
	}
	var pieces []piece
	var flatten func([]doc.Inline)
	flatten = func(inlines []doc.Inline) {
		for _, inline := range inlines {
			if l, ok := inline.(*doc.Link); ok {
				flatten(l.Content)
				continue
			}
			width := 0.0
			switch v := inline.(type) {
			case *doc.Equation:
				// An equation is as wide as its box and margins (a centred display equation between tabs).
				if m, ok := v.Math.(MathNode); ok {
					width = MeasureMath(m, float64(val(v.Text.Format.Size, size))).Width + 2*equationMargin
				} else {
					width = TextWidth(v.Text.Text, val(v.Text.Format.Size, size))
				}
			case *doc.Text:
				width = TextWidth(v.Text, val(v.Format.Size, size))
			}
			pieces = append(pieces, piece{inline, width})
		}
	}
	flatten(p.Inlines)

	for i, pc := range pieces {
		switch pc.inline.(type) {
		case *doc.Text, *doc.Equation:
			x += pc.width
		case *doc.LineBreak:
			x = float64(left)
		case *doc.Tab:
			// Text up to the next tab or line break, for right / center aligned stops.
			following := 0.0
			for j := i + 1; j < len(pieces); j++ {
				if _, tab := pieces[j].inline.(*doc.Tab); tab {
					break
				}
				if _, br := pieces[j].inline.(*doc.LineBreak); br {
					break
				}
				following += pieces[j].width
			}
			var stop *doc.TabStop
			for k := range stops {
				if float64(stops[k].Position) > x+1 {
					stop = &stops[k]
					break
				}
			}
			var kind, leader int
			var target float64
			if stop != nil {
				switch stop.Kind {
				case doc.TabRight:
					kind, target = 2, float64(stop.Position)-following
				case doc.TabCenter:
					kind, target = 3, float64(stop.Position)-following/2
				case doc.TabDecimal:
					kind, target = 4, float64(stop.Position)-following
				default:
					kind, target = 1, float64(stop.Position)
				}
				switch stop.Leader {
				case "SOLID":
					leader = 1
				case "DOT":
					leader = 2
				case "DASH":
					leader = 3
				}
			} else {
				const defaultStop = 4000
				kind, leader, target = 1, 0, (math.Floor(x/defaultStop)+1)*defaultStop
			}
			width := max(0, int(target-x))
			result = append(result, fmt.Sprintf(`<hp:tab width="%d" leader="%d" type="%d"/>`, width, leader, kind))
			x += float64(width)
		}
	}
	return result
}
