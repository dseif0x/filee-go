package doc

import (
	"math"
	"strconv"
	"strings"
)

// Units: HWPUNIT is 1/7200 inch.
const (
	PerMm    = 7200 / 25.4
	PerPixel = 7200 / 96.0
)

func FromTwips(twips float64) int   { return int(math.Round(twips * 5)) }
func FromEmu(emu float64) int       { return int(math.Round(emu / 127)) }
func FromPoints(points float64) int { return int(math.Round(points * 100)) }
func FromMm(mm float64) int         { return int(math.Round(mm * PerMm)) }

// ParseLength parses a CSS-like length ("2in", "300px", "12pt", "50%") into HWPUNIT.
func ParseLength(value string) (int, bool) {
	text := strings.ToLower(strings.TrimSpace(value))
	if text == "" {
		return 0, false
	}
	split := 0
	for split < len(text) && (text[split] >= '0' && text[split] <= '9' || text[split] == '.' || text[split] == '-') {
		split++
	}
	number, err := strconv.ParseFloat(text[:split], 64)
	if err != nil {
		return 0, false
	}
	var mm float64
	switch strings.TrimSpace(text[split:]) {
	case "in":
		mm = number * 25.4
	case "cm":
		mm = number * 10
	case "mm":
		mm = number
	case "pt":
		mm = number * 25.4 / 72
	case "pc":
		mm = number * 25.4 / 6
	case "emu":
		mm = number / 36000
	case "%":
		mm = number * 1.5 // % of a ~150 mm text width
	default:
		mm = number * 25.4 / 96 // px or unitless
	}
	return int(math.Round(mm * PerMm)), true
}

// InlineLists visits every inline list (paragraphs, link content, notes,
// text boxes, table cells and captions), depth first.
func InlineLists(blocks []Block, visit func(list []Inline)) {
	for _, b := range blocks {
		switch b := b.(type) {
		case *Paragraph:
			inlineLists(b.Inlines, visit)
		case *Table:
			InlineLists(b.Caption, visit)
			for _, r := range b.Rows {
				for _, c := range r.Cells {
					InlineLists(c.Blocks, visit)
				}
			}
		}
	}
}

func inlineLists(inlines []Inline, visit func([]Inline)) {
	visit(inlines)
	for _, in := range inlines {
		switch in := in.(type) {
		case *Link:
			inlineLists(in.Content, visit)
		case *Note:
			InlineLists(in.Blocks, visit)
		case *TextBox:
			InlineLists(in.Blocks, visit)
		}
	}
}

// Inlines returns every inline of blocks, depth first.
func Inlines(blocks []Block) []Inline {
	var out []Inline
	InlineLists(blocks, func(l []Inline) { out = append(out, l...) })
	return out
}

// InlinesOf returns the inlines of one paragraph with the content of its links.
func InlinesOf(inlines []Inline) []Inline {
	var out []Inline
	for _, in := range inlines {
		out = append(out, in)
		if l, ok := in.(*Link); ok {
			out = append(out, InlinesOf(l.Content)...)
		}
	}
	return out
}

// Images returns every picture of the document in reading order.
func Images(d *Document) []*Image {
	var out []*Image
	for _, s := range d.Sections {
		for _, in := range Inlines(s.Blocks) {
			if im, ok := in.(*Image); ok {
				out = append(out, im)
			}
		}
	}
	return out
}

// PlainText is the visible text of inlines (links included, notes left
// out), line breaks and tabs as spaces, trimmed.
func PlainText(inlines []Inline) string {
	var sb strings.Builder
	for _, in := range InlinesOf(inlines) {
		switch in := in.(type) {
		case *Text:
			sb.WriteString(in.Text)
		case *Equation:
			sb.WriteString(in.Text.Text)
		case *LineBreak, *Tab:
			sb.WriteByte(' ')
		}
	}
	return strings.TrimSpace(sb.String())
}

// PlacedCell is a cell with its grid column.
type PlacedCell struct {
	Cell   *Cell
	Column int
}

// PlaceCells puts every cell of a table on the grid (row spans skipped) and
// returns the rows and the column count.
func PlaceCells(t *Table) ([][]PlacedCell, int) {
	placed := make([][]PlacedCell, 0, len(t.Rows))
	type rc struct{ r, c int }
	occupied := map[rc]bool{}
	columns := t.ColumnCount
	for r, row := range t.Rows {
		var out []PlacedCell
		column := 0
		for _, cell := range row.Cells {
			if cell.Column >= 0 {
				column = cell.Column
			}
			for occupied[rc{r, column}] {
				column++
			}
			for dr := 0; dr < max(1, cell.RowSpan); dr++ {
				for dc := 0; dc < max(1, cell.ColSpan); dc++ {
					occupied[rc{r + dr, column + dc}] = true
				}
			}
			out = append(out, PlacedCell{cell, column})
			column += max(1, cell.ColSpan)
			columns = max(columns, column)
		}
		placed = append(placed, out)
	}
	return placed, max(1, columns)
}

// ColumnWidths fits the columns into available: absolute widths (scaled
// down when too wide), relative ones or equal shares.
func ColumnWidths(t *Table, columns, available int) []int {
	widths := make([]int, columns)
	if len(t.ColumnWidths) > 0 {
		sum := 0
		for _, w := range t.ColumnWidths {
			sum += w
		}
		avg := sum / len(t.ColumnWidths)
		total := 0
		for i := range widths {
			if i < len(t.ColumnWidths) {
				widths[i] = t.ColumnWidths[i]
			} else {
				widths[i] = avg
			}
			total += widths[i]
		}
		if total <= available || total == 0 {
			return widths
		}
		for i := range widths {
			widths[i] = int(int64(widths[i]) * int64(available) / int64(total))
		}
		return widths
	}
	fractions := make([]float64, columns)
	known, unknown := 0.0, 0
	for i := range fractions {
		if i < len(t.RelativeWidths) {
			fractions[i] = t.RelativeWidths[i]
		}
		if fractions[i] > 0 {
			known += fractions[i]
		} else {
			unknown++
		}
	}
	scale := 1.0
	if known > 1 {
		scale = 1 / known
	}
	rest := 0.0
	if unknown > 0 {
		rest = max(0, 1-known*scale) / float64(unknown)
		if rest <= 0 {
			for i := range widths {
				widths[i] = available / columns
			}
			return widths
		}
	}
	for i, f := range fractions {
		if f > 0 {
			widths[i] = int(float64(available) * f * scale)
		} else {
			widths[i] = int(float64(available) * rest)
		}
	}
	return widths
}

// EscapeXML escapes text for XML content and attributes, dropping characters
// XML cannot hold.
func EscapeXML(text string) string {
	var sb strings.Builder
	sb.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '<':
			sb.WriteString("&lt;")
		case r == '>':
			sb.WriteString("&gt;")
		case r == '&':
			sb.WriteString("&amp;")
		case r == '"':
			sb.WriteString("&quot;")
		case r == '\'':
			sb.WriteString("&apos;")
		case r == '\t' || r == '\n' || r == '\r':
			sb.WriteRune(r)
		case r < 0x20 || r == 0xFFFE || r == 0xFFFF:
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}
