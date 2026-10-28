package spreadsheet

import (
	"fmt"
	"math"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Workbook → document: every non-empty sheet becomes a section with one
// table, so spreadsheets → DOCX / HWPX (and PDF through them) work without
// Excel or LibreOffice. A4 with narrow margins, landscape when the columns
// are much wider than a portrait page, columns (and text) scaled to fit the
// width, light grey grid lines where cells have no border, the print title
// rows (or frozen top rows) repeated on every page.

const (
	maxDocRows      = 5000
	maxDocColumns   = 60
	a4Short         = 59528
	a4Long          = 84188
	sheetMargin     = 2268 // 8 mm
	defaultFontSize = 11.0
)

var gridLine = doc.Border{Style: doc.BorderSolid, WidthMm: 0.1, Color: "#D9D9D9"}

// Document builds the document of a workbook.
func Document(book *Workbook) *doc.Document {
	d := &doc.Document{}
	var sheets []*Worksheet
	for _, s := range book.Sheets {
		if _, ok := s.UsedRange(); ok {
			sheets = append(sheets, s)
		}
	}
	for _, s := range sheets {
		d.Sections = append(d.Sections, sheetSection(s, len(sheets) > 1))
	}
	if len(d.Sections) == 0 {
		s := doc.NewSection()
		s.Blocks = append(s.Blocks, &doc.Paragraph{})
		d.Sections = append(d.Sections, s)
	}
	return d
}

func sheetSection(sheet *Worksheet, withTitle bool) *doc.Section {
	used, _ := sheet.UsedRange()
	var rows, columns []int
	for r := used.Top; r <= used.Bottom; r++ {
		if !sheet.HiddenRows[r] {
			rows = append(rows, r)
		}
	}
	for c := used.Left; c <= used.Right; c++ {
		if !sheet.HiddenColumns[c] {
			columns = append(columns, c)
		}
	}
	cutRows, cutColumns := len(rows) > maxDocRows, len(columns) > maxDocColumns
	rows, columns = rows[:min(len(rows), maxDocRows)], columns[:min(len(columns), maxDocColumns)]

	// Excel widths count digits of Calibri 11 (7 px + 5 px padding); the fonts that
	// render the table are wider: 8 px per character plus the cell margins, 75 HWPUNIT per pixel.
	widths := make([]int, len(columns))
	total := 0
	for i, c := range columns {
		widths[i] = int(math.Round((sheet.ColumnWidth(c)*8 + 10) * 75))
		total += widths[i]
	}
	total = max(1, total)
	page := &doc.Page{Width: a4Short, Height: a4Long, Left: sheetMargin, Right: sheetMargin, Top: sheetMargin / 2, Bottom: sheetMargin / 2, Header: sheetMargin / 2, Footer: sheetMargin / 2}
	if total > a4Short-2*sheetMargin {
		page.Width, page.Height = a4Long, a4Short
	}
	scale := math.Min(1, float64(page.TextWidth())/float64(total))
	fontScale := math.Max(0.5, scale)
	scaled := make([]int, len(widths))
	for i, w := range widths {
		scaled[i] = max(300, int(float64(w)*scale))
	}

	section := doc.NewSection()
	section.Page = page
	if withTitle {
		title := &doc.Paragraph{Format: doc.ParaFormat{After: doc.Ptr(400)}, MarkFormat: doc.CharFormat{Size: doc.Ptr(1300)}}
		title.Inlines = append(title.Inlines, &doc.Text{Text: sheet.Name, Format: doc.CharFormat{Bold: doc.Ptr(true), Size: doc.Ptr(1300)}})
		section.Blocks = append(section.Blocks, title)
	}

	// Page breaks set in Excel start a new table on a new page; the title rows repeat at its top.
	var titles *[2]int
	if sheet.TitleRows != nil {
		titles = sheet.TitleRows
	} else if sheet.FrozenRows > 0 && sheet.FrozenRows <= 5 {
		titles = &[2]int{0, sheet.FrozenRows - 1}
	}
	headerRows := map[int]bool{}
	var lead, header, body []int
	for _, r := range rows {
		switch {
		case titles != nil && r >= titles[0] && r <= titles[1]:
			headerRows[r] = true
			header = append(header, r)
		case titles != nil && r < titles[0]:
			lead = append(lead, r)
		default:
			body = append(body, r)
		}
	}
	if len(lead) > 0 {
		section.Blocks = append(section.Blocks, sheetTable(sheet, lead, columns, nil, fontScale, scaled))
	}
	pages := [][]int{append([]int(nil), header...)}
	previous := -1
	for _, r := range body {
		// A break above a hidden row still counts: the next visible row starts the page.
		if len(pages[len(pages)-1]) > len(header) && hasBreak(sheet, previous+1, r) {
			pages = append(pages, append([]int(nil), header...))
		}
		pages[len(pages)-1] = append(pages[len(pages)-1], r)
		previous = r
	}
	for i, p := range pages {
		t := sheetTable(sheet, p, columns, headerRows, fontScale, scaled)
		t.PageBreakBefore = i > 0
		section.Blocks = append(section.Blocks, t)
	}
	if cutRows || cutColumns {
		var parts []string
		if cutRows {
			parts = append(parts, fmt.Sprintf("first %d rows", maxDocRows))
		}
		if cutColumns {
			parts = append(parts, fmt.Sprintf("first %d columns", maxDocColumns))
		}
		note := &doc.Paragraph{}
		note.Inlines = append(note.Inlines, &doc.Text{Text: "… " + sheet.Name + ": " + strings.Join(parts, ", ") + " shown",
			Format: doc.CharFormat{Italic: doc.Ptr(true), Size: doc.Ptr(900), Color: doc.Ptr("#808080")}})
		section.Blocks = append(section.Blocks, note)
	}
	return section
}

func hasBreak(sheet *Worksheet, from, to int) bool {
	for r := range sheet.RowBreaks {
		if r >= from && r <= to {
			return true
		}
	}
	return false
}

func sheetTable(sheet *Worksheet, rows, columns []int, headerRows map[int]bool, fontScale float64, widths []int) *doc.Table {
	t := &doc.Table{ColumnCount: len(columns), ColumnWidths: widths, CellMargin: &doc.Insets{Left: 141, Right: 141, Top: 57, Bottom: 57},
		Borders: &doc.Borders{Left: gridLine, Right: gridLine, Top: gridLine, Bottom: gridLine}}
	columnIndex := map[int]int{}
	for i, c := range columns {
		columnIndex[c] = i
	}
	// Merged areas: the top-left visible cell spans the visible rows and columns of the area.
	type span struct{ rows, cols int }
	anchors := map[rc]span{}
	covered := map[rc]bool{}
	for _, m := range sheet.Merges {
		var mr, mc []int
		for _, r := range rows {
			if r >= m.Top && r <= m.Bottom {
				mr = append(mr, r)
			}
		}
		for _, c := range columns {
			if c >= m.Left && c <= m.Right {
				mc = append(mc, c)
			}
		}
		if len(mr) == 0 || len(mc) == 0 {
			continue
		}
		anchors[rc{mr[0], mc[0]}] = span{len(mr), len(mc)}
		for _, r := range mr {
			for _, c := range mc {
				if r != mr[0] || c != mc[0] {
					covered[rc{r, c}] = true
				}
			}
		}
	}
	for _, r := range rows {
		row := &doc.Row{Header: headerRows[r]}
		if h, ok := sheet.RowHeights[r]; ok {
			row.Height = doc.Ptr(int(h * 100 * fontScale))
		}
		for _, c := range columns {
			if covered[rc{r, c}] {
				continue
			}
			s, ok := anchors[rc{r, c}]
			if !ok {
				s = span{1, 1}
			}
			row.Cells = append(row.Cells, sheetCell(sheet, r, c, s.rows, s.cols, columnIndex[c], fontScale))
		}
		t.Rows = append(t.Rows, row)
	}
	return t
}

func sheetCell(sheet *Worksheet, r, c, rowSpan, colSpan, column int, fontScale float64) *doc.Cell {
	value := sheet.Get(r, c)
	style := DefaultStyle
	if value != nil {
		style = value.Style
	}
	fs := style.FontSize
	if fs == 0 {
		fs = defaultFontSize
	}
	size := int(math.Round(fs * 100 * fontScale))
	format := doc.CharFormat{Size: doc.Ptr(size)}
	if style.Bold {
		format.Bold = doc.Ptr(true)
	}
	if style.Italic {
		format.Italic = doc.Ptr(true)
	}
	if style.Underline {
		format.Underline = doc.Ptr(true)
	}
	if style.Strike {
		format.Strike = doc.Ptr(true)
	}
	if style.Color != "" && style.Color != "#000000" {
		format.Color = doc.Ptr(style.Color)
	}
	align := doc.AlignLeft
	switch style.Align {
	case AlignCenter:
		align = doc.AlignCenter
	case AlignRight:
		align = doc.AlignRight
	case AlignGeneral:
		if value != nil && value.IsNumber {
			align = doc.AlignRight
		}
	}
	p := &doc.Paragraph{Format: doc.ParaFormat{Align: doc.Ptr(align)}, MarkFormat: doc.CharFormat{Size: doc.Ptr(size)}}
	text := ""
	if value != nil {
		text = value.Text
	}
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if i > 0 {
			p.Inlines = append(p.Inlines, &doc.LineBreak{Format: format})
		}
		if line != "" {
			p.Inlines = append(p.Inlines, &doc.Text{Text: line, Format: format})
		}
	}
	cell := doc.NewCell()
	cell.Column, cell.RowSpan, cell.ColSpan, cell.Fill = column, rowSpan, colSpan, style.Fill
	switch style.VerticalAlign {
	case VAlignTop:
		cell.VerticalAlign = doc.VAlignTop
	case VAlignCenter:
		cell.VerticalAlign = doc.VAlignCenter
	default:
		cell.VerticalAlign = doc.VAlignBottom
	}
	side := func(rr, cc int, pick func(CellStyle) CellBorder) *CellBorder {
		if n := sheet.Get(rr, cc); n != nil {
			b := pick(n.Style)
			return &b
		}
		return nil
	}
	// A border drawn by the neighbour counts too: in Excel both cells share the line.
	cell.Borders = &doc.Borders{
		Left:   docBorder(style.Left, side(r, c-1, func(s CellStyle) CellBorder { return s.Right })),
		Right:  docBorder(style.Right, side(r, c+colSpan, func(s CellStyle) CellBorder { return s.Left })),
		Top:    docBorder(style.Top, side(r-1, c, func(s CellStyle) CellBorder { return s.Bottom })),
		Bottom: docBorder(style.Bottom, side(r+rowSpan, c, func(s CellStyle) CellBorder { return s.Top })),
	}
	cell.Blocks = append(cell.Blocks, p)
	return cell
}

