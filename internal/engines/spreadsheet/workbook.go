// Package spreadsheet converts between XLSX, XLS, ODS, CSV and TSV in-process
// (XLS / XLT and the XLSM / XLTX variants are read, not written), and reads
// sheets into the document model (sheets become tables) for the DOCX and
// HWPX writers.
//
// The in-memory Workbook sits between the readers and the writers. Cells
// hold the text as the spreadsheet shows it (numbers and dates already
// formatted) plus, for numbers, the value and its number format, so
// spreadsheet → spreadsheet conversions keep real numbers.
package spreadsheet

import (
	"sort"
	"strconv"
	"strings"
)

// Workbook is a list of sheets.
type Workbook struct{ Sheets []*Worksheet }

// StandardColumnWidth is Excel's default column width in characters.
const StandardColumnWidth = 8.43

// Worksheet is one sheet.
type Worksheet struct {
	Name string
	// Rows holds cells by 0-based row, then column: only cells with a value or visible formatting.
	Rows map[int]map[int]*Cell
	// ColumnWidths in characters, for columns that set one.
	ColumnWidths       map[int]float64
	DefaultColumnWidth float64
	// RowHeights in points, for rows with a custom height.
	RowHeights    map[int]float64
	HiddenRows    map[int]bool
	HiddenColumns map[int]bool
	Merges        []CellRange
	// FrozenRows at the top (headers) repeat on every printed page.
	FrozenRows int
	// TitleRows are 0-based rows to repeat on every printed page (Print Titles), or nil.
	TitleRows *[2]int
	// RowBreaks are rows that start a new printed page.
	RowBreaks map[int]bool
}

// NewWorksheet returns an empty sheet.
func NewWorksheet(name string) *Worksheet {
	return &Worksheet{
		Name: name, Rows: map[int]map[int]*Cell{}, ColumnWidths: map[int]float64{}, DefaultColumnWidth: StandardColumnWidth,
		RowHeights: map[int]float64{}, HiddenRows: map[int]bool{}, HiddenColumns: map[int]bool{}, RowBreaks: map[int]bool{},
	}
}

func (s *Worksheet) Set(row, column int, c *Cell) {
	cells := s.Rows[row]
	if cells == nil {
		cells = map[int]*Cell{}
		s.Rows[row] = cells
	}
	cells[column] = c
}

// Get returns the cell, or nil.
func (s *Worksheet) Get(row, column int) *Cell { return s.Rows[row][column] }

func (s *Worksheet) ColumnWidth(column int) float64 {
	if w, ok := s.ColumnWidths[column]; ok {
		return w
	}
	return s.DefaultColumnWidth
}

// RowIndexes returns the rows holding cells, sorted.
func (s *Worksheet) RowIndexes() []int { return sortedKeys(s.Rows) }

// Columns returns the columns of a row holding cells, sorted.
func (s *Worksheet) Columns(row int) []int { return sortedKeys(s.Rows[row]) }

