// PDF → XLSX, ODS, CSV: the tables drawn with lines in a PDF, one sheet each
// (the ruled tables the reader finds, with merged cells, column widths and
// row heights); text outside them is left out.
//
// - A table that goes on at the top of the next page with the same columns
//   joins the one before it on one sheet; a header row repeated on the new
//   page is left out.
// - Cells that read as numbers ("1,234.50", "(120)", "$9.99"), percentages
//   ("12.5%") or dates with the year first ("2026-10-23", "2026. 10. 23.")
//   become real numbers and dates in a format that shows them as the PDF did.
//   Anything else stays text: leading zeros, more than 15 digits, a decimal
//   comma, or day-first dates.
// - A PDF without such a table fails with a message instead of an empty workbook.

package pdf

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dseif0x/filee-go/internal/convert"
)

// pointsPerCharacter of Excel's column width (Calibri 11: 7 px at 96 dpi).
const pointsPerCharacter = 5.25

// PdfTables writes the ruled tables of a PDF as a spreadsheet.
type PdfTables struct{}

func (PdfTables) ID() string          { return "pdf-tables" }
func (PdfTables) Name() string        { return "PDF tables (built-in)" }
func (PdfTables) MaxParallelism() int { return pdfiumWorkers }

// Edges: only from a PDF, and never a step on a longer route (a picture made a
// PDF has no ruled tables).
func (PdfTables) Edges() []convert.Edge {
	return []convert.Edge{
		{From: "pdf", To: "xlsx", Cost: 10, DirectOnly: true},
		{From: "pdf", To: "ods", Cost: 10, DirectOnly: true},
		{From: "pdf", To: "csv", Cost: 10, DirectOnly: true},
	}
}

func (PdfTables) Status() convert.Status {
	return convert.Available("PDF → XLSX, ODS, CSV", "built-in")
}

func (PdfTables) Convert(ctx context.Context, step *convert.Step, progress convert.Progress) ([]string, error) {
	doc, err := ReadDocument(ctx, step.InputPath, mediaDir(step), ReadFull)
	if err != nil {
		return nil, err
	}
	progress.Report(0.7)
	book := buildWorkbook(doc)
	if len(book.Sheets) == 0 {
		return nil, convert.Refuse("error.pdf_no_tables", "")
	}
	out, err := writeWorkbook(book, step)
	progress.Report(1)
	return out, err
}

// ---- a small workbook model ----

type cellStyle struct {
	Bold   bool
	Fill   string
	VAlign VerticalAlign
}

type sheetCell struct {
	Text     string
	IsNumber bool
	IsDate   bool
	Value    float64
	Format   string
	Style    cellStyle
}

type cellRange struct{ Top, Left, Bottom, Right int }

type worksheet struct {
	Name         string
	Cells        map[[2]int]*sheetCell
	ColumnWidths map[int]float64
	RowHeights   map[int]float64
	Merges       []cellRange
}

func newWorksheet(name string) *worksheet {
	return &worksheet{Name: name, Cells: map[[2]int]*sheetCell{}, ColumnWidths: map[int]float64{}, RowHeights: map[int]float64{}}
}

// usedRange is the last row and column with a cell (-1 when empty).
func (s *worksheet) usedRange() (rows, cols int) {
	rows, cols = -1, -1
	for k := range s.Cells {
		rows, cols = max(rows, k[0]), max(cols, k[1])
	}
	return
}

type workbook struct{ Sheets []*worksheet }

type placedTable struct {
	table                   *Table
	page                    int
	firstOnPage, lastOnPage bool
}

