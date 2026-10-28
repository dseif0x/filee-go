package spreadsheet

import (
	"archive/zip"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"os"

	"strconv"
	"strings"

	"github.com/dseif0x/filee-go/internal/doc"
)

// Workbook → XLSX package: every sheet, numbers and dates as numbers with
// their number formats, booleans, text as inline strings, fonts / fills /
// borders / alignment, merged cells, column widths, row heights, hidden rows
// and columns, frozen header rows.

const relationshipsNs = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"

// builtInIds are the format ids Excel knows without a numFmt element.
var builtInIds = map[string]int{"0": 1, "0.00": 2, "#,##0": 3, "#,##0.00": 4, "0%": 9, "0.00%": 10, "0.00E+00": 11, "@": 49}

// WriteXLSX writes book as an XLSX file.
func WriteXLSX(book *Workbook, path string) error {
	sheets := book.Sheets
	if len(sheets) == 0 {
		sheets = []*Worksheet{NewWorksheet("Sheet1")}
	}
	names := make([]string, len(sheets))
	for i, s := range sheets {
		names[i] = s.Name
	}
	names = UniqueNames(names)
	styles := newStyleTable()
	sheetXML := make([]string, len(sheets))
	for i, s := range sheets {
		sheetXML[i] = xlsxSheet(s, styles, i == 0)
	}

	var ct strings.Builder
	ct.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	var rels, wb strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	fmt.Fprintf(&wb, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="%s" xmlns:r="%s"><sheets>`, sNs, relationshipsNs)
	for i := range sheets {
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, doc.EscapeXML(names[i]), i+1, i+1)
	}
	ct.WriteString("</Types>")
	rels.WriteString(`<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`)
	wb.WriteString("</sheets></workbook>")

	parts := [][2]string{
		{"[Content_Types].xml", ct.String()},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n" + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/_rels/workbook.xml.rels", rels.String()},
		{"xl/workbook.xml", wb.String()},
		{"xl/styles.xml", styles.xml()},
	}
	for i := range sheets {
		parts = append(parts, [2]string{fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sheetXML[i]})
	}
	return WriteZip(path, parts)
}

// WriteZip writes parts (name, content) as a deflated zip; an entry named
// "mimetype" is stored (OpenDocument).
func WriteZip(path string, parts [][2]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, p := range parts {
		var err error
		if p[0] == "mimetype" {
			err = StoreEntry(zw, p[0], []byte(p[1]))
		} else {
			var w io.Writer
			if w, err = zw.CreateHeader(&zip.FileHeader{Name: p[0], Method: zip.Deflate}); err == nil {
				_, err = io.WriteString(w, p[1])
			}
		}
		if err != nil {
			zw.Close()
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// fmtG formats like .NET's "0.###" (up to three decimals, no trailing zeros).
func fmtG(v float64, places int) string {
	s := strconv.FormatFloat(v, 'f', places, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	if s == "-0" {
		s = "0"
	}
	return s
}

func xlsxSheet(sheet *Worksheet, styles *styleTable, selected bool) string {
	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sel := ""
	if selected {
		sel = ` tabSelected="1"`
	}
	fmt.Fprintf(&sb, `<worksheet xmlns="%s" xmlns:r="%s"><sheetViews><sheetView%s workbookViewId="0">`, sNs, relationshipsNs, sel)
	if sheet.FrozenRows > 0 {
		fmt.Fprintf(&sb, `<pane ySplit="%d" topLeftCell="A%d" activePane="bottomLeft" state="frozen"/>`, sheet.FrozenRows, sheet.FrozenRows+1)
	}
	sb.WriteString(`</sheetView></sheetViews><sheetFormatPr defaultRowHeight="15" `)
	if math.Abs(sheet.DefaultColumnWidth-StandardColumnWidth) > 0.005 {
		fmt.Fprintf(&sb, `defaultColWidth="%s" `, fmtG(sheet.DefaultColumnWidth, 3))
	}
	sb.WriteString("/>")
	xlsxColumns(&sb, sheet)

	sb.WriteString("<sheetData>")
	rowSet := map[int]bool{}
	for r := range sheet.Rows {
		rowSet[r] = true
	}
	for r := range sheet.RowHeights {
		rowSet[r] = true
	}
	for r, h := range sheet.HiddenRows {
		if h {
			rowSet[r] = true
		}
	}
	for _, row := range sortedSet(rowSet) {
		fmt.Fprintf(&sb, `<row r="%d"`, row+1)
		if h, ok := sheet.RowHeights[row]; ok {
			fmt.Fprintf(&sb, ` ht="%s" customHeight="1"`, fmtG(h, 2))
		}
		if sheet.HiddenRows[row] {
			sb.WriteString(` hidden="1"`)
		}
		sb.WriteByte('>')
		for _, c := range sheet.Columns(row) {
			xlsxCell(&sb, ColumnName(c)+strconv.Itoa(row+1), sheet.Rows[row][c], styles)
		}
		sb.WriteString("</row>")
	}
	sb.WriteString("</sheetData>")
	if len(sheet.Merges) > 0 {
		fmt.Fprintf(&sb, `<mergeCells count="%d">`, len(sheet.Merges))
		for _, m := range sheet.Merges {
			fmt.Fprintf(&sb, `<mergeCell ref="%s%d:%s%d"/>`, ColumnName(m.Left), m.Top+1, ColumnName(m.Right), m.Bottom+1)
		}
		sb.WriteString("</mergeCells>")
	}
	sb.WriteString("</worksheet>")
	return sb.String()
}

// xlsxColumns: runs of neighbouring columns with the same width and visibility share one <col>.
func xlsxColumns(sb *strings.Builder, sheet *Worksheet) {
	set := map[int]bool{}
	for c := range sheet.ColumnWidths {
		set[c] = true
	}
	for c, h := range sheet.HiddenColumns {
		if h {
			set[c] = true
		}
	}
	columns := sortedSet(set)
	if len(columns) == 0 {
		return
	}
	sb.WriteString("<cols>")
	for i := 0; i < len(columns); {
		first := columns[i]
		width := sheet.ColumnWidth(first)
		hidden := sheet.HiddenColumns[first]
		last := first
		for i++; i < len(columns) && columns[i] == last+1 && sheet.ColumnWidth(columns[i]) == width && sheet.HiddenColumns[columns[i]] == hidden; i++ {
			last = columns[i]
		}
		h := ""
		if hidden {
			h = ` hidden="1"`
		}
		fmt.Fprintf(sb, `<col min="%d" max="%d" width="%s" customWidth="1"%s/>`, first+1, last+1, fmtG(width, 3), h)
	}
	sb.WriteString("</cols>")
}

// cellNumber is the value of a cell, or the number its text holds when it is a number without one.
func cellNumber(c *Cell) *float64 {
	if c.Value != nil {
		return c.Value
	}
	if c.IsNumber {
		if v, err := strconv.ParseFloat(strings.TrimSpace(c.Text), 64); err == nil {
			return &v
		}
	}
	return nil
}

func xlsxCell(sb *strings.Builder, ref string, c *Cell, styles *styleTable) {
	number := cellNumber(c)
	format := c.NumberFormat
	if number == nil || c.IsBoolean {
		format = ""
	}
	s := ""
	if i := styles.index(c.Style, format); i != 0 {
		s = fmt.Sprintf(` s="%d"`, i)
	}
	switch {
	case c.IsBoolean && number != nil:
		b := 0
		if *number != 0 {
			b = 1
		}
		fmt.Fprintf(sb, `<c r="%s"%s t="b"><v>%d</v></c>`, ref, s, b)
	case number != nil && !math.IsNaN(*number) && !math.IsInf(*number, 0):
		fmt.Fprintf(sb, `<c r="%s"%s><v>%s</v></c>`, ref, s, strconv.FormatFloat(*number, 'g', -1, 64))
	case c.Text == "":
		fmt.Fprintf(sb, `<c r="%s"%s/>`, ref, s)
	default:
		fmt.Fprintf(sb, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, s, doc.EscapeXML(c.Text))
	}
}

// UniqueNames makes Excel sheet names: at most 31 characters, none of
// : \ / ? * [ ], unique ignoring case.
func UniqueNames(names []string) []string {
	used := map[string]bool{}
	var out []string
	for _, name := range names {
		clean := strings.Map(func(r rune) rune {
			if strings.ContainsRune(`:\/?*[]`, r) {
				return -1
			}
			return r
		}, name)
		clean = strings.TrimSpace(strings.Trim(clean, "'"))
		if clean == "" {
			clean = "Sheet" + strconv.Itoa(len(out)+1)
		}
		clean = truncRunes(clean, 31)
		unique := clean
		for n := 2; used[strings.ToLower(unique)]; n++ {
			suffix := fmt.Sprintf(" (%d)", n)
			unique = truncRunes(clean, 31-len(suffix)) + suffix
		}
		used[strings.ToLower(unique)] = true
		out = append(out, unique)
	}
	return out
}

func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// styleTable is styles.xml: one xf per distinct style and number format, sharing fonts, fills and borders.
type styleTable struct {
	xfs     map[styleKey]int
	xfXML   []string
	formats map[string]int
	fmtList []string
	fonts   []string
	fills   []string
	borders []string
}

type styleKey struct {
	style  CellStyle
	format string
}

func newStyleTable() *styleTable {
	return &styleTable{
		xfs:     map[styleKey]int{{DefaultStyle, ""}: 0},
		xfXML:   []string{`<xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/>`},
		formats: map[string]int{},
		fonts:   []string{`<font><sz val="11"/><name val="Calibri"/><family val="2"/></font>`},
		fills:   []string{`<fill><patternFill patternType="none"/></fill>`, `<fill><patternFill patternType="gray125"/></fill>`},
		borders: []string{"<border><left/><right/><top/><bottom/><diagonal/></border>"},
	}
}

func (t *styleTable) index(style CellStyle, format string) int {
	style = style.normalized()
	key := styleKey{style, format}
	if i, ok := t.xfs[key]; ok {
		return i
	}
	formatID := 0
	if format != "" {
		if id, ok := builtInIds[format]; ok {
			formatID = id
		} else if id, ok := t.formats[format]; ok {
			formatID = id
		} else {
			formatID = 164 + len(t.formats)
			t.formats[format] = formatID
			t.fmtList = append(t.fmtList, format)
		}
	}
	fontID := add(&t.fonts, xlsxFont(style))
	fillID := 0
	if style.Fill != "" {
		fillID = add(&t.fills, fmt.Sprintf(`<fill><patternFill patternType="solid"><fgColor rgb="%s"/><bgColor indexed="64"/></patternFill></fill>`, argb(style.Fill)))
	}
	borderID := add(&t.borders, "<border>"+side("left", style.Left)+side("right", style.Right)+side("top", style.Top)+side("bottom", style.Bottom)+"<diagonal/></border>")
	var al strings.Builder
	switch style.Align {
	case AlignLeft:
		al.WriteString(` horizontal="left"`)
	case AlignCenter:
		al.WriteString(` horizontal="center"`)
	case AlignRight:
		al.WriteString(` horizontal="right"`)
	}
	switch style.VerticalAlign {
	case VAlignCenter:
		al.WriteString(` vertical="center"`)
	case VAlignTop:
		al.WriteString(` vertical="top"`)
	}
	if style.Wrap {
		al.WriteString(` wrapText="1"`)
	}
	x := fmt.Sprintf(`<xf numFmtId="%d" fontId="%d" fillId="%d" borderId="%d" xfId="0" applyNumberFormat="1" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1">`, formatID, fontID, fillID, borderID)
	if al.Len() > 0 {
		x += "<alignment" + al.String() + "/>"
	}
	t.xfXML = append(t.xfXML, x+"</xf>")
	t.xfs[key] = len(t.xfXML) - 1
	return len(t.xfXML) - 1
}

func (t *styleTable) xml() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="%s">`, sNs)
	if len(t.fmtList) > 0 {
		fmt.Fprintf(&sb, `<numFmts count="%d">`, len(t.fmtList))
		for _, code := range t.fmtList {
			fmt.Fprintf(&sb, `<numFmt numFmtId="%d" formatCode="%s"/>`, t.formats[code], doc.EscapeXML(code))
		}
		sb.WriteString("</numFmts>")
	}
	fmt.Fprintf(&sb, `<fonts count="%d">%s</fonts>`, len(t.fonts), strings.Join(t.fonts, ""))
	fmt.Fprintf(&sb, `<fills count="%d">%s</fills>`, len(t.fills), strings.Join(t.fills, ""))
	fmt.Fprintf(&sb, `<borders count="%d">%s</borders>`, len(t.borders), strings.Join(t.borders, ""))
	sb.WriteString(`<cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>`)
	fmt.Fprintf(&sb, `<cellXfs count="%d">%s</cellXfs>`, len(t.xfXML), strings.Join(t.xfXML, ""))
	sb.WriteString(`<cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>`)
	return sb.String()
}

func xlsxFont(s CellStyle) string {
	var sb strings.Builder
	sb.WriteString("<font>")
	if s.Bold {
		sb.WriteString("<b/>")
	}
	if s.Italic {
		sb.WriteString("<i/>")
	}
	if s.Strike {
		sb.WriteString("<strike/>")
	}
	if s.Underline {
		sb.WriteString("<u/>")
	}
	size := s.FontSize
	if size == 0 {
		size = 11
	}
	fmt.Fprintf(&sb, `<sz val="%s"/>`, fmtG(size, 2))
	if s.Color != "" {
		fmt.Fprintf(&sb, `<color rgb="%s"/>`, argb(s.Color))
	}
	sb.WriteString(`<name val="Calibri"/><family val="2"/></font>`)
	return sb.String()
}

func side(name string, b CellBorder) string {
	if !b.IsVisible() {
		return "<" + name + "/>"
	}
	c := b.Color
	if c == "" {
		c = "#000000"
	}
	return fmt.Sprintf(`<%s style="%s"><color rgb="%s"/></%s>`, name, doc.EscapeXML(b.Style), argb(c), name)
}

// argb: "#1F4E79" → "FF1F4E79".
func argb(c string) string { return "FF" + strings.ToUpper(strings.TrimPrefix(c, "#")) }

func add(list *[]string, xml string) int {
	for i, x := range *list {
		if x == xml {
			return i
		}
	}
	*list = append(*list, xml)
	return len(*list) - 1
}

// StoreEntry writes an uncompressed entry without a data descriptor, as the
// OpenDocument mimetype must be (LibreOffice refuses stored entries with one).
func StoreEntry(zw *zip.Writer, name string, data []byte) error {
	h := &zip.FileHeader{Name: name, Method: zip.Store, CRC32: crc32.ChecksumIEEE(data),
		CompressedSize64: uint64(len(data)), UncompressedSize64: uint64(len(data))}
	w, err := zw.CreateRaw(h)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
