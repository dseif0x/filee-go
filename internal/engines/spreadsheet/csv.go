package spreadsheet

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
	"github.com/dseif0x/filee-go/internal/formats"
)

// CSV / TSV ↔ Worksheet. Reading follows RFC 4180 (quotes, doubled quotes,
// line breaks inside quotes), detects the CSV delimiter (comma, semicolon or
// tab; TSV is always tab) and the encoding. Writing produces what Excel
// opens correctly everywhere: UTF-8 with BOM, CRLF line ends, quotes only
// when needed.

// ReadCSV reads a CSV file into one sheet named after the file.
func ReadCSV(path string) (*Workbook, error) { return readDelimited(path, 0) }

// ReadTSV reads a tab-separated file.
func ReadTSV(path string) (*Workbook, error) { return readDelimited(path, '\t') }

func readDelimited(path string, delimiter rune) (*Workbook, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := doc.DecodeText(b)
	if delimiter == 0 {
		delimiter = DetectDelimiter(text)
	}
	sheet := NewWorksheet(formats.NameWithoutExtension(filepath.Base(path)))
	widths := map[int]float64{}
	for r, row := range ParseCSV(text, delimiter) {
		for c, value := range row {
			if value == "" {
				continue
			}
			if IsCSVNumber(value) {
				v, _ := strconv.ParseFloat(value, 64)
				sheet.Set(r, c, &Cell{Text: value, Style: DefaultStyle, IsNumber: true, Value: &v})
			} else {
				sheet.Set(r, c, TextCell(value, DefaultStyle))
			}
			widths[c] = math.Max(widths[c], DisplayWidth(value))
		}
	}
	// CSV has no widths: size columns to their content like AutoFit would, within reason.
	for c, w := range widths {
		sheet.ColumnWidths[c] = math.Max(4, math.Min(50, w+1))
	}
	return &Workbook{Sheets: []*Worksheet{sheet}}, nil
}

// WriteCSV writes the used range of a sheet as CSV (or TSV with '\t').
func WriteCSV(sheet *Worksheet, path string, delimiter rune) error {
	var sb strings.Builder
	sb.WriteString("\xEF\xBB\xBF")
	if r, ok := sheet.UsedRange(); ok {
		for row := 0; row <= r.Bottom; row++ {
			if sheet.HiddenRows[row] {
				continue
			}
			var fields []string
			for c := 0; c <= r.Right; c++ {
				if sheet.HiddenColumns[c] {
					continue
				}
				text := ""
				if cell := sheet.Get(row, c); cell != nil {
					text = cell.Text
				}
				fields = append(fields, quote(text, delimiter))
			}
			// Excel keeps the column count stable, also for empty rows.
			sb.WriteString(strings.Join(fields, string(delimiter)))
			sb.WriteString("\r\n")
		}
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// ParseCSV splits text into rows of fields.
func ParseCSV(text string, delimiter rune) [][]string {
	var rows [][]string
	var row []string
	var field strings.Builder
	quoted := false
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		ch := rs[i]
		switch {
		case quoted:
			if ch == '"' && i+1 < len(rs) && rs[i+1] == '"' {
				field.WriteRune('"')
				i++
			} else if ch == '"' {
				quoted = false
			} else {
				field.WriteRune(ch)
			}
		case ch == '"' && field.Len() == 0:
			quoted = true
		case ch == delimiter:
			row = append(row, field.String())
			field.Reset()
		case ch == '\r' || ch == '\n':
			if ch == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			row = append(row, field.String())
			field.Reset()
			rows = append(rows, row)
			row = nil
		default:
			field.WriteRune(ch)
		}
	}
	if field.Len() > 0 || len(row) > 0 {
		rows = append(rows, append(row, field.String()))
	}
	return rows
}

// DetectDelimiter is the most frequent of comma, semicolon and tab outside
// quotes in the first 4096 characters (comma wins ties).
func DetectDelimiter(text string) rune {
	rs := []rune(text)
	if len(rs) > 4096 {
		rs = rs[:4096]
	}
	counts := map[rune]int{}
	quoted := false
	for _, ch := range rs {
		if ch == '"' {
			quoted = !quoted
		} else if !quoted && (ch == ',' || ch == ';' || ch == '\t') {
			counts[ch]++
		}
	}
	best := ','
	if counts[';'] > counts[best] {
		best = ';'
	}
	if counts['\t'] > counts[best] {
		best = '\t'
	}
	return best
}

func quote(value string, delimiter rune) string {
	if strings.ContainsAny(value, string(delimiter)+"\"\r\n") || strings.HasPrefix(value, " ") || strings.HasSuffix(value, " ") {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return value
}

// IsCSVNumber: a plain number Excel would store as a number: "12", "-3.5",
// "1e3" (not "007" or "1,234").
func IsCSVNumber(v string) bool {
	if len(v) == 0 || len(v) >= 20 {
		return false
	}
	if len(v) > 1 && v[0] == '0' && v[1] != '.' {
		return false
	}
	if len(v) > 2 && v[0] == '-' && v[1] == '0' && v[2] != '.' {
		return false
	}
	// .NET's NumberStyles.Float: no hex, no "Inf"/"NaN" words, no underscores.
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' || c == ' ') {
			return false
		}
	}
	_, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	return err == nil
}

// DisplayWidth is the width in characters as Excel counts them: wide (CJK) glyphs count twice.
func DisplayWidth(text string) float64 {
	longest := 0.0
	for _, line := range strings.Split(text, "\n") {
		w := 0.0
		for _, ch := range line {
			if ch >= 0x1100 && (ch <= 0x115F || ch >= 0x2E80 && ch <= 0xA4CF || ch >= 0xAC00 && ch <= 0xD7A3 || ch >= 0xF900 && ch <= 0xFAFF || ch >= 0xFF00 && ch <= 0xFF60) {
				w += 2
			} else {
				w++
			}
		}
		longest = math.Max(longest, w)
	}
	return longest
}