// buildWorkbook: one sheet per table, a table going on over the next page joined.
func buildWorkbook(doc *Document) *workbook {
	type entry struct {
		table *placedTable
		page  int
	}
	var items []entry
	page := -1
	for _, s := range doc.Sections {
		page++
		for _, b := range s.Blocks {
			switch v := b.(type) {
			case *Paragraph:
				if v.PageBreakBefore {
					page++
				}
				for _, in := range v.Inlines {
					if t, ok := in.(*Text); ok && strings.TrimSpace(t.Text) != "" {
						items = append(items, entry{nil, page})
						break
					}
				}
			case *Table:
				if len(v.Rows) > 0 {
					items = append(items, entry{&placedTable{table: v, page: page}, page})
				}
			}
		}
	}
	var tables []*placedTable
	for i, e := range items {
		if e.table == nil {
			continue
		}
		e.table.firstOnPage = i == 0 || items[i-1].page != e.table.page
		e.table.lastOnPage = i == len(items)-1 || items[i+1].page != e.table.page
		tables = append(tables, e.table)
	}
	book := &workbook{}
	var sheet *worksheet
	var previous *placedTable
	var header []string
	row := 0
	for _, p := range tables {
		goesOn := previous != nil && sheet != nil && previous.lastOnPage && p.firstOnPage && p.page == previous.page+1 && sameColumns(previous.table, p.table)
		skip := 0
		if goesOn && header != nil && slicesEqual(rowTexts(p.table.Rows[0]), header) {
			skip = 1
		}
		if !goesOn {
			sheet = newWorksheet(fmt.Sprintf("Table %d", len(book.Sheets)+1))
			book.Sheets = append(book.Sheets, sheet)
			row = 0
			header = rowTexts(p.table.Rows[0])
			for c, w := range p.table.ColumnWidths {
				sheet.ColumnWidths[c] = math.Round(max(2, min(float64(w)/100/pointsPerCharacter, 100))*100) / 100
			}
		}
		row = placeTable(sheet, p.table, row, skip)
		previous = p
	}
	return book
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameColumns: the same number of columns, each within 5 % of the other's width.
func sameColumns(a, b *Table) bool {
	if a.ColumnCount != b.ColumnCount {
		return false
	}
	if len(a.ColumnWidths) == 0 || len(a.ColumnWidths) != len(b.ColumnWidths) {
		return true
	}
	for i := range a.ColumnWidths {
		x, y := float64(a.ColumnWidths[i]), float64(b.ColumnWidths[i])
		if math.Abs(x-y) > 0.05*max(x, y)+100 {
			return false
		}
	}
	return true
}

// placeTable writes the table's rows from top on, leaving out its first skip rows; it returns the next free row.
func placeTable(sheet *worksheet, t *Table, top, skip int) int {
	taken := map[[2]int]bool{}
	row := top
	maxRow := top - 1
	for _, src := range t.Rows[skip:] {
		if src.Height > 0 {
			sheet.RowHeights[row] = math.Round(float64(src.Height)) / 100
		}
		column := 0
		for _, c := range src.Cells {
			if c.Column >= 0 {
				column = c.Column
			}
			for taken[[2]int{row, column}] {
				column++
			}
			style := styleOf(c)
			sheet.Cells[[2]int{row, column}] = shownCell(cellText(c), style)
			rows, cols := max(1, c.RowSpan), max(1, c.ColSpan)
			for r := row; r < row+rows; r++ {
				for k := column; k < column+cols; k++ {
					taken[[2]int{r, k}] = true
					maxRow = max(maxRow, r)
					// The rest of a merged area keeps the borders, as Excel draws them from every cell.
					if r != row || k != column {
						sheet.Cells[[2]int{r, k}] = &sheetCell{Style: style}
					}
				}
			}
			if rows > 1 || cols > 1 {
				sheet.Merges = append(sheet.Merges, cellRange{row, column, row + rows - 1, column + cols - 1})
			}
			column += cols
		}
		row++
	}
	return max(row, maxRow+1)
}

func styleOf(c *Cell) cellStyle {
	bold := false
	any := false
	allBold := true
	for _, b := range c.Blocks {
		if p, ok := b.(*Paragraph); ok {
			for _, in := range p.Inlines {
				if t, ok := in.(*Text); ok {
					any = true
					if !t.Format.Bold && strings.TrimSpace(t.Text) != "" {
						allBold = false
					}
				}
			}
		}
	}
	bold = any && allBold
	return cellStyle{Bold: bold, Fill: c.Fill, VAlign: c.VerticalAlign}
}

func rowTexts(r *Row) []string {
	out := make([]string, len(r.Cells))
	for i, c := range r.Cells {
		out[i] = cellText(c)
	}
	return out
}

// cellText: a cell's paragraphs as lines.
func cellText(c *Cell) string {
	var lines []string
	for _, b := range c.Blocks {
		p, ok := b.(*Paragraph)
		if !ok {
			continue
		}
		var s strings.Builder
		for _, in := range p.Inlines {
			switch v := in.(type) {
			case *Text:
				s.WriteString(v.Text)
			case *Tab:
				s.WriteByte('\t')
			case *LineBreak:
				s.WriteByte('\n')
			}
		}
		lines = append(lines, s.String())
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

var (
	numberPattern = regexp.MustCompile(`^(?P<minus>-)?(?P<open>\()?(?P<currency>[$€£¥₩₹])?(?P<whole>\d{1,3}(?:,\d{3})+|\d+)(?:\.(?P<fraction>\d+))?(?P<percent>%)?(?P<close>\))?$`)
	datePattern   = regexp.MustCompile(`^(\d{4})(-|/|\. ?)(\d{1,2})(-|/|\. ?)(\d{1,2})(\.?)$`)
)

// shownCell is a cell as the PDF shows the text: a number or date with a matching format, or text.
func shownCell(text string, style cellStyle) *sheetCell {
	if v, f, ok := parseNumber(text); ok {
		return &sheetCell{Text: text, IsNumber: true, Value: v, Format: f, Style: style}
	}
	if v, f, ok := parseDate(text); ok {
		return &sheetCell{Text: text, IsNumber: true, IsDate: true, Value: v, Format: f, Style: style}
	}
	return &sheetCell{Text: text, Style: style}
}

// parseNumber: "1,234.50", "-7", "(120)", "$9.99", "12.5%" as a value and an Excel number format.
func parseNumber(text string) (float64, string, bool) {
	m := numberPattern.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil {
		return 0, "", false
	}
	g := func(n string) string { return m[numberPattern.SubexpIndex(n)] }
	whole, fraction := g("whole"), g("fraction")
	digits := strings.ReplaceAll(whole, ",", "") + fraction
	// Codes and phone numbers keep their leading zero; Excel holds 15 significant digits.
	if (len(whole) > 1 && whole[0] == '0') || len(strings.TrimLeft(digits, "0")) > 15 {
		return 0, "", false
	}
	open, closed := g("open") != "", g("close") != ""
	if open != closed {
		return 0, "", false
	}
	s := strings.ReplaceAll(whole, ",", "")
	if fraction != "" {
		s += "." + fraction
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, "", false
	}
	format := "0"
	if strings.Contains(whole, ",") {
		format = "#,##0"
	}
	if fraction != "" {
		format += "." + strings.Repeat("0", len(fraction))
	}
	if g("percent") != "" {
		v /= 100
		format += "%"
	}
	if c := g("currency"); c != "" {
		format = `"` + c + `"` + format
	}
	if open {
		format = format + ";(" + format + ")"
	}
	if g("minus") != "" || open {
		v = -v
	}
	return v, format, true
}

// parseDate: "2026-10-23", "2026/1/5", "2026. 10. 23." as an Excel date serial and format.
func parseDate(text string) (float64, string, bool) {
	m := datePattern.FindStringSubmatch(strings.TrimSpace(text))
	if m == nil || m[2] != m[4] {
		return 0, "", false
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[3])
	d, _ := strconv.Atoi(m[5])
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return 0, "", false
	}
	sep := m[2]
	mf, df := "m", "d"
	if len(m[3]) == 2 {
		mf = "mm"
	}
	if len(m[5]) == 2 {
		df = "dd"
	}
	serial := t.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24
	return math.Round(serial), "yyyy" + sep + mf + sep + df + m[6], true
}
