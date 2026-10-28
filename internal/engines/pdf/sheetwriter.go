package pdf

import (
	"archive/zip"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"

	"github.com/dseif0x/filee-go/internal/convert"
)

// writeWorkbook writes XLSX or ODS, or one CSV per sheet.
func writeWorkbook(book *workbook, step *convert.Step) ([]string, error) {
	switch step.To {
	case "xlsx", "ods":
		path := step.Output.Allocate(step.To, "")
		if path == "" {
			return nil, nil
		}
		var err error
		if step.To == "xlsx" {
			err = writeXlsx(book, path)
		} else {
			err = writeOds(book, path)
		}
		if err != nil {
			os.Remove(path)
			return nil, err
		}
		return []string{path}, nil
	}
	// CSV holds one sheet: several sheets become one file each.
	var sheets []*worksheet
	for _, s := range book.Sheets {
		if r, _ := s.usedRange(); r >= 0 {
			sheets = append(sheets, s)
		}
	}
	if len(sheets) == 0 && len(book.Sheets) > 0 {
		sheets = book.Sheets[:1]
	}
	var out []string
	for _, s := range sheets {
		suffix := ""
		if len(sheets) > 1 {
			suffix = "_" + safeName(s.Name)
		}
		path := step.Output.Allocate(step.To, suffix)
		if path == "" {
			continue
		}
		if err := writeCsv(s, path); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

func safeName(name string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`<>:"/\|?*`, r) || r < 0x20 {
			return '_'
		}
		return r
	}, name)
}

func csvQuote(v string) string {
	if strings.ContainsAny(v, ",\"\r\n") || strings.HasPrefix(v, " ") || strings.HasSuffix(v, " ") {
		return `"` + strings.ReplaceAll(v, `"`, `""`) + `"`
	}
	return v
}

// writeCsv writes the used range (UTF-8 with BOM, CRLF).
func writeCsv(s *worksheet, path string) error {
	var b strings.Builder
	b.WriteString("\uFEFF")
	rows, cols := s.usedRange()
	for r := 0; r <= rows; r++ {
		fields := make([]string, cols+1)
		for c := 0; c <= cols; c++ {
			if cell := s.Cells[[2]int{r, c}]; cell != nil {
				fields[c] = csvQuote(cell.Text)
			}
		}
		b.WriteString(strings.Join(fields, ","))
		b.WriteString("\r\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeXlsx(book *workbook, path string) error {
	f := excelize.NewFile()
	defer f.Close()
	styles := map[string]int{}
	styleID := func(c *sheetCell) (int, error) {
		key := fmt.Sprintf("%v|%s|%d|%s", c.Style.Bold, c.Style.Fill, c.Style.VAlign, c.Format)
		if id, ok := styles[key]; ok {
			return id, nil
		}
		thin := []excelize.Border{{Type: "left", Color: "000000", Style: 1}, {Type: "right", Color: "000000", Style: 1},
			{Type: "top", Color: "000000", Style: 1}, {Type: "bottom", Color: "000000", Style: 1}}
		st := &excelize.Style{Border: thin, Font: &excelize.Font{Bold: c.Style.Bold},
			Alignment: &excelize.Alignment{WrapText: true, Vertical: map[VerticalAlign]string{VAlignTop: "top", VAlignCenter: "center", VAlignBottom: "bottom"}[c.Style.VAlign]}}
		if c.Style.Fill != "" {
			st.Fill = excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{strings.TrimPrefix(c.Style.Fill, "#")}}
		}
		if c.Format != "" {
			nf := c.Format
			st.CustomNumFmt = &nf
		}
		id, err := f.NewStyle(st)
		styles[key] = id
		return id, err
	}
	for i, s := range book.Sheets {
		name := s.Name
		if i == 0 {
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return err
			}
		} else if _, err := f.NewSheet(name); err != nil {
			return err
		}
		for c, w := range s.ColumnWidths {
			col, _ := excelize.ColumnNumberToName(c + 1)
			f.SetColWidth(name, col, col, w)
		}
		for r, h := range s.RowHeights {
			f.SetRowHeight(name, r+1, min(h, 409))
		}
		keys := make([][2]int, 0, len(s.Cells))
		for k := range s.Cells {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i][0] != keys[j][0] {
				return keys[i][0] < keys[j][0]
			}
			return keys[i][1] < keys[j][1]
		})
		for _, k := range keys {
			c := s.Cells[k]
			ref, _ := excelize.CoordinatesToCellName(k[1]+1, k[0]+1)
			var err error
			switch {
			case c.IsNumber:
				err = f.SetCellFloat(name, ref, c.Value, -1, 64)
			case c.Text != "":
				err = f.SetCellStr(name, ref, c.Text)
			}
			if err != nil {
				return err
			}
			id, err := styleID(c)
			if err != nil {
				return err
			}
			if err := f.SetCellStyle(name, ref, ref, id); err != nil {
				return err
			}
		}
		for _, m := range s.Merges {
			a, _ := excelize.CoordinatesToCellName(m.Left+1, m.Top+1)
			b, _ := excelize.CoordinatesToCellName(m.Right+1, m.Bottom+1)
			if err := f.MergeCell(name, a, b); err != nil {
				return err
			}
		}
	}
	return f.SaveAs(path)
}