func docBorder(own CellBorder, neighbour *CellBorder) doc.Border {
	s := NoBorder
	if own.IsVisible() {
		s = own
	} else if neighbour != nil && neighbour.IsVisible() {
		s = *neighbour
	}
	if !s.IsVisible() {
		return gridLine
	}
	color := s.Color
	if color == "" {
		color = "#000000"
	}
	switch s.Style {
	case "hair":
		return doc.Border{Style: doc.BorderSolid, WidthMm: 0.1, Color: color}
	case "medium":
		return doc.Border{Style: doc.BorderSolid, WidthMm: 0.4, Color: color}
	case "thick":
		return doc.Border{Style: doc.BorderSolid, WidthMm: 0.7, Color: color}
	case "double":
		return doc.Border{Style: doc.BorderDouble, WidthMm: 0.5, Color: color}
	case "dotted":
		return doc.Border{Style: doc.BorderDot, WidthMm: 0.12, Color: color}
	case "dashed":
		return doc.Border{Style: doc.BorderDash, WidthMm: 0.12, Color: color}
	case "mediumDashed":
		return doc.Border{Style: doc.BorderDash, WidthMm: 0.4, Color: color}
	case "dashDot", "dashDotDot", "mediumDashDot", "mediumDashDotDot", "slantDashDot":
		return doc.Border{Style: doc.BorderDashDot, WidthMm: 0.12, Color: color}
	}
	return doc.Border{Style: doc.BorderSolid, WidthMm: 0.12, Color: color}
}