func sortedKeys[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func sortedSet(m map[int]bool) []int {
	var out []int
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Ints(out)
	return out
}

// UsedRange is the smallest range holding every cell with text, fill or
// borders (merged areas included); ok is false for an empty sheet.
func (s *Worksheet) UsedRange() (CellRange, bool) {
	top, left, bottom, right := int(^uint(0)>>1), int(^uint(0)>>1), -1, -1
	for r, cells := range s.Rows {
		for c, cell := range cells {
			if !cell.HasContent() {
				continue
			}
			top, bottom = min(top, r), max(bottom, r)
			left, right = min(left, c), max(right, c)
		}
	}
	if bottom < 0 {
		return CellRange{}, false
	}
	for _, m := range s.Merges {
		if m.Top <= bottom && m.Left <= right && m.Top >= top && m.Left >= left {
			bottom, right = max(bottom, m.Bottom), max(right, m.Right)
		}
	}
	return CellRange{top, left, bottom, right}, true
}

// CellRange is an inclusive, 0-based range of cells.
type CellRange struct{ Top, Left, Bottom, Right int }

func (r CellRange) Contains(row, column int) bool {
	return row >= r.Top && row <= r.Bottom && column >= r.Left && column <= r.Right
}

// ParseRange parses "B2" or "A1:C3".
func ParseRange(ref string) (CellRange, bool) {
	ref = strings.ReplaceAll(strings.TrimSpace(ref), "$", "")
	if ref == "" {
		return CellRange{}, false
	}
	parts := strings.Split(ref, ":")
	r1, c1, ok := ParseCell(parts[0])
	if !ok {
		return CellRange{}, false
	}
	if len(parts) == 1 {
		return CellRange{r1, c1, r1, c1}, true
	}
	r2, c2, ok := ParseCell(parts[1])
	if !ok {
		return CellRange{}, false
	}
	return CellRange{min(r1, r2), min(c1, c2), max(r1, r2), max(c1, c2)}, true
}

// ParseCell: "AB12" → row 11, column 27.
func ParseCell(ref string) (row, column int, ok bool) {
	i, col := 0, 0
	for i < len(ref) && isLetter(ref[i]) {
		ch := ref[i]
		if ch >= 'a' {
			ch -= 'a' - 'A'
		}
		col = col*26 + int(ch-'A'+1)
		i++
		if col > 1<<20 {
			return -1, -1, false
		}
	}
	if i == 0 || i == len(ref) {
		return -1, -1, false
	}
	r, err := strconv.Atoi(ref[i:])
	if err != nil || r < 1 {
		return -1, -1, false
	}
	return r - 1, col - 1, true
}

func isLetter(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' }

// ColumnName: 0 → "A", 26 → "AA".
func ColumnName(column int) string {
	name := ""
	for c := column + 1; c > 0; c = (c - 1) / 26 {
		name = string(rune('A'+(c-1)%26)) + name
	}
	return name
}

type CellAlign int

const (
	AlignGeneral CellAlign = iota
	AlignLeft
	AlignCenter
	AlignRight
)

type CellVerticalAlign int

const (
	VAlignBottom CellVerticalAlign = iota
	VAlignCenter
	VAlignTop
)

// CellBorder is one border side: Excel style name ("thin", "medium", "dashed", …) and colour.
type CellBorder struct{ Style, Color string }

// NoBorder is an invisible side.
var NoBorder = CellBorder{Style: "none"}

func (b CellBorder) IsVisible() bool { return b.Style != "none" && b.Style != "" }

// CellStyle is the look of a cell. It is comparable (used as a map key).
type CellStyle struct {
	Bold, Italic, Underline, Strike bool
	// FontSize in points; 0 = the workbook default.
	FontSize float64
	// Color and Fill are "#RRGGBB"; "" = automatic / none.
	Color, Fill              string
	Align                    CellAlign
	VerticalAlign            CellVerticalAlign
	Wrap                     bool
	Left, Right, Top, Bottom CellBorder
}

// DefaultStyle is the plain look.
var DefaultStyle = CellStyle{Left: NoBorder, Right: NoBorder, Top: NoBorder, Bottom: NoBorder}

func (s CellStyle) HasBorders() bool {
	return s.Left.IsVisible() || s.Right.IsVisible() || s.Top.IsVisible() || s.Bottom.IsVisible()
}

// normalized treats empty border styles as "none", so equal looks compare equal.
func (s CellStyle) normalized() CellStyle {
	for _, b := range []*CellBorder{&s.Left, &s.Right, &s.Top, &s.Bottom} {
		if !b.IsVisible() {
			*b = NoBorder
		}
	}
	return s
}

// Cell is a cell as displayed: formatted text plus style.
type Cell struct {
	// Text is what the cell shows ("1,234.50", "2026-10-01", "TRUE").
	Text  string
	Style CellStyle
	// IsNumber: numbers (and dates) align right under General alignment.
	IsNumber bool
	// Value is the number behind Text (dates as Excel serial days in the 1900
	// system, booleans 1 / 0); nil for text and errors.
	Value *float64
	// NumberFormat is the Excel format code of Value; "" = General.
	NumberFormat string
	IsBoolean    bool
}

// TextCell returns a text cell.
func TextCell(text string, style CellStyle) *Cell { return &Cell{Text: text, Style: style} }

func (c *Cell) HasContent() bool {
	return c != nil && (c.Text != "" || c.Style.Fill != "" || c.Style.HasBorders())
}

func fptr(v float64) *float64 { return &v }