// writeOds writes a minimal OpenDocument spreadsheet: values, dates, merged
// cells, bold, shading, borders, column widths and row heights.
func writeOds(book *workbook, path string) error {
	var content strings.Builder
	var autoStyles strings.Builder
	cellStyles := map[string]string{}
	cellStyle := func(c *sheetCell) string {
		key := fmt.Sprintf("%v|%s|%d", c.Style.Bold, c.Style.Fill, c.Style.VAlign)
		if n, ok := cellStyles[key]; ok {
			return n
		}
		n := fmt.Sprintf("ce%d", len(cellStyles)+1)
		cellStyles[key] = n
		fill := ""
		if c.Style.Fill != "" {
			fill = ` fo:background-color="` + c.Style.Fill + `"`
		}
		va := map[VerticalAlign]string{VAlignTop: "top", VAlignCenter: "middle", VAlignBottom: "bottom"}[c.Style.VAlign]
		weight := ""
		if c.Style.Bold {
			weight = `<style:text-properties fo:font-weight="bold"/>`
		}
		fmt.Fprintf(&autoStyles, `<style:style style:name="%s" style:family="table-cell"><style:table-cell-properties fo:border="0.5pt solid #000000" fo:wrap-option="wrap" style:vertical-align="%s"%s/>%s</style:style>`, n, va, fill, weight)
		return n
	}
	colStyles := map[float64]string{}
	rowStyles := map[float64]string{}
	var body strings.Builder
	for _, s := range book.Sheets {
		rows, cols := s.usedRange()
		fmt.Fprintf(&body, `<table:table table:name="%s">`, xmlEscape(s.Name))
		for c := 0; c <= cols; c++ {
			w := s.ColumnWidths[c]
			if w <= 0 {
				w = 8.43
			}
			name, ok := colStyles[w]
			if !ok {
				name = fmt.Sprintf("co%d", len(colStyles)+1)
				colStyles[w] = name
				fmt.Fprintf(&autoStyles, `<style:style style:name="%s" style:family="table-column"><style:table-column-properties style:column-width="%.2fpt"/></style:style>`, name, w*pointsPerCharacter+5)
			}
			fmt.Fprintf(&body, `<table:table-column table:style-name="%s"/>`, name)
		}
		covered := map[[2]int]bool{}
		spans := map[[2]int]cellRange{}
		for _, m := range s.Merges {
			spans[[2]int{m.Top, m.Left}] = m
			for r := m.Top; r <= m.Bottom; r++ {
				for c := m.Left; c <= m.Right; c++ {
					if r != m.Top || c != m.Left {
						covered[[2]int{r, c}] = true
					}
				}
			}
		}
		for r := 0; r <= rows; r++ {
			if h, ok := s.RowHeights[r]; ok && h > 0 {
				name, ok := rowStyles[h]
				if !ok {
					name = fmt.Sprintf("ro%d", len(rowStyles)+1)
					rowStyles[h] = name
					fmt.Fprintf(&autoStyles, `<style:style style:name="%s" style:family="table-row"><style:table-row-properties style:row-height="%.2fpt" style:use-optimal-row-height="false"/></style:style>`, name, h)
				}
				fmt.Fprintf(&body, `<table:table-row table:style-name="%s">`, name)
			} else {
				body.WriteString("<table:table-row>")
			}
			for c := 0; c <= cols; c++ {
				cell := s.Cells[[2]int{r, c}]
				if covered[[2]int{r, c}] {
					body.WriteString("<table:covered-table-cell/>")
					continue
				}
				if cell == nil {
					body.WriteString("<table:table-cell/>")
					continue
				}
				attrs := fmt.Sprintf(` table:style-name="%s"`, cellStyle(cell))
				if m, ok := spans[[2]int{r, c}]; ok {
					attrs += fmt.Sprintf(` table:number-rows-spanned="%d" table:number-columns-spanned="%d"`, m.Bottom-m.Top+1, m.Right-m.Left+1)
				}
				switch {
				case cell.IsDate:
					d := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(cell.Value))
					attrs += fmt.Sprintf(` office:value-type="date" office:date-value="%s"`, d.Format("2006-01-02"))
				case cell.IsNumber:
					attrs += ` office:value-type="float" office:value="` + strconv.FormatFloat(cell.Value, 'f', -1, 64) + `"`
				case cell.Text != "":
					attrs += ` office:value-type="string"`
				}
				fmt.Fprintf(&body, "<table:table-cell%s>", attrs)
				for _, line := range strings.Split(cell.Text, "\n") {
					if cell.Text != "" {
						fmt.Fprintf(&body, "<text:p>%s</text:p>", xmlEscape(line))
					}
				}
				body.WriteString("</table:table-cell>")
			}
			body.WriteString("</table:table-row>")
		}
		body.WriteString("</table:table>")
	}
	const ns = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
		`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
		`xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" office:version="1.3"`
	content.WriteString(xmlDeclaration + `<office:document-content ` + ns + `><office:automatic-styles>` + autoStyles.String() +
		`</office:automatic-styles><office:body><office:spreadsheet>` + body.String() + `</office:spreadsheet></office:body></office:document-content>`)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	z := zip.NewWriter(f)
	// The mimetype comes first, stored, so tools recognise the file.
	put := func(name, data string, method uint16) {
		if err != nil {
			return
		}
		var w interface{ Write([]byte) (int, error) }
		if w, err = z.CreateHeader(&zip.FileHeader{Name: name, Method: method}); err == nil {
			_, err = w.Write([]byte(data))
		}
	}
	put("mimetype", "application/vnd.oasis.opendocument.spreadsheet", zip.Store)
	put("content.xml", content.String(), zip.Deflate)
	put("styles.xml", xmlDeclaration+`<office:document-styles `+ns+`/>`, zip.Deflate)
	put("meta.xml", xmlDeclaration+`<office:document-meta xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:meta="urn:oasis:names:tc:opendocument:xmlns:meta:1.0" office:version="1.3"><office:meta><meta:generator>Filee</meta:generator></office:meta></office:document-meta>`, zip.Deflate)
	put("META-INF/manifest.xml", xmlDeclaration+`<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">`+
		`<manifest:file-entry manifest:full-path="/" manifest:version="1.3" manifest:media-type="application/vnd.oasis.opendocument.spreadsheet"/>`+
		`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>`+
		`<manifest:file-entry manifest:full-path="styles.xml" manifest:media-type="text/xml"/>`+
		`<manifest:file-entry manifest:full-path="meta.xml" manifest:media-type="text/xml"/></manifest:manifest>`, zip.Deflate)
	if cerr := z.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
