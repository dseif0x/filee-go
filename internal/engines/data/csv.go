package data

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dseif0x/filee-go/internal/engines/subtitles"
)

// CSV ↔ the data tree. CSV is read like the spreadsheet engine reads it
// (delimiter and encoding detected): the first row names the columns and every
// other row becomes an object. Cells that are plain numbers (not "007") or
// true / false get that type; everything else, empty cells included, stays text.
// Only tables become CSV: an array of objects (columns = every key, in the
// order first seen), an object of arrays (one column per key), an array of
// arrays (rows as they are) or a single flat object (one row). Objects inside a
// row one level deep become "parent.child" columns; deeper nesting, or a list
// inside a row, is an error. Wrappers around the array are looked through.
// Written as the spreadsheet engine writes CSV: UTF-8 with BOM, CRLF, so Excel
// opens Korean text correctly.

// parseCSV reads RFC 4180 CSV (quotes, doubled quotes, line breaks inside quotes).
func parseCSV(text string, delimiter rune) [][]string {
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
		row = append(row, field.String())
		rows = append(rows, row)
	}
	return rows
}

// detectDelimiter is the most frequent of comma, semicolon and tab outside
// quotes in the first 4096 characters (comma on a tie).
func detectDelimiter(text string) rune {
	rs := []rune(text)
	if len(rs) > 4096 {
		rs = rs[:4096]
	}
	counts := map[rune]int{',': 0, ';': 0, '\t': 0}
	quoted := false
	for _, ch := range rs {
		if ch == '"' {
			quoted = !quoted
		} else if _, ok := counts[ch]; !quoted && ok {
			counts[ch]++
		}
	}
	best := ','
	for _, c := range []rune{';', '\t'} {
		if counts[c] > counts[best] {
			best = c
		}
	}
	return best
}

func quoteCSV(v string, delimiter rune) string {
	if strings.ContainsAny(v, string(delimiter)+"\"\r\n") || strings.HasPrefix(v, " ") || strings.HasSuffix(v, " ") {
		return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return v
}

// writeCSVRows writes rows as CSV (UTF-8 with BOM, CRLF) to a file beside the
// target that replaces it once every row is written.
func writeCSVRows(rows [][]string, path string) error {
	if _, err := os.Stat(path); err == nil {
		// Replacing a file needs only the folder's permission on Unix: refuse what writing into the file would refuse.
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		f.Close()
	}
	var b strings.Builder
	b.WriteString("\xEF\xBB\xBF")
	for _, row := range rows {
		for i, v := range row {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(quoteCSV(v, ','))
		}
		b.WriteString("\r\n")
	}
	return writeAtomic(path, []byte(b.String()))
}

func writeAtomic(path string, data []byte) error {
	var r [4]byte
	_, _ = rand.Read(r[:])
	abs, _ := filepath.Abs(path)
	partial := filepath.Join(filepath.Dir(abs), "."+filepath.Base(path)+"."+hex.EncodeToString(r[:])+".filee-tmp")
	if err := os.WriteFile(partial, data, 0o644); err != nil {
		os.Remove(partial)
		return err
	}
	if err := os.Rename(partial, path); err != nil {
		os.Remove(partial)
		return err
	}
	return nil
}

// readCSVRows is the rows of a CSV file as text (blank rows dropped).
func readCSVRows(path string) ([][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	t := subtitles.DecodeText(b)
	var rows [][]string
	for _, row := range parseCSV(t, detectDelimiter(t)) {
		for _, c := range row {
			if c != "" {
				rows = append(rows, row)
				break
			}
		}
	}
	return rows, nil
}

// readCSV is a CSV file as an array of objects keyed by the header row.
func readCSV(path string) (any, error) {
	rows, err := readCSVRows(path)
	if err != nil {
		return nil, err
	}
	records := []any{}
	if len(rows) == 0 {
		return records, nil
	}
	headers := uniqueHeaders(rows[0])
	for _, row := range rows[1:] {
		for len(headers) < len(row) {
			headers = append(headers, fmt.Sprintf("column%d", len(headers)+1))
		}
		rec := NewMap()
		for c, h := range headers {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			rec.Set(h, typedCell(cell))
		}
		records = append(records, rec)
	}
	return records, nil
}

// uniqueHeaders names blank columns "columnN" and repeated ones "_2", "_3".
func uniqueHeaders(row []string) []string {
	used := map[string]bool{}
	var headers []string
	for c, h := range row {
		name := strings.TrimSpace(h)
		if name == "" {
			name = fmt.Sprintf("column%d", c+1)
		}
		unique := name
		for i := 2; used[unique]; i++ {
			unique = fmt.Sprintf("%s_%d", name, i)
		}
		used[unique] = true
		headers = append(headers, unique)
	}
	return headers
}

var reJSONNumber = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][-+]?[0-9]+)?$`)

func typedCell(cell string) any {
	if n := len([]rune(cell)); n > 0 && n < 20 && reJSONNumber.MatchString(cell) {
		return number(cell)
	}
	if strings.EqualFold(cell, "true") {
		return true
	}
	if strings.EqualFold(cell, "false") {
		return false
	}
	return cell
}

func notATable(what string) error {
	return errors.New(what + ": CSV holds a table. Convert an array of objects (one level of nesting at most), an object of arrays or an array of arrays.")
}

// csvTable is the header row and the rows of a table-like tree.
func csvTable(root any) ([][]string, error) {
	node := unwrapRecords(root)
	switch x := node.(type) {
	case []any:
		if len(x) == 0 {
			return nil, nil
		}
		allRecords, allRows, allScalars := true, true, true
		for _, item := range x {
			switch it := item.(type) {
			case *Map:
				allRows, allScalars = false, false
			case nil:
				allRows = false
			case []any:
				allRecords, allScalars = false, false
				for _, c := range it {
					if !isScalar(c) {
						allRows = false
					}
				}
			default:
				allRecords, allRows = false, false
			}
		}
		switch {
		case allRecords:
			recs := make([]*Map, len(x))
			for i, item := range x {
				if m, ok := item.(*Map); ok {
					recs[i] = m
				} else {
					recs[i] = NewMap()
				}
			}
			return csvRecords(recs)
		case allRows:
			out := make([][]string, len(x))
			for i, item := range x {
				for _, c := range item.([]any) {
					out[i] = append(out[i], text(c))
				}
				if out[i] == nil {
					out[i] = []string{}
				}
			}
			return out, nil
		case allScalars:
			out := [][]string{{"value"}}
			for _, item := range x {
				out = append(out, []string{text(item)})
			}
			return out, nil
		}
		return nil, notATable("The array mixes objects, lists and single values")
	case *Map:
		if x.Len() > 0 && columnsOnly(x) {
			return csvColumns(x), nil
		}
		return csvRecords([]*Map{x})
	}
	return nil, notATable("The data is a single value")
}

func columnsOnly(m *Map) bool {
	for _, k := range m.keys {
		list, ok := m.vals[k].([]any)
		if !ok {
			return false
		}
		for _, c := range list {
			if !isScalar(c) {
				return false
			}
		}
	}
	return true
}

func csvRecords(records []*Map) ([][]string, error) {
	var columns []string
	known := map[string]bool{}
	flat := make([]map[string]string, 0, len(records))
	for r, rec := range records {
		cells := map[string]string{}
		add := func(col string, v any) {
			if !known[col] {
				known[col] = true
				columns = append(columns, col)
			}
			cells[col] = text(v)
		}
		for _, k := range rec.keys {
			switch v := rec.vals[k].(type) {
			case *Map:
				for _, ck := range v.keys {
					child := v.vals[ck]
					if !isScalar(child) {
						return nil, notATable(fmt.Sprintf("Row %d, “%s.%s” is nested more than one level", r+1, k, ck))
					}
					add(k+"."+ck, child)
				}
			case []any:
				return nil, notATable(fmt.Sprintf("Row %d, “%s” holds a list", r+1, k))
			default:
				add(k, v)
			}
		}
		flat = append(flat, cells)
	}
	out := [][]string{append([]string{}, columns...)}
	for _, cells := range flat {
		row := make([]string, len(columns))
		for i, c := range columns {
			row[i] = cells[c]
		}
		out = append(out, row)
	}
	return out, nil
}

func csvColumns(m *Map) [][]string {
	height := 0
	for _, k := range m.keys {
		height = max(height, len(m.vals[k].([]any)))
	}
	out := [][]string{append([]string{}, m.keys...)}
	for r := 0; r < height; r++ {
		row := make([]string, len(m.keys))
		for i, k := range m.keys {
			if col := m.vals[k].([]any); r < len(col) {
				row[i] = text(col[r])
			}
		}
		out = append(out, row)
	}
	return out
}
